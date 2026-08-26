package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/onec"
)

// fake1C — заглушка 1С: записывает тела полученных запросов и отдаёт заранее заданный ответ.
type fake1C struct {
	mu       sync.Mutex
	requests []recorded
	response string
}

type recorded struct {
	path string
	body map[string]any
}

func (f *fake1C) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		f.mu.Lock()
		f.requests = append(f.requests, recorded{path: r.URL.Path, body: body})
		resp := f.response
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if resp == "" {
			resp = `{"columns":[],"rows":[],"totals":{}}`
		}
		_, _ = io.WriteString(w, resp)
	})
}

func (f *fake1C) recorded(t *testing.T, n int) recorded {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.requests) <= n {
		t.Fatalf("1C got %d requests, wanted at least %d", len(f.requests), n+1)
	}
	return f.requests[n]
}

func (f *fake1C) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// newTestHandler поднимает заглушку 1С и MCP-хендлер поверх неё. Аутентификация выключена
// (bearerToken пустой), поэтому tools/call проходит без OAuth-контекста.
func newTestHandler(t *testing.T) (*Handler, *fake1C) {
	t.Helper()

	fake := &fake1C{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	client := onec.NewClient(onec.Settings{
		BaseURL:         srv.URL,
		Timeout:         5 * time.Second,
		ReportTimeout:   5 * time.Second,
		ResolveCacheTTL: time.Minute,
	}, slog.New(slog.DiscardHandler))

	cfg := &config.Config{}
	cfg.Limits.MaxRows = 5000
	cfg.Limits.ResolveLimit = 10

	return NewHandler(client, cfg, "", slog.New(slog.DiscardHandler)), fake
}

// callTool прогоняет tools/call через ServeHTTP и возвращает распарсенный CallToolResult.
func callTool(t *testing.T, h *Handler, tool string, args map[string]any) CallToolResult {
	t.Helper()

	params, err := json.Marshal(map[string]any{"name": tool, "arguments": args})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  json.RawMessage(params),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call: HTTP %d, body %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Result CallToolResult `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response %s: %v", rec.Body.String(), err)
	}
	if envelope.Error != nil {
		t.Fatalf("tools/call returned a JSON-RPC error: %s", envelope.Error.Message)
	}

	return envelope.Result
}

func resultText(t *testing.T, res CallToolResult) string {
	t.Helper()

	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	return res.Content[0].Text
}

// cash_balance умеет фильтровать только по кассам. Ключи cash_flow (operation_ids и прочие)
// не должны доезжать до 1С: та их не применит, и агент получит полную выборку, считая её
// отфильтрованной.
func TestCashBalanceForwardsOnlyCashFilter(t *testing.T) {
	h, fake := newTestHandler(t)

	callTool(t, h, ToolCashBalance, map[string]any{
		"filters": map[string]any{
			"cash_ids":         []string{"cash-1"},
			"operation_ids":    []string{"op-1"},
			"cost_article_ids": []string{"art-1"},
			"customer_ids":     []string{"cust-1"},
		},
	})

	got := fake.recorded(t, 0)
	if got.path != "/mcp/reports/cash_balance" {
		t.Fatalf("path = %s", got.path)
	}

	filters, ok := got.body["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters missing in %v", got.body)
	}

	if _, ok := filters["cash_ids"]; !ok {
		t.Error("cash_ids must be forwarded")
	}
	for _, key := range []string{"operation_ids", "cost_article_ids", "customer_ids"} {
		if _, leaked := filters[key]; leaked {
			t.Errorf("%s must not reach 1C from cash_balance", key)
		}
	}
}

// receivables объявляет customer_ids, payables — supplier_ids. Ключ чужой роли не forwardится.
func TestSettlementsFiltersAreRoleSpecific(t *testing.T) {
	h, fake := newTestHandler(t)

	callTool(t, h, ToolReceivablesBalance, map[string]any{
		"filters": map[string]any{
			"customer_ids": []string{"cust-1"},
			"supplier_ids": []string{"sup-1"},
		},
	})

	filters := fake.recorded(t, 0).body["filters"].(map[string]any)
	if _, ok := filters["customer_ids"]; !ok {
		t.Error("receivables must forward customer_ids")
	}
	if _, leaked := filters["supplier_ids"]; leaked {
		t.Error("receivables must not forward supplier_ids")
	}

	callTool(t, h, ToolPayablesBalance, map[string]any{
		"filters": map[string]any{
			"customer_ids": []string{"cust-1"},
			"supplier_ids": []string{"sup-1"},
		},
	})

	filters = fake.recorded(t, 1).body["filters"].(map[string]any)
	if _, ok := filters["supplier_ids"]; !ok {
		t.Error("payables must forward supplier_ids")
	}
	if _, leaked := filters["customer_ids"]; leaked {
		t.Error("payables must not forward customer_ids")
	}
}

// top_products — обёртка над sales-отчётом, но в схеме объявляет только контрагентов и склады.
func TestTopProductsDropsSalesOnlyFilters(t *testing.T) {
	h, fake := newTestHandler(t)

	callTool(t, h, ToolTopProducts, map[string]any{
		"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
		"filters": map[string]any{
			"customer_ids":      []string{"cust-1"},
			"sales_channel_ids": []string{"chan-1"},
			"customer_cohort":   "new",
			"product_status":    []string{"active"},
		},
	})

	filters := fake.recorded(t, 0).body["filters"].(map[string]any)
	if _, ok := filters["customer_ids"]; !ok {
		t.Error("customer_ids must be forwarded")
	}
	for _, key := range []string{"sales_channel_ids", "customer_cohort", "product_status"} {
		if _, leaked := filters[key]; leaked {
			t.Errorf("%s must not reach 1C from top_products", key)
		}
	}
}

// Отчёты декодируются в типизированную структуру, и раньше period/applied_filters из ответа 1С
// терялись при пересборке — хотя схема инструмента их обещает.
func TestSalesReportPreservesEchoFields(t *testing.T) {
	h, fake := newTestHandler(t)
	fake.response = `{"columns":[{"name":"amount","type":"number"}],"rows":[[10]],"totals":{"amount":10},
		"period":{"from":"2026-01-01T00:00:00","to":"2026-01-31T23:59:59"},
		"applied_filters":{"customers":[],"customer_cohort":"new"}}`

	res := callTool(t, h, ToolSalesReport, map[string]any{
		"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}

	if _, ok := got["period"]; !ok {
		t.Error("period from 1C must survive to the LLM")
	}
	if _, ok := got["applied_filters"]; !ok {
		t.Error("applied_filters from 1C must survive to the LLM")
	}
	if _, ok := got["rows"]; !ok {
		t.Error("rows must still be there")
	}
}

// Остатки на дату отдают date, взаиморасчёты — date и role: те же echo-поля.
func TestSettlementsPreservesRole(t *testing.T) {
	h, fake := newTestHandler(t)
	fake.response = `{"columns":[],"rows":[],"totals":{},"date":"2026-01-31T23:59:59","role":"customer"}`

	res := callTool(t, h, ToolReceivablesBalance, map[string]any{})

	var got map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}

	if got["role"] != "customer" {
		t.Errorf("role = %v, want customer", got["role"])
	}
	if got["date"] != "2026-01-31T23:59:59" {
		t.Errorf("date = %v", got["date"])
	}
}

// Кэш резолва статей затрат обязан учитывать include_groups: иначе выдача без групп
// затирает выдачу с группами на весь TTL, и запрошенные UUID групп не возвращаются.
func TestResolveCostArticleCacheKeyIncludesGroups(t *testing.T) {
	h, fake := newTestHandler(t)
	fake.response = `{"candidates":[]}`

	args := map[string]any{"query": "аренда"}
	callTool(t, h, ToolResolveCostArticle, args)

	withGroups := map[string]any{"query": "аренда", "include_groups": true}
	callTool(t, h, ToolResolveCostArticle, withGroups)

	if fake.count() != 2 {
		t.Fatalf("1C got %d requests, want 2 — include_groups must not hit the cached no-groups answer", fake.count())
	}

	// повтор того же запроса обязан прийти из кэша
	callTool(t, h, ToolResolveCostArticle, withGroups)
	if fake.count() != 2 {
		t.Fatalf("1C got %d requests, want 2 — identical resolve must be cached", fake.count())
	}
}

// Админские инструменты шли мимо unstringifyJSON, и двойное кодирование period — ровно тот косяк
// LLM, ради которого он написан, — ломало именно их.
func TestEventLogUnstringifiesArguments(t *testing.T) {
	h, fake := newTestHandler(t)
	fake.response = `{"events":[]}`

	callTool(t, h, ToolEventLog, map[string]any{
		"period": `{"from":"2026-01-01","to":"2026-01-02"}`,
		"limit":  10,
	})

	body := fake.recorded(t, 0).body
	period, ok := body["period"].(map[string]any)
	if !ok {
		t.Fatalf("period reached 1C as %T (%v), want an object", body["period"], body["period"])
	}
	if period["from"] != "2026-01-01" {
		t.Errorf("period.from = %v", period["from"])
	}
}

// Журнал регистрации сканируется долго — он должен идти по «отчётному» таймауту, а не по
// восьмисекундному, иначе выборка за сутки падает с context deadline exceeded.
func TestAdminCallsUseReportTimeout(t *testing.T) {
	fake := &fake1C{}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"events":[]}`)
	}))
	t.Cleanup(slow.Close)
	_ = fake

	client := onec.NewClient(onec.Settings{
		BaseURL:       slow.URL,
		Timeout:       50 * time.Millisecond, // короткий: резолвы/auth
		ReportTimeout: 5 * time.Second,       // длинный: отчёты и админ-вызовы
	}, slog.New(slog.DiscardHandler))

	cfg := &config.Config{}
	cfg.Limits.MaxRows = 5000
	cfg.Limits.ResolveLimit = 10

	h := NewHandler(client, cfg, "", slog.New(slog.DiscardHandler))

	res := callTool(t, h, ToolEventLog, map[string]any{"limit": 10})

	if res.IsError {
		t.Fatalf("event_log failed on the short timeout: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "events") {
		t.Errorf("unexpected event_log result: %s", resultText(t, res))
	}
}

