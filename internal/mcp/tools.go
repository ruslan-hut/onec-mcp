package mcp

const (
	ToolResolveCustomer     = "resolve_customer"
	ToolResolveWarehouse    = "resolve_warehouse"
	ToolResolveProduct      = "resolve_product"
	ToolResolveMaterial     = "resolve_material"
	ToolProductDetails      = "product_details"
	ToolSalesReport         = "sales_report"
	ToolStockBalance        = "stock_balance"
	ToolAvailabilityReport  = "availability_report"
	ToolTopProducts         = "top_products"
	ToolCustomerSummary     = "customer_summary"
	ToolResolveSalesChannel = "resolve_sales_channel"
	ToolResolveFirm         = "resolve_firm"
	ToolResolveCash         = "resolve_cash"
	ToolResolveCostArticle  = "resolve_cost_article"
	ToolResolveOperation    = "resolve_operation"
	ToolCashBalance         = "cash_balance"
	ToolCashFlow            = "cash_flow"
	ToolReceivablesBalance  = "receivables_balance"
	ToolPayablesBalance     = "payables_balance"
	ToolPurchasesReport     = "purchases_report"
	ToolGoodsInTransit      = "goods_in_transit"
	ToolEventLog            = "event_log"
	ToolObjectHistory       = "object_history"
	ToolFindDocument        = "find_document"

	// Производственный блок: спецификации (нормы расхода) и документы Производство.
	ToolProductSpecification     = "product_specification"
	ToolSpecificationCost        = "specification_cost"
	ToolSpecificationExplode     = "specification_explode"
	ToolSpecificationWhereUsed   = "specification_where_used"
	ToolSpecificationVersions    = "specification_versions"
	ToolSpecificationList        = "specification_list"
	ToolProductionOutput         = "production_output"
	ToolProductionConsumption    = "production_consumption"
	ToolProductionDocumentDetail = "production_document"
)

// ScopeReportCost — доступ к себестоимости. Исторически это measure-level право (меры
// cost/profit/margin внутри sales_report), поэтому оно проверяется отдельно от ToolScopes
// при фильтрации схемы в handleToolsList. Начиная с производственного блока это ещё и
// tool-level scope: нормы расхода сырья и себестоимость выпуска — та же чувствительная
// информация, что и закупочные цены, поэтому производственные инструменты закрыты им же
// (см. ToolScopes ниже). Финально право проверяется на стороне 1С по заголовку
// X-MCP-Scopes (defense in depth).
const ScopeReportCost = "mcp:report:cost"

// CostMeasures — меры sales_report, закрытые правом ScopeReportCost. Должны быть синхронны
// с белым списком мер в CommonModules/MCP (BSL) и со значениями enum в схеме sales_report.
var CostMeasures = []string{"cost", "profit", "margin"}

// ToolScopes — обязательный scope для каждого MCP-инструмента.
// Проверяется в handleToolsCall и используется для фильтрации tools/list по правам пользователя.
// При добавлении нового инструмента — обязательно прописать его сюда, иначе вызов будет отказан.
var ToolScopes = map[string]string{
	ToolResolveCustomer:    "mcp:resolve",
	ToolResolveWarehouse:   "mcp:resolve",
	ToolResolveProduct:     "mcp:resolve",
	ToolProductDetails:     "mcp:resolve",
	ToolSalesReport:        "mcp:report:sales",
	ToolStockBalance:       "mcp:report:stock",
	ToolAvailabilityReport: "mcp:report:stock",
	// Товары в пути — те же остатки, только в отдельном регистре: право как у stock_balance.
	ToolGoodsInTransit:      "mcp:report:stock",
	ToolTopProducts:         "mcp:report:sales",
	ToolCustomerSummary:     "mcp:report:sales",
	ToolResolveSalesChannel: "mcp:resolve",
	// Фирма (юрлицо) — измерение отчётов, а не чувствительные данные сама по себе:
	// общий mcp:resolve. В многофирменных базах видимый список дополнительно урезается
	// правами учётной записи на стороне 1С.
	ToolResolveFirm: "mcp:resolve",
	// Кассы / виды операций / статьи затрат используются ТОЛЬКО денежными отчётами, поэтому их
	// резолверы закрыты тем же scope mcp:report:money — иначе пользователь без доступа к деньгам
	// видел бы резолверы, ссылающиеся в описании на недоступные cash_flow/cash_balance, и мог бы
	// решить, что отчёт «есть, но спрятан». Скрываем резолверы вместе с отчётами.
	ToolResolveCash:        "mcp:report:money",
	ToolResolveCostArticle: "mcp:report:money",
	ToolResolveOperation:   "mcp:report:money",
	ToolCashBalance:        "mcp:report:money",
	ToolCashFlow:           "mcp:report:money",
	// Взаиморасчёты (ДЗ/КЗ) — та же чувствительность, что денежные отчёты: один scope mcp:report:money.
	ToolReceivablesBalance: "mcp:report:money",
	ToolPayablesBalance:    "mcp:report:money",
	// Закупки раскрывают суммы по поставщикам — закрываем тем же money-правом (CCC-связка целиком).
	ToolPurchasesReport: "mcp:report:money",
	// Админ-инструменты: чтение журнала регистрации и резолв документов для аудита.
	// Журнал содержит PII — отдельное чувствительное право, выдаётся только доверенным аккаунтам.
	ToolEventLog:      "mcp:admin:eventlog",
	ToolObjectHistory: "mcp:admin:eventlog",
	ToolFindDocument:  "mcp:admin:eventlog",
	// Производственный блок целиком закрыт правом на себестоимость: спецификация раскрывает
	// рецептуру продукта, а отчёты по выпуску — стоимость сырья в каждом изделии.
	// resolve_material — часть того же контура: названия сырья спецификации и так раскрывают,
	// но общий mcp:resolve производственную номенклатуру видеть не должен.
	ToolResolveMaterial:          ScopeReportCost,
	ToolProductSpecification:     ScopeReportCost,
	ToolSpecificationCost:        ScopeReportCost,
	ToolSpecificationExplode:     ScopeReportCost,
	ToolSpecificationWhereUsed:   ScopeReportCost,
	ToolSpecificationVersions:    ScopeReportCost,
	ToolSpecificationList:        ScopeReportCost,
	ToolProductionOutput:         ScopeReportCost,
	ToolProductionConsumption:    ScopeReportCost,
	ToolProductionDocumentDetail: ScopeReportCost,
}

