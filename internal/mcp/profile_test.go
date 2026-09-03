package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"example.com/mcp-sales-mvp/internal/onec"
)

// capsFromJSON собирает профиль так же, как он приходит из 1С, — через JSON, а не через
// литерал структуры: тест заодно проверяет, что теги разбора совпадают с форматом 1С.
func capsFromJSON(t *testing.T, payload string) *onec.Capabilities {
	t.Helper()

	var caps onec.Capabilities
	if err := json.Unmarshal([]byte(payload), &caps); err != nil {
		t.Fatalf("failed to parse capabilities: %v", err)
	}

	return &caps
}

func findTool(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}

	t.Fatalf("tool %s not found", name)
	return Tool{}
}

func enumOf(t *testing.T, tool Tool, field string) []string {
	t.Helper()

	props := schemaProperties(tool)
	if props == nil {
		t.Fatalf("tool %s has no properties", tool.Name)
	}

	holder := enumHolder(props, field)
	if holder == nil {
		t.Fatalf("tool %s has no field %s", tool.Name, field)
	}

	values, ok := holder["enum"].([]string)
	if !ok {
		t.Fatalf("tool %s field %s has no enum", tool.Name, field)
	}

	return values
}

func hasValue(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func hasFilter(t *testing.T, tool Tool, name string) bool {
	t.Helper()

	props := schemaProperties(tool)
	filters, ok := props["filters"].(map[string]any)
	if !ok {
		t.Fatalf("tool %s has no filters", tool.Name)
	}

	filterProps, ok := filters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool %s filters have no properties", tool.Name)
	}

	_, found := filterProps[name]
	return found
}

// uppProfile — профиль, который отдаёт УПП 1.3 (сокращённый до проверяемых граней).
const uppProfile = `{
	"profile": "upp-1.3",
	"version": 1,
	"unsupported": {
		"cash_flow": {"filters": ["cost_article_ids"]},
		"stock_balance": {"filters": ["firm_ids", "product_status"], "group_by": ["firm"]},
		"specification_explode": {"params": ["matrix_id", "composition_type_id", "production_group_id"]}
	},
	"extra": {
		"production_consumption": {"group_by": ["cost_article"]}
	},
	"tools": {"unavailable": ["availability_report", "goods_in_transit"]},
	"resolvers": {"always_empty": ["material"]}
}`

func TestApplyProfileStripsUnsupportedFacets(t *testing.T) {
	tools := applyProfile(GetTools(), capsFromJSON(t, uppProfile))

	if hasFilter(t, findTool(t, tools, ToolCashFlow), "cost_article_ids") {
		t.Error("cash_flow still offers cost_article_ids, which this database rejects with 400")
	}

	stock := findTool(t, tools, ToolStockBalance)

	if hasFilter(t, stock, "firm_ids") {
		t.Error("stock_balance still offers firm_ids")
	}

	if hasFilter(t, stock, "product_status") {
		t.Error("stock_balance still offers product_status")
	}

	if groups := enumOf(t, stock, "group_by"); hasValue(groups, "firm") {
		t.Errorf("stock_balance group_by still offers firm: %v", groups)
	}

	// Соседние грани не должны пострадать: вырезаем точечно, а не «всё про фирмы».
	if !hasFilter(t, stock, "warehouse_ids") {
		t.Error("stock_balance lost warehouse_ids, which is supported")
	}

	if !hasFilter(t, findTool(t, tools, ToolCashFlow), "operation_ids") {
		t.Error("cash_flow lost operation_ids, which is the supported alternative")
	}
}

func TestApplyProfileStripsParams(t *testing.T) {
	explode := findTool(t, applyProfile(GetTools(), capsFromJSON(t, uppProfile)), ToolSpecificationExplode)

	props := schemaProperties(explode)

	for _, name := range []string{"matrix_id", "composition_type_id", "production_group_id"} {
		if _, found := props[name]; found {
			t.Errorf("specification_explode still offers %s", name)
		}
	}

	if _, found := props["product_id"]; !found {
		t.Error("specification_explode lost product_id, which is required")
	}
}

