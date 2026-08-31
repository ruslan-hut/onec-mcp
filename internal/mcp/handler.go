package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/oauth"
	"example.com/mcp-sales-mvp/internal/onec"
)

// Handler обслуживает /{tenant}/mcp одной базы: onecClient уже указывает на нужную 1С,
// bearerToken — статический токен этой базы (пусто, когда включён OAuth и аутентификацию
// делает внешний middleware). cfg нужен только ради общих лимитов.
type Handler struct {
	onecClient  *onec.Client
	cfg         *config.Config
	logger      *slog.Logger
	bearerToken string
}

func NewHandler(onecClient *onec.Client, cfg *config.Config, bearerToken string, logger *slog.Logger) *Handler {
	return &Handler{
		onecClient:  onecClient,
		cfg:         cfg,
		logger:      logger,
		bearerToken: bearerToken,
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

	// Уведомление (JSON-RPC 2.0 §4.1): нет id — ответа быть не должно, даже об ошибке.
	// Сюда попадает notifications/initialized, который шлёт каждый MCP-клиент после initialize.
	// Раньше он проваливался в default и получал "Method not found" — формально нарушение спеки.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

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
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(h.bearerToken)) == 1
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

	// Меры по закупочной стоимости закрыты отдельным правом. Раньше оно жило только в схеме
	// (stripCostMeasures вырезает их из enum в tools/list), а вызов не проверялся вовсе: клиент,
	// проигнорировавший схему, спокойно просил measures:["profit"]. Единственной защитой оставался
	// заголовок X-MCP-Scopes на стороне 1С — которого в режиме статического токена вообще нет.
	if auth != nil && !auth.HasScope(ScopeReportCost) {
		if blocked := costMeasuresIn(params.Arguments); len(blocked) > 0 {
			h.logger.Warn("oauth.scope.denied",
				"tool", params.Name, "required", ScopeReportCost, "sub", auth.Sub, "measures", blocked)
			h.auditToolCall(auth, params.Name, false, "scope_denied", started)
			return NewResponse(req.ID, &CallToolResult{
				Content: []ContentBlock{TextContent(
					fmt.Sprintf("permission denied: measures %v require scope %q", blocked, ScopeReportCost),
				)},
				IsError: true,
			})
		}
	}

	// Неизвестный ключ в filters — ошибка, а не пустое место. Раньше такой ключ доходил
	// до разбора тела и молча отбрасывался (в Go неизвестные поля JSON игнорируются):
	// агент просил sales_report с product_ids, получал выборку по всей базе и читал её
	// как выборку по одному SKU. Тихо проигнорированный фильтр опаснее отказа — цифры
	// выглядят правдоподобно, и подмену никто не замечает.
	if unknown, allowed := unknownFilterKeys(params.Name, params.Arguments); len(unknown) > 0 {
		h.logger.Warn("tool.filters.unknown",
			"tool", params.Name, "keys", unknown)
		h.auditToolCall(auth, params.Name, false, "unknown_filter", started)
		return NewResponse(req.ID, &CallToolResult{
			Content: []ContentBlock{TextContent(
				fmt.Sprintf("unsupported filter %v for tool %q; supported filters: %v",
					unknown, params.Name, allowed),
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
	case ToolResolveMaterial:
		result, err = h.callResolveMaterial(r, params.Arguments)
	case ToolResolveSalesChannel:
		result, err = h.callResolveSalesChannel(r, params.Arguments)
	case ToolResolveFirm:
		result, err = h.callResolveFirm(r, params.Arguments)
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
	case ToolReceivablesBalance:
		result, err = h.callReceivablesBalance(r, params.Arguments)
	case ToolPayablesBalance:
		result, err = h.callPayablesBalance(r, params.Arguments)
	case ToolPurchasesReport:
		result, err = h.callPurchasesReport(r, params.Arguments)
	case ToolGoodsInTransit:
		result, err = h.callGoodsInTransit(r, params.Arguments)
	case ToolSalesReport:
		result, err = h.callSalesReport(r, params.Arguments)
	case ToolStockBalance:
		result, err = h.callStockBalance(r, params.Arguments)
	case ToolAvailabilityReport:
		result, err = h.callAvailabilityReport(r, params.Arguments)
	case ToolProductDetails:
		result, err = h.callProductDetails(r, params.Arguments)
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
	case ToolProductSpecification:
		result, err = h.callSpecification(r, onec.ReportSpecification, params.Arguments)
	case ToolSpecificationCost:
		result, err = h.callSpecification(r, onec.ReportSpecificationCost, params.Arguments)
	case ToolSpecificationExplode:
		result, err = h.callSpecification(r, onec.ReportSpecificationExplode, params.Arguments)
	case ToolSpecificationWhereUsed:
		result, err = h.callSpecification(r, onec.ReportSpecificationWhereUsed, params.Arguments)
	case ToolSpecificationVersions:
		result, err = h.callSpecification(r, onec.ReportSpecificationVersions, params.Arguments)
	case ToolSpecificationList:
		result, err = h.callSpecification(r, onec.ReportSpecificationList, params.Arguments)
	case ToolProductionOutput:
		result, err = h.callProductionReport(r, onec.ReportProductionOutput, params.Arguments)
	case ToolProductionConsumption:
		result, err = h.callProductionReport(r, onec.ReportProductionConsumption, params.Arguments)
	case ToolProductionDocumentDetail:
		result, err = h.callProductionDocument(r, params.Arguments)
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
	Query         string   `json:"query"`
	Limit         flexInt  `json:"limit"`
	IncludeGroups flexBool `json:"include_groups"`
}

func (h *Handler) callResolveCustomer(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := h.clampLimit(a.Limit)

	resp, err := h.onecClient.ResolveCustomer(r.Context(), a.Query, limit, bool(a.IncludeGroups))
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

	limit := h.clampLimit(a.Limit)

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

	limit := h.clampLimit(a.Limit)

	resp, err := h.onecClient.ResolveProduct(r.Context(), a.Query, limit, bool(a.IncludeGroups))
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

// callResolveMaterial — сырьё и комплектующие. Отдельный вызов, а не флаг в resolve_product:
// наборы не пересекаются (пометка ДляПроизводства), и право у них разное.
func (h *Handler) callResolveMaterial(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := h.clampLimit(a.Limit)

	resp, err := h.onecClient.ResolveMaterial(r.Context(), a.Query, limit, bool(a.IncludeGroups))
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

	limit := h.clampLimit(a.Limit)

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

func (h *Handler) callResolveFirm(r *http.Request, args any) (*CallToolResult, error) {
	var a resolveArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	limit := h.clampLimit(a.Limit)

	resp, err := h.onecClient.ResolveFirm(r.Context(), a.Query, limit)
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

	limit := h.clampLimit(a.Limit)

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

	limit := h.clampLimit(a.Limit)

	resp, err := h.onecClient.ResolveCostArticle(r.Context(), a.Query, limit, bool(a.IncludeGroups))
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

	limit := h.clampLimit(a.Limit)

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
	Date     string                  `json:"date"`
	Filters  onec.CashBalanceFilters `json:"filters"`
	GroupBy  []string                `json:"group_by"`
	Measures []string                `json:"measures"`
	Top      flexInt                 `json:"top"`
	Sort     []onec.SortSpec         `json:"sort"`
}

func (h *Handler) callCashBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a cashBalanceArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.CashBalanceRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
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
	Top      flexInt          `json:"top"`
	Sort     []onec.SortSpec  `json:"sort"`
}

func (h *Handler) callCashFlow(r *http.Request, args any) (*CallToolResult, error) {
	var a cashFlowArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.CashFlowRequest{
		Period:   a.Period,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
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

// Аргументы ДЗ и КЗ различаются только именем ключа контрагента (customer_ids / supplier_ids):
// отдельные типы вместо одного общего — чтобы ключ чужой роли не мог попасть в тело запроса к 1С.
type receivablesArgs struct {
	Date     string                  `json:"date"`
	Filters  onec.ReceivablesFilters `json:"filters"`
	GroupBy  []string                `json:"group_by"`
	Measures []string                `json:"measures"`
	Top      flexInt                 `json:"top"`
	Sort     []onec.SortSpec         `json:"sort"`
}

type payablesArgs struct {
	Date     string               `json:"date"`
	Filters  onec.PayablesFilters `json:"filters"`
	GroupBy  []string             `json:"group_by"`
	Measures []string             `json:"measures"`
	Top      flexInt              `json:"top"`
	Sort     []onec.SortSpec      `json:"sort"`
}

func (h *Handler) callReceivablesBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a receivablesArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.ReceivablesRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.Receivables(r.Context(), req)
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

func (h *Handler) callPayablesBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a payablesArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.PayablesRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.Payables(r.Context(), req)
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

type purchasesArgs struct {
	Period  onec.Period           `json:"period"`
	Filters onec.PurchasesFilters `json:"filters"`
	// InTransit — булево или строка "any"; интерпретирует значение 1С, гейт только пробрасывает
	InTransit interface{}     `json:"in_transit"`
	GroupBy   []string        `json:"group_by"`
	Measures  []string        `json:"measures"`
	Top       flexInt         `json:"top"`
	Sort      []onec.SortSpec `json:"sort"`
}

func (h *Handler) callPurchasesReport(r *http.Request, args any) (*CallToolResult, error) {
	var a purchasesArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.PurchasesRequest{
		Period:    a.Period,
		Filters:   a.Filters,
		InTransit: a.InTransit,
		GroupBy:   a.GroupBy,
		Measures:  a.Measures,
		Top:       top,
		Sort:      a.Sort,
	}

	resp, err := h.onecClient.Purchases(r.Context(), req)
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

type goodsInTransitArgs struct {
	Date     string                     `json:"date"`
	Filters  onec.GoodsInTransitFilters `json:"filters"`
	GroupBy  []string                   `json:"group_by"`
	Measures []string                   `json:"measures"`
	Top      flexInt                    `json:"top"`
	Sort     []onec.SortSpec            `json:"sort"`
}

// callGoodsInTransit — остатки товаров в пути. Ответ 1С пробрасывается сырым (как у
// availability_report): состав колонок задаёт 1С.
func (h *Handler) callGoodsInTransit(r *http.Request, args any) (*CallToolResult, error) {
	var a goodsInTransitArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.GoodsInTransitRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      h.clampTop(a.Top),
		Sort:     a.Sort,
	}

	resp, err := h.onecClient.GoodsInTransit(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type salesReportArgs struct {
	Period   onec.Period       `json:"period"`
	Filters  onec.SalesFilters `json:"filters"`
	GroupBy  []string          `json:"group_by"`
	Measures []string          `json:"measures"`
	Top      flexInt           `json:"top"`
	Sort     []onec.SortSpec   `json:"sort"`
}

func (h *Handler) callSalesReport(r *http.Request, args any) (*CallToolResult, error) {
	var a salesReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.SalesReportRequest{
		Period:   a.Period,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
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
	Top      flexInt           `json:"top"`
	Sort     []onec.SortSpec   `json:"sort"`
}

func (h *Handler) callStockBalance(r *http.Request, args any) (*CallToolResult, error) {
	var a stockReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.StockReportRequest{
		Date:     a.Date,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
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

type availabilityReportArgs struct {
	Period   onec.Period              `json:"period"`
	Filters  onec.AvailabilityFilters `json:"filters"`
	GroupBy  []string                 `json:"group_by"`
	Measures []string                 `json:"measures"`
	Top      flexInt                  `json:"top"`
	Sort     []onec.SortSpec          `json:"sort"`
}

func (h *Handler) callAvailabilityReport(r *http.Request, args any) (*CallToolResult, error) {
	var a availabilityReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTop(a.Top)

	req := &onec.AvailabilityReportRequest{
		Period:   a.Period,
		Filters:  a.Filters,
		GroupBy:  a.GroupBy,
		Measures: a.Measures,
		Top:      top,
		Sort:     a.Sort,
	}

	// ответ 1С проброс as-is (RawMessage): помимо columns/rows/totals содержит period/days/applied_filters
	resp, err := h.onecClient.AvailabilityReport(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type productDetailsArgs struct {
	ProductIDs []string `json:"product_ids"`
	Fields     []string `json:"fields"`
}

func (h *Handler) callProductDetails(r *http.Request, args any) (*CallToolResult, error) {
	var a productDetailsArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.ProductDetailsRequest{
		ProductIDs: a.ProductIDs,
		Fields:     a.Fields,
	}

	resp, err := h.onecClient.ProductDetails(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type topProductsArgs struct {
	Period  onec.Period             `json:"period"`
	Filters onec.TopProductsFilters `json:"filters"`
	By      string                  `json:"by"`
	Top     flexInt                 `json:"top"`
}

func (h *Handler) callTopProducts(r *http.Request, args any) (*CallToolResult, error) {
	var a topProductsArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	top := h.clampTopDefault(a.Top, 10)

	req := &onec.TopProductsRequest{
		Period:  a.Period,
		Filters: a.Filters,
		By:      a.By,
		Top:     top,
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
	TopProducts flexInt     `json:"top_products"`
}

func (h *Handler) callCustomerSummary(r *http.Request, args any) (*CallToolResult, error) {
	var a customerSummaryArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	// top_products — такой же счётчик строк, как top в остальных отчётах, и клэмпится так же:
	// без этого отрицательное или заведомо огромное значение уезжало в 1С как есть.
	req := &onec.CustomerSummaryRequest{
		CustomerID:  a.CustomerID,
		Period:      a.Period,
		TopProducts: h.clampTopDefault(a.TopProducts, 5),
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
// (1С сама разбирает user/session/level/events/object_type/object_id/period/limit),
// но через unstringifyJSON: админские инструменты шли мимо него, и двойное кодирование period —
// то самое, ради которого он написан, — ломало ровно эти три вызова, а не остальные.
func (h *Handler) callEventLog(r *http.Request, args any) (*CallToolResult, error) {
	resp, err := h.onecClient.EventLog(r.Context(), unstringifyJSON(args))
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

func (h *Handler) callFindDocument(r *http.Request, args any) (*CallToolResult, error) {
	resp, err := h.onecClient.FindDocument(r.Context(), unstringifyJSON(args))
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

// specificationArgs — аргументы шести инструментов по спецификациям. Структура одна на всех:
// набор параметров у них общий (продукция/материал, дата среза, три разреза состава), каждый
// читает своё подмножество, а лишние ключи 1С игнорирует. Разводить шесть почти одинаковых
// структур ради этого смысла нет.
type specificationArgs struct {
	Date              string      `json:"date"`
	ProductID         string      `json:"product_id"`
	ProductIDs        []string    `json:"product_ids"`
	MaterialID        string      `json:"material_id"`
	MaterialIDs       []string    `json:"material_ids"`
	MatrixID          string      `json:"matrix_id"`
	CompositionTypeID string      `json:"composition_type_id"`
	ProductionGroupID string      `json:"production_group_id"`
	PriceTypeID       string      `json:"price_type_id"`
	Qty               flexFloat   `json:"qty"`
	MaxDepth          flexInt     `json:"max_depth"`
	WithCost          flexBool    `json:"with_cost"`
	MissingOnly       flexBool    `json:"missing_only"`
	Period            onec.Period `json:"period"`
	Limit             flexInt     `json:"limit"`
}

// callSpecification обслуживает product_specification, specification_cost,
// specification_explode, specification_where_used, specification_versions и
// specification_list — все шесть ходят в /mcp/reports/{reportType} с одним телом.
// Ответ 1С пробрасывается as-is (RawMessage): у инструментов разный конверт
// (таблица columns/rows против дерева версий), и типизировать его в гейте нечего.
func (h *Handler) callSpecification(r *http.Request, reportType string, args any) (*CallToolResult, error) {
	var a specificationArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.SpecificationRequest{
		Date:              a.Date,
		ProductID:         a.ProductID,
		ProductIDs:        a.ProductIDs,
		MaterialID:        a.MaterialID,
		MaterialIDs:       a.MaterialIDs,
		MatrixID:          a.MatrixID,
		CompositionTypeID: a.CompositionTypeID,
		ProductionGroupID: a.ProductionGroupID,
		PriceTypeID:       a.PriceTypeID,
		Qty:               float64(a.Qty),
		MaxDepth:          clampDepth(a.MaxDepth),
		WithCost:          bool(a.WithCost),
		MissingOnly:       bool(a.MissingOnly),
		Limit:             h.clampRows(a.Limit),
	}

	// период нужен только истории версий и режиму missing_only; пустой объект наверх не шлём,
	// иначе 1С поймёт его как «сегодня» и молча сузит выборку до текущего дня
	if a.Period.From != "" || a.Period.To != "" {
		period := a.Period
		req.Period = &period
	}

	resp, err := h.onecClient.ProductionReport(r.Context(), reportType, req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type productionReportArgs struct {
	Period        onec.Period            `json:"period"`
	OperationType string                 `json:"operation_type"`
	Filters       onec.ProductionFilters `json:"filters"`
	GroupBy       []string               `json:"group_by"`
	Measures      []string               `json:"measures"`
	Top           flexInt                `json:"top"`
	Sort          []onec.SortSpec        `json:"sort"`
}

// callProductionReport обслуживает production_output и production_consumption: секция задана
// эндпойнтом 1С, тело у них общее.
func (h *Handler) callProductionReport(r *http.Request, reportType string, args any) (*CallToolResult, error) {
	var a productionReportArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.ProductionReportRequest{
		Period:        a.Period,
		OperationType: a.OperationType,
		Filters:       a.Filters,
		GroupBy:       a.GroupBy,
		Measures:      a.Measures,
		Top:           h.clampTop(a.Top),
		Sort:          a.Sort,
	}

	resp, err := h.onecClient.ProductionReport(r.Context(), reportType, req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

type productionDocumentArgs struct {
	DocumentID string `json:"document_id"`
}

func (h *Handler) callProductionDocument(r *http.Request, args any) (*CallToolResult, error) {
	var a productionDocumentArgs
	if err := mapToStruct(args, &a); err != nil {
		return nil, err
	}

	req := &onec.ProductionDocumentRequest{DocumentID: a.DocumentID}

	resp, err := h.onecClient.ProductionReport(r.Context(), onec.ReportProductionDocument, req)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []ContentBlock{TextContent(string(resp))},
	}, nil
}

// clampRows — необязательный limit инструментов по спецификациям: 0 означает «по умолчанию 1С»
// и наверх не уходит вовсе, всё остальное режется общим потолком max_rows.
func (h *Handler) clampRows(limit flexInt) int {
	if limit <= 0 {
		return 0
	}
	if int(limit) > h.cfg.Limits.MaxRows {
		return h.cfg.Limits.MaxRows
	}
	return int(limit)
}

// clampDepth — глубина разузлования. Верхняя граница дублирует 1С (там тот же потолок 10):
// дерево растёт экспоненциально, и просьба «раскрой на 100 уровней» должна упираться в лимит
// на входе, а не в таймаут запроса.
func clampDepth(depth flexInt) int {
	const maxDepth = 10
	if depth <= 0 {
		return 0
	}
	if int(depth) > maxDepth {
		return maxDepth
	}
	return int(depth)
}

// costMeasuresIn возвращает запрошенные меры, закрытые правом mcp:report:cost.
// Аргументы разбираются как есть (map от json.Unmarshal), с поправкой на двойное кодирование:
// measures может приехать строкой "[\"profit\"]" — тогда его развернёт unstringifyJSON.
// unknownFilterKeys сверяет ключи filters в теле вызова со схемой инструмента и
// возвращает те, которых схема не объявляет, вместе со списком поддержанных.
// Источник истины — та же InputSchema, что уходит клиенту в tools/list, поэтому
// проверка не может разойтись с объявленным контрактом.
func unknownFilterKeys(tool string, args any) ([]string, []string) {
	m, ok := unstringifyJSON(args).(map[string]any)
	if !ok {
		return nil, nil
	}

	filters, ok := m["filters"].(map[string]any)
	if !ok || len(filters) == 0 {
		return nil, nil
	}

	declared := filterProperties(tool)
	if declared == nil {
		// Инструмент фильтров не объявляет вовсе — сверять не с чем, пропускаем.
		return nil, nil
	}

	allowed := make([]string, 0, len(declared))
	for name := range declared {
		allowed = append(allowed, name)
	}
	sort.Strings(allowed)

	var unknown []string
	for name := range filters {
		if _, ok := declared[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)

	return unknown, allowed
}

// filterProperties достаёт properties объекта filters из схемы инструмента.
// nil означает "инструмент фильтров не объявляет", пустая карта — "объявляет пустой набор".
func filterProperties(tool string) map[string]any {
	for _, t := range GetTools() {
		if t.Name != tool {
			continue
		}

		schema, ok := t.InputSchema.(map[string]any)
		if !ok {
			return nil
		}

		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return nil
		}

		filters, ok := props["filters"].(map[string]any)
		if !ok {
			return nil
		}

		declared, ok := filters["properties"].(map[string]any)
		if !ok {
			return nil
		}

		return declared
	}

	return nil
}

func costMeasuresIn(args any) []string {
	m, ok := unstringifyJSON(args).(map[string]any)
	if !ok {
		return nil
	}

	raw, ok := m["measures"].([]any)
	if !ok {
		return nil
	}

	blocked := make(map[string]bool, len(CostMeasures))
	for _, name := range CostMeasures {
		blocked[name] = true
	}

	var found []string
	for _, v := range raw {
		name, ok := v.(string)
		if ok && blocked[name] {
			found = append(found, name)
		}
	}

	return found
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
	data, err := json.Marshal(unstringifyJSON(m))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// unstringifyJSON рекурсивно заменяет строки, содержащие валидный JSON-объект или массив,
// на распарсенное значение. Компенсирует LLM-клиентов, которые иногда сериализуют вложенные
// аргументы (period, filters, sort) в строку — тогда прямой Unmarshal падает с
// "cannot unmarshal string into Go struct field ...". Пустую строку / невалидный JSON не трогаем
// (эти случаи по object-типам добираются толерантным UnmarshalJSON в onec.models).
func unstringifyJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = unstringifyJSON(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = unstringifyJSON(val)
		}
		return t
	case string:
		s := strings.TrimSpace(t)
		if len(s) >= 2 && (s[0] == '{' || s[0] == '[') {
			var parsed any
			if json.Unmarshal([]byte(s), &parsed) == nil {
				return unstringifyJSON(parsed)
			}
		}
		return t
	default:
		return v
	}
}
