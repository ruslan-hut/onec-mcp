package oauth

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// FixedWindowLimiter — простой счётчик с фиксированным окном по ключу.
// Подходит для защиты OAuth endpoint-ов от перебора (authorize, register, token).
// Точный rate-bursting (token bucket) для этого не нужен: цель — отбить грубый brute-force.
type FixedWindowLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*windowBucket
}

type windowBucket struct {
	count   int
	resetAt time.Time
}

// NewFixedWindowLimiter — limit запросов на window времени. При limit <= 0 лимитер фактически отключён
// (Allow всегда true), это удобно для конфига «0 = выключено».
func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*windowBucket),
	}
}

// Allow возвращает (true, _) если запрос можно пропустить, либо (false, retryAfter) с временем до следующего окна.
func (l *FixedWindowLimiter) Allow(key string) (bool, time.Duration) {
	if l.limit <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		l.buckets[key] = &windowBucket{count: 1, resetAt: now.Add(l.window)}
		return true, 0
	}
	if b.count >= l.limit {
		return false, time.Until(b.resetAt)
	}
	b.count++
	return true, 0
}

// Cleanup удаляет просроченные бакеты. Дёрнуть из тикера; без неё map растёт с числом уникальных IP.
func (l *FixedWindowLimiter) Cleanup() {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.After(b.resetAt) {
			delete(l.buckets, k)
		}
	}
}

// Middleware — chi-совместимая обёртка. Ключ берётся из RemoteAddr (chi.RealIP уже выставил его из XFF).
// При превышении лимита отдаём 429 с Retry-After (RFC 6585).
func (l *FixedWindowLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			ok, retryAfter := l.Allow(key)
			if !ok {
				secs := int(retryAfter.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeOAuthError(w, http.StatusTooManyRequests, "rate_limited",
					"too many requests, retry later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP — без порта; пустая строка если разобрать не получилось (всё равно лимитим под одним пустым ключом).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
