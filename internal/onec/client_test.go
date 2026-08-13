package onec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/mcp-sales-mvp/internal/oauth"
)

// withScopes собирает контекст с правами вызывающего — ровно то, что кладёт oauth.Middleware.
func withScopes(t *testing.T, scopes []string) context.Context {
	t.Helper()
	return oauth.ContextWithAuth(context.Background(), &oauth.AuthInfo{
		Sub:    "u1",
		Scopes: scopes,
		Scope:  strings.Join(scopes, " "),
	})
}

// TestCostScoped — признак «есть право на себестоимость». Он входит в ключ кэша resolve_warehouse,
// поэтому ошибка здесь означает выдачу производственных складов тем, кому они не положены.
func TestCostScoped(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"no auth (legacy static token)", context.Background(), true},
		{"has cost scope", withScopes(t, []string{"mcp:resolve", scopeReportCost}), true},
		{"without cost scope", withScopes(t, []string{"mcp:resolve", "mcp:report:sales"}), false},
		{"no scopes at all", withScopes(t, nil), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := costScoped(tc.ctx); got != tc.want {
				t.Errorf("costScoped() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveWarehouseCacheIsScopeSeparated — регрессия на отравление кэша: ответ 1С на
// resolve_warehouse ЗАВИСИТ от прав вызывающего (производственные склады отдаются только
// с mcp:report:cost). Если признак не входит в ключ, первый же cost-вызов раздаёт
// производственные склады всем остальным до конца TTL.
func TestResolveWarehouseCacheIsScopeSeparated(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer srv.Close()

	client := NewClient(Settings{
		BaseURL:         srv.URL,
		Timeout:         5 * time.Second,
		ReportTimeout:   5 * time.Second,
		ResolveCacheTTL: time.Minute,
	}, testLogger())
	defer client.Close()

	costCtx := withScopes(t, []string{"mcp:resolve", scopeReportCost})
	plainCtx := withScopes(t, []string{"mcp:resolve"})

	if _, err := client.ResolveWarehouse(costCtx, "склад", 10); err != nil {
		t.Fatalf("cost-scoped call: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}

	// Тот же запрос без права на себестоимость обязан сходить в 1С заново.
	if _, err := client.ResolveWarehouse(plainCtx, "склад", 10); err != nil {
		t.Fatalf("plain call: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2 — запрос без cost-права взял ответ из cost-кэша", got)
	}

	// А повтор с теми же правами — уже из кэша.
	if _, err := client.ResolveWarehouse(plainCtx, "склад", 10); err != nil {
		t.Fatalf("plain repeat: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2 — повтор с теми же правами должен браться из кэша", got)
	}
}

// TestResolveCustomerCacheSeparatesIncludeGroups — тот же класс ошибки на другом резолвере:
// выдача без групп не должна затирать выдачу с группами.
func TestResolveCustomerCacheSeparatesIncludeGroups(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer srv.Close()

	client := NewClient(Settings{
		BaseURL:         srv.URL,
		Timeout:         5 * time.Second,
		ReportTimeout:   5 * time.Second,
		ResolveCacheTTL: time.Minute,
	}, testLogger())
	defer client.Close()

	ctx := context.Background()
	if _, err := client.ResolveCustomer(ctx, "ромашка", 10, false); err != nil {
		t.Fatalf("without groups: %v", err)
	}
	if _, err := client.ResolveCustomer(ctx, "ромашка", 10, true); err != nil {
		t.Fatalf("with groups: %v", err)
	}

	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2 — include_groups обязан входить в ключ кэша", got)
	}
}

// TestDoRequestSurfacesStructuredError — 1С отдаёт {error,message}; гейт обязан донести
// причину до клиента, а не схлопнуть её в «status 400».
func TestDoRequestSurfacesStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_period","message":"period start is after end"}`))
	}))
	defer srv.Close()

	client := NewClient(Settings{
		BaseURL:       srv.URL,
		Timeout:       5 * time.Second,
		ReportTimeout: 5 * time.Second,
	}, testLogger())
	defer client.Close()

	_, err := client.ResolveCustomer(context.Background(), "x", 10, false)
	if err == nil {
		t.Fatal("expected an error")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError (structured body потерян)", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "period start is after end" {
		t.Errorf("message = %q", apiErr.Message)
	}
}

// TestScopeHeadersForwarded — 1С пере-проверяет права по X-MCP-Scopes (defense in depth).
// Если гейт перестанет их слать, 1С трактует отсутствие заголовка как полный доступ.
func TestScopeHeadersForwarded(t *testing.T) {
	var gotSub, gotScopes string
	var headerPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSub = r.Header.Get("X-MCP-Sub")
		gotScopes = r.Header.Get("X-MCP-Scopes")
		_, headerPresent = r.Header["X-Mcp-Scopes"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer srv.Close()

	client := NewClient(Settings{
		BaseURL:       srv.URL,
		Timeout:       5 * time.Second,
		ReportTimeout: 5 * time.Second,
	}, testLogger())
	defer client.Close()

	ctx := withScopes(t, []string{"mcp:resolve", "mcp:report:sales"})
	if _, err := client.ResolveCustomer(ctx, "x", 10, false); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if !headerPresent {
		t.Fatal("X-MCP-Scopes не отправлен — 1С сочтёт это полным доступом")
	}
	if gotSub != "u1" {
		t.Errorf("X-MCP-Sub = %q, want u1", gotSub)
	}
	if gotScopes != "mcp:resolve,mcp:report:sales" {
		t.Errorf("X-MCP-Scopes = %q, want comma-separated list", gotScopes)
	}
}
