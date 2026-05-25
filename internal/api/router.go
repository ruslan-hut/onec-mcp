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

// OAuthLimiters — набор per-endpoint лимитеров для OAuth-маршрутов.
// Хранится в main.go, чтобы там же можно было запускать Cleanup() в фоновом тикере.
type OAuthLimiters struct {
	Authorize *oauth.FixedWindowLimiter
	Register  *oauth.FixedWindowLimiter
	Token     *oauth.FixedWindowLimiter
}

// NewRouter собирает chi-роутер. Если oauthServer != nil — публикуются метаданные/oauth-endpoint-ы
// и /mcp защищается OAuth Bearer middleware; иначе работает легаси-схема (статический Bearer внутри mcp.Handler).
// limiters может быть nil (тогда rate-limit не применяется), либо содержать настроенные лимитеры.
func NewRouter(h *Handler, mcpHandler http.Handler, apiBearerToken string, oauthServer *oauth.Server, limiters *OAuthLimiters, logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(RequestLogger(logger))
	r.Use(middleware.Recoverer)

	r.NotFound(notFoundHandler)

	r.Get("/health", h.Health)

	if apiBearerToken != "" {
		r.Group(func(r chi.Router) {
			r.Use(BearerAuth(apiBearerToken, logger))
			r.Post("/resolve/customer", h.ResolveCustomer)
			r.Post("/resolve/warehouse", h.ResolveWarehouse)
			r.Post("/resolve/product", h.ResolveProduct)
			r.Post("/resolve/sales_channel", h.ResolveSalesChannel)
			r.Post("/reports/sales", h.SalesReport)
			r.Post("/reports/stock", h.StockReport)
		})
	}

	if oauthServer != nil {
		// Discovery — публичные, без auth и без rate-limit (это просто статика метаданных)
		r.Get("/.well-known/oauth-protected-resource", oauthServer.HandleProtectedResourceMetadata)
		r.Get("/.well-known/oauth-authorization-server", oauthServer.HandleAuthorizationServerMetadata)

		// Rate-limit навешиваем только на чувствительные POST-операции.
		// GET /oauth/authorize не лимитим — это просто рендер формы, реальная атака идёт через POST.
		registerMW := chainMaybe(limiters != nil && limiters.Register != nil, func() func(http.Handler) http.Handler {
			return limiters.Register.Middleware()
		})
		authorizeMW := chainMaybe(limiters != nil && limiters.Authorize != nil, func() func(http.Handler) http.Handler {
			return limiters.Authorize.Middleware()
		})
		tokenMW := chainMaybe(limiters != nil && limiters.Token != nil, func() func(http.Handler) http.Handler {
			return limiters.Token.Middleware()
		})

		r.With(registerMW).Post("/oauth/register", oauthServer.HandleRegister)
		r.Get("/oauth/authorize", oauthServer.HandleAuthorize)
		r.With(authorizeMW).Post("/oauth/authorize", oauthServer.HandleAuthorize)
		r.With(tokenMW).Post("/oauth/token", oauthServer.HandleToken)
	}

	if mcpHandler != nil {
		if oauthServer != nil {
			r.With(oauthServer.Middleware).Post("/mcp", mcpHandler.ServeHTTP)
		} else {
			r.Post("/mcp", mcpHandler.ServeHTTP)
		}
	}

	return r
}

// chainMaybe — возвращает реальное middleware если условие выполнено, иначе passthrough.
// Нужно чтобы r.With(mw) принял функцию даже когда лимит выключен (limit=0 / лимитер nil).
func chainMaybe(enabled bool, factory func() func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	if !enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return factory()
}
