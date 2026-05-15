package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// CachedVerifier оборачивает «дорогой» VerifyKeyFunc (поход в 1С) в потокобезопасный TTL-кэш.
// Ключ кэша — SHA-256 от access key, чтобы не держать секреты в map как ключи строки.
// Негативные результаты (ключ невалиден) тоже кэшируются, но на более короткий срок —
// иначе атакующий мог бы дёргать тот же неверный ключ и каждый раз грузить 1С.
type CachedVerifier struct {
	inner   VerifyKeyFunc
	ttl     time.Duration
	negTTL  time.Duration
	mu      sync.Mutex
	entries map[string]cachedEntry
}

type cachedEntry struct {
	info      *UserInfo // nil для негативного результата
	err       error
	expiresAt time.Time
}

// NewCachedVerifier создаёт обёртку с заданным позитивным TTL.
// Негативный TTL = min(ttl, 30s) — короткий, чтобы ротация ключа в 1С быстро подхватывалась.
func NewCachedVerifier(inner VerifyKeyFunc, ttl time.Duration) *CachedVerifier {
	negTTL := 30 * time.Second
	if ttl < negTTL {
		negTTL = ttl
	}
	return &CachedVerifier{
		inner:   inner,
		ttl:     ttl,
		negTTL:  negTTL,
		entries: make(map[string]cachedEntry),
	}
}

// Verify — реализация VerifyKeyFunc. При попадании в кэш — мгновенный возврат,
// при промахе — вызов inner и сохранение результата.
func (c *CachedVerifier) Verify(ctx context.Context, key string) (*UserInfo, error) {
	h := hashKey(key)
	now := time.Now()

	c.mu.Lock()
	if e, ok := c.entries[h]; ok && now.Before(e.expiresAt) {
		c.mu.Unlock()
		return e.info, e.err
	}
	c.mu.Unlock()

	info, err := c.inner(ctx, key)

	exp := now.Add(c.ttl)
	if err != nil || info == nil {
		exp = now.Add(c.negTTL)
	}

	c.mu.Lock()
	c.entries[h] = cachedEntry{info: info, err: err, expiresAt: exp}
	c.mu.Unlock()

	return info, err
}

// Cleanup — периодическая чистка просроченных записей. Дёргать из горутины с тикером.
// Без неё map растёт по числу уникальных проверенных ключей за uptime.
func (c *CachedVerifier) Cleanup() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for h, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, h)
		}
	}
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
