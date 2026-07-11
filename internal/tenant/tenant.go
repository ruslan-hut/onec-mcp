// Package tenant хранит настройки баз 1С, обслуживаемых гейтом. Раньше они лежали в YAML;
// теперь — в SQLite, и редактируются через веб-интерфейс /admin без перезапуска процесса.
package tenant

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultTimeoutMs          = 8000
	DefaultReportTimeoutMs    = 45000
	DefaultResolveCacheTTLSec = 600
)

// Tenant — одна база 1С. Slug — первичный ключ и первый сегмент пути: /{slug}/mcp,
// /{slug}/oauth/*, /{slug}/resolve/*.
type Tenant struct {
	Slug string
	// Name — человекочитаемое имя, показывается на форме логина OAuth и в /admin.
	Name string
	// Enabled — выключенная база не отдаёт ни одного маршрута (быстрый способ отрезать доступ,
	// не удаляя настройки и не трогая выпущенные токены).
	Enabled bool

	// Подключение к 1С. Авторизация всегда basic.
	BaseURL  string
	Username string
	Password string

	TimeoutMs          int
	ReportTimeoutMs    int
	ResolveCacheTTLSec int
	TenantHeader       string
	DefaultTenant      string

	// MCPToken — статический Bearer для /{slug}/mcp. Работает только при oauth.enabled=false.
	MCPToken string
	// APIToken — Bearer для REST /{slug}/resolve/*, /{slug}/reports/*. Пусто = REST не публикуется.
	APIToken string
	// DevAccessKey — fallback-ключ для формы логина, когда нет доступа к 1С. Пусто = только 1С.
	DevAccessKey string

	// DefaultScopes / SupportedScopes — переопределяют глобальные списки из oauth.*. Пусто = глобальные.
	DefaultScopes   []string
	SupportedScopes []string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (t *Tenant) Timeout() time.Duration {
	return time.Duration(t.TimeoutMs) * time.Millisecond
}

// ReportTimeout — таймаут отчётных эндпойнтов. Падает обратно на Timeout(), если не задан,
// чтобы не обнулить таймаут (нулевой Timeout у http.Client означает «ждать вечно»).
func (t *Tenant) ReportTimeout() time.Duration {
	if t.ReportTimeoutMs <= 0 {
		return t.Timeout()
	}
	return time.Duration(t.ReportTimeoutMs) * time.Millisecond
}

// ResolveCacheTTL — TTL кэша resolve_*. Отрицательное значение отключает кэш.
func (t *Tenant) ResolveCacheTTL() time.Duration {
	return time.Duration(t.ResolveCacheTTLSec) * time.Second
}

// slugRe — слаг попадает в путь URL и в OAuth issuer: только нижний регистр, цифры и дефис.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// reservedSlugs — сегменты, занятые корневыми маршрутами гейта.
var reservedSlugs = map[string]bool{
	"health":      true,
	"admin":       true,
	"mcp":         true,
	"oauth":       true,
	".well-known": true,
}

// Normalize проставляет дефолты и приводит поля к каноническому виду. Вызывается перед Validate
// и перед записью в БД — чтобы в хранилище не попадали полуфабрикаты вроде timeout_ms=0.
func (t *Tenant) Normalize() {
	t.Slug = strings.ToLower(strings.TrimSpace(t.Slug))
	t.Name = strings.TrimSpace(t.Name)
	t.BaseURL = strings.TrimRight(strings.TrimSpace(t.BaseURL), "/")

	if t.Name == "" {
		t.Name = t.Slug
	}
	if t.TimeoutMs == 0 {
		t.TimeoutMs = DefaultTimeoutMs
	}
	if t.ReportTimeoutMs == 0 {
		t.ReportTimeoutMs = DefaultReportTimeoutMs
	}
	if t.ResolveCacheTTLSec == 0 {
		t.ResolveCacheTTLSec = DefaultResolveCacheTTLSec
	}

	t.DefaultScopes = cleanScopes(t.DefaultScopes)
	t.SupportedScopes = cleanScopes(t.SupportedScopes)
}

// Validate — то, что нельзя доверить форме: слаг участвует в маршрутизации и в issuer,
// а пустой base_url означает клиента, который будет молча падать на каждом вызове.
func (t *Tenant) Validate() error {
	if !slugRe.MatchString(t.Slug) {
		return fmt.Errorf("слаг должен состоять из строчных латинских букв, цифр и дефиса и начинаться с буквы или цифры")
	}
	if reservedSlugs[t.Slug] {
		return fmt.Errorf("слаг %q зарезервирован", t.Slug)
	}
	if t.BaseURL == "" {
		return fmt.Errorf("укажите адрес базы 1С")
	}
	u, err := url.Parse(t.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("адрес базы 1С должен быть абсолютным URL, например https://1c.example.com/api")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("адрес базы 1С должен использовать http или https")
	}
	if t.Username == "" {
		return fmt.Errorf("укажите имя пользователя 1С")
	}
	if t.TimeoutMs < 0 || t.ReportTimeoutMs < 0 {
		return fmt.Errorf("таймауты не могут быть отрицательными")
	}
	return nil
}

func cleanScopes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
