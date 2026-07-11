package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"example.com/mcp-sales-mvp/internal/oauth"
)

// Tenant — рантайм-обвязка одной базы 1С: всё, что нужно, чтобы обслужить запрос по её слагу.
// Собирается билдером (см. NewRegistry) из записи в БД. MCP и OAuth могут быть nil, если
// соответствующая подсистема выключена в конфиге; APIToken пустой = REST для базы не публикуется.
type Tenant struct {
	Slug     string
	Name     string
	Handler  *Handler
	MCP      http.Handler
	APIToken string
	OAuth    *oauth.Server
	// Verifier — TTL-кэш проверки MCP-ключей этой базы. Держим ссылку, чтобы фоновый тикер
	// мог чистить просроченные записи. nil, когда OAuth выключен.
	Verifier *oauth.CachedVerifier
}

// BuildFunc собирает рантайм-обвязки всех включённых баз, читая их из хранилища.
// Живёт в main.go, где доступны config/onec/mcp/oauth — так реестр не тянет эти зависимости в api.
type BuildFunc func(ctx context.Context) ([]*Tenant, error)

// Registry — то, что превращает слаг из URL в обвязку базы. Базы правятся в /admin на живую,
// поэтому маршруты не могут быть статическими: роутер держит шаблоны с {tenant}, а реестр
// резолвит слаг на каждый запрос.
type Registry struct {
	build  BuildFunc
	logger *slog.Logger

	mu      sync.RWMutex
	byslug  map[string]*Tenant
	ordered []*Tenant
}

func NewRegistry(build BuildFunc, logger *slog.Logger) *Registry {
	return &Registry{
		build:  build,
		logger: logger,
		byslug: make(map[string]*Tenant),
	}
}

// Reload перечитывает базы из хранилища и атомарно подменяет карту. Вызывается на старте
// и после каждой правки в /admin.
//
// Перестраивается всё целиком, а не только изменённая база: правки редки, а частичное обновление
// потребовало бы диффа. Побочный эффект — сброс кэшей резолвов и верификации у всех баз;
// это лишний поход в 1С, но не ошибка.
func (r *Registry) Reload(ctx context.Context) error {
	tenants, err := r.build(ctx)
	if err != nil {
		return err
	}

	byslug := make(map[string]*Tenant, len(tenants))
	for _, t := range tenants {
		byslug[t.Slug] = t
	}

	r.mu.Lock()
	r.byslug = byslug
	r.ordered = tenants
	r.mu.Unlock()

	r.logger.Info("tenant registry reloaded", "count", len(tenants))
	return nil
}

// Get — обвязка базы по слагу. Второй результат false, если базы нет или она выключена
// (выключенные базы билдер не отдаёт вовсе).
func (r *Registry) Get(slug string) (*Tenant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byslug[slug]
	return t, ok
}

// All — снимок текущих баз. Используется фоновым тикером для чистки кэшей верификации.
func (r *Registry) All() []*Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Tenant, len(r.ordered))
	copy(out, r.ordered)
	return out
}

// Handle — обёртка маршрута: достаёт слаг из пути, резолвит базу и передаёт её обработчику.
// Неизвестный или выключенный слаг → 404 (выключенные базы билдер не отдаёт вовсе).
//
// Резолв делается на каждый запрос, а не один раз при сборке роутера: базы правятся в /admin
// на живую, и обвязка под слагом может быть заменена в любой момент.
func (r *Registry) Handle(fn func(*Tenant, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		t, ok := r.Get(chi.URLParam(req, "tenant"))
		if !ok {
			notFoundHandler(w, req)
			return
		}
		fn(t, w, req)
	}
}