// costMeasuresIn — основа проверки права mcp:report:cost на вызове (а не только в схеме).
func TestCostMeasuresIn(t *testing.T) {
	cases := []struct {
		name string
		args any
		want int
	}{
		{"no measures", map[string]any{"top": 10}, 0},
		{"open measures", map[string]any{"measures": []any{"amount", "qty"}}, 0},
		{"cost measure", map[string]any{"measures": []any{"amount", "profit"}}, 1},
		{"all cost measures", map[string]any{"measures": []any{"cost", "profit", "margin"}}, 3},
		{"double-encoded", map[string]any{"measures": `["margin"]`}, 1},
		{"not a map", []any{"nonsense"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := costMeasuresIn(tc.args); len(got) != tc.want {
				t.Errorf("costMeasuresIn(%v) = %v, want %d entries", tc.args, got, tc.want)
			}
		})
	}
}

func TestClampLimits(t *testing.T) {
	h, _ := newTestHandler(t) // MaxRows=5000, ResolveLimit=10

	t.Run("resolve limit", func(t *testing.T) {
		cases := []struct{ in, want int }{
			{0, 10}, {-5, 10}, {3, 3}, {10, 10}, {999, 10},
		}
		for _, tc := range cases {
			if got := h.clampLimit(flexInt(tc.in)); got != tc.want {
				t.Errorf("clampLimit(%d) = %d, want %d", tc.in, got, tc.want)
			}
		}
	})

	t.Run("report rows", func(t *testing.T) {
		cases := []struct{ in, want int }{
			{0, 5000}, {-1, 5000}, {100, 100}, {5000, 5000}, {99999, 5000},
		}
		for _, tc := range cases {
			if got := h.clampTop(flexInt(tc.in)); got != tc.want {
				t.Errorf("clampTop(%d) = %d, want %d", tc.in, got, tc.want)
			}
		}
	})

	// max_rows — потолок и для значения по умолчанию: иначе вызывающий с завышенным дефолтом
	// протаскивал бы мимо лимита ровно то, ради чего лимит и заведён.
	t.Run("default is clamped too", func(t *testing.T) {
		if got := h.clampTopDefault(0, 10); got != 10 {
			t.Errorf("clampTopDefault(0, 10) = %d, want 10", got)
		}
		if got := h.clampTopDefault(0, 99999); got != 5000 {
			t.Errorf("clampTopDefault(0, 99999) = %d, want 5000 — дефолт выше max_rows не урезан", got)
		}
		if got := h.clampTopDefault(50, 99999); got != 50 {
			t.Errorf("clampTopDefault(50, 99999) = %d, want 50", got)
		}
	})
}

