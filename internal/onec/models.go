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

type ProductCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveProductResponse struct {
	Candidates []ProductCandidate `json:"candidates"`
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
	CustomerIDs     []string `json:"customer_ids,omitempty"`
	WarehouseIDs    []string `json:"warehouse_ids,omitempty"`
	// SalesChannelIDs — UUIDs элементов справочника SalesChannel. Применяется через IN HIERARCHY,
	// поэтому можно передать как родительский узел (B2B) для агрегата по всем дочерним каналам,
	// так и конкретный лист (B2B Online).
	SalesChannelIDs []string `json:"sales_channel_ids,omitempty"`
	// CustomerCohort ограничивает выборку «новыми» или «повторными» контрагентами.
	// «Новый» = ДатаСоздания контрагента >= начало месяца, предшествующего PeriodBegin.
	// Допустимо: "new" | "returning". Пустая строка / отсутствие = без фильтра.
	CustomerCohort string `json:"customer_cohort,omitempty"`
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
