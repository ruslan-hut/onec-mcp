package onec

import "fmt"

// APIError — структурированная ошибка от HTTP-сервиса 1С.
// Если тело ответа парсится как {"error": "...", "message": "..."} — возвращается этот тип,
// чтобы API/MCP-слой мог пробросить осмысленное сообщение клиенту, а не generic «onec_error».
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"error"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("1C %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("1C %d %s", e.StatusCode, e.Code)
}

type ResolveRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
	// IncludeGroups — для иерархических справочников (Контрагенты, Товары) включает в выдачу
	// группы каталога вместе с листьями. UUID группы можно передать в filters.customer_ids /
	// filters.product_ids — фильтр применится через IN HIERARCHY (захватит всех потомков).
	IncludeGroups bool `json:"include_groups,omitempty"`
}

type CustomerCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Phone    string `json:"phone,omitempty"`
	City     string `json:"city,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveCustomerResponse struct {
	Candidates []CustomerCandidate `json:"candidates"`
}

type WarehouseCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveWarehouseResponse struct {
	Candidates []WarehouseCandidate `json:"candidates"`
}

// CodeLabel — пара «стабильный код + человекочитаемая метка» для статусных атрибутов,
// приходящих из 1С (status, eu_certification). Код стабилен для логики LLM, label — для показа.
type CodeLabel struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type ProductCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
	// Статусные атрибуты жизненного цикла позиции (Category Watchdog). Вычисляются на стороне
	// 1С из реквизитов карточки; здесь только сквозной проброс. omitempty — старые сборки 1С
	// их не отдают. Коды синхронны с хелперами CommonModules/MCP (BSL): ProductStatusInfo,
	// ProductMarkets, ProductCertificationInfo.
	Status          *CodeLabel `json:"status,omitempty"`            // new | active | phasing_out | excluded
	StatusChangedAt string     `json:"status_changed_at,omitempty"` // дата последней смены статуса (регистр ProductStatus)
	Markets         []string   `json:"markets,omitempty"`           // UA | EU | OTHER
	EUCertification *CodeLabel `json:"eu_certification,omitempty"`  // certified | in_process | not_required
}

type ResolveProductResponse struct {
	Candidates []ProductCandidate `json:"candidates"`
}

// ProductDetailsRequest — пакетная выдача статусных атрибутов номенклатуры.
// Ответ 1С ({products:[...]}) пробрасывается как есть (json.RawMessage).
type ProductDetailsRequest struct {
	ProductIDs []string `json:"product_ids"`
	Fields     []string `json:"fields,omitempty"`
}

type SalesChannelCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Archived bool   `json:"archived"`
}

type ResolveSalesChannelResponse struct {
	Candidates []SalesChannelCandidate `json:"candidates"`
}

type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SalesFilters struct {
	CustomerIDs  []string `json:"customer_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// SalesChannelIDs — UUIDs элементов справочника SalesChannel. Применяется через IN HIERARCHY,
	// поэтому можно передать как родительский узел (B2B) для агрегата по всем дочерним каналам,
	// так и конкретный лист (B2B Online).
	SalesChannelIDs []string `json:"sales_channel_ids,omitempty"`
	// CustomerCohort ограничивает выборку «новыми» или «повторными» контрагентами.
	// «Новый» = ДатаСоздания контрагента >= начало месяца, предшествующего PeriodBegin.
	// Допустимо: "new" | "returning". Пустая строка / отсутствие = без фильтра.
	CustomerCohort string `json:"customer_cohort,omitempty"`
	// ProductStatus — фильтр по логическому статусу ЖЦ позиции (см. StockFilters.ProductStatus).
	ProductStatus []string `json:"product_status,omitempty"`
}

type SortSpec struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

