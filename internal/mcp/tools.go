package mcp

const (
	ToolResolveCustomer  = "resolve_customer"
	ToolResolveWarehouse = "resolve_warehouse"
	ToolSalesReport      = "sales_report"
)

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
			Name:        ToolSalesReport,
			Description: "Get sales report for a specified period with optional filters by customer and warehouse. Supports grouping and sorting.",
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
						"items":       map[string]any{"type": "string", "enum": []string{"customer", "warehouse"}},
						"description": "Group results by dimensions",
					},
					"measures": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"amount", "qty"}},
						"description": "Measures to include (default: amount, qty)",
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