// TestFirmFilterReachesOneC — фильтр по фирме объявлен в структурах фильтров всех отчётов,
// а не только у взаиморасчётов/закупок. Ключи, не объявленные в структуре, mapToStruct молча
// выбрасывает — поэтому пропущенное поле проявилось бы как «фильтр применился», хотя в 1С
// он не доехал, и цифры вернулись бы по всем фирмам.
func TestFirmFilterReachesOneC(t *testing.T) {
	cases := []struct {
		tool string
		path string
		args map[string]any
	}{
		{ToolSalesReport, "/mcp/reports/sales", map[string]any{
			"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
		}},
		{ToolStockBalance, "/mcp/reports/stock", map[string]any{}},
		{ToolAvailabilityReport, "/mcp/reports/availability", map[string]any{
			"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
		}},
		{ToolTopProducts, "/mcp/reports/top_products", map[string]any{
			"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
		}},
		{ToolCashBalance, "/mcp/reports/cash_balance", map[string]any{}},
		{ToolCashFlow, "/mcp/reports/cash_flow", map[string]any{
			"period": map[string]any{"from": "2026-01-01", "to": "2026-01-31"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			h, fake := newTestHandler(t)

			args := map[string]any{}
			for k, v := range tc.args {
				args[k] = v
			}
			args["filters"] = map[string]any{"firm_ids": []string{"firm-1"}}

			callTool(t, h, tc.tool, args)

			got := fake.recorded(t, 0)
			if got.path != tc.path {
				t.Fatalf("path = %s, want %s", got.path, tc.path)
			}

			filters, ok := got.body["filters"].(map[string]any)
			if !ok {
				t.Fatalf("filters missing in %v", got.body)
			}
			ids, ok := filters["firm_ids"].([]any)
			if !ok || len(ids) != 1 || ids[0] != "firm-1" {
				t.Errorf("firm_ids = %v, want [firm-1]", filters["firm_ids"])
			}
		})
	}
}

// TestResolveFirmCallsOneC — маршрут нового резолвера: инструмент должен ходить
// в /mcp/resolve/firm, а не переиспользовать чужой эндпойнт.
func TestResolveFirmCallsOneC(t *testing.T) {
	h, fake := newTestHandler(t)

	callTool(t, h, ToolResolveFirm, map[string]any{"query": "ТОВ"})

	got := fake.recorded(t, 0)
	if got.path != "/mcp/resolve/firm" {
		t.Fatalf("path = %s, want /mcp/resolve/firm", got.path)
	}
	if got.body["query"] != "ТОВ" {
		t.Errorf("query = %v, want ТОВ", got.body["query"])
	}
}
