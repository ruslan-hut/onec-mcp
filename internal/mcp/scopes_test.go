package mcp

import (
	"slices"
	"testing"
)

// Каждое право из ToolScopes обязано попасть в AllToolScopes: именно по этому списку
// supported_scopes дополняется правами инструментов.
func TestAllToolScopesCoversToolScopes(t *testing.T) {
	all := AllToolScopes()

	for tool, scope := range ToolScopes {
		if scope == "" {
			continue
		}
		if !slices.Contains(all, scope) {
			t.Errorf("scope %q of tool %q is missing from AllToolScopes()", scope, tool)
		}
	}

	if !slices.Contains(all, ScopeReportCost) {
		t.Errorf("measure-level scope %q is missing from AllToolScopes()", ScopeReportCost)
	}
}

// Конфиг, в котором право инструмента забыли, дополняется им, а порядок исходного
// списка и отсутствие дублей сохраняются.
func TestEnsureToolScopesAddsMissingKeepsOrder(t *testing.T) {
	got := EnsureToolScopes([]string{"mcp:resolve", "mcp:report:sales", "mcp:resolve"})

	if got[0] != "mcp:resolve" || got[1] != "mcp:report:sales" {
		t.Fatalf("original order not preserved: %v", got)
	}

	if slices.Contains(got[2:], "mcp:resolve") {
		t.Errorf("duplicate scope in result: %v", got)
	}

	for _, scope := range []string{ScopeAdminData, "mcp:admin:eventlog", "mcp:report:money"} {
		if !slices.Contains(got, scope) {
			t.Errorf("scope %q was not added: %v", scope, got)
		}
	}
}

// Пустой список - это весь набор прав инструментов, а не пустота: база без своего списка
// и без глобального всё равно должна анонсировать то, чем закрыты инструменты.
func TestEnsureToolScopesFromEmpty(t *testing.T) {
	got := EnsureToolScopes(nil)

	if len(got) != len(AllToolScopes()) {
		t.Fatalf("expected all tool scopes, got %v", got)
	}
}
