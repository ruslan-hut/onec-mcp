package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"example.com/mcp-sales-mvp/internal/oauth"
)

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	err := json.NewEncoder(w).Encode(ErrorResponse{
		Error:   "not_found",
		Message: "Endpoint not found",
	})
	if err != nil {
		return
	}
}

// OAuthLimiters — набор per-endpoint лимитеров для OAuth-маршрутов. Лимиты пер-IP и общие
// для всех баз. Хранится в main.go, чтобы там же можно было запускать Cleanup() в фоновом тикере.
type OAuthLimiters struct {
	Authorize *oauth.FixedWindowLimiter
	Register  *oauth.FixedWindowLimiter
	Token     *oauth.FixedWindowLimiter
}

// NewRouter собирает chi-роутер. Маршруты баз описаны шаблонами с {tenant} и резолвятся через
// реестр на каждый запрос — базы правятся в /admin на живую, статически их развесить нельзя.
//
// В корне живут только /health, /admin/* и канонические well-known пути: RFC 9728/8414 требуют
// вставлять path-компонент ресурса ПОСЛЕ well-known сегмента, а не перед ним.
//
// adminHandler может быть nil (admin выключен в конфиге).
func NewRouter(reg *Registry, adminHandler http.Handler, limiters *OAuthLimiters, logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(RequestLogger(logger))
	r.Use(middleware.Recoverer)

	r.NotFound(notFoundHandler)

	r.Get("/health", Health)

	if adminHandler != nil {
		r.Mount("/admin", adminHandler)
	}

	// Rate-limit. POST /oauth/authorize — это перебор ключей; GET — рендер формы, но он тоже
	// неаутентифицирован и ходит в SQLite за клиентом, а соединение одно на весь гейт: без лимита
	// шквал GET'ов на одну базу сериализуется с выпуском токенов и админкой всех остальных.
	registerMW := chainMaybe(limiters != nil && limiters.Register != nil, func() func(http.Handler) http.Handler {
		return limiters.Register.Middleware()
	})
	authorizeMW := chainMaybe(limiters != nil && limiters.Authorize != nil, func() func(http.Handler) http.Handler {
		return limiters.Authorize.Middleware()
	})
	tokenMW := chainMaybe(limiters != nil && limiters.Token != nil, func() func(http.Handler) http.Handler {
		return limiters.Token.Middleware()
	})

	// Канонические пути метаданных (RFC 9728 §3.1 / RFC 8414 §3.1). Именно их запрашивает
	// MCP-клиент, получив resource_metadata в заголовке WWW-Authenticate.
	r.Get("/.well-known/oauth-protected-resource/{tenant}/mcp",
		reg.Handle(oauthEndpoint((*oauth.Server).HandleProtectedResourceMetadata)))
	r.Get("/.well-known/oauth-authorization-server/{tenant}",
		reg.Handle(oauthEndpoint((*oauth.Server).HandleAuthorizationServerMetadata)))

	r.Route("/{tenant}", func(r chi.Router) {
		// Совместимая форма well-known: часть клиентов ищет метаданные по префиксу самого
		// ресурса, а не по каноническому path-insertion. Дёшево отдать оба варианта.
		r.Get("/.well-known/oauth-protected-resource",
			reg.Handle(oauthEndpoint((*oauth.Server).HandleProtectedResourceMetadata)))
		r.Get("/.well-known/oauth-authorization-server",
			reg.Handle(oauthEndpoint((*oauth.Server).HandleAuthorizationServerMetadata)))

		r.With(registerMW).Post("/oauth/register", reg.Handle(oauthEndpoint((*oauth.Server).HandleRegister)))
		r.With(authorizeMW).Get("/oauth/authorize", reg.Handle(oauthEndpoint((*oauth.Server).HandleAuthorize)))
		r.With(authorizeMW).Post("/oauth/authorize", reg.Handle(oauthEndpoint((*oauth.Server).HandleAuthorize)))
		r.With(tokenMW).Post("/oauth/token", reg.Handle(oauthEndpoint((*oauth.Server).HandleToken)))

		r.Post("/mcp", reg.Handle(serveMCP))

		r.Post("/resolve/customer", reg.Handle(restEndpoint((*Handler).ResolveCustomer)))
		r.Post("/resolve/warehouse", reg.Handle(restEndpoint((*Handler).ResolveWarehouse)))
		r.Post("/resolve/product", reg.Handle(restEndpoint((*Handler).ResolveProduct)))
		r.Post("/resolve/sales_channel", reg.Handle(restEndpoint((*Handler).ResolveSalesChannel)))
		r.Post("/reports/sales", reg.Handle(restEndpoint((*Handler).SalesReport)))
		r.Post("/reports/stock", reg.Handle(restEndpoint((*Handler).StockReport)))
	})

	return r
}

// oauthEndpoint адаптирует метод oauth.Server к маршруту базы. Если OAuth в гейте выключен —
// у базы его нет, и endpoint просто не существует (404).
func oauthEndpoint(method func(*oauth.Server, http.ResponseWriter, *http.Request)) func(*Tenant, http.ResponseWriter, *http.Request) {
	return func(t *Tenant, w http.ResponseWriter, r *http.Request) {
		if t.OAuth == nil {
			notFoundHandler(w, r)
			return
		}
		method(t.OAuth, w, r)
	}
}

// serveMCP отдаёт JSON-RPC базы, предварительно навесив аутентификацию. При включённом OAuth
// это middleware сервера этой базы (оно же проверяет audience токена); иначе — статический
// Bearer, который mcp.Handler проверяет сам.
func serveMCP(t *Tenant, w http.ResponseWriter, r *http.Request) {
	if t.MCP == nil {
		notFoundHandler(w, r)
		return
	}
	if t.OAuth != nil {
		t.OAuth.Middleware(t.MCP).ServeHTTP(w, r)
		return
	}
	t.MCP.ServeHTTP(w, r)
}

// restEndpoint закрывает REST-обработчик базы её же Bearer-токеном. Токен не задан — маршрут
// для этой базы не существует: REST это серверная интеграция, включать её «по умолчанию» нельзя.
func restEndpoint(method func(*Handler, http.ResponseWriter, *http.Request)) func(*Tenant, http.ResponseWriter, *http.Request) {
	return func(t *Tenant, w http.ResponseWriter, r *http.Request) {
		if t.APIToken == "" {
			notFoundHandler(w, r)
			return
		}
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method(t.Handler, w, r)
		})
		BearerAuth(t.APIToken, t.Handler.logger)(h).ServeHTTP(w, r)
	}
}

// chainMaybe — возвращает реальное middleware если условие выполнено, иначе passthrough.
// Нужно чтобы r.With(mw) принял функцию даже когда лимит выключен (limit=0 / лимитер nil).
func chainMaybe(enabled bool, factory func() func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	if !enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return factory()
}
