package mcp

const (
	ToolResolveCustomer  = "resolve_customer"
	ToolResolveWarehouse = "resolve_warehouse"
	ToolResolveProduct   = "resolve_product"
	ToolSalesReport      = "sales_report"
	ToolStockBalance     = "stock_balance"
)

// ToolScopes — обязательный scope для каждого MCP-инструмента.
// Проверяется в handleToolsCall и используется для фильтрации tools/list по правам пользователя.
// При добавлении нового инструмента — обязательно прописать его сюда, иначе вызов будет отказан.
var ToolScopes = map[string]string{
	ToolResolveCustomer:  "mcp:resolve",
	ToolResolveWarehouse: "mcp:resolve",
	ToolResolveProduct:   "mcp:resolve",
	ToolSalesReport:      "mcp:report:sales",
	ToolStockBalance:     "mcp:report:stock",
}

func GetTools() []Tool {
	return []Tool{
		{
			Name:        ToolResolveCustomer,
			Description: "Search customers by name, phone, or other identifying information. Returns a list of matching candidates for disambiguation.",
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
			Description: "Search products by name or code (артикул). Returns a list of matching candidates for disambiguation. Pass a UUID directly to look up a known product.",
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
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        ToolSalesReport,
			Description: "Get sales report from the «РеализацияТоваров» register for a specified period. By default groups by warehouse and customer and returns amount and qty. Dimensions: warehouse, customer, product, day, month (day/month return ISO date strings 'YYYY-MM-DD'). Measures: amount, qty, receipts (number of sales documents), avg_check (amount / receipts). Use group_by to pick dimensions, measures to pick metrics, top to limit rows, and sort to order results. sort.field must be one of the selected group_by dimensions or measures (otherwise the entry is ignored).",
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
								"description": "Filter by customer IDs (from resolve_customer)",
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
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "customer", "product", "day", "month"}},
						"description": "Group results by dimensions. day/month bucket by document date.",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"amount", "qty", "receipts", "avg_check"}},
						"description": "Measures to include (default: amount, qty). receipts = COUNT(DISTINCT document), avg_check = amount / receipts.",
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
			Name:        ToolStockBalance,
			Description: "Get product stock balance from the «ОстаткиТоваров» register as of a given date. By default groups by both warehouse and product and returns the qty measure. Use group_by to pick dimensions (warehouse, product), measures to pick metrics (qty, amount), top to limit rows, and sort to order (sort.field must be one of the selected group_by dimensions or measures).",
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
								"description": "Filter by product IDs (from resolve_product)",
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
						"items":       map[string]any{"type": "string", "enum": []string{"warehouse", "product"}},
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
	}
}