func GetTools() []Tool {
	return []Tool{
		{
			Name:        ToolResolveCustomer,
			Description: "Search customers by name, phone, or other identifying information. Returns a list of matching candidates for disambiguation. Set include_groups=true to also search the customer catalog GROUPS (folders) — UUIDs of groups can be passed to sales_report.filters.customer_ids and will be applied via IN HIERARCHY (matches all customers within the group).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (name, phone, etc.)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
					"include_groups": map[string]any{
						"type":        "boolean",
						"description": "Include catalog groups (folders) in results. Useful for filtering reports by an entire customer group rather than individual customers.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveWarehouse,
			Description: "Search warehouses by name or code. Returns a list of matching candidates for disambiguation. Each candidate carries for_production (bool): a production warehouse — where materials are written off and finished goods are received. Those are returned only to callers holding the mcp:report:cost permission; without it they do not exist as far as this tool is concerned. Their UUIDs are accepted by production_output / production_consumption / production_document and (with the same permission) by stock_balance and availability_report.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (warehouse name or code)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveMaterial,
			Description: "Search raw materials and components (сырьё, комплектующие, тара, этикетки) by name or article. This is the counterpart of resolve_product: the two split the same 1C item catalog and never overlap — resolve_product returns goods for sale, resolve_material returns items flagged as production-only, which resolve_product will never find. Returns id, label, code (артикул) and unit (the unit rates are expressed in). Feed the id into specification_where_used (which products consume this material), specification_explode, product_specification or production_consumption. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (material name or article). Pass a UUID directly to look up a known material.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
					"include_groups": map[string]any{
						"type":        "boolean",
						"description": "Include catalog groups (folders) in results.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveProduct,
			Description: "Search products by name or code (артикул). Returns a list of matching candidates for disambiguation. Pass a UUID directly to look up a known product. Set include_groups=true to also search the product catalog GROUPS (товарные группы) — UUIDs of groups can be passed to stock_balance.filters.product_ids or sales_report (via top_products) and will be applied via IN HIERARCHY (matches all products within the group). Each candidate also carries lifecycle fields: status {code,label} (new|active|phasing_out|excluded), status_changed_at (date), markets ([UA|EU|OTHER]) and eu_certification {code,label} (certified|in_process|not_required) — use them to tell an expected sales drop (product being phased out / withdrawn from a market) from an anomaly worth investigating.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (product name or code)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
					"include_groups": map[string]any{
						"type":        "boolean",
						"description": "Include catalog groups (folders) in results. Useful for filtering reports by an entire product group rather than individual products.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolProductDetails,
			Description: "Batch-fetch lifecycle/status attributes for a list of products or product GROUPS in one call — use it to enrich many SKUs at once (e.g. a whole category) instead of calling resolve_product per item. product_ids accepts leaf product UUIDs and group UUIDs (from resolve_product with include_groups=true), expanded via IN HIERARCHY; up to 500 products are returned. For each product returns id, label, code, group {id,label}, status {code,label} (new|active|phasing_out|excluded), status_changed_at (date), markets ([UA|EU|OTHER]) and eu_certification {code,label} (certified|in_process|not_required). Use it in the weekly category report to classify each SKU's sales drop as expected (being phased out / withdrawn from a market) vs an anomaly.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"product_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Product or group UUIDs (from resolve_product). Group UUIDs are expanded via IN HIERARCHY. Up to 500 products returned.",
					},
					"fields": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"label", "code", "group", "status", "status_changed_at", "markets", "eu_certification"}},
						"description": "Optional subset of fields to return (id is always included). Default: all.",
					},
				},
				"required": []string{"product_ids"},
			},
		},
		{
			Name:        ToolResolveSalesChannel,
			Description: "Search sales channels by name. The catalog is hierarchical: returns both parent nodes (e.g. 'B2B', 'B2C') and their children (e.g. 'B2B Online', 'B2B Offline'). Pass a parent node UUID into sales_report.filters.sales_channel_ids to aggregate over all descendants (filter is applied via IN HIERARCHY), or a leaf UUID for a single channel.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (channel name)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveFirm,
			Description: "Search firms (legal entities the documents are issued by — Фирмы / Организации) by name or code. Returns candidates whose UUIDs go into the firm_ids filter or let you read the firm dimension of a report. In multi-company databases an access key may be restricted to a subset of firms — this tool returns only the firms the current key is allowed to see, and reports are limited to the same set even when firm_ids is omitted.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (firm name or code)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolSalesReport,
			Description: "Get sales report from the «РеализацияТоваров» register for a specified period. By default groups by warehouse and customer and returns amount and qty. Filters: customer_ids (accepts both leaf customer UUIDs and customer-group UUIDs — applied via IN HIERARCHY), warehouse_ids, firm_ids (from resolve_firm), sales_channel_ids (accepts both leaf channel UUIDs and parent-node UUIDs like 'B2B'/'B2C' — applied via IN HIERARCHY, captures all descendants), customer_cohort ('new' | 'returning'). Dimensions (group_by): warehouse, customer, product, seller, sales_channel, firm, day, week, month, cohort, product_group, customer_group (cohort = 'new'/'returning'; day/week/month return ISO date strings 'YYYY-MM-DD'; product_group / customer_group aggregate by parent group of the hierarchical catalog — товарная группа / группа контрагентов). Measures: amount, qty, receipts (number of sales documents), avg_check (amount / receipts), customers (COUNT DISTINCT customer), and — for users with the mcp:report:cost permission — cost (purchase cost), profit (amount - cost), margin (profit / amount, percent). customer_cohort='new'|'returning' restricts the sample (new = customer ДатаСоздания within the calendar month preceding the period start). To compare new vs returning side-by-side use group_by=['cohort'] instead of the cohort filter. Reference cells in rows come back as {id,label} objects (no extra resolve call needed). Response also includes period {from,to} and applied_filters (customers, warehouses, sales_channels, customer_cohort, new_since). Use group_by to pick dimensions, measures to pick metrics, top to limit rows, and sort to order results. sort.field must be one of the selected group_by dimensions or measures (otherwise the entry is ignored).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"period": map[string]any{
						"type":        "object",
						"description": "Report period",
						"properties": map[string]any{
							"from": map[string]any{
								"type":        "string",
								"format":      "date",
								"description": "Start date (YYYY-MM-DD)",
							},
							"to": map[string]any{
								"type":        "string",
								"format":      "date",
								"description": "End date (YYYY-MM-DD)",
							},
						},
						"required": []string{"from", "to"},
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"customer_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by customer IDs (from resolve_customer). Accepts both leaf customer UUIDs and group UUIDs (resolve_customer with include_groups=true) — applied as IN HIERARCHY, so passing a group matches all customers within it.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by warehouse IDs (from resolve_warehouse)",
							},
							"sales_channel_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by sales channel IDs (from resolve_sales_channel). Applied as IN HIERARCHY — passing a parent node (e.g. 'B2B') matches all descendant channels.",
							},
							"customer_cohort": map[string]any{
								"type":        "string",
								"enum":        []string{"new", "returning"},
								"description": "Restrict to new (customer created within or after the calendar month preceding period start) or returning customers. Omit to include both.",
							},
							"product_status": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string", "enum": []string{"new", "active", "phasing_out", "excluded"}},
								"description": "Filter by product lifecycle status: new (Новинка), active (Активна), phasing_out (На виводі), excluded (Виведена). Lets you e.g. see sales only for products being phased out. Combine with resolve_product, which returns each product's current status.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "customer", "product", "seller", "sales_channel", "day", "week", "month", "cohort", "product_group", "customer_group", "firm"}},
						"description": "Group results by dimensions. day/week/month bucket by document date. cohort splits rows into 'new' vs 'returning' customers. product_group / customer_group aggregate by parent group of the hierarchical catalog. Do not combine a leaf dim with its group (customer+customer_group, product+product_group) — the group column would be fully determined by the leaf and adds no information; the server silently drops the redundant *_group in that case.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"amount", "qty", "receipts", "avg_check", "customers", "cost", "profit", "margin"}},
						"description": "Measures to include (default: amount, qty). receipts = COUNT(DISTINCT document), avg_check = amount / receipts, customers = COUNT(DISTINCT customer). cost = purchase cost (закупочная стоимость), profit = amount - cost, margin = profit / amount as a percentage. cost/profit/margin require the mcp:report:cost permission — they are only offered to authorized users (omitted from this enum otherwise); for correct margin/profit totals request them together with amount and cost.",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
				"required": []string{"period"},
			},
		},
		{
			Name:        ToolTopProducts,
			Description: "Get top-N best-selling products for a period. Thin wrapper over sales_report grouped by product and sorted by the selected metric. Use this instead of sales_report when the user asks 'top products', 'bestsellers', 'what sold most' — the tool name is a strong hint for LLM tool selection.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"period": map[string]any{
						"type":        "object",
						"description": "Report period",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
						"required": []string{"from", "to"},
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"customer_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Restrict to specific customers"},
							"warehouse_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Restrict to specific warehouses"},
						},
					},
					"by": map[string]any{
						"type":        "string",
						"enum":        []string{"amount", "qty"},
						"description": "Ranking metric (default: amount)",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Number of products to return (default: 10)",
					},
				},
				"required": []string{"period"},
			},
		},
		{
			Name:        ToolCustomerSummary,
			Description: "Get a summary card for a single customer over a period: total amount, qty, number of receipts, average check, last purchase date, and top-N most bought products. For users with the mcp:report:cost permission, totals also include cost (purchase cost), profit (amount - cost) and margin (profit / amount, percent). Replaces 3-4 sequential sales_report calls with one. Use when the user asks about a specific customer (e.g. 'how much did X buy', 'tell me about customer Y').",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{
						"type":        "string",
						"description": "Customer UUID (from resolve_customer)",
					},
					"period": map[string]any{
						"type":        "object",
						"description": "Period for the summary",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
						"required": []string{"from", "to"},
					},
					"top_products": map[string]any{
						"type":        "integer",
						"description": "How many top products to include (default: 5)",
					},
				},
				"required": []string{"customer_id", "period"},
			},
		},
		{
			Name:        ToolStockBalance,
			Description: "Get product stock balance from the «ОстаткиТоваров» register as of a given date. By default groups by both warehouse and product and returns the qty measure. Use group_by to pick dimensions (warehouse, product, product_group, firm), measures to pick metrics (qty, amount), top to limit rows, and sort to order (sort.field must be one of the selected group_by dimensions or measures). Use product_group to aggregate by parent group of the hierarchical product catalog (товарная группа), useful for answering questions about totals per group rather than per item. Do not combine product with product_group — the group column would be fully determined by the leaf; the server silently drops the redundant product_group in that case. Coverage depends on permissions: by default the report shows goods for sale on trading warehouses only. With the mcp:report:cost permission it also covers the production side — raw materials and components (see resolve_material) on production warehouses (see resolve_warehouse.for_production).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Balance date (YYYY-MM-DD). Defaults to current moment.",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"product_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by product IDs (from resolve_product; with the mcp:report:cost permission also from resolve_material). Accepts both leaf product UUIDs and group UUIDs (resolve_product with include_groups=true) — applied as IN HIERARCHY, so passing a group matches all products within it.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by warehouse IDs (from resolve_warehouse). A production warehouse id is accepted only with the mcp:report:cost permission; without it such an id is silently ignored.",
							},
							"product_status": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string", "enum": []string{"new", "active", "phasing_out", "excluded"}},
								"description": "Filter by product lifecycle status: new (Новинка), active (Активна), phasing_out (На виводі), excluded (Виведена). E.g. product_status=[\"excluded\"] returns the frozen stock still sitting on withdrawn SKUs.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "product", "product_group", "firm"}},
						"description": "Group results by dimensions",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"qty", "amount"}},
						"description": "Measures to include (default: qty)",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
			},
		},
		{
			Name:        ToolAvailabilityReport,
			Description: "Get product availability (out-of-stock days) over a period from the daily stock register. For each SKU/group × warehouse (and optionally per week) returns oos_days (days out of stock), days (calendar days in the period), availability_pct (fraction 0..1 = in-stock days / days) and avg_qty (average daily balance). Use this to tell a demand-driven sales drop from a supply problem — 'was there simply nothing to sell?'. A day counts as out of stock when the end-of-day balance is <= 0. Items that were never stocked are excluded. Caveat: a SKU that was out of stock for the ENTIRE period (it dropped to zero before the window) has no rows and won't appear — combine with resolve_product status to spot active items with zero availability. Like stock_balance, the report covers trading warehouses and goods for sale; the production side (materials on production warehouses) is included only with the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"period": map[string]any{
						"type":        "object",
						"description": "Reporting period (required)",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
						"required": []string{"from", "to"},
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"product_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by product IDs (from resolve_product; with the mcp:report:cost permission also from resolve_material). Leaf or group UUIDs — applied as IN HIERARCHY.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by warehouse IDs (from resolve_warehouse). A production warehouse id is accepted only with the mcp:report:cost permission; without it such an id is silently ignored.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"product", "product_group", "warehouse", "week", "firm"}},
						"description": "Group results by dimensions. Default: product + warehouse. Use week to bucket metrics per ISO week; product_group aggregates by the parent товарная группа.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"oos_days", "days", "availability_pct", "avg_qty"}},
						"description": "Measures to include (default: all)",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order (default: oos_days desc). sort.field must be one of the selected group_by dimensions or measures.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
				"required": []string{"period"},
			},
		},
		{
			Name:        ToolResolveCash,
			Description: "Search cash desks (кассы) by name or code. Returns matching candidates for disambiguation. Pass a UUID directly to look up a known cash desk. Use the returned id in cash_balance.filters.cash_ids or cash_flow.filters.cash_ids.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (cash desk name or code)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveCostArticle,
			Description: "Search cost articles (статьи затрат) by name or code. The catalog is hierarchical: set include_groups=true to also return groups (cost-article folders). Pass a group UUID into cash_flow.filters.cost_article_ids to aggregate over all articles within it (applied via IN HIERARCHY), or a leaf UUID for a single article. Use the returned id in cash_flow.filters.cost_article_ids.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (cost article name or code)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
					"include_groups": map[string]any{
						"type":        "boolean",
						"description": "Include catalog groups (cost-article folders) in results. Pass a group UUID to cash_flow.filters.cost_article_ids for an IN HIERARCHY filter over the whole group.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolResolveOperation,
			Description: "Search cash-flow operation types (виды движения денег — e.g. settlements with customers / suppliers / investors) by name. Use the returned id in cash_flow.filters.operation_ids, or pass it as group_by=[\"operation\"] to break cash flow down by operation type.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query (operation type name)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolCashBalance,
			Description: "Get cash-on-hand balance from the «ДеньгиВКассе» register as of a given date, broken down by cash desk (касса). Use group_by to pick dimensions (cash, firm), measures (balance), top to limit rows, and sort (sort.field must be a selected dimension or measure). Requires the mcp:report:money permission. NOTE: amounts are in each cash desk's own currency (the register has no currency dimension); the grand total simply sums them, so it is only meaningful when all selected cash desks share one currency.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Balance date (YYYY-MM-DD). Defaults to current moment.",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"cash_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by cash desk IDs (from resolve_cash)",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"cash", "firm"}},
						"description": "Group results by dimensions (default: cash). firm = owning company of the cash desk.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"balance"}},
						"description": "Measures to include (default: balance).",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
			},
		},
		{
			Name:        ToolCashFlow,
			Description: "Get cash flow (turnovers) from the «ДвижениеДенежныхСредств» register for a period. Amounts are net of the base currency only (the register stores a duplicate row in the management-accounting currency which is excluded). Measures: inflow (gross money in), outflow (gross money out, positive), net (inflow - outflow). Dimensions (group_by): account (cash desk / bank account), operation (operation type — ВидОперации), analytics (counterparty / cost article / employee / ... — composite, returned as {id,label,kind} where kind is the entity type), firm, day, week, month. Default groups by operation and returns inflow/outflow/net. Filters: cash_ids (account dimension), operation_ids (operation type), cost_article_ids and customer_ids (both filter the analytics dimension and are combined via OR). sort.field must be a selected dimension or measure. Requires the mcp:report:money permission. Use this for questions like 'how much cash came in/out', 'spending by cost article', 'cash movements by counterparty'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"period": map[string]any{
						"type":        "object",
						"description": "Report period",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
						"required": []string{"from", "to"},
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"cash_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by cash desk IDs (from resolve_cash); applied to the account dimension.",
							},
							"operation_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by operation type IDs (from resolve_operation); applied to the ВидОперации dimension.",
							},
							"cost_article_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter the analytics dimension by cost article IDs (from resolve_cost_article). Accepts both leaf and group UUIDs — applied as IN HIERARCHY. Combined with customer_ids via OR.",
							},
							"customer_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter the analytics dimension by counterparty IDs (from resolve_customer). Combined with cost_article_ids via OR.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"account", "operation", "analytics", "firm", "day", "week", "month"}},
						"description": "Group results by dimensions (default: operation). analytics is a composite dimension (counterparty / cost article / employee / ...) returned as {id,label,kind}. day/week/month bucket by movement date.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"inflow", "outflow", "net"}},
						"description": "Measures to include (default: inflow, outflow, net). inflow/outflow are gross and positive; net = inflow - outflow.",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
				"required": []string{"period"},
			},
		},
		{
			Name:        ToolReceivablesBalance,
			Description: "Get accounts-receivable balances from customers (взаиморасчёты с покупателями) from the «Взаиморасчеты» register as of a given date, broken down by customer. The balance is shown EXPANDED, not netted: receivable (ДЗ — what customers owe us) and advance (авансы полученные — prepayments we still owe goods for) are returned as separate measures, split by the sign of each customer's net balance. Note: the register has no contract/order dimension, so a receivable and an advance of the SAME customer across different deals are already netted into one figure — expansion is across customers, not within one. Dimensions (group_by): customer, firm (default: customer). Measures: receivable, advance, net (= receivable - advance; >0 means the customer is a net debtor). Filters: customer_ids (UUIDs from resolve_customer — applied via IN HIERARCHY, accepts customer-group UUIDs), firm_ids (UA/PL legal entity — use group_by=[\"firm\"] to see the split and to exclude intra-group settlements when consolidating). Requires the mcp:report:money permission. Amounts are in the base currency. sort.field must be a selected dimension or measure.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Balance date (YYYY-MM-DD). Defaults to current moment.",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"customer_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by customer IDs (from resolve_customer). Accepts both leaf and customer-group UUIDs — applied via IN HIERARCHY.",
							},
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"customer", "firm"}},
						"description": "Group results by dimensions (default: customer). firm = owning legal entity (UA/PL).",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"receivable", "advance", "net"}},
						"description": "Measures to include (default: receivable, advance, net). receivable = ДЗ (customers owe us), advance = prepayments received (we owe goods), net = receivable - advance.",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
			},
		},
		{
			Name:        ToolPayablesBalance,
			Description: "Get accounts-payable balances to suppliers (расчёты с поставщиками) from the «Взаиморасчеты» register as of a given date, broken down by supplier. Suppliers live in the same counterparty catalog as customers, so supplier UUIDs are resolved via resolve_customer. The balance is shown EXPANDED, not netted: payable (КЗ — what we owe suppliers) and advance (авансы выданные — prepayments we made that suppliers still owe goods for) are returned as separate measures, split by the sign of each supplier's net balance. Note: the register has no contract/order dimension, so a payable and an advance of the SAME supplier across different deals are already netted into one figure — expansion is across suppliers, not within one. Dimensions (group_by): supplier, firm (default: supplier). Measures: payable, advance, net (= payable - advance; >0 means we are a net debtor to the supplier). Filters: supplier_ids (UUIDs from resolve_customer — applied via IN HIERARCHY), firm_ids (UA/PL legal entity — use group_by=[\"firm\"] to see the split and exclude intra-group settlements when consolidating). Requires the mcp:report:money permission. Amounts are in the base currency. sort.field must be a selected dimension or measure.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Balance date (YYYY-MM-DD). Defaults to current moment.",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"supplier_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by supplier IDs (from resolve_customer — suppliers share the counterparty catalog). Accepts both leaf and group UUIDs — applied via IN HIERARCHY.",
							},
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"supplier", "firm"}},
						"description": "Group results by dimensions (default: supplier). firm = owning legal entity (UA/PL).",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"payable", "advance", "net"}},
						"description": "Measures to include (default: payable, advance, net). payable = КЗ (we owe suppliers), advance = prepayments issued (suppliers owe goods), net = payable - advance.",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
			},
		},
		{
			Name:        ToolPurchasesReport,
			Description: "Get goods-purchase turnover (обороты поступления ТМЦ) from «ПриходнаяНакладная» documents for a period — by supplier, product, warehouse or time bucket. Amounts are NET of returns (ВидОперации=Возврат is subtracted) and include VAT — the correct purchases base for a DPO denominator. Only posted documents are counted. CURRENCY: line amounts are stored in the document currency, so `amount` is converted to the base currency at the document's rate; `amount_currency` keeps the raw document-currency figure and is only meaningful together with group_by=[\"currency\"]. IN TRANSIT: an invoice marked «в пути» posts neither stock nor a payable (the goods have not arrived), so by default such documents are EXCLUDED — pass in_transit=true for exactly those, or in_transit=\"any\" for everything; use goods_in_transit for the stock still on its way. Dimensions (group_by): supplier, firm, warehouse, product, product_group, currency, in_transit, day, week, month, delivery_date (default: supplier, month; day/week/month/delivery_date return ISO date strings). Measures: amount (base currency, incl. VAT), amount_currency, amount_without_vat, qty, documents (count of invoices) — default: amount. Filters: supplier_ids (UUIDs from resolve_customer — suppliers share the counterparty catalog; applied via IN HIERARCHY), firm_ids (UA/PL legal entity), product_ids (IN HIERARCHY, accepts group UUIDs), warehouse_ids. Requires the mcp:report:money permission; without mcp:report:cost the report covers goods for sale only — purchases of raw materials are excluded. sort.field must be a selected dimension or measure.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"period": map[string]any{
						"type":        "object",
						"description": "Report period",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
						"required": []string{"from", "to"},
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"supplier_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by supplier IDs (from resolve_customer). Accepts both leaf and group UUIDs — applied via IN HIERARCHY.",
							},
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"product_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by product IDs (from resolve_product; with the mcp:report:cost permission also from resolve_material). Leaf or group UUIDs — applied as IN HIERARCHY.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by receiving warehouse IDs (from resolve_warehouse).",
							},
						},
					},
					"in_transit": map[string]any{
						"description": "Which invoices to count: false (default) — only goods that actually arrived; true — only the ones still «в пути»; \"any\" — both. An in-transit invoice posts no stock movement and no payable, which is why it is excluded by default.",
						"anyOf": []map[string]any{
							{"type": "boolean"},
							{"type": "string", "enum": []string{"any"}},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"supplier", "firm", "warehouse", "product", "product_group", "currency", "in_transit", "day", "week", "month", "delivery_date"}},
						"description": "Group results by dimensions (default: supplier, month). day/week/month bucket by document date; delivery_date buckets by the expected delivery date (ДатаПоставки) — the useful one together with in_transit. Do not combine product with product_group; the redundant one is dropped.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"amount", "amount_currency", "amount_without_vat", "qty", "documents"}},
						"description": "Measures to include (default: amount). amount = purchase sum incl. VAT in the BASE currency (converted at the document's rate), net of returns; amount_currency = the same sum left in the document currency — add group_by=[\"currency\"] or the figure mixes currencies; amount_without_vat = base currency, VAT excluded; qty = quantity, net of returns; documents = number of distinct invoices (not signed by returns).",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
				"required": []string{"period"},
			},
		},
		{
			Name:        ToolGoodsInTransit,
			Description: "Stock that is IN TRANSIT as of a date — paid for or ordered, already booked to the firm, but not yet accepted at the warehouse. It lives in a separate 1C register («ОстаткиТоваровВПути»), which is why none of it shows up in stock_balance: a purchase invoice flagged «в пути» posts here instead, and the same document moves the goods into the normal stock register once it is re-posted as arrived. Use it to answer 'what is on its way and when does it land', 'do we need to reorder or is it already coming', and to reconcile a stockout against incoming supply. Dimensions (group_by): warehouse (the destination), product, product_group, firm, status (how far along the delivery is), supplier, document (the source invoice) and delivery_date — the EXPECTED ARRIVAL date (ДатаПоставки from the invoice header; empty means no date was set, not 'no delivery'). Default: warehouse + product. Measures: qty, amount (base currency), amount_in_currency (cost-accounting currency) — default qty + amount; both come pre-converted from the register, no rate juggling. Rows whose balance nets to zero are omitted — those deliveries already arrived. Requires the mcp:report:stock permission; without mcp:report:cost, materials and production warehouses are excluded as everywhere else.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Balance date (YYYY-MM-DD). Defaults to the current moment.",
					},
					"filters": map[string]any{
						"type":        "object",
						"description": "Optional filters",
						"properties": map[string]any{
							"product_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by product IDs (from resolve_product; with the mcp:report:cost permission also from resolve_material). Leaf or group UUIDs — applied as IN HIERARCHY.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by destination warehouse IDs (from resolve_warehouse).",
							},
							"firm_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by firm (legal entity) IDs from resolve_firm. In multi-company databases the key may be limited to a subset of firms; omitting this filter means all firms the key is allowed to see.",
							},
							"status_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by document status IDs. Take them from a call with group_by=[\"status\"] — there is no status resolver.",
							},
							"supplier_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by supplier IDs (from resolve_customer — suppliers share the counterparty catalog). Applied via IN HIERARCHY to the source invoice's counterparty.",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "product", "product_group", "firm", "status", "supplier", "document", "delivery_date"}},
						"description": "Group results by dimensions (default: warehouse, product). delivery_date is the expected arrival date (ДатаПоставки of the source invoice), bucketed by day — group by it to get a delivery calendar. Do not combine product with product_group; the redundant one is dropped.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"qty", "amount", "amount_in_currency"}},
						"description": "Measures to include (default: qty, amount). amount is in the base currency, amount_in_currency in the cost-accounting currency — both are stored already converted.",
					},
					"top": map[string]any{
						"type":        "integer",
						"description": "Limit number of rows returned",
					},
					"sort": map[string]any{
						"type":        "array",
						"description": "Sort order (sort.field must be a selected dimension or measure)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"field": map[string]any{"type": "string"},
								"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
							},
						},
					},
				},
			},
		},
		{
			Name:        ToolEventLog,
			Description: "Read the 1C event log (журнал регистрации). List events for a period filtered by severity and/or technical event type, and optionally by user or session — all filters are independent and optional, and the period defaults to the current day. Common questions: 'errors today' → level=[\"error\"]; 'all postings today' → events=[\"_$Data$_.Post\"]; 'logins today' → events=[\"_$Session$_.Start\"]; 'what did user X do' → user=\"X\". To reconstruct what led to an error: first call with level=[\"error\"] (and user, if known) to locate the failure — each event carries its session number and timestamp — then call again with that session number and no level filter to get the full chronological trace of that session up to the error. Events come back in chronological order (oldest first) with date, level, user, user_id (the author's IB-user UUID), event (technical name like _$Data$_.Post), event_presentation, comment, metadata, object, session, transaction_status, computer. For the audit trail of one specific document or catalog item, use object_history instead. NOTE on attribution: each event belongs to the user who AUTHORED it in the log; background/scheduled jobs are recorded under a service user, not a document's 'responsible' person — so a user= filter can legitimately return nothing for changes actually made by a background process (use object_history, or a session filter, to see those). Requires the mcp:admin:eventlog permission (the log contains PII).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"error", "warning", "information", "note"}},
						"description": "Filter by severity. Omit for all levels. Use [\"error\"] for 'list errors', [\"error\",\"warning\"] for problems.",
					},
					"events": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by technical event names, e.g. _$Data$_.Post (posting), _$Data$_.Update, _$Data$_.New, _$Data$_.Delete, _$Session$_.Start (login). Use this for 'what was posted/created/deleted/logged in'.",
					},
					"user": map[string]any{
						"type":        "string",
						"description": "Optional. Substring of the user's login or full name — resolved to the matching infobase user(s), then events are filtered strictly by those users' IB-user UUID (the id the log stores as the event author). Resolved users are echoed in matched_users; every event also returns user_id. On a busy day a user's events may be sparse — if the result has a 'note', narrow the period with a time window.",
					},
					"session": map[string]any{
						"type":        "integer",
						"description": "Optional. Session number — pull the full action trace of one session (e.g. the session in which an error occurred).",
					},
					"period": map[string]any{
						"type":        "object",
						"description": "Time window (defaults to the current day if omitted). IMPORTANT for performance: on a busy base the log is huge and a whole-day scan can TIME OUT — if you know the approximate time (e.g. from object_history), pass a narrow time window. from/to accept a plain date OR a date-time. If the result is capped by limit, the earliest events in the window are returned.",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "description": "Start: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS"},
							"to":   map[string]any{"type": "string", "description": "End: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS (a date with no time = end of that day)"},
						},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of events to return (default 100, max 500).",
					},
				},
			},
		},
		{
			Name:        ToolObjectHistory,
			Description: "Read the 1C event log (журнал регистрации) for a specific OBJECT or object TYPE — who created, changed, posted, unposted or deleted it, and when. Pass object_type plus object_id (UUID) to audit one specific object, or object_type alone for all events of that type in the period. object_type is the full metadata name: for catalog items use 'Catalog.<Name>' (e.g. Catalog.Контрагенты) and get the UUID from resolve_customer/resolve_product/resolve_warehouse; for documents use 'Document.<Name>' (e.g. Document.ДокументОтгрузки) and get the UUID from find_document (by type+number+date). Returns events (chronological) with date, user, event/event_presentation (Создание/Изменение/Проведение/Отмена проведения/Удаление), comment, session. Requires the mcp:admin:eventlog permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"object_type": map[string]any{
						"type":        "string",
						"description": "Full metadata name: 'Document.<Name>' or 'Catalog.<Name>' (e.g. Document.ДокументОтгрузки, Catalog.Контрагенты).",
					},
					"object_id": map[string]any{
						"type":        "string",
						"description": "UUID of the specific object (from find_document for documents, or resolve_* for catalog items). Omit to get events for ALL objects of object_type in the period.",
					},
					"events": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional technical event names to narrow to, e.g. _$Data$_.Post (posting), _$Data$_.Update, _$Data$_.New, _$Data$_.Delete.",
					},
					"period": map[string]any{
						"type":        "object",
						"description": "Time window (defaults to the current day if omitted). from/to accept a plain date (YYYY-MM-DD) or a date-time (YYYY-MM-DDTHH:MM:SS); a date with no time as 'to' means end of that day.",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "description": "Start: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS"},
							"to":   map[string]any{"type": "string", "description": "End: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS"},
						},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of events to return (default 100, max 500).",
					},
				},
				"required": []string{"object_type"},
			},
		},
		{
			Name:        ToolFindDocument,
			Description: "Find a 1C document by type, number and/or date — returns matching candidates with their UUID (id) so you can audit them with object_history. doc_type is the document metadata name, e.g. 'ДокументОтгрузки' (the 'Document.' prefix is optional). You must provide at least 'number' (substring match) or 'period' (search window). Returns candidates with id, object_type, number, date, posted, deletion_mark, presentation. Requires the mcp:admin:eventlog permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"doc_type": map[string]any{
						"type":        "string",
						"description": "Document metadata name, e.g. ДокументОтгрузки, ПриходнаяНакладная, РозничныйЧек, ЗаказПокупателя, РасходнаяНакладная. 'Document.' prefix optional.",
					},
					"number": map[string]any{
						"type":        "string",
						"description": "Document number or a substring of it.",
					},
					"period": map[string]any{
						"type":        "object",
						"description": "Date window to search within",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum candidates to return (default 20, max 100).",
					},
				},
				"required": []string{"doc_type"},
			},
		},
		{
			Name: ToolProductSpecification,
			Description: "Bill of materials (спецификация) for a product as of a date: which materials go into it and at what rate. " +
				"Rates come from the «MaterialSpecification» register, read as a slice at `date`, so you can ask what the composition looked like at any past moment. " +
				"A composition is identified by FOUR keys: product + matrix (матрица) + composition_type (тип состава) + production_group (группировка производства). " +
				"If you omit those three modifiers, every variant of the product's composition is returned — each row carries them as columns, so check them before summing anything. " +
				"Columns: product, material, article, unit, qty_per_unit (rate per one unit of product), qty_total (rate × qty), is_main_raw (основное сырьё), matrix, composition_type, production_group, spec_date, spec_document. " +
				"Use specification_cost for the same data valued at material prices. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"product_id": map[string]any{
						"type":        "string",
						"description": "Product UUID (from resolve_product). Either product_id or product_ids is required.",
					},
					"product_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Several product UUIDs at once. Leaf products only — group UUIDs will not match (a composition is defined per item).",
					},
					"date": map[string]any{
						"type":        "string",
						"format":      "date",
						"description": "Read the composition as of this date (YYYY-MM-DD). Defaults to now.",
					},
					"qty": map[string]any{
						"type":        "number",
						"description": "Quantity of product to compute qty_total for (default: 1). May be fractional.",
					},
					"matrix_id": map[string]any{
						"type":        "string",
						"description": "Narrow to one matrix (матрица) variant of the composition.",
					},
					"composition_type_id": map[string]any{
						"type":        "string",
						"description": "Narrow to one composition type (тип состава).",
					},
					"production_group_id": map[string]any{
						"type":        "string",
						"description": "Narrow to one production group (группировка производства).",
					},
				},
			},
		},
		{
			Name: ToolSpecificationCost,
			Description: "Planned material cost of a product per its bill of materials: the same rows as product_specification plus `price` (material price as of the date) and `amount` (qty_total × price), with an `amount` total. " +
				"This is the PLANNED cost from the composition, not the actual cost of a production run — for actuals use production_document (FIFO cost of materials actually written off). " +
				"Prices come from the «ЦеныТоваров» register; price_type defaults to «ЦенаЗакупки» (purchase price). " +
				"Note that only materials are costed: the configuration keeps no labour or overhead in production, so this is a materials-only cost. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"product_id":  map[string]any{"type": "string", "description": "Product UUID (from resolve_product). Either product_id or product_ids is required."},
					"product_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Several product UUIDs at once."},
					"date":        map[string]any{"type": "string", "format": "date", "description": "Composition and prices as of this date (YYYY-MM-DD). Defaults to now."},
					"qty":         map[string]any{"type": "number", "description": "Quantity of product to cost (default: 1)."},
					"price_type_id": map[string]any{
						"type":        "string",
						"description": "Price type UUID to value materials with. Defaults to «ЦенаЗакупки» (purchase price) — the same type the production document itself uses.",
					},
					"matrix_id":           map[string]any{"type": "string", "description": "Narrow to one matrix variant."},
					"composition_type_id": map[string]any{"type": "string", "description": "Narrow to one composition type."},
					"production_group_id": map[string]any{"type": "string", "description": "Narrow to one production group."},
				},
			},
		},
		{
			Name: ToolSpecificationExplode,
			Description: "Multi-level explosion (разузлование) of a bill of materials: any material that has its own composition is expanded further, down to raw materials. " +
				"Answers 'what raw materials does making N of this actually consume', which product_specification cannot — it only shows the first level, including semi-finished items. " +
				"Rows are flat with `level` (1 = direct materials) and `path` (chain from the top product), plus `has_spec` telling whether a material is itself further expandable. " +
				"The matrix / composition_type / production_group modifiers apply to the TOP level only — a semi-finished item may have its composition registered under a different matrix, and filtering deeper levels would silently truncate the tree. " +
				"Guards: recursion stops at max_depth, a material already seen in the same branch is not expanded again (cycle), and the result is capped at 2000 rows — check the `truncated` flag. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"product_id": map[string]any{"type": "string", "description": "Product UUID to explode (from resolve_product). Required."},
					"date":       map[string]any{"type": "string", "format": "date", "description": "Compositions as of this date (YYYY-MM-DD). Defaults to now."},
					"qty":        map[string]any{"type": "number", "description": "Quantity of the top product (default: 1). qty_total on every level is scaled by it."},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "How many levels to expand (default 3, max 10). Rows with has_spec=true at the deepest level mean the tree continues below max_depth.",
					},
					"with_cost": map[string]any{
						"type":        "boolean",
						"description": "Add price/amount columns valued at price_type_id (default «ЦенаЗакупки»).",
					},
					"price_type_id":       map[string]any{"type": "string", "description": "Price type UUID for with_cost. Defaults to «ЦенаЗакупки»."},
					"matrix_id":           map[string]any{"type": "string", "description": "Narrow the TOP level to one matrix variant."},
					"composition_type_id": map[string]any{"type": "string", "description": "Narrow the TOP level to one composition type."},
					"production_group_id": map[string]any{"type": "string", "description": "Narrow the TOP level to one production group."},
				},
				"required": []string{"product_id"},
			},
		},
		{
			Name: ToolSpecificationWhereUsed,
			Description: "Reverse explosion: which products contain a given material, and at what rate per unit. " +
				"Use it for impact questions — 'this raw material got more expensive / is out of stock, which products are affected'. " +
				"Only CURRENT compositions are returned: a material dropped from a composition by a newer «СпецификацияМатериалов» document does not show up, even though its old record still lives in the register slice. " +
				"Columns: material, product, qty_per_unit, matrix, composition_type, production_group, spec_date, spec_document. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"material_id": map[string]any{
						"type":        "string",
						"description": "Material UUID (from resolve_material). Either material_id or material_ids is required.",
					},
					"material_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Several material UUIDs at once."},
					"date":                map[string]any{"type": "string", "format": "date", "description": "Compositions as of this date (YYYY-MM-DD). Defaults to now."},
					"matrix_id":           map[string]any{"type": "string", "description": "Narrow to one matrix variant."},
					"composition_type_id": map[string]any{"type": "string", "description": "Narrow to one composition type."},
					"production_group_id": map[string]any{"type": "string", "description": "Narrow to one production group."},
					"limit":               map[string]any{"type": "integer", "description": "Maximum rows to return (default 100, max 500)."},
				},
			},
		},
		{
			Name: ToolSpecificationVersions,
			Description: "Change history of a product's bill of materials: one version per «СпецификацияМатериалов» document that changed it, newest first, each with its full material list and a diff against the previous version. " +
				"Answers 'when and how did the recipe change' — e.g. to explain a jump in material cost. " +
				"The diff is computed against the previous version OF THE SAME variant (matrix + composition_type + production_group): variants live in parallel and comparing across them is meaningless. " +
				"Unlike the other tools here, the result is not a columns/rows table but {product, total_versions, versions:[{date, document, matrix, composition_type, production_group, materials[], changes:{added,removed,changed}, is_first}]}. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"product_id": map[string]any{"type": "string", "description": "Product UUID (from resolve_product). Required."},
					"period": map[string]any{
						"type":        "object",
						"description": "Optional window to limit history to. Omit for the whole history.",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
					},
					"matrix_id":           map[string]any{"type": "string", "description": "Narrow to one matrix variant."},
					"composition_type_id": map[string]any{"type": "string", "description": "Narrow to one composition type."},
					"production_group_id": map[string]any{"type": "string", "description": "Narrow to one production group."},
					"limit":               map[string]any{"type": "integer", "description": "Maximum versions to return, newest first (default 10, max 50)."},
				},
				"required": []string{"product_id"},
			},
		},
		{
			Name: ToolSpecificationList,
			Description: "Inventory of bills of materials. Default mode lists products that HAVE a composition — one row per variant: product, matrix, composition_type, production_group, materials_count, spec_date, spec_document (plus total_variants and a truncated flag). " +
				"With missing_only=true it flips around and lists products that were PRODUCED in `period` but have NO composition as of the date — the usual cause of the «Не задан состав для продукции» error when filling materials in a production document. " +
				"In that mode columns are product, produced_qty, documents, last_production_date, and only assembly operations are counted. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"missing_only": map[string]any{
						"type":        "boolean",
						"description": "List products produced in `period` that have no composition, instead of listing existing compositions.",
					},
					"period": map[string]any{
						"type":        "object",
						"description": "Production period — used by missing_only mode (defaults to today, so pass it explicitly).",
						"properties": map[string]any{
							"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
							"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
						},
					},
					"date":                map[string]any{"type": "string", "format": "date", "description": "Compositions as of this date (YYYY-MM-DD). Defaults to now."},
					"product_ids":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Narrow to these products."},
					"matrix_id":           map[string]any{"type": "string", "description": "Narrow to one matrix variant."},
					"composition_type_id": map[string]any{"type": "string", "description": "Narrow to one composition type."},
					"production_group_id": map[string]any{"type": "string", "description": "Narrow to one production group."},
					"limit":               map[string]any{"type": "integer", "description": "Maximum rows (default 200, max 500; 100 in missing_only mode)."},
				},
			},
		},
		{
			Name: ToolProductionOutput,
			Description: "Production OUTPUT turnover from posted «Производство» documents for a period — what was manufactured. Reads the Продукция table of the document. " +
				"By default only ASSEMBLY (сборка) operations are counted: disassembly is the mirror operation, where the Продукция table holds what was taken apart, and summing both into one 'produced' figure is wrong. Pass operation_type to change that; the effective value is echoed in applied_filters. " +
				"Dimensions (group_by): product, product_group, warehouse (склад продукции), employee, matrix, composition_type, production_group, firm, operation, document, day, week, month (default: product, month). " +
				"Measures: qty, amount (incl. VAT), amount_novat, documents, plus plan/fact — qty_plan, raw_qty_plan, qty_variance (fact − plan). Plan fields are not always filled in, so a zero plan means 'not planned', not 'zero output'. " +
				"Amounts here are the sums entered in the document; for the actual FIFO cost of a run use production_document. Requires the mcp:report:cost permission. sort.field must be a selected dimension or measure.",
			InputSchema: productionSchema(true),
		},
		{
			Name: ToolProductionConsumption,
			Description: "Material CONSUMPTION from posted «Производство» documents for a period — what was written off. Reads the Материалы table of the document. " +
				"This table carries both the material and the product it was consumed for, so `material` and `product` can be combined in group_by to get cost of materials per manufactured item. " +
				"By default only ASSEMBLY (сборка) operations are counted (in disassembly this table is what comes OUT, not what is consumed); pass operation_type to change that. " +
				"Dimensions (group_by): material, material_group, product (the item it went into), product_group, warehouse (склад материалов), matrix, composition_type, production_group, firm, operation, document, day, week, month (default: material, month). " +
				"Measures: qty, amount (incl. VAT), amount_novat, documents. " +
				"Amounts are the sums entered in the document, NOT the FIFO cost the batch accounting actually wrote off — for that use production_document. Requires the mcp:report:cost permission.",
			InputSchema: productionSchema(false),
		},
		{
			Name: ToolProductionDocumentDetail,
			Description: "Full detail of one «Производство» document: header, both tables and its actual register movements. " +
				"Use it to explain a single production run, or to compare planned and actual cost. Get the document UUID from production_output/production_consumption with group_by=[\"document\"] (or from find_document). " +
				"Returns {document, products[], materials[], movements[], summary}. movements are the real «ОстаткиТоваров» postings (direction expense/receipt, warehouse, product, batch, qty, amount). " +
				"summary.cost_written_off is the ACTUAL FIFO cost of the materials consumed (computed by batch accounting), while summary.cost_received is what was capitalised for the output; summary.difference is the gap between them, and output_amount / materials_amount are the sums as typed into the document. " +
				"Note: the configuration has no waste accounting (the Отходы table carries no fields) and no labour or overhead in production. Requires the mcp:report:cost permission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{
						"type":        "string",
						"description": "UUID of the «Производство» document.",
					},
				},
				"required": []string{"document_id"},
			},
		},
	}
}

