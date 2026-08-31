package onec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// unmarshalObjectOrString парсит JSON-объект в out, но терпит частый косяк LLM в tool-calling:
// вложенный объект filters приходит как JSON-СТРОКА (двойное кодирование) или как пустая строка "".
// Объект → парсим строго; строка с валидным JSON-объектом → разворачиваем; ""/"{}"/невалидная
// строка/null → оставляем out пустым (лучше отчёт без фильтра, чем упавший вызов).
func unmarshalObjectOrString(data []byte, out any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" || s == "{}" {
			return nil
		}
		_ = json.Unmarshal([]byte(s), out) // невалидную строку тихо игнорируем (пустой фильтр)
		return nil
	}
	return json.Unmarshal(trimmed, out)
}

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
	// ForProduction — производственный склад (реквизит ДляПроизводства). Такие склады 1С
	// отдаёт только ключу с mcp:report:cost; для остальных прав их в выдаче просто нет.
	// omitempty не ставим: false — значимый ответ («это торговый склад»).
	ForProduction bool `json:"for_production"`
}

type ResolveWarehouseResponse struct {
	Candidates []WarehouseCandidate `json:"candidates"`
}

// MaterialCandidate — сырьё/комплектующие (номенклатура с пометкой ДляПроизводства).
// Живёт в том же справочнике Товары, что и ProductCandidate, но без статусных атрибутов:
// статус ЖЦ, рынки и сертификация описывают витрину и к сырью неприменимы. Вместо них —
// единица измерения, в которой заданы нормы расхода в спецификации.
type MaterialCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveMaterialResponse struct {
	Candidates []MaterialCandidate `json:"candidates"`
}

// CodeLabel — пара «стабильный код + человекочитаемая метка» для статусных атрибутов,
// приходящих из 1С (status, eu_certification). Код стабилен для логики LLM, label — для показа.
type CodeLabel struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type ProductCandidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Code  string `json:"code,omitempty"`
	// Unit — базовая единица измерения (БазоваяЕдиницаИзмерения). В ней выражены остатки
	// и доступность, поэтому число из stock_balance без неё нечитаемо. omitempty — как
	// у MaterialCandidate: старые сборки 1С поле не отдают.
	Unit     string `json:"unit,omitempty"`
	Archived bool   `json:"archived"`
	// IsGroup — позиция является группой справочника (реквизит ЭтоГруппа), а не товаром.
	// В выдаче появляется только при include_groups=true, но отдаётся всегда: без него
	// группу не отличить от товара, а её id ведёт себя иначе — в фильтрах отчётов он
	// раскрывается через IN HIERARCHY. omitempty не ставим: false — значимый ответ.
	IsGroup bool `json:"is_group"`
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

// FirmCandidate — юрлицо (фирма/организация), от имени которого оформлены документы.
// В rior-cf это Справочник.Фирмы, в УПП — Справочник.Организации. Резолвер появился, чтобы
// не добывать UUID фирмы побочным вызовом отчёта с group_by=["firm"].
type FirmCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Code     string `json:"code,omitempty"`
	Archived bool   `json:"archived"`
}

type ResolveFirmResponse struct {
	Candidates []FirmCandidate `json:"candidates"`
}

type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// UnmarshalJSON терпит тот же косяк LLM, что и фильтры: period может прийти объектом,
// объектом-в-строке (двойное кодирование) или пустой строкой. Покрывает все отчёты с period.
func (p *Period) UnmarshalJSON(data []byte) error {
	type alias Period
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*p = Period(a)
	return nil
}

