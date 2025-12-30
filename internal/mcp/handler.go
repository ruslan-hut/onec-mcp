package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/onec"
)

type Handler struct {
	onecClient  *onec.Client
	cfg         *config.Config
	logger      *slog.Logger
	bearerToken string
}

func NewHandler(onecClient *onec.Client, cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		onecClient:  onecClient,
		cfg:         cfg,
		logger:      logger,
		bearerToken: cfg.MCP.BearerToken,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.bearerToken != "" && !h.authenticate(r) {
		h.writeResponse(w, Unauthorized(nil))
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeResponse(w, ParseError(nil))
		return
	}

	if req.JSONRPC != JSONRPCVersion {
		h.writeResponse(w, InvalidRequest(req.ID))
		return
	}

	h.logger.Info("mcp request", "method", req.Method, "id", req.ID)

	var resp *Response
	switch req.Method {
	case "initialize":
		resp = h.handleInitialize(req)
	case "tools/list":
		resp = h.handleToolsList(req)
	case "tools/call":
		resp = h.handleToolsCall(r, req)
	default:
		resp = MethodNotFound(req.ID, req.Method)
	}

	h.writeResponse(w, resp)
}

func (h *Handler) authenticate(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	return parts[1] == h.bearerToken
}

func (h *Handler) writeResponse(w http.ResponseWriter, resp *Response) {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

func (h *Handler) handleInitialize(req Request) *Response {
	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		ServerInfo: ServerInfo{
			Name:    "mcp-sales-mvp",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
	}
	return NewResponse(req.ID, result)
}

func (h *Handler) handleToolsList(req Request) *Response {
	result := ListToolsResult{
		Tools: GetTools(),
	}
	return NewResponse(req.ID, result)
}

func (h *Handler) handleToolsCall(r *http.Request, req Request) *Response {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return InvalidParams(req.ID, "failed to parse params")
	}

	var result *CallToolResult
	var err error

	switch params.Name {
	case ToolResolveCustomer:
		result, err = h.callResolveCustomer(r, params.Arguments)
	case ToolResolveWarehouse:
		result, err = h.callResolveWarehouse(r, params.Arguments)
	case ToolSalesReport:
		result, err = h.callSalesReport(r, params.Arguments)
	default:
		return InvalidParams(req.ID, "unknown tool: "+params.Name)
	}

	if err != nil {
		h.logger.Error("tool call failed", "tool", params.Name, "error", err)
		return NewResponse(req.ID, &CallToolResult{
			Content: []ContentBlock{TextContent(err.Error())},
			IsError: true,
		})
	}

	return NewResponse(req.ID, result)
}

type resolveArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (h *Handler) callResolveCustomer(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveCustomer(r.Context(), a.Query, limit)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(data))},
	}, nil
}

func (h *Handler) callResolveWarehouse(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveWarehouse(r.Context(), a.Query, limit)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(data))},
	}, nil
}

type salesReportArgs struct {
	Period   onec.Period       `json:"period"`
	Filters  onec.SalesFilters `json:"filters"`
	GroupBy  []string          `json:"group_by"`
	Measures []string          `json:"measures"`
	Top      int               `json:"top"`
	Sort     []onec.SortSpec   `json:"sort"`
}

func (h *Handler) callSalesReport(r *http.Request, args any) (*CallToolResult, error) {
	var a salesReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	if a.Top <= 0 || a.Top > h.cfg.Limits.MaxRows {
		a.Top = h.cfg.Limits.MaxRows
	}

	req := &onec.SalesReportRequest{
		Period:   a.Period,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      a.Top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.SalesReport(r.Context(), req)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(data))},
	}, nil
}

func mapToStruct(m any, v any) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