// productionSchema строит схему production_output / production_consumption. Схемы отличаются
// только набором измерений и мер: сотрудник и план живут в ТЧ Продукция, а связка
// материал→изделие — в ТЧ Материалы. Общая часть (период, вид операции, фильтры, top, sort)
// одинакова, поэтому собирается здесь, а не дублируется двумя литералами.
func productionSchema(output bool) map[string]any {
	dims := []string{"product", "product_group", "warehouse", "matrix", "composition_type",
		"production_group", "firm", "operation", "document", "day", "week", "month"}
	measures := []string{"qty", "amount", "amount_novat", "documents"}

	if output {
		dims = append(dims, "employee")
		measures = append(measures, "qty_plan", "raw_qty_plan", "qty_variance")
	} else {
		dims = append(dims, "material", "material_group")
	}

	filters := map[string]any{
		"product_ids": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
			"description": "Manufactured item UUIDs (from resolve_product); accepts group UUIDs — applied via IN HIERARCHY. " +
				"In production_consumption this filters the product a material was consumed FOR, not the material itself.",
		},
		"warehouse_ids": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Warehouse UUIDs (from resolve_warehouse): склад продукции for output, склад материалов for consumption.",
		},
		"matrix_ids":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Matrix (матрица) UUIDs, taken from a prior call with group_by=[\"matrix\"] — there is no separate resolver."},
		"composition_type_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Composition type UUIDs, taken from a prior call with group_by=[\"composition_type\"]."},
		"production_group_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Production group UUIDs, taken from a prior call with group_by=[\"production_group\"]."},
		"firm_ids":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Firm (legal entity) UUIDs from resolve_firm. Omitting the filter means all firms the key is allowed to see."},
	}

	if output {
		filters["employee_ids"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Employee UUIDs, taken from a prior call with group_by=[\"employee\"] — there is no employee resolver.",
		}
	} else {
		filters["material_ids"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Material UUIDs (from resolve_material); accepts group UUIDs — applied via IN HIERARCHY.",
		}
	}

	defaultDim := "product"
	if !output {
		defaultDim = "material"
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"period": map[string]any{
				"type":        "object",
				"description": "Report period",
				"properties": map[string]any{
					"from": map[string]any{"type": "string", "format": "date", "description": "Start date (YYYY-MM-DD)"},
					"to":   map[string]any{"type": "string", "format": "date", "description": "End date (YYYY-MM-DD)"},
				},
				"required": []string{"from", "to"},
			},
			"operation_type": map[string]any{
				"type":        "string",
				"enum":        []string{"assembly", "disassembly", "all"},
				"description": "Which production operations to count (default: assembly). 'all' mixes assembly and disassembly — group_by ['operation'] to keep them apart.",
			},
			"filters": map[string]any{
				"type":        "object",
				"description": "Optional filters",
				"properties":  filters,
			},
			"group_by": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": dims},
				"description": "Group results by dimensions (default: " + defaultDim + ", month). day/week/month return ISO date strings; operation returns 'assembly'/'disassembly'.",
			},
			"measures": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": measures},
				"description": "Measures to include (default: qty, amount).",
			},
			"top": map[string]any{"type": "integer", "description": "Limit number of rows returned"},
			"sort": map[string]any{
				"type":        "array",
				"description": "Sort order",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"field": map[string]any{"type": "string"},
						"dir":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
					},
				},
			},
		},
		"required": []string{"period"},
	}
}
