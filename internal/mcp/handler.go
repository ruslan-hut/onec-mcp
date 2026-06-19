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

		// Measure-level ACL: без права mcp:report:cost убираем cost/profit/margin из enum мер
		// sales_report, чтобы LLM их даже не предлагал. Финальная защита — на стороне 1С.
		if !auth.HasScope(ScopeReportCost) {
			stripCostMeasures(tools)
		}
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
	case ToolResolveSalesChannel:
		result, err = h.callResolveSalesChannel(r, params.Arguments)
	case ToolResolveCash:
		result, err = h.callResolveCash(r, params.Arguments)
	case ToolResolveCostArticle:
		result, err = h.callResolveCostArticle(r, params.Arguments)
	case ToolResolveOperation:
		result, err = h.callResolveOperation(r, params.Arguments)
	case ToolCashBalance:
		result, err = h.callCashBalance(r, params.Arguments)
	case ToolCashFlow:
		result, err = h.callCashFlow(r, params.Arguments)
	case ToolSalesReport:
		result, err = h.callSalesReport(r, params.Arguments)
	case ToolStockBalance:
		result, err = h.callStockBalance(r, params.Arguments)
	case ToolTopProducts:
		result, err = h.callTopProducts(r, params.Arguments)
	case ToolCustomerSummary:
		result, err = h.callCustomerSummary(r, params.Arguments)
	case ToolEventLog:
		result, err = h.callEventLog(r, params.Arguments)
	case ToolObjectHistory:
		result, err = h.callEventLog(r, params.Arguments)
	case ToolFindDocument:
		result, err = h.callFindDocument(r, params.Arguments)
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
	Query         string `json:"query"`
	Limit         int    `json:"limit"`
	IncludeGroups bool   `json:"include_groups"`
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

	resp, err := h.onecClient.ResolveCustomer(r.Context(), a.Query, limit, a.IncludeGroups)
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

	resp, err := h.onecClient.ResolveProduct(r.Context(), a.Query, limit, a.IncludeGroups)
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

func (h *Handler) callResolveSalesChannel(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveSalesChannel(r.Context(), a.Query, limit)
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

func (h *Handler) callResolveCash(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveCash(r.Context(), a.Query, limit)
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

func (h *Handler) callResolveCostArticle(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveCostArticle(r.Context(), a.Query, limit, a.IncludeGroups)
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

func (h *Handler) callResolveOperation(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := a.Limit
	if limit <= 0 || limit > h.cfg.Limits.ResolveLimit {
		limit = h.cfg.Limits.ResolveLimit
	}

	resp, err := h.onecClient.ResolveOperation(r.Context(), a.Query, limit)
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

type cashBalanceArgs struct {
	Date     string           `json:"date"`
	Filters  onec.CashFilters `json:"filters"`
	GroupBy  []string         `json:"group_by"`
	Measures []string         `json:"measures"`
	Top      int              `json:"top"`
	Sort     []onec.SortSpec  `json:"sort"`
}

func (h *Handler) callCashBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a cashBalanceArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	if a.Top <= 0 || a.Top > h.cfg.Limits.MaxRows {
		a.Top = h.cfg.Limits.MaxRows
	}

	req := &onec.CashBalanceRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      a.Top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.CashBalance(r.Context(), req)
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

type cashFlowArgs struct {
	Period   onec.Period      `json:"period"`
	Filters  onec.CashFilters `json:"filters"`
	GroupBy  []string         `json:"group_by"`
	Measures []string         `json:"measures"`
	Top      int              `json:"top"`
	Sort     []onec.SortSpec  `json:"sort"`
}

func (h *Handler) callCashFlow(r *http.Request, args any) (*CallToolResult, error) {
	var a cashFlowArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	if a.Top <= 0 || a.Top > h.cfg.Limits.MaxRows {
		a.Top = h.cfg.Limits.MaxRows
	}

	req := &onec.CashFlowRequest{
		Period:   a.Period,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      a.Top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.CashFlow(r.Context(), req)
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

type topProductsArgs struct {
	Period  onec.Period       `json:"period"`
	Filters onec.SalesFilters `json:"filters"`
	By      string            `json:"by"`
	Top     int               `json:"top"`
}

func (h *Handler) callTopProducts(r *http.Request, args any) (*CallToolResult, error) {
	var a topProductsArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	if a.Top <= 0 || a.Top > h.cfg.Limits.MaxRows {
		if a.Top <= 0 {
			a.Top = 10
		} else {
			a.Top = h.cfg.Limits.MaxRows
		}
	}

	req := &onec.TopProductsRequest{
		Period:  a.Period,
		Filters: a.Filters,
		By:      a.By,
		Top:     a.Top,
	}

	resp, err := h.onecClient.TopProducts(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type customerSummaryArgs struct {
	CustomerID  string      `json:"customer_id"`
	Period      onec.Period `json:"period"`
	TopProducts int         `json:"top_products"`
}

func (h *Handler) callCustomerSummary(r *http.Request, args any) (*CallToolResult, error) {
	var a customerSummaryArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.CustomerSummaryRequest{
		CustomerID:  a.CustomerID,
		Period:      a.Period,
		TopProducts: a.TopProducts,
	}

	resp, err := h.onecClient.CustomerSummary(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

// callEventLog обслуживает event_log и object_history — оба ходят в один админ-эндпоинт
// чтения журнала регистрации. Аргументы инструмента прокидываются в 1С без переупаковки
// (1С сама разбирает user/session/level/events/object_type/object_id/period/limit).
func (h *Handler) callEventLog(r *http.Request, args any) (*CallToolResult, error) {
	resp, err := h.onecClient.EventLog(r.Context(), args)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

func (h *Handler) callFindDocument(r *http.Request, args any) (*CallToolResult, error) {
	resp, err := h.onecClient.FindDocument(r.Context(), args)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

// stripCostMeasures удаляет cost/profit/margin из enum мер инструмента sales_report.
// Вызывается для пользователей без права mcp:report:cost. Мутирует вложенные map'ы схемы;
// это безопасно, т.к. GetTools() конструирует свежие map'ы на каждый запрос.
func stripCostMeasures(tools []Tool) {
	blocked := make(map[string]bool, len(CostMeasures))
	for _, m := range CostMeasures {
		blocked[m] = true
	}

	for _, t := range tools {
		if t.Name != ToolSalesReport {
			continue
		}

		schema, ok := t.InputSchema.(map[string]any)
		if !ok {
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		measures, ok := props["measures"].(map[string]any)
		if !ok {
			continue
		}
		items, ok := measures["items"].(map[string]any)
		if !ok {
			continue
		}
		enum, ok := items["enum"].([]string)
		if !ok {
			continue
		}

		filtered := enum[:0:0]
		for _, e := range enum {
			if !blocked[e] {
				filtered = append(filtered, e)
			}
		}
		items["enum"] = filtered
	}
}

func mapToStruct(m any, v any) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
