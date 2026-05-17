package onec

import (
	"strings"
	"sync"
	"time"
)

// resolveCache — простой TTL-кэш для resolve_* ответов.
// Ключ — комбинация (entity, normalized_query, limit). Значение — сериализованный []byte ответа
// (опросы возвращают разные типы — храним сырой JSON, чтобы не плодить дженерики).
// Очистка протухших ключей делается ленива при Get; периодически — фоновой джоной.
type resolveCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	expiresAt time.Time
	payload   []byte
}

func newResolveCache(ttl time.Duration) *resolveCache {
	if ttl <= 0 {
		return nil
	}
	c := &resolveCache{
		ttl:     ttl,
		entries: make(map[string]cacheEntry),
	}
	go c.janitor()
	return c
}

func (c *resolveCache) key(entity, query string, limit int) string {
	var b strings.Builder
	b.WriteString(entity)
	b.WriteByte('|')
	b.WriteString(strings.ToLower(strings.TrimSpace(query)))
	b.WriteByte('|')
	// limit меняет размер ответа, поэтому участвует в ключе
	for limit > 0 {
		b.WriteByte('0' + byte(limit%10))
		limit /= 10
	}
	return b.String()
}

func (c *resolveCache) Get(entity, query string, limit int) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	k := c.key(entity, query, limit)

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, k)
		return nil, false
	}
	return e.payload, true
}

func (c *resolveCache) Set(entity, query string, limit int, payload []byte) {
	if c == nil {
		return
	}
	k := c.key(entity, query, limit)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[k] = cacheEntry{
		expiresAt: time.Now().Add(c.ttl),
		payload:   payload,
	}
}

func (c *resolveCache) janitor() {
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for range t.C {
		c.sweep()
	}
}

func (c *resolveCache) sweep() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
}