type SalesFilters struct {
	CustomerIDs  []string `json:"customer_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// ProductIDs — UUID номенклатуры из resolve_product. Применяется через IN HIERARCHY,
	// поэтому принимает и лист, и UUID товарной группы (include_groups=true).
	//
	// Поля долго не было, хотя описание resolve_product уже отсылало к отбору продаж по
	// товару. Ключ product_ids в теле проходил валидацию схемы (additionalProperties
	// тогда не запрещал лишние ключи) и отбрасывался здесь при разборе — до 1С не
	// доходил вовсе. Агент получал выборку по всей базе и читал её как выборку по одному
	// SKU. Ср. тот же класс дефекта в комментарии к CashFilters.
	ProductIDs []string `json:"product_ids,omitempty"`
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
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *SalesFilters) UnmarshalJSON(data []byte) error {
	type alias SalesFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = SalesFilters(a)
	return nil
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

// ReportEcho — то, чем 1С сопровождает выборку: разобранный период (или дата остатков), роль
// в отчёте взаиморасчётов и эхо реально применённых фильтров. Отчёты декодируются в типизированные
// структуры, поэтому без явных полей эти данные молча терялись при пересборке ответа — а схемы
// инструментов их обещают, и агенту они нужны, чтобы убедиться, что фильтр действительно применён
// (1С, например, нормализует период к границам суток и разворачивает группы контрагентов).
//
// Поля сырые: форма applied_filters у каждого отчёта своя (customers/warehouses/cashes/…),
// типизировать её здесь незачем — гейт ничего в ней не считает, только прокидывает наверх.
type ReportEcho struct {
	Period         json.RawMessage `json:"period,omitempty"`
	Date           string          `json:"date,omitempty"`
	Role           string          `json:"role,omitempty"`
	AppliedFilters json.RawMessage `json:"applied_filters,omitempty"`
}

type SalesReportResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
	ReportEcho
}

type StockFilters struct {
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// ProductStatus — фильтр по логическому статусу ЖЦ позиции (new|active|phasing_out|excluded).
	// На стороне 1С разворачивается в пред-резолв ссылок товаров по статусу, а НЕ в условие на
	// виртуальную таблицу Balance() — иначе теряется таблица итогов и Balance() уходит в таймаут.
	ProductStatus []string `json:"product_status,omitempty"`
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *StockFilters) UnmarshalJSON(data []byte) error {
	type alias StockFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = StockFilters(a)
	return nil
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
	ReportEcho
}

type AvailabilityFilters struct {
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *AvailabilityFilters) UnmarshalJSON(data []byte) error {
	type alias AvailabilityFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = AvailabilityFilters(a)
	return nil
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

// CashFilters — фильтр отчёта движения денег (cash_flow).
//
//	CashIDs        — кассы (resolve_cash), измерение Счет;
//	OperationIDs   — виды операций (resolve_operation), измерение ВидОперации;
//	CostArticleIDs — статьи затрат (resolve_cost_article), аналитика, IN HIERARCHY;
//	CustomerIDs    — контрагенты (resolve_customer), аналитика, IN.
//
// CostArticleIDs и CustomerIDs объединяются по аналитике через OR.
//
// Раньше эту же структуру использовал и cash_balance, у которого из всего набора применим только
// CashIDs (у регистра ДеньгиВКассе одно измерение). Остальные ключи молча уезжали в 1С и там
// игнорировались: агент считал выборку отфильтрованной, а получал полную. У каждого инструмента
// теперь свой тип фильтра — тело запроса не может содержать ключ, которого нет в схеме инструмента.
type CashFilters struct {
	CashIDs        []string `json:"cash_ids,omitempty"`
	OperationIDs   []string `json:"operation_ids,omitempty"`
	CostArticleIDs []string `json:"cost_article_ids,omitempty"`
	CustomerIDs    []string `json:"customer_ids,omitempty"`
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *CashFilters) UnmarshalJSON(data []byte) error {
	type alias CashFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = CashFilters(a)
	return nil
}

// CashBalanceFilters — фильтр остатков в кассах (cash_balance). Только кассы: разрезать остаток
// по видам операций или аналитике нечем, у регистра ДеньгиВКассе единственное измерение Касса.
type CashBalanceFilters struct {
	CashIDs []string `json:"cash_ids,omitempty"`
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *CashBalanceFilters) UnmarshalJSON(data []byte) error {
	type alias CashBalanceFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = CashBalanceFilters(a)
	return nil
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
	Date     string             `json:"date,omitempty"`
	Filters  CashBalanceFilters `json:"filters,omitempty"`
	GroupBy  []string           `json:"group_by,omitempty"`
	Measures []string           `json:"measures,omitempty"`
	Top      int                `json:"top,omitempty"`
	Sort     []SortSpec         `json:"sort,omitempty"`
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
	ReportEcho
}

// TopProductsFilters — фильтр топа товаров. Подмножество SalesFilters: обёртка над sales-отчётом
// объявляет в схеме только контрагентов и склады, поэтому канал/когорта/статус сюда не проходят
// (раньше проходили — структура была общая с sales_report).
type TopProductsFilters struct {
	CustomerIDs  []string `json:"customer_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	// ProductIDs — как в SalesFilters. Осмысленно прежде всего с UUID товарной группы:
	// «топ внутри группы» — это топ товаров с отбором по её иерархии.
	ProductIDs []string `json:"product_ids,omitempty"`
	// FirmIDs — фильтр по фирмам (юрлицам) из resolve_firm. В многофирменных базах (УПП)
	// набор доступных фирм дополнительно ограничен правами учётной записи на стороне 1С:
	// пустой фильтр = все разрешённые ключу фирмы, а не все фирмы базы.
	FirmIDs []string `json:"firm_ids,omitempty"`
}

func (f *TopProductsFilters) UnmarshalJSON(data []byte) error {
	type alias TopProductsFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = TopProductsFilters(a)
	return nil
}