type SalesReportRequest struct {
	Period   Period       `json:"period"`
	Filters  SalesFilters `json:"filters,omitempty"`
	GroupBy  []string     `json:"group_by,omitempty"`
	Measures []string     `json:"measures,omitempty"`
	Top      int          `json:"top,omitempty"`
	Sort     []SortSpec   `json:"sort,omitempty"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type SalesReportResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
}

type StockFilters struct {
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// ProductStatus — фильтр по логическому статусу ЖЦ позиции (new|active|phasing_out|excluded).
	// На стороне 1С разворачивается в пред-резолв ссылок товаров по статусу, а НЕ в условие на
	// виртуальную таблицу Balance() — иначе теряется таблица итогов и Balance() уходит в таймаут.
	ProductStatus []string `json:"product_status,omitempty"`
}

type StockReportRequest struct {
	Date     string       `json:"date,omitempty"`
	Filters  StockFilters `json:"filters,omitempty"`
	GroupBy  []string     `json:"group_by,omitempty"`
	Measures []string     `json:"measures,omitempty"`
	Top      int          `json:"top,omitempty"`
	Sort     []SortSpec   `json:"sort,omitempty"`
}

type StockReportResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
}

type AvailabilityFilters struct {
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
}

// AvailabilityReportRequest — out-of-stock дни из регистра ОстаткиТоваровПоДням.
// Ответ 1С ({columns, rows, totals, period, days, applied_filters}) пробрасывается как есть
// (json.RawMessage), поэтому отдельной структуры ответа нет.
type AvailabilityReportRequest struct {
	Period   Period              `json:"period"`
	Filters  AvailabilityFilters `json:"filters,omitempty"`
	GroupBy  []string            `json:"group_by,omitempty"`
	Measures []string            `json:"measures,omitempty"`
	Top      int                 `json:"top,omitempty"`
	Sort     []SortSpec          `json:"sort,omitempty"`
}

type CashCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveCashResponse struct {
	Candidates []CashCandidate `json:"candidates"`
}

// CashFilters — общий фильтр для денежных отчётов. CashIDs — UUID касс (resolve_cash);
// для cash_flow применяется к измерению Счет, для cash_balance — к измерению Касса.
// Остальные поля используются только в cash_flow (фильтры по измерениям ВидОперации и Аналитика):
//
//	OperationIDs   — виды операций (resolve_operation), измерение ВидОперации;
//	CostArticleIDs — статьи затрат (resolve_cost_article), аналитика, IN HIERARCHY;
//	CustomerIDs    — контрагенты (resolve_customer), аналитика, IN.
//
// CostArticleIDs и CustomerIDs объединяются по аналитике через OR.
type CashFilters struct {
	CashIDs        []string `json:"cash_ids,omitempty"`
	OperationIDs   []string `json:"operation_ids,omitempty"`
	CostArticleIDs []string `json:"cost_article_ids,omitempty"`
	CustomerIDs    []string `json:"customer_ids,omitempty"`
}

type CostArticleCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveCostArticleResponse struct {
	Candidates []CostArticleCandidate `json:"candidates"`
}

type OperationCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Archived bool   `json:"archived"`
}

type ResolveOperationResponse struct {
	Candidates []OperationCandidate `json:"candidates"`
}

type CashBalanceRequest struct {
	Date     string      `json:"date,omitempty"`
	Filters  CashFilters `json:"filters,omitempty"`
	GroupBy  []string    `json:"group_by,omitempty"`
	Measures []string    `json:"measures,omitempty"`
	Top      int         `json:"top,omitempty"`
	Sort     []SortSpec  `json:"sort,omitempty"`
}

type CashFlowRequest struct {
	Period   Period      `json:"period"`
	Filters  CashFilters `json:"filters,omitempty"`
	GroupBy  []string    `json:"group_by,omitempty"`
	Measures []string    `json:"measures,omitempty"`
	Top      int         `json:"top,omitempty"`
	Sort     []SortSpec  `json:"sort,omitempty"`
}

type CashReportResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
}

type TopProductsRequest struct {
	Period  Period       `json:"period"`
	Filters SalesFilters `json:"filters,omitempty"`
	By      string       `json:"by,omitempty"`
	Top     int          `json:"top,omitempty"`
}

type CustomerSummaryRequest struct {
	CustomerID  string `json:"customer_id"`
	Period      Period `json:"period"`
	TopProducts int    `json:"top_products,omitempty"`
}

// SettlementsFilters — фильтр отчётов взаиморасчётов (receivables / payables).
// CustomerIDs / SupplierIDs — UUID контрагентов (оба резолвятся через resolve_customer:
// поставщики тоже лежат в справочнике Контрагенты). 1С берёт ключ по роли отчёта (customer_ids
// для receivables, supplier_ids для payables), с запасным customer_ids. Применяется через IN HIERARCHY.
// FirmIDs — UUID фирм (UA/PL юрлиц); измерение Фирма, применяется через IN. UUID фирмы можно взять
// из group_by=["firm"] предыдущего вызова (отдельного резолвера фирм нет — их единицы).
type SettlementsFilters struct {
	CustomerIDs []string `json:"customer_ids,omitempty"`
	SupplierIDs []string `json:"supplier_ids,omitempty"`
	FirmIDs     []string `json:"firm_ids,omitempty"`
}

// SettlementsRequest — тело POST /mcp/reports/{receivables|payables}.
// Развёрнутые остатки взаиморасчётов на дату по регистру «Взаиморасчеты».
type SettlementsRequest struct {
	Date     string             `json:"date,omitempty"`
	Filters  SettlementsFilters `json:"filters,omitempty"`
	GroupBy  []string           `json:"group_by,omitempty"`
	Measures []string           `json:"measures,omitempty"`
	Top      int                `json:"top,omitempty"`
	Sort     []SortSpec         `json:"sort,omitempty"`
}

type SettlementsResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
}

// PurchasesFilters — фильтр отчёта закупок (purchases_report).
// SupplierIDs — UUID поставщиков (резолвятся через resolve_customer, IN HIERARCHY);
// FirmIDs — UUID фирм (UA/PL), измерение Фирма, IN.
type PurchasesFilters struct {
	SupplierIDs []string `json:"supplier_ids,omitempty"`
	FirmIDs     []string `json:"firm_ids,omitempty"`
}

// PurchasesRequest — тело POST /mcp/reports/purchases.
// Обороты поступления ТМЦ за период по документу «ПриходнаяНакладная» (нетто возвратов).
type PurchasesRequest struct {
	Period   Period           `json:"period"`
	Filters  PurchasesFilters `json:"filters,omitempty"`
	GroupBy  []string         `json:"group_by,omitempty"`
	Measures []string         `json:"measures,omitempty"`
	Top      int              `json:"top,omitempty"`
	Sort     []SortSpec       `json:"sort,omitempty"`
}

type PurchasesResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
}

// AuthVerifyRequest — тело POST /mcp/auth/verify к 1С.
type AuthVerifyRequest struct {
	Key string `json:"key"`
}

// AuthVerifyResponse — ответ 1С при валидном ключе. sub — UUID учётной записи MCP,
// scopes — список OAuth-скоупов в каноническом строковом формате.
type AuthVerifyResponse struct {
	Sub    string   `json:"sub"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}
