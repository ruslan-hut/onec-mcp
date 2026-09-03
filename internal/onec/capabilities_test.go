package onec

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// capsServer поднимает заглушку 1С, отвечающую заданным телом на /mcp/health,
// и считает обращения — по счётчику видно, работает ли кэш.
func capsServer(t *testing.T, body string) (*Client, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/health" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Settings{
		BaseURL:       srv.URL,
		Timeout:       2 * time.Second,
		ReportTimeout: 2 * time.Second,
	}, slog.New(slog.DiscardHandler))

	return client, &hits
}

const healthWithProfile = `{
	"status": "ok",
	"time": "2026-09-03T10:00:00",
	"capabilities": {
		"profile": "upp-1.3",
		"version": 1,
		"unsupported": {"cash_flow": {"filters": ["cost_article_ids"]}},
		"extra": {"production_consumption": {"group_by": ["cost_article"]}},
		"tools": {"unavailable": ["goods_in_transit"]},
		"resolvers": {"always_empty": ["material"]}
	}
}`

func TestCapabilitiesParsesProfile(t *testing.T) {
	client, _ := capsServer(t, healthWithProfile)

	caps := client.Capabilities(context.Background())
	if caps == nil {
		t.Fatal("capabilities are nil for a health response that carries a profile")
	}

	if caps.Profile != "upp-1.3" {
		t.Errorf("profile = %q, want upp-1.3", caps.Profile)
	}

	facets, ok := caps.Unsupported["cash_flow"]
	if !ok || len(facets.Filters) != 1 || facets.Filters[0] != "cost_article_ids" {
		t.Errorf("unsupported.cash_flow parsed as %+v", facets)
	}

	if got := caps.Extra["production_consumption"].GroupBy; len(got) != 1 || got[0] != "cost_article" {
		t.Errorf("extra.production_consumption.group_by = %v", got)
	}

	if len(caps.Tools.Unavailable) != 1 || caps.Tools.Unavailable[0] != "goods_in_transit" {
		t.Errorf("tools.unavailable = %v", caps.Tools.Unavailable)
	}

	if len(caps.Resolvers.AlwaysEmpty) != 1 || caps.Resolvers.AlwaysEmpty[0] != "material" {
		t.Errorf("resolvers.always_empty = %v", caps.Resolvers.AlwaysEmpty)
	}
}

// tools/list зовётся на каждую сессию модели, а профиль меняется вместе с кодом 1С.
// Без кэша каждый список инструментов тащил бы за собой поход в 1С.
func TestCapabilitiesAreCached(t *testing.T) {
	client, hits := capsServer(t, healthWithProfile)

	for range 5 {
		if client.Capabilities(context.Background()) == nil {
			t.Fatal("capabilities went nil mid-run")
		}
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("1C hit %d times, want 1 — the profile is not cached", got)
	}
}

// Старая 1С отвечает health без профиля. Это не ошибка: гейт просто показывает схемы
// как раньше.
func TestCapabilitiesAbsentProfile(t *testing.T) {
	client, _ := capsServer(t, `{"status":"ok","time":"2026-09-03T10:00:00"}`)

	if caps := client.Capabilities(context.Background()); caps != nil {
		t.Errorf("expected nil capabilities for a health without a profile, got %+v", caps)
	}
}

// Профиль незнакомой версии игнорируется целиком: применить его наполовину опаснее,
// чем не применять вовсе.
func TestCapabilitiesVersionMismatch(t *testing.T) {
	client, _ := capsServer(t, `{"status":"ok","capabilities":{"profile":"upp-1.3","version":99}}`)

	if caps := client.Capabilities(context.Background()); caps != nil {
		t.Errorf("expected nil capabilities for version 99, got %+v", caps)
	}
}

// Fail-open: недоступная 1С не должна сужать выдачу инструментов. И повторные вызовы
// не должны каждый раз ждать сетевого таймаута.
func TestCapabilitiesFailOpen(t *testing.T) {
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Settings{
		BaseURL:       srv.URL,
		Timeout:       2 * time.Second,
		ReportTimeout: 2 * time.Second,
	}, slog.New(slog.DiscardHandler))

	if caps := client.Capabilities(context.Background()); caps != nil {
		t.Errorf("expected nil capabilities when 1C is down, got %+v", caps)
	}

	client.Capabilities(context.Background())

	if got := hits.Load(); got != 1 {
		t.Errorf("1C hit %d times after a failure, want 1 — the failure is not cached", got)
	}
}