type TopProductsRequest struct {
	Period  Period             `json:"period"`
	Filters TopProductsFilters `json:"filters,omitempty"`
	By      string             `json:"by,omitempty"`
	Top     int                `json:"top,omitempty"`
}

type CustomerSummaryRequest struct {
	CustomerID  string `json:"customer_id"`
	Period      Period `json:"period"`
	TopProducts int    `json:"top_products,omitempty"`
}

// ReceivablesFilters / PayablesFilters — фильтры отчётов взаиморасчётов. Ключ контрагента
// именуется по роли отчёта: customer_ids у ДЗ, supplier_ids у КЗ (и те и другие — справочник
// Контрагенты, резолвятся через resolve_customer; применяются через IN HIERARCHY).
// FirmIDs — UUID фирм (UA/PL юрлиц), измерение Фирма, IN. UUID фирмы берётся из group_by=["firm"]
// предыдущего вызова — отдельного резолвера фирм нет, их единицы.
//
// Типы раздельные, потому что общая структура пропускала в тело запроса ключ чужой роли
// (supplier_ids в receivables и наоборот) — ключ, которого нет в схеме инструмента.
type ReceivablesFilters struct {
	CustomerIDs []string `json:"customer_ids,omitempty"`
	FirmIDs     []string `json:"firm_ids,omitempty"`
}

func (f *ReceivablesFilters) UnmarshalJSON(data []byte) error {
	type alias ReceivablesFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = ReceivablesFilters(a)
	return nil
}

type PayablesFilters struct {
	SupplierIDs []string `json:"supplier_ids,omitempty"`
	FirmIDs     []string `json:"firm_ids,omitempty"`
}

func (f *PayablesFilters) UnmarshalJSON(data []byte) error {
	type alias PayablesFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = PayablesFilters(a)
	return nil
}

// ReceivablesRequest / PayablesRequest — тело POST /mcp/reports/{receivables|payables}.
// Развёрнутые остатки взаиморасчётов на дату по регистру «Взаиморасчеты».
type ReceivablesRequest struct {
	Date     string             `json:"date,omitempty"`
	Filters  ReceivablesFilters `json:"filters,omitempty"`
	GroupBy  []string           `json:"group_by,omitempty"`
	Measures []string           `json:"measures,omitempty"`
	Top      int                `json:"top,omitempty"`
	Sort     []SortSpec         `json:"sort,omitempty"`
}

type PayablesRequest struct {
	Date     string          `json:"date,omitempty"`
	Filters  PayablesFilters `json:"filters,omitempty"`
	GroupBy  []string        `json:"group_by,omitempty"`
	Measures []string        `json:"measures,omitempty"`
	Top      int             `json:"top,omitempty"`
	Sort     []SortSpec      `json:"sort,omitempty"`
}

type SettlementsResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
	ReportEcho
}

// PurchasesFilters — фильтр отчёта закупок (purchases_report).
// SupplierIDs — UUID поставщиков (резолвятся через resolve_customer, IN HIERARCHY);
// FirmIDs — UUID фирм (UA/PL), измерение Фирма, IN.
type PurchasesFilters struct {
	SupplierIDs  []string `json:"supplier_ids,omitempty"`
	FirmIDs      []string `json:"firm_ids,omitempty"`
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
}

func (f *PurchasesFilters) UnmarshalJSON(data []byte) error {
	type alias PurchasesFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = PurchasesFilters(a)
	return nil
}

// PurchasesRequest — тело POST /mcp/reports/purchases.
// Обороты поступления ТМЦ за период по документу «ПриходнаяНакладная» (нетто возвратов).
type PurchasesRequest struct {
	Period  Period           `json:"period"`
	Filters PurchasesFilters `json:"filters,omitempty"`
	// InTransit — режим отбора накладных «в пути»: false (по умолчанию, только поступивший
	// товар), true (только в пути) или "any". Тип свободный, потому что 1С принимает и булево,
	// и строку; гейт значение не интерпретирует, только пробрасывает.
	InTransit interface{} `json:"in_transit,omitempty"`
	GroupBy   []string    `json:"group_by,omitempty"`
	Measures  []string    `json:"measures,omitempty"`
	Top       int         `json:"top,omitempty"`
	Sort      []SortSpec  `json:"sort,omitempty"`
}

type PurchasesResponse struct {
	Columns []Column               `json:"columns"`
	Rows    [][]interface{}        `json:"rows"`
	Totals  map[string]interface{} `json:"totals,omitempty"`
	ReportEcho
}

// GoodsInTransitFilters — отборы отчёта по товарам в пути. Поставщик берётся из
// документа-регистратора (сам регистр контрагента не хранит), поэтому отбор по нему
// возможен только вместе с чтением записей регистра — как это и сделано на стороне 1С.
type GoodsInTransitFilters struct {
	ProductIDs   []string `json:"product_ids,omitempty"`
	WarehouseIDs []string `json:"warehouse_ids,omitempty"`
	FirmIDs      []string `json:"firm_ids,omitempty"`
	StatusIDs    []string `json:"status_ids,omitempty"`
	SupplierIDs  []string `json:"supplier_ids,omitempty"`
}

