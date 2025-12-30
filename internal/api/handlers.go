package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/onec"
)

type Handler struct {
	onecClient *onec.Client
	cfg        *config.Config
	logger     *slog.Logger
}

func NewHandler(onecClient *onec.Client, cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		onecClient: onecClient,
		cfg:        cfg,
		logger:     logger,
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, err string, message string) {
	h.writeJSON(w, status, ErrorResponse{Error: err, Message: message})
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ResolveCustomer(w http.ResponseWriter, r *http.Request) {
	var req ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to parse request body")
		return
	}

	if req.Query == "" {
		h.writeError(w, http.StatusBadRequest, "validation_error", "Query is required")
		return
	}

	limit := req.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveCustomer(r.Context(), req.Query, limit)
	if err != nil {
		h.logger.Error("failed to resolve customer", "error", err, "query", req.Query)
		h.writeError(w, http.StatusBadGateway, "onec_error", "Failed to resolve customer from 1C")
		return
	}

	apiResp := ResolveCustomerResponse{
		Candidates: make([]CustomerCandidate, len(resp.Candidates)),
	}
	for i, c := range resp.Candidates {
		apiResp.Candidates[i] = CustomerCandidate{
			ID:       c.ID,
			Label:    c.Label,
			INN:      c.INN,
			City:     c.City,
			Archived: c.Archived,
		}
	}

	h.writeJSON(w, http.StatusOK, apiResp)
}

func (h *Handler) ResolveWarehouse(w http.ResponseWriter, r *http.Request) {
	var req ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to parse request body")
		return
	}

	if req.Query == "" {
		h.writeError(w, http.StatusBadRequest, "validation_error", "Query is required")
		return
	}

	limit := req.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveWarehouse(r.Context(), req.Query, limit)
	if err != nil {
		h.logger.Error("failed to resolve warehouse", "error", err, "query", req.Query)
		h.writeError(w, http.StatusBadGateway, "onec_error", "Failed to resolve warehouse from 1C")
		return
	}

	apiResp := ResolveWarehouseResponse{
		Candidates: make([]WarehouseCandidate, len(resp.Candidates)),
	}
	for i, c := range resp.Candidates {
		apiResp.Candidates[i] = WarehouseCandidate{
			ID:       c.ID,
			Label:    c.Label,
			Code:     c.Code,
			Archived: c.Archived,
		}
	}

	h.writeJSON(w, http.StatusOK, apiResp)
}

var validMeasures = map[string]bool{"amount": true, "qty": true}
var validGroupBy = map[string]bool{"customer": true, "warehouse": true}

func (h *Handler) SalesReport(w http.ResponseWriter, r *http.Request) {
	var req SalesReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to parse request body")
		return
	}

	if req.Period.From == "" || req.Period.To == "" {
		h.writeError(w, http.StatusBadRequest, "validation_error", "Period from and to are required")
		return
	}

	for _, m := range req.Measures {
		if !validMeasures[m] {
			h.writeError(w, http.StatusBadRequest, "validation_error", "Invalid measure: "+m+". Supported: amount, qty")
			return
		}
	}

	for _, g := range req.GroupBy {
		if !validGroupBy[g] {
			h.writeError(w, http.StatusBadRequest, "validation_error", "Invalid group_by: "+g+". Supported: customer, warehouse")
			return
		}
	}

	if req.Top <= 0 || req.Top > h.cfg.Limits.MaxRows {
		req.Top = h.cfg.Limits.MaxRows
	}

	onecReq := &onec.SalesReportRequest{
		Period: onec.Period{
			From: req.Period.From,
			To:   req.Period.To,
		},
		Filters: onec.SalesFilters{
			CustomerIDs:  req.Filters.CustomerIDs,
			WarehouseIDs: req.Filters.WarehouseIDs,
		},
		GroupBy:  req.GroupBy,
		Measures: req.Measures,
		Top:      req.Top,
	}

	for _, s := range req.Sort {
		onecReq.Sort = append(onecReq.Sort, onec.SortSpec{
			Field: s.Field,
			Dir:   s.Dir,
		})
	}

	resp, err := h.onecClient.SalesReport(r.Context(), onecReq)
	if err != nil {
		h.logger.Error("failed to get sales report", "error", err)
		h.writeError(w, http.StatusBadGateway, "onec_error", "Failed to get sales report from 1C")
		return
	}

	if len(resp.Rows) > h.cfg.Limits.MaxRows {
		h.writeError(w, http.StatusBadRequest, "limit_exceeded", "Result exceeds max_rows limit")
		return
	}

	apiResp := SalesReportResponse{
		Columns: make([]Column, len(resp.Columns)),
		Rows:    resp.Rows,
		Totals:  resp.Totals,
	}
	for i, c := range resp.Columns {
		apiResp.Columns[i] = Column{
			Name: c.Name,
			Type: c.Type,
		}
	}

	h.writeJSON(w, http.StatusOK, apiResp)
}
