package mcp

const (
	ToolResolveCustomer  = "resolve_customer"
	ToolResolveWarehouse = "resolve_warehouse"
	ToolResolveProduct   = "resolve_product"
	ToolSalesReport      = "sales_report"
	ToolStockBalance     = "stock_balance"
	ToolTopProducts      = "top_products"
	ToolCustomerSummary  = "customer_summary"
	ToolResolveSalesChannel = "resolve_sales_channel"
	ToolResolveCash      = "resolve_cash"
	ToolResolveCostArticle = "resolve_cost_article"
	ToolResolveOperation = "resolve_operation"
	ToolCashBalance      = "cash_balance"
	ToolCashFlow         = "cash_flow"
)

// ScopeReportCost — measure-level scope: доступ к закупочной стоимости / прибыли / марже
// в sales_report. Не привязан к отдельному инструменту (это меры внутри sales_report),
// поэтому отсутствует в ToolScopes; проверяется при фильтрации схемы в handleToolsList,
// а финально — на стороне 1С по заголовку X-MCP-Scopes (defense in depth).
const ScopeReportCost = "mcp:report:cost"

// CostMeasures — меры sales_report, закрытые правом ScopeReportCost. Должны быть синхронны
// с белым списком мер в CommonModules/MCP (BSL) и со значениями enum в схеме sales_report.
var CostMeasures = []string{"cost", "profit", "margin"}

// ToolScopes — обязательный scope для каждого MCP-инструмента.
// Проверяется в handleToolsCall и используется для фильтрации tools/list по правам пользователя.
// При добавлении нового инструмента — обязательно прописать его сюда, иначе вызов будет отказан.
var ToolScopes = map[string]string{
	ToolResolveCustomer:  "mcp:resolve",
	ToolResolveWarehouse: "mcp:resolve",
	ToolResolveProduct:   "mcp:resolve",
	ToolSalesReport:      "mcp:report:sales",
	ToolStockBalance:     "mcp:report:stock",
	ToolTopProducts:      "mcp:report:sales",
	ToolCustomerSummary:  "mcp:report:sales",
	ToolResolveSalesChannel: "mcp:resolve",
	// Кассы / виды операций / статьи затрат используются ТОЛЬКО денежными отчётами, поэтому их
	// резолверы закрыты тем же scope mcp:report:money — иначе пользователь без доступа к деньгам
	// видел бы резолверы, ссылающиеся в описании на недоступные cash_flow/cash_balance, и мог бы
	// решить, что отчёт «есть, но спрятан». Скрываем резолверы вместе с отчётами.
	ToolResolveCash:        "mcp:report:money",
	ToolResolveCostArticle: "mcp:report:money",
	ToolResolveOperation:   "mcp:report:money",
	ToolCashBalance:        "mcp:report:money",
	ToolCashFlow:           "mcp:report:money",
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
			Description: "Search warehouses by name or code. Returns a list of matching candidates for disambiguation.",
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
			Name:        ToolResolveProduct,
			Description: "Search products by name or code (артикул). Returns a list of matching candidates for disambiguation. Pass a UUID directly to look up a known product. Set include_groups=true to also search the product catalog GROUPS (товарные группы) — UUIDs of groups can be passed to stock_balance.filters.product_ids or sales_report (via top_products) and will be applied via IN HIERARCHY (matches all products within the group).",
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
			Name:        ToolSalesReport,
			Description: "Get sales report from the «РеализацияТоваров» register for a specified period. By default groups by warehouse and customer and returns amount and qty. Filters: customer_ids (accepts both leaf customer UUIDs and customer-group UUIDs — applied via IN HIERARCHY), warehouse_ids, sales_channel_ids (accepts both leaf channel UUIDs and parent-node UUIDs like 'B2B'/'B2C' — applied via IN HIERARCHY, captures all descendants), customer_cohort ('new' | 'returning'). Dimensions (group_by): warehouse, customer, product, seller, sales_channel, day, week, month, cohort, product_group, customer_group (cohort = 'new'/'returning'; day/week/month return ISO date strings 'YYYY-MM-DD'; product_group / customer_group aggregate by parent group of the hierarchical catalog — товарная группа / группа контрагентов). Measures: amount, qty, receipts (number of sales documents), avg_check (amount / receipts), customers (COUNT DISTINCT customer), and — for users with the mcp:report:cost permission — cost (purchase cost), profit (amount - cost), margin (profit / amount, percent). customer_cohort='new'|'returning' restricts the sample (new = customer ДатаСоздания within the calendar month preceding the period start). To compare new vs returning side-by-side use group_by=['cohort'] instead of the cohort filter. Reference cells in rows come back as {id,label} objects (no extra resolve call needed). Response also includes period {from,to} and applied_filters (customers, warehouses, sales_channels, customer_cohort, new_since). Use group_by to pick dimensions, measures to pick metrics, top to limit rows, and sort to order results. sort.field must be one of the selected group_by dimensions or measures (otherwise the entry is ignored).",
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
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "customer", "product", "seller", "sales_channel", "day", "week", "month", "cohort", "product_group", "customer_group"}},
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
			Description: "Get product stock balance from the «ОстаткиТоваров» register as of a given date. By default groups by both warehouse and product and returns the qty measure. Use group_by to pick dimensions (warehouse, product, product_group), measures to pick metrics (qty, amount), top to limit rows, and sort to order (sort.field must be one of the selected group_by dimensions or measures). Use product_group to aggregate by parent group of the hierarchical product catalog (товарная группа), useful for answering questions about totals per group rather than per item. Do not combine product with product_group — the group column would be fully determined by the leaf; the server silently drops the redundant product_group in that case.",
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
							"product_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by product IDs (from resolve_product). Accepts both leaf product UUIDs and group UUIDs (resolve_product with include_groups=true) — applied as IN HIERARCHY, so passing a group matches all products within it.",
							},
							"warehouse_ids": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Filter by warehouse IDs (from resolve_warehouse)",
							},
						},
					},
					"group_by": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "product", "product_group"}},
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
	}
}
