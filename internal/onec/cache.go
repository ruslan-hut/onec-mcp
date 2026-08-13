package onec

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// resolveCache — простой TTL-кэш для resolve_* ответов.
// Ключ — комбинация (entity, normalized_query, limit). Значение — сериализованный []byte ответа
// (опросы возвращают разные типы — храним сырой JSON, чтобы не плодить дженерики).
// Очистка протухших ключей делается ленива при Get; периодически — фоновой джоной.
// resolveCacheMaxEntries — потолок числа записей. Ключ включает произвольную строку запроса,
// поэтому без потолка карта растёт по числу уникальных запросов, а не по размеру справочников:
// модель, перебирающая формулировки, раздувает её до следующего sweep. Потолок щедрый —
// нормальная работа до него не доходит, он ограничивает только патологию.
const resolveCacheMaxEntries = 10000

type resolveCache struct {
	ttl     time.Duration
	maxSize int
	mu      sync.Mutex
	entries map[string]cacheEntry
	// stop останавливает janitor. Закрывается в Close; повторное закрытие защищено stopOnce,
	// чтобы двойной Close (например, тенант попал и в старую, и в новую карту реестра) не паниковал.
	stop     chan struct{}
	stopOnce sync.Once
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
		maxSize: resolveCacheMaxEntries,
		entries: make(map[string]cacheEntry),
		stop:    make(chan struct{}),
	}
	go c.janitor()
	return c
}

// Close останавливает janitor. Обязателен при замене клиента: реестр пересобирает все базы
// на каждую правку в /admin, и без остановки каждая правка оставляла бы вечную горутину,
// держащую свою (уже никому не нужную) карту entries.
func (c *resolveCache) Close() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *resolveCache) key(entity, query string, limit int) string {
	var b strings.Builder
	b.WriteString(entity)
	b.WriteByte('|')
	b.WriteString(strings.ToLower(strings.TrimSpace(query)))
	b.WriteByte('|')
	// limit меняет размер ответа, поэтому участвует в ключе
	b.WriteString(strconv.Itoa(limit))
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

	// Дошли до потолка — сначала пробуем обойтись протухшими, и только если их не хватило,
	// вытесняем живые. Порядок обхода map в Go случайный, так что это случайное вытеснение:
	// для кэша резолвов этого достаточно, а LRU потребовал бы списка на каждый Get.
	if _, exists := c.entries[k]; !exists && len(c.entries) >= c.maxSize {
		c.evictLocked()
	}

	c.entries[k] = cacheEntry{
		expiresAt: time.Now().Add(c.ttl),
		payload:   payload,
	}
}

// evictLocked освобождает место под одну новую запись. Вызывается под уже взятым c.mu.
func (c *resolveCache) evictLocked() {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	for k := range c.entries {
		if len(c.entries) < c.maxSize {
			break
		}
		delete(c.entries, k)
	}
}

func (c *resolveCache) janitor() {
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.sweep()
		case <-c.stop:
			return
		}
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