// Параметр, вырезанный из properties, обязан исчезнуть и из required, иначе схема
// становится невыполнимой: модель не может передать то, чего в ней нет.
func TestApplyProfileKeepsRequiredConsistent(t *testing.T) {
	profile := `{"version": 1, "unsupported": {"specification_explode": {"params": ["product_id"]}}}`

	explode := findTool(t, applyProfile(GetTools(), capsFromJSON(t, profile)), ToolSpecificationExplode)

	schema, ok := explode.InputSchema.(map[string]any)
	if !ok {
		t.Fatal("specification_explode has no schema")
	}

	required, _ := schema["required"].([]string)
	if hasValue(required, "product_id") {
		t.Errorf("product_id stripped from properties but left in required: %v", required)
	}
}

func TestApplyProfileAddsExtraFacets(t *testing.T) {
	consumption := findTool(t, applyProfile(GetTools(), capsFromJSON(t, uppProfile)), ToolProductionConsumption)

	groups := enumOf(t, consumption, "group_by")

	if !hasValue(groups, "cost_article") {
		t.Errorf("production_consumption group_by missing cost_article, which this database supports: %v", groups)
	}

	// Добавление не должно вытеснять то, что уже было.
	if !hasValue(groups, "material") {
		t.Errorf("production_consumption group_by lost material: %v", groups)
	}
}

func TestApplyProfileDropsUnavailableTools(t *testing.T) {
	tools := applyProfile(GetTools(), capsFromJSON(t, uppProfile))

	for _, tool := range tools {
		if tool.Name == ToolAvailabilityReport || tool.Name == ToolGoodsInTransit {
			t.Errorf("tool %s is unavailable in this database but still listed", tool.Name)
		}
	}

	if len(tools) != len(GetTools())-2 {
		t.Errorf("expected exactly 2 tools dropped, got %d of %d", len(tools), len(GetTools()))
	}
}

// Резолвер, который всегда пуст, остаётся в списке: он работает, просто ничего не находит.
// Убрать его совсем — значит потерять инструмент из виду, когда база заполнит признак.
func TestApplyProfileMarksAlwaysEmptyResolver(t *testing.T) {
	material := findTool(t, applyProfile(GetTools(), capsFromJSON(t, uppProfile)), ToolResolveMaterial)

	if !strings.Contains(material.Description, "always returns an empty list") {
		t.Errorf("resolve_material description carries no warning: %q", material.Description)
	}

	product := findTool(t, applyProfile(GetTools(), capsFromJSON(t, uppProfile)), ToolResolveProduct)

	if strings.Contains(product.Description, "always returns an empty list") {
		t.Error("resolve_product wrongly marked as always empty")
	}
}

// Нет профиля — поведение ровно прежнее. Это и есть fail-open: недоступность 1С не должна
// сужать выдачу инструментов.
func TestApplyProfileNilIsNoop(t *testing.T) {
	tools := applyProfile(GetTools(), nil)

	if len(tools) != len(GetTools()) {
		t.Errorf("nil profile changed the tool count: %d vs %d", len(tools), len(GetTools()))
	}

	if !hasFilter(t, findTool(t, tools, ToolCashFlow), "cost_article_ids") {
		t.Error("nil profile stripped cost_article_ids")
	}
}

// Профиль не должен пережить вызов: GetTools() строит схемы заново, и правка одной
// сессии не может протечь в следующую.
func TestApplyProfileDoesNotLeakIntoNextCall(t *testing.T) {
	applyProfile(GetTools(), capsFromJSON(t, uppProfile))

	if !hasFilter(t, findTool(t, GetTools(), ToolCashFlow), "cost_article_ids") {
		t.Error("applyProfile mutated shared schema state: cost_article_ids gone from a fresh GetTools()")
	}
}
