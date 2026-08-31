package onec

import (
	"encoding/json"
	"testing"
)

// Второй рубеж обороны. Первый — проверка неизвестных ключей в mcp.Handler, она
// отклоняет вызов раньше. Но структуры фильтров типизированы по ролям именно для того,
// чтобы чужой ключ не доехал до 1С, даже если проверка когда-нибудь окажется в обход:
// применённым он там всё равно не будет, а агент прочитает полную выборку как
// отфильтрованную.
func TestFilterStructsDropForeignKeys(t *testing.T) {
	t.Run("cash_balance keeps only its own keys", func(t *testing.T) {
		var f CashBalanceFilters
		decode(t, `{"cash_ids":["c1"],"firm_ids":["f1"],
			"operation_ids":["o1"],"cost_article_ids":["a1"],"customer_ids":["k1"]}`, &f)

		if len(f.CashIDs) != 1 || len(f.FirmIDs) != 1 {
			t.Fatalf("own keys lost: %+v", f)
		}

		assertAbsent(t, f, "operation_ids", "cost_article_ids", "customer_ids")
	})

	t.Run("receivables keeps customers, not suppliers", func(t *testing.T) {
		var f ReceivablesFilters
		decode(t, `{"customer_ids":["k1"],"supplier_ids":["s1"]}`, &f)

		if len(f.CustomerIDs) != 1 {
			t.Fatalf("customer_ids lost: %+v", f)
		}

		assertAbsent(t, f, "supplier_ids")
	})

	t.Run("payables keeps suppliers, not customers", func(t *testing.T) {
		var f PayablesFilters
		decode(t, `{"customer_ids":["k1"],"supplier_ids":["s1"]}`, &f)

		if len(f.SupplierIDs) != 1 {
			t.Fatalf("supplier_ids lost: %+v", f)
		}

		assertAbsent(t, f, "customer_ids")
	})

	t.Run("top_products drops sales-only keys", func(t *testing.T) {
		var f TopProductsFilters
		decode(t, `{"customer_ids":["k1"],"warehouse_ids":["w1"],"product_ids":["p1"],
			"sales_channel_ids":["c1"],"customer_cohort":"new","product_status":["active"]}`, &f)

		if len(f.CustomerIDs) != 1 || len(f.WarehouseIDs) != 1 || len(f.ProductIDs) != 1 {
			t.Fatalf("own keys lost: %+v", f)
		}

		assertAbsent(t, f, "sales_channel_ids", "customer_cohort", "product_status")
	})
}

// product_ids долго не было в SalesFilters, и ключ молча пропадал при разборе: агент
// просил продажи по одному SKU, а получал цифры по всей базе.
func TestSalesFiltersCarryProductIDs(t *testing.T) {
	var f SalesFilters
	decode(t, `{"product_ids":["p1","p2"]}`, &f)

	if len(f.ProductIDs) != 2 {
		t.Fatalf("product_ids lost: %+v", f)
	}

	out, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := body["product_ids"]; !ok {
		t.Errorf("product_ids must reach 1C, got %s", out)
	}
}

func decode(t *testing.T, raw string, v any) {
	t.Helper()

	if err := json.Unmarshal([]byte(raw), v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}

// assertAbsent проверяет по сериализованному телу, что чужие ключи не переживают
// round-trip: именно это тело уходит в 1С.
func assertAbsent(t *testing.T, v any, keys ...string) {
	t.Helper()

	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}

	for _, key := range keys {
		if _, leaked := body[key]; leaked {
			t.Errorf("%s must not reach 1C, got %s", key, out)
		}
	}
}
