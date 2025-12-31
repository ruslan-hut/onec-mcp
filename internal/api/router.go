package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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

func NewRouter(h *Handler, mcpHandler http.Handler, apiBearerToken string, logger *slog.Logger) *chi.Mux {
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
			r.Post("/reports/sales", h.SalesReport)
		})
	}

	if mcpHandler != nil {
		r.Post("/mcp", mcpHandler.ServeHTTP)
	}

	return r
}