func (f *GoodsInTransitFilters) UnmarshalJSON(data []byte) error {
	type alias GoodsInTransitFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = GoodsInTransitFilters(a)
	return nil
}

type GoodsInTransitRequest struct {
	Date     string                `json:"date,omitempty"`
	Filters  GoodsInTransitFilters `json:"filters,omitempty"`
	GroupBy  []string              `json:"group_by,omitempty"`
	Measures []string              `json:"measures,omitempty"`
	Top      int                   `json:"top,omitempty"`
	Sort     []SortSpec            `json:"sort,omitempty"`
}

// Производственный блок: типы отчётов 1С (последний сегмент пути /mcp/reports/{type}).
// Значения приходят только из этих констант — путь наружу не параметризуется.
const (
	ReportSpecification          = "specification"
	ReportSpecificationCost      = "specification_cost"
	ReportSpecificationExplode   = "specification_explode"
	ReportSpecificationWhereUsed = "specification_where_used"
	ReportSpecificationVersions  = "specification_versions"
	ReportSpecificationList      = "specification_list"
	ReportProductionOutput       = "production_output"
	ReportProductionConsumption  = "production_consumption"
	ReportProductionDocument     = "production_document"
)

// SpecificationRequest — общее тело запросов по спецификациям (шесть инструментов делят один
// набор параметров, каждый читает нужное подмножество; лишние ключи 1С игнорирует).
// Состав однозначно определяется четвёркой Продукция × Матрица × ТипСостава × ГруппировкаПроизводства,
// поэтому три «разреза» присутствуют во всех инструментах блока.
type SpecificationRequest struct {
	Date              string   `json:"date,omitempty"`
	ProductID         string   `json:"product_id,omitempty"`
	ProductIDs        []string `json:"product_ids,omitempty"`
	MaterialID        string   `json:"material_id,omitempty"`
	MaterialIDs       []string `json:"material_ids,omitempty"`
	MatrixID          string   `json:"matrix_id,omitempty"`
	CompositionTypeID string   `json:"composition_type_id,omitempty"`
	ProductionGroupID string   `json:"production_group_id,omitempty"`
	PriceTypeID       string   `json:"price_type_id,omitempty"`
	Qty               float64  `json:"qty,omitempty"`
	MaxDepth          int      `json:"max_depth,omitempty"`
	WithCost          bool     `json:"with_cost,omitempty"`
	MissingOnly       bool     `json:"missing_only,omitempty"`
	// Period нужен только specification_versions (окно истории) и specification_list
	// с missing_only (период выпуска). Указатель — чтобы не слать пустой объект остальным.
	Period *Period `json:"period,omitempty"`
	Limit  int     `json:"limit,omitempty"`
}

// ProductionFilters — фильтры отчётов по документам Производство.
// MaterialIDs применим только к секции consumption, EmployeeIDs — только к output;
// 1С молча игнорирует неприменимый к секции фильтр.
type ProductionFilters struct {
	ProductIDs         []string `json:"product_ids,omitempty"`
	MaterialIDs        []string `json:"material_ids,omitempty"`
	WarehouseIDs       []string `json:"warehouse_ids,omitempty"`
	EmployeeIDs        []string `json:"employee_ids,omitempty"`
	MatrixIDs          []string `json:"matrix_ids,omitempty"`
	CompositionTypeIDs []string `json:"composition_type_ids,omitempty"`
	ProductionGroupIDs []string `json:"production_group_ids,omitempty"`
	FirmIDs            []string `json:"firm_ids,omitempty"`
}

func (f *ProductionFilters) UnmarshalJSON(data []byte) error {
	type alias ProductionFilters
	var a alias
	if err := unmarshalObjectOrString(data, &a); err != nil {
		return err
	}
	*f = ProductionFilters(a)
	return nil
}

// ProductionReportRequest — тело POST /mcp/reports/production_output|production_consumption.
// Секция задана самим эндпойнтом, поэтому в теле её нет.
type ProductionReportRequest struct {
	Period        Period            `json:"period"`
	OperationType string            `json:"operation_type,omitempty"`
	Filters       ProductionFilters `json:"filters,omitempty"`
	GroupBy       []string          `json:"group_by,omitempty"`
	Measures      []string          `json:"measures,omitempty"`
	Top           int               `json:"top,omitempty"`
	Sort          []SortSpec        `json:"sort,omitempty"`
}

// ProductionDocumentRequest — тело POST /mcp/reports/production_document.
type ProductionDocumentRequest struct {
	DocumentID string `json:"document_id"`
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
