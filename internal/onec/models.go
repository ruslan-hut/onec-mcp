package onec

type ResolveRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type CustomerCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	INN      string `json:"inn,omitempty"`
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

type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SalesFilters struct {
	CustomerIDs  []string `json:"customer_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
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
