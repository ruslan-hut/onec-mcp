package onec

import (
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestResolveCacheRoundTrip(t *testing.T) {
	c := newResolveCache(time.Minute)
	defer c.Close()

	if _, ok := c.Get("customer", "acme", 10); ok {
		t.Fatal("empty cache returned a hit")
	}

	c.Set("customer", "acme", 10, []byte(`{"ok":true}`))

	got, ok := c.Get("customer", "acme", 10)
	if !ok {
		t.Fatal("miss after Set")
	}
	if string(got) != `{"ok":true}` {
		t.Errorf("payload = %s", got)
	}
}

// TestResolveCacheKeyDimensions пиннит все три измерения ключа. Схлопывание любого из них —
// это выдача одной базы/области видимости в ответ на запрос другой; ровно так уже ломались
// includeGroups у cost_article и производственные склады у warehouse.
func TestResolveCacheKeyDimensions(t *testing.T) {
	c := newResolveCache(time.Minute)
	defer c.Close()

	c.Set("customer", "acme", 10, []byte("A"))

	cases := []struct {
		name          string
		entity, query string
		limit         int
		wantHit       bool
	}{
		{"same key", "customer", "acme", 10, true},
		{"case-insensitive query", "customer", "ACME", 10, true},
		{"query trimmed", "customer", "  acme  ", 10, true},
		{"other entity", "product", "acme", 10, false},
		{"scoped entity variant", "customer+groups", "acme", 10, false},
		{"other query", "customer", "beta", 10, false},
		{"other limit", "customer", "acme", 25, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := c.Get(tc.entity, tc.query, tc.limit)
			if ok != tc.wantHit {
				t.Errorf("Get(%q, %q, %d) hit = %v, want %v", tc.entity, tc.query, tc.limit, ok, tc.wantHit)
			}
		})
	}
}

func TestResolveCacheExpiresEntries(t *testing.T) {
	c := newResolveCache(10 * time.Millisecond)
	defer c.Close()

	c.Set("customer", "acme", 10, []byte("A"))
	time.Sleep(30 * time.Millisecond)

	if _, ok := c.Get("customer", "acme", 10); ok {
		t.Error("протухшая запись отдана из кэша")
	}
}

func TestResolveCacheSweepDropsExpired(t *testing.T) {
	c := newResolveCache(10 * time.Millisecond)
	defer c.Close()

	c.Set("customer", "acme", 10, []byte("A"))
	c.Set("customer", "beta", 10, []byte("B"))
	time.Sleep(30 * time.Millisecond)
	c.sweep()

	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("entries = %d, want 0", n)
	}
}

// TestResolveCacheEnforcesMaxSize — ключ включает произвольную строку запроса, поэтому без
// потолка карта растёт по числу уникальных формулировок, а не по размеру справочника.
func TestResolveCacheEnforcesMaxSize(t *testing.T) {
	c := newResolveCache(time.Hour)
	defer c.Close()
	c.maxSize = 50

	for i := 0; i < 500; i++ {
		c.Set("customer", "query-"+strconv.Itoa(i), 10, []byte("x"))
	}

	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()

	if n > c.maxSize {
		t.Errorf("entries = %d, want <= %d — потолок не соблюдается", n, c.maxSize)
	}
	if n == 0 {
		t.Error("вытеснение опустошило кэш целиком")
	}
}

// Перезапись существующего ключа не должна считаться новой записью и провоцировать вытеснение.
func TestResolveCacheOverwriteDoesNotEvict(t *testing.T) {
	c := newResolveCache(time.Hour)
	defer c.Close()
	c.maxSize = 3

	c.Set("customer", "a", 10, []byte("1"))
	c.Set("customer", "b", 10, []byte("1"))
	c.Set("customer", "c", 10, []byte("1"))
	for i := 0; i < 10; i++ {
		c.Set("customer", "a", 10, []byte("2"))
	}

	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 3 {
		t.Errorf("entries = %d, want 3", n)
	}
	got, ok := c.Get("customer", "a", 10)
	if !ok || string(got) != "2" {
		t.Errorf("перезапись потеряна: %q, ok=%v", got, ok)
	}
}

// Вытеснение должно в первую очередь съедать протухшее, а не живое.
func TestResolveCacheEvictsExpiredFirst(t *testing.T) {
	c := newResolveCache(50 * time.Millisecond)
	defer c.Close()
	c.maxSize = 4

	c.Set("customer", "old-a", 10, []byte("x"))
	c.Set("customer", "old-b", 10, []byte("x"))
	c.Set("customer", "old-c", 10, []byte("x"))
	time.Sleep(80 * time.Millisecond) // все три протухли

	c.Set("customer", "fresh", 10, []byte("x"))

	if _, ok := c.Get("customer", "fresh", 10); !ok {
		t.Error("свежая запись вытеснена вместо протухших")
	}
}

// Отключённый кэш — nil-указатель: Get/Set/Close обязаны быть безопасны, иначе база с
// resolve_cache_ttl=0 падала бы на первом же резолве.
func TestNilResolveCacheIsSafe(t *testing.T) {
	c := newResolveCache(0)
	if c != nil {
		t.Fatal("ttl<=0 должен отключать кэш")
	}

	if _, ok := c.Get("customer", "acme", 10); ok {
		t.Error("nil cache вернул попадание")
	}
	c.Set("customer", "acme", 10, []byte("A"))
	c.Close()
}

// TestResolveCacheCloseStopsJanitor — регрессия на утечку горутин. Реестр пересобирает клиентов
// всех баз на КАЖДУЮ правку в /admin, и без остановки janitor каждая правка оставляла бы вечную
// горутину, держащую свою карту entries.
func TestResolveCacheCloseStopsJanitor(t *testing.T) {
	before := runtime.NumGoroutine()

	caches := make([]*resolveCache, 0, 50)
	for i := 0; i < 50; i++ {
		caches = append(caches, newResolveCache(time.Hour))
	}
	for _, c := range caches {
		c.Close()
	}

	// Горутинам нужен момент, чтобы фактически завершиться после close(stop).
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if leaked := runtime.NumGoroutine() - before; leaked > 5 {
		t.Errorf("после Close осталось +%d горутин — janitor не останавливается", leaked)
	}
}

func TestResolveCacheCloseIsIdempotent(t *testing.T) {
	c := newResolveCache(time.Minute)
	c.Close()
	c.Close() // повторный Close не должен паниковать на close(nil-канала)
}

func TestClientCloseStopsCacheJanitor(t *testing.T) {
	before := runtime.NumGoroutine()

	client := NewClient(Settings{
		BaseURL:         "http://127.0.0.1:1",
		Timeout:         time.Second,
		ReportTimeout:   time.Second,
		ResolveCacheTTL: time.Hour,
	}, testLogger())
	client.Close()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if leaked := runtime.NumGoroutine() - before; leaked > 2 {
		t.Errorf("после Client.Close осталось +%d горутин", leaked)
	}
}

// Клиент без кэша (ttl=0) тоже обязан закрываться — Close идёт по nil-указателю кэша.
func TestClientCloseWithoutCache(t *testing.T) {
	client := NewClient(Settings{
		BaseURL:       "http://127.0.0.1:1",
		Timeout:       time.Second,
		ReportTimeout: time.Second,
	}, testLogger())
	client.Close()
}
