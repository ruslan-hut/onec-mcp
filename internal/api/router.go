package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *Handler, mcpHandler http.Handler, apiBearerToken string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	r.Get("/health", h.Health)

	if apiBearerToken != "" {
		r.Group(func(r chi.Router) {
			r.Use(BearerAuth(apiBearerToken))
			r.Post("/resolve/customer", h.ResolveCustomer)
			r.Post("/resolve/warehouse", h.ResolveWarehouse)
			r.Post("/reports/sales", h.SalesReport)
		})
	}

	if mcpHandler != nil {
		r.Post("/mcp", mcpHandler.ServeHTTP)
	}

	return r
}
