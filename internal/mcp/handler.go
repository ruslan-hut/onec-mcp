package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/oauth"
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
		resp = h.handleToolsList(r, req)
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

// handleToolsList фильтрует список инструментов по scopes авторизованного пользователя:
// LLM показывается только то, что разрешено, и не пытается вызывать заведомо запрещённое.
// Когда OAuth не активен (FromContext возвращает nil) — отдаём всё, как было.
func (h *Handler) handleToolsList(r *http.Request, req Request) *Response {
	auth := oauth.FromContext(r.Context())
	tools := GetTools()

	if auth != nil {
		filtered := make([]Tool, 0, len(tools))
		for _, t := range tools {
			required, mapped := ToolScopes[t.Name]
			if !mapped {
				continue
			}
			if auth.HasScope(required) {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	sub, cid := authIdentity(auth)
	h.logger.Info("mcp.tool.list", "sub", sub, "client_id", cid, "count", len(tools))

	return NewResponse(req.ID, ListToolsResult{Tools: tools})
}

func (h *Handler) handleToolsCall(r *http.Request, req Request) *Response {
	started := time.Now()
	auth := oauth.FromContext(r.Context())

	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		h.auditToolCall(auth, "", false, "invalid_params", started)
		return InvalidParams(req.ID, "failed to parse params")
	}

	required, ok := ToolScopes[params.Name]
	if !ok {
		h.auditToolCall(auth, params.Name, false, "unknown_tool", started)
		return InvalidParams(req.ID, "unknown tool: "+params.Name)
	}

	// Per-tool ACL: проверяем, что в Bearer-токене присутствует нужный scope.
	// При OAuth=off (FromContext nil) проверка пропускается — поведение совместимо с легаси-бирером.
	if auth != nil && !auth.HasScope(required) {
		h.logger.Warn("oauth.scope.denied",
			"tool", params.Name, "required", required, "sub", auth.Sub, "have", auth.Scope)
		h.auditToolCall(auth, params.Name, false, "scope_denied", started)
		return NewResponse(req.ID, &CallToolResult{
			Content: []ContentBlock{TextContent(
				fmt.Sprintf("permission denied: tool %q requires scope %q", params.Name, required),
			)},
			IsError: true,
		})
	}

	var result *CallToolResult
	var err error

	switch params.Name {
	case ToolResolveCustomer:
		result, err = h.callResolveCustomer(r, params.Arguments)
	case ToolResolveWarehouse:
		result, err = h.callResolveWarehouse(r, params.Arguments)
	case ToolResolveProduct:
		result, err = h.callResolveProduct(r, params.Arguments)
	case ToolSalesReport:
		result, err = h.callSalesReport(r, params.Arguments)
	case ToolStockBalance:
		result, err = h.callStockBalance(r, params.Arguments)
	default:
		h.auditToolCall(auth, params.Name, false, "unknown_tool", started)
		return InvalidParams(req.ID, "unknown tool: "+params.Name)
	}

	if err != nil {
		h.logger.Error("tool call failed", "tool", params.Name, "error", err)
		h.auditToolCall(auth, params.Name, false, "tool_error", started)
		return NewResponse(req.ID, &CallToolResult{
			Content: []ContentBlock{TextContent(err.Error())},
			IsError: true,
		})
	}

	h.auditToolCall(auth, params.Name, true, "", started)
	return NewResponse(req.ID, result)
}

// auditToolCall пишет audit-запись по факту обработки tools/call.
// Логируется всегда (включая ошибки парсинга и scope denial) — каждая попытка вызова инструмента
// должна быть видна в журнале с привязкой к sub/client_id для расследования инцидентов.
func (h *Handler) auditToolCall(auth *oauth.AuthInfo, tool string, ok bool, reason string, started time.Time) {
	sub, cid := authIdentity(auth)
	fields := []any{
		"sub", sub,
		"client_id", cid,
		"tool", tool,
		"ok", ok,
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if reason != "" {
		fields = append(fields, "reason", reason)
	}
	h.logger.Info("mcp.tool.call", fields...)
}

func authIdentity(auth *oauth.AuthInfo) (sub, clientID string) {
	if auth == nil {
		return "", ""
	}
	return auth.Sub, auth.ClientID
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

func (h *Handler) callResolveProduct(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveProduct(r.Context(), a.Query, limit)
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

type stockReportArgs struct {
	Date     string            `json:"date"`
	Filters  onec.StockFilters `json:"filters"`
	GroupBy  []string          `json:"group_by"`
	Measures []string          `json:"measures"`
	Top      int               `json:"top"`
	Sort     []onec.SortSpec   `json:"sort"`
}

func (h *Handler) callStockBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a stockReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	if a.Top <= 0 || a.Top > h.cfg.Limits.MaxRows {
		a.Top = h.cfg.Limits.MaxRows
	}

	req := &onec.StockReportRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      a.Top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.StockReport(r.Context(), req)
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
