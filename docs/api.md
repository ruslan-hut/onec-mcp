# API Reference

## Paths

Every endpoint below except `/health` and `/admin/*` is scoped to a 1C database
and lives under that database's slug — `/{slug}/resolve/customer`,
`/{slug}/reports/sales`, `/{slug}/mcp`. Databases are created in the admin UI
(see [OAuth Setup & Admin Guide](oauth-setup.md)); an unknown, disabled, or
misspelled slug returns 404.

For brevity the sections below write the paths without the `/{slug}` prefix.

## Authentication

### REST API (`/{slug}/resolve/*`, `/{slug}/reports/*`)

Protected endpoints require Bearer token authentication:

```
Authorization: Bearer <api-token>
```

The token is the database's own API token, set in `/admin`. A database without
one does not expose REST endpoints at all.

### MCP Endpoint (`/{slug}/mcp`)

OAuth 2.0 is the primary authentication. LLM clients register dynamically,
obtain a per-user access token, and the token's granted scopes drive tool access:

```
Authorization: Bearer <oauth-access-token>
```

Each database runs its own authorization server, and tokens are bound to that
database's audience — a token issued for one slug is rejected on another.

When `oauth.enabled = false`, the endpoint falls back to the database's static
MCP token, set in `/admin` — intended only for local development and `curl`
tests:

```
Authorization: Bearer <mcp-token>
```

A database with neither OAuth nor a static token gets no MCP route, rather than
an unauthenticated one.

---

## REST API Endpoints

### Health Check (No Auth Required)

```
GET /health
```

**Response:**
```json
{"status": "ok"}
```

---

### Resolve Customer

Search customers by free-text query for disambiguation.

```
POST /resolve/customer
Content-Type: application/json
```

**Request:**
```json
{
  "query": "Shatokhin",
  "limit": 10
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Search query (name, phone, etc.) |
| `limit` | integer | No | Max results (default: 10, max: `limits.resolve_limit`) |

**Response:**
```json
{
  "candidates": [
    {
      "id": "GUID-1",
      "label": "Shatokhin Andriy Petrovych",
      "phone": "+380501234567",
      "city": "Madrid",
      "archived": false
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `candidates` | array | List of matching customers |
| `candidates[].id` | string | Customer GUID |
| `candidates[].label` | string | Human-readable name |
| `candidates[].phone` | string | Phone number (optional) |
| `candidates[].city` | string | City (optional) |
| `candidates[].archived` | boolean | Archive status |

---

### Resolve Warehouse

Search warehouses by name or code for disambiguation.

```
POST /resolve/warehouse
Content-Type: application/json
```

**Request:**
```json
{
  "query": "Office",
  "limit": 10
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Search query (name or code) |
| `limit` | integer | No | Max results (default: 10, max: `limits.resolve_limit`) |

**Response:**
```json
{
  "candidates": [
    {
      "id": "W-GUID-1",
      "label": "Office Warehouse",
      "code": "WH-OFFICE",
      "archived": false
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `candidates` | array | List of matching warehouses |
| `candidates[].id` | string | Warehouse GUID |
| `candidates[].label` | string | Human-readable name |
| `candidates[].code` | string | Warehouse code (optional) |
| `candidates[].archived` | boolean | Archive status |

---

### Resolve Product

Search products by name or артикул (code) for disambiguation. Passing a UUID directly returns that product without searching.

```
POST /resolve/product
Content-Type: application/json
```

**Request:**
```json
{
  "query": "gel polish",
  "limit": 10
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Search query (product name or артикул) |
| `limit` | integer | No | Max results (default: 10, max: `limits.resolve_limit`) |

**Response:**
```json
{
  "candidates": [
    {
      "id": "P-GUID-1",
      "label": "Gel polish No.42",
      "code": "GP-042",
      "archived": false
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `candidates` | array | List of matching products |
| `candidates[].id` | string | Product GUID |
| `candidates[].label` | string | Human-readable name |
| `candidates[].code` | string | Артикул (optional) |
| `candidates[].archived` | boolean | Archive status |
| `candidates[].status` | object | Lifecycle `{code,label}`: `new`/`active`/`phasing_out`/`excluded` (optional) |
| `candidates[].status_changed_at` | string | Last status-change date, YYYY-MM-DD (optional) |
| `candidates[].markets` | array | Allowed markets, subset of `UA`/`EU`/`OTHER` (optional) |
| `candidates[].eu_certification` | object | EU cert `{code,label}`: `certified`/`in_process`/`not_required` (optional) |

> Lifecycle fields let a report separate an expected sales drop (product phased out / withdrawn from a market) from an anomaly. See `category-watchdog-contract.md` for the full contract.

---

### Sales Report

Get sales report with filters, grouping, and sorting.

```
POST /reports/sales
Content-Type: application/json
```

**Request:**
```json
{
  "period": {
    "from": "2025-12-01",
    "to": "2025-12-31"
  },
  "filters": {
    "customer_ids": ["GUID-1"],
    "warehouse_ids": ["W-GUID-1"]
  },
  "group_by": ["warehouse"],
  "measures": ["amount", "qty"],
  "top": 50,
  "sort": [{"field": "amount", "dir": "desc"}]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `period.from` | string | Yes | Start date (YYYY-MM-DD) |
| `period.to` | string | Yes | End date (YYYY-MM-DD) |
| `filters.customer_ids` | array | No | Filter by customer GUIDs |
| `filters.warehouse_ids` | array | No | Filter by warehouse GUIDs |
| `group_by` | array | No | Grouping: `customer`, `warehouse` |
| `measures` | array | No | Measures: `amount`, `qty` |
| `top` | integer | No | Limit rows (max: `limits.max_rows`) |
| `sort` | array | No | Sort order |
| `sort[].field` | string | - | Field name |
| `sort[].dir` | string | - | Direction: `asc`, `desc` |

**Response:**
```json
{
  "columns": [
    {"name": "warehouse", "type": "ref"},
    {"name": "qty", "type": "number"},
    {"name": "amount", "type": "number"}
  ],
  "rows": [
    ["W-GUID-1", 12, 340.50]
  ],
  "totals": {
    "qty": 12,
    "amount": 340.50
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `columns` | array | Column definitions |
| `columns[].name` | string | Column name |
| `columns[].type` | string | Column type: `ref`, `number`, `string` |
| `rows` | array | Data rows (values match column order) |
| `totals` | object | Totals by measure (optional) |

---

### Stock Report

Get product stock balance as of a given date with filters, grouping, and sorting.

```
POST /reports/stock
Content-Type: application/json
```

**Request:**
```json
{
  "date": "2025-12-31",
  "filters": {
    "product_ids": ["P-GUID-1"],
    "warehouse_ids": ["W-GUID-1"]
  },
  "group_by": ["warehouse", "product"],
  "measures": ["qty"],
  "top": 50,
  "sort": [{"field": "qty", "dir": "desc"}]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `date` | string | No | Balance date (YYYY-MM-DD). Defaults to current moment. |
| `filters.product_ids` | array | No | Filter by product GUIDs |
| `filters.warehouse_ids` | array | No | Filter by warehouse GUIDs |
| `group_by` | array | No | Grouping: `warehouse`, `product` (default: both) |
| `measures` | array | No | Measures: `qty`, `amount` (default: `qty`) |
| `top` | integer | No | Limit rows (max: `limits.max_rows`) |
| `sort` | array | No | Sort order (only fields from selected group_by/measures are honored) |

**Response:** same shape as `/reports/sales` — `columns`, `rows`, `totals`.

---

## MCP Endpoint (JSON-RPC 2.0)

```
POST /mcp
Content-Type: application/json
Authorization: Bearer <token>
```

### Initialize

Get server info and capabilities.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "initialize",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "protocolVersion": "2024-11-05",
    "serverInfo": {
      "name": "mcp-sales-mvp",
      "version": "1.0.0"
    },
    "capabilities": {
      "tools": {}
    }
  },
  "id": 1
}
```

---

### List Tools

Get available tools and their JSON schemas.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/list",
  "id": 2
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "tools": [
      {
        "name": "resolve_customer",
        "description": "Search customers by name, phone, or other identifying information...",
        "inputSchema": { ... }
      },
      {
        "name": "resolve_warehouse",
        "description": "Search warehouses by name or code...",
        "inputSchema": { ... }
      },
      {
        "name": "resolve_product",
        "description": "Search products by name or артикул...",
        "inputSchema": { ... }
      },
      {
        "name": "sales_report",
        "description": "Get sales report for a specified period...",
        "inputSchema": { ... }
      },
      {
        "name": "stock_balance",
        "description": "Get product stock balance as of a given date...",
        "inputSchema": { ... }
      },
      {
        "name": "cash_balance",
        "description": "Cash-on-hand balance from ДеньгиВКассе as of a date, by cash desk...",
        "inputSchema": { ... }
      },
      {
        "name": "cash_flow",
        "description": "Cash flow (turnovers) from ДвижениеДенежныхСредств for a period...",
        "inputSchema": { ... }
      },
      {
        "name": "receivables_balance",
        "description": "Accounts-receivable balances from customers (ДЗ / advances), expanded...",
        "inputSchema": { ... }
      },
      {
        "name": "payables_balance",
        "description": "Accounts-payable balances to suppliers (КЗ / advances), expanded...",
        "inputSchema": { ... }
      },
      {
        "name": "purchases_report",
        "description": "Goods-purchase turnover from ПриходнаяНакладная, net of returns, incl. VAT...",
        "inputSchema": { ... }
      },
      {
        "name": "event_log",
        "description": "Read the 1C event log (журнал регистрации): errors/events for a period by level or type...",
        "inputSchema": { ... }
      },
      {
        "name": "object_history",
        "description": "Event log for a specific object or type — who created/changed/posted/deleted it...",
        "inputSchema": { ... }
      },
      {
        "name": "find_document",
        "description": "Find a document by type+number+date, returns its UUID for object_history...",
        "inputSchema": { ... }
      }
    ]
  },
  "id": 2
}
```

> The list above is abbreviated. The actual set returned by `tools/list` is **filtered by the
> caller's OAuth scopes** (when OAuth is enabled): a tool is only shown if its required scope is
> granted. So the admin tools (`event_log`, `object_history`, `find_document`) appear only for
> tokens carrying `mcp:admin:eventlog`, and `sales_report`'s `cost`/`profit`/`margin` measures are
> stripped without `mcp:report:cost`. There are also tools not shown here (resolve
> `sales_channel`/`cash`/`cost_article`/`operation`, reports `top_products`/`customer_summary`).

---

### Call Tool

Execute a tool with arguments.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "resolve_customer",
    "arguments": {
      "query": "Shatokhin",
      "limit": 5
    }
  },
  "id": 3
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `params.name` | string | Yes | Tool name |
| `params.arguments` | object | No | Tool-specific arguments |

**Response (success):**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"candidates\":[...]}"
      }
    ]
  },
  "id": 3
}
```

**Response (error):**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Error message..."
      }
    ],
    "isError": true
  },
  "id": 3
}
```

---

## Money Report Tools (cash, settlements & purchases)

Five report tools gated by the **`mcp:report:money`** scope (they expose money figures). Like other
report tools, the result `content[].text` is the standard report envelope — `{columns, rows, totals}`,
same shape as `sales_report` / `stock_balance`. Amounts are in the base currency, except
`purchases_report` (document currency) and `cash_balance` (each cash desk's own currency — see its
note). For all of them, `sort.field` must be one of the selected `group_by` dimensions or `measures`.

### `cash_balance`

Cash-on-hand balance from the «ДеньгиВКассе» register as of a date, broken down by cash desk (касса).

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `date` | string | No | Balance date (YYYY-MM-DD). Defaults to current moment. |
| `filters.cash_ids` | array | No | Cash desk UUIDs (from `resolve_cash`). |
| `group_by` | array | No | `cash`, `firm` (default: `cash`). firm = owning company of the cash desk. |
| `measures` | array | No | `balance` (default). |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`. |

> NOTE: amounts are in **each cash desk's own currency** (the register has no currency dimension);
> the grand total simply sums them, so it is only meaningful when all selected cash desks share one
> currency.

### `cash_flow`

Cash flow (turnovers) from the «ДвижениеДенежныхСредств» register for a **period**. Net of the base
currency only (the register's duplicate management-accounting-currency row is excluded).

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `period.from` / `period.to` | string | Yes | Period bounds (YYYY-MM-DD). |
| `filters.cash_ids` | array | No | Cash desk / bank account UUIDs (from `resolve_cash`); applied to the `account` dimension. |
| `filters.operation_ids` | array | No | Operation type UUIDs (from `resolve_operation`); applied to the `ВидОперации` dimension. |
| `filters.cost_article_ids` | array | No | Cost article UUIDs (from `resolve_cost_article`); filters the `analytics` dimension via IN HIERARCHY. Combined with `customer_ids` via OR. |
| `filters.customer_ids` | array | No | Counterparty UUIDs (from `resolve_customer`); filters the `analytics` dimension. Combined with `cost_article_ids` via OR. |
| `group_by` | array | No | `account`, `operation`, `analytics`, `firm`, `day`, `week`, `month` (default: `operation`). `analytics` is composite, returned as `{id,label,kind}`; day/week/month return ISO date strings. |
| `measures` | array | No | `inflow` (gross in), `outflow` (gross out, positive), `net` (= inflow − outflow). Default: all three. |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`. |

`receivables_balance` and `payables_balance` read the «Взаиморасчеты» register **as of a date** and
show the balance **expanded, not netted**: the receivable/payable and the advance are returned as
separate measures, split by the sign of each counterparty's net balance. The register has no
contract/order dimension, so for a single counterparty a receivable and an advance across different
deals are already netted into one figure — expansion is across counterparties, not within one.
Suppliers share the counterparty catalog with customers, so supplier UUIDs are resolved via
`resolve_customer`. There is no firm resolver — obtain firm UUIDs from a prior call with
`group_by=["firm"]`.

### `receivables_balance`

Accounts receivable from customers (ДЗ — what customers owe us) as of a date, broken down by customer.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `date` | string | No | Balance date (YYYY-MM-DD). Defaults to current moment. |
| `filters.customer_ids` | array | No | Customer UUIDs (from `resolve_customer`); accepts customer-group UUIDs — applied via IN HIERARCHY. |
| `filters.firm_ids` | array | No | Firm (UA/PL legal entity) UUIDs. |
| `group_by` | array | No | `customer`, `firm` (default: `customer`). |
| `measures` | array | No | `receivable` (ДЗ), `advance` (prepayments received), `net` (= receivable − advance). Default: all three. |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`. |

### `payables_balance`

Accounts payable to suppliers (КЗ — what we owe suppliers) as of a date, broken down by supplier.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `date` | string | No | Balance date (YYYY-MM-DD). Defaults to current moment. |
| `filters.supplier_ids` | array | No | Supplier UUIDs (from `resolve_customer` — shared catalog); accepts group UUIDs — applied via IN HIERARCHY. |
| `filters.firm_ids` | array | No | Firm (UA/PL legal entity) UUIDs. |
| `group_by` | array | No | `supplier`, `firm` (default: `supplier`). |
| `measures` | array | No | `payable` (КЗ), `advance` (prepayments issued), `net` (= payable − advance). Default: all three. |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`. |

### `purchases_report`

Goods-purchase turnover from posted «ПриходнаяНакладная» documents for a **period**. Amounts are net
of returns (`ВидОперации=Возврат` subtracted) and **include VAT** — the correct purchases base for a
DPO denominator. Amounts are in the document currency.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `period.from` / `period.to` | string | Yes | Period bounds (YYYY-MM-DD). |
| `filters.supplier_ids` | array | No | Supplier UUIDs (from `resolve_customer`); accepts group UUIDs — applied via IN HIERARCHY. |
| `filters.firm_ids` | array | No | Firm (UA/PL legal entity) UUIDs. |
| `group_by` | array | No | `supplier`, `firm`, `day`, `week`, `month` (default: `supplier`, `month`; day/week/month return ISO date strings). |
| `measures` | array | No | `amount` (sum incl. VAT, net of returns), `qty` (default: `amount`). |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`. |

---

## Production Tools (bills of materials & manufacturing)

Nine tools gated by the **`mcp:report:cost`** scope — the same permission that unlocks the
`cost` / `profit` / `margin` measures of `sales_report`. A bill of materials is the product's
recipe and the production reports expose the cost of raw material in every item, so they carry
the same sensitivity as purchase prices; no separate scope was introduced.

A composition is identified by **four** keys: product + `matrix_id` (матрица) +
`composition_type_id` (тип состава) + `production_group_id` (группировка производства). Omit the
three modifiers and every variant comes back — each row carries them as columns, so check them
before summing. There are no resolvers for those three catalogs: take their UUIDs from a
`production_output` call with `group_by=["matrix"]` (etc.).

Known limits of the underlying configuration, worth stating before the user asks: there is no
waste accounting (the Отходы table of the production document carries no fields at all) and no
labour or overhead in production — the cost of output is materials only.

### `product_specification`

Bill of materials as of a date: which materials go into a product and at what rate.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `product_id` | string | Yes\* | Product UUID (from `resolve_product`). |
| `product_ids` | array | Yes\* | Several products at once. Leaf products only — group UUIDs do not match. |
| `date` | string | No | Composition as of this date (YYYY-MM-DD). Defaults to now. |
| `qty` | number | No | Product quantity to scale `qty_total` by (default 1; may be fractional). |
| `matrix_id`, `composition_type_id`, `production_group_id` | string | No | Narrow to one variant of the composition. |

\* one of `product_id` / `product_ids`.

Columns: `product`, `material`, `article`, `unit`, `qty_per_unit`, `qty_total`, `is_main_raw`,
`matrix`, `composition_type`, `production_group`, `spec_date`, `spec_document`.

### `specification_cost`

The same rows valued at material prices: adds `price` and `amount` (= `qty_total` × `price`) with
an `amount` total. This is the **planned** cost from the composition — for the actual cost of a
run use `production_document`.

Arguments as in `product_specification`, plus `price_type_id` (defaults to «ЦенаЗакупки», the
same price type the production document itself uses).

### `specification_explode`

Multi-level explosion: any material that has its own composition is expanded further, down to raw
materials. Rows are flat with `level` (1 = direct materials) and `path` (chain from the top
product), plus `has_spec` — whether a material is itself expandable.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `product_id` | string | Yes | Product to explode. |
| `date` | string | No | Compositions as of this date. |
| `qty` | number | No | Quantity of the top product (default 1); scales `qty_total` on every level. |
| `max_depth` | integer | No | Levels to expand (default 3, max 10). |
| `with_cost` | boolean | No | Add `price` / `amount`. |
| `price_type_id` | string | No | Price type for `with_cost`. |
| `matrix_id`, `composition_type_id`, `production_group_id` | string | No | Applied to the **top level only**. |

The modifiers deliberately do not propagate: a semi-finished item may have its composition
registered under a different matrix, and filtering deeper levels would silently truncate the tree.
Recursion also stops on a cycle (a material already seen in the same branch) and at 2000 rows —
check the `truncated` flag.

### `specification_where_used`

Reverse explosion: which products contain a given material and at what rate. Use it for impact
questions ("this raw material got more expensive — what is affected"). Only **current**
compositions are returned: a material dropped by a newer «СпецификацияМатериалов» document does
not appear, even though its old record still lives in the register slice.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `material_id` / `material_ids` | string / array | Yes | Material UUIDs (from `resolve_product` — raw materials share the product catalog). |
| `date` | string | No | Compositions as of this date. |
| `matrix_id`, `composition_type_id`, `production_group_id` | string | No | Narrow to one variant. |
| `limit` | integer | No | Max rows (default 100, max 500). |

### `specification_versions`

Change history of a composition — one version per «СпецификацияМатериалов» document, newest
first, each with its material list and a diff against the previous version **of the same
variant** (variants live in parallel; comparing across them is meaningless).

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `product_id` | string | Yes | Product UUID. |
| `period.from` / `period.to` | string | No | Window to limit history to. Omit for the whole history. |
| `matrix_id`, `composition_type_id`, `production_group_id` | string | No | Narrow to one variant. |
| `limit` | integer | No | Max versions (default 10, max 50). |

Unlike the rest, the result is not a `columns`/`rows` table but
`{product, total_versions, versions:[{date, document, matrix, composition_type, production_group, materials[], changes:{added,removed,changed}, is_first}]}`.

### `specification_list`

Inventory of compositions — one row per variant: `product`, `matrix`, `composition_type`,
`production_group`, `materials_count`, `spec_date`, `spec_document` (plus `total_variants` and
`truncated`).

With `missing_only: true` it flips around and lists products that were **produced in `period` but
have no composition** — the usual cause of the «Не задан состав для продукции» error when filling
materials in a production document. Columns then are `product`, `produced_qty`, `documents`,
`last_production_date`, and only assembly operations are counted.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `missing_only` | boolean | No | Switch to the "produced without a composition" mode. |
| `period.from` / `period.to` | string | No | Production period for `missing_only` (defaults to today — pass it explicitly). |
| `date` | string | No | Compositions as of this date. |
| `product_ids` | array | No | Narrow to these products. |
| `matrix_id`, `composition_type_id`, `production_group_id` | string | No | Narrow to one variant. |
| `limit` | integer | No | Max rows (default 200, max 500; 100 in `missing_only` mode). |

### `production_output` / `production_consumption`

Turnover from posted «Производство» documents for a period. `production_output` reads the
Продукция table (what was manufactured), `production_consumption` reads the Материалы table (what
was written off). The Материалы table carries both the material and the product it was consumed
for, so `material` + `product` in `group_by` gives material cost per manufactured item.

Both default to **assembly only**. Disassembly is the mirror operation — there the Продукция
table holds what was taken apart and Материалы is what came out — so summing both into one
"produced" figure is wrong. The effective value is always echoed in `applied_filters`.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `period.from` / `period.to` | string | Yes | Period bounds (YYYY-MM-DD). |
| `operation_type` | string | No | `assembly` (default), `disassembly`, `all`. |
| `filters.product_ids` | array | No | Manufactured item UUIDs; group UUIDs applied via IN HIERARCHY. In `production_consumption` this filters the product a material was consumed **for**. |
| `filters.material_ids` | array | No | `production_consumption` only. |
| `filters.warehouse_ids` | array | No | Склад продукции for output, склад материалов for consumption. |
| `filters.employee_ids` | array | No | `production_output` only. |
| `filters.matrix_ids`, `composition_type_ids`, `production_group_ids`, `firm_ids` | array | No | UUIDs from a prior call grouped by that dimension. |
| `group_by` | array | No | `product`, `product_group`, `warehouse`, `matrix`, `composition_type`, `production_group`, `firm`, `operation`, `document`, `day`, `week`, `month`; `employee` (output only); `material`, `material_group` (consumption only). Default: `product`/`material` + `month`. |
| `measures` | array | No | `qty`, `amount` (incl. VAT), `amount_novat`, `documents`; output also `qty_plan`, `raw_qty_plan`, `qty_variance` (fact − plan). Default: `qty`, `amount`. |
| `top` | integer | No | Limit rows. |
| `sort` | array | No | `[{field, dir}]`; field must be a selected dimension or measure. |

> NOTE: plan fields are not always filled in — a zero `qty_plan` means "not planned", not "zero
> output". Amounts are the sums entered in the document, **not** the FIFO cost batch accounting
> actually wrote off; for that use `production_document`.

### `production_document`

Full detail of one production run: header, both tables and its actual register movements. Get the
UUID from `production_output` / `production_consumption` with `group_by=["document"]`, or from
`find_document`.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `document_id` | string | Yes | UUID of the «Производство» document. |

Returns `{document, products[], materials[], movements[], summary}`. `movements` are the real
«ОстаткиТоваров» postings (`direction` expense/receipt, warehouse, product, batch, qty, amount).
In `summary`, `cost_written_off` is the **actual FIFO cost** of the materials consumed (computed
by batch accounting) and `cost_received` is what was capitalised for the output; `difference` is
the gap between them, while `output_amount` / `materials_amount` are the sums as typed into the
document. Those two pairs differing is normal, not an error.

---

## Admin Tools (event log)

Three tools for event-log analysis, all gated by the **`mcp:admin:eventlog`** scope (the log
contains PII). They are visible in `tools/list` only for tokens that carry this scope. The tool
result `content[].text` is the JSON returned by 1C (see `onec-integration.md` for the full payload
shape). On the 1C side reads run in privileged mode, so the service user needs no extra rights.

### `event_log`

List events for a period, filtered by severity and/or technical event type, optionally by user or
session. All filters are independent and optional; `period` defaults to the current day. Events come
back chronological (oldest first).

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `level` | array | No | Severity: `error` / `warning` / `information` / `note`. e.g. `["error"]` for "errors today". |
| `events` | array | No | Technical event names: `_$Data$_.Post` (posting), `_$Data$_.New`, `_$Data$_.Update`, `_$Data$_.Delete`, `_$Session$_.Start` (login), … |
| `user` | string | No | Substring of the infobase user login / full name; resolved to all matching users. |
| `session` | integer | No | Session number — pull the full trace of one session (e.g. the one where an error occurred). |
| `period.from` / `period.to` | string | No | Window (YYYY-MM-DD). Defaults to the current day. |
| `limit` | integer | No | Max events (default 100, max 500). |

### `object_history`

Event log for one specific object or a whole object type — who created/changed/posted/unposted/deleted it, and when.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `object_type` | string | Yes | Full metadata name: `Document.<Name>` or `Catalog.<Name>`. |
| `object_id` | string | No | Object UUID (from `find_document` or a `resolve_*` tool). Omit for all objects of `object_type`. |
| `events` | array | No | Optional technical event names to narrow to. |
| `period.from` / `period.to` | string | No | Window (YYYY-MM-DD). Defaults to the current day. |
| `limit` | integer | No | Max events (default 100, max 500). |

### `find_document`

Resolve a document to its UUID so it can be audited via `object_history`.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `doc_type` | string | Yes | Document metadata name (`ДокументОтгрузки`); `Document.` prefix optional. |
| `number` | string | No\* | Document number or substring. |
| `period.from` / `period.to` | string | No\* | Date window (YYYY-MM-DD). |
| `limit` | integer | No | Max candidates (default 20, max 100). |

\* At least one of `number` or `period` is required.

**Example — "list errors today":**
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "event_log",
    "arguments": { "level": ["error"] }
  },
  "id": 4
}
```

---

## Error Responses

### REST API Errors

```json
{
  "error": "error_code",
  "message": "Human-readable message"
}
```

| HTTP Status | Error Code | Description |
|-------------|------------|-------------|
| 400 | `invalid_request` | Failed to parse request body |
| 400 | `validation_error` | Missing required fields or invalid values |
| 400 | `limit_exceeded` | Result exceeds max_rows limit |
| 400 / 401 / 502 | `onec_error` | 1C backend request failed — status is taken from 1C (400/401), otherwise 502. The `message` field mirrors 1C's `message` when the body is a structured `{error, message}` JSON. |

### JSON-RPC Errors

| Code | Message | Description |
|------|---------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid Request | Missing jsonrpc version |
| -32601 | Method not found | Unknown method |
| -32602 | Invalid params | Bad tool parameters |
| -32603 | Internal error | Server error |
| -32000 | Unauthorized | Invalid/missing Bearer token |
