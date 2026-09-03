# 1C Integration Guide

This document describes the HTTP endpoints that must be implemented on the 1C side.

## Overview

The Go service acts as a gateway and expects 1C to provide three HTTP endpoints:

| Endpoint | Description |
|----------|-------------|
| `POST /mcp/resolve/customer` | Search customers |
| `POST /mcp/resolve/warehouse` | Search warehouses |
| `POST /mcp/resolve/product` | Search products |
| `POST /mcp/resolve/firm` | Search firms / legal entities (Фирмы / Организации) |
| `POST /mcp/reports/sales` | Generate sales report |
| `POST /mcp/reports/stock` | Stock balance report |
| `POST /mcp/reports/availability` | Availability (out-of-stock days) report — see `category-watchdog-contract.md` |
| `POST /mcp/reports/product_details` | Batch product status attributes — see `category-watchdog-contract.md` |
| `POST /mcp/admin/eventlog` | Read the event log (журнал регистрации) — gated by `mcp:admin:eventlog` |
| `POST /mcp/admin/find_document` | Resolve a document to its UUID — gated by `mcp:admin:eventlog` |

Base URL is configured via `onec.base_url` in config.

> Note: this list shows the core endpoints. The gate also calls the resolve endpoints for
> `sales_channel` / `cash` / `cost_article` / `operation`, the report endpoints
> `top_products` / `customer_summary` / `cash_balance` / `cash_flow`, plus `POST /mcp/auth/verify`
> and `GET /mcp/health`.

The production block (latest addition) adds nine more report endpoints, all gated by
`mcp:report:cost` and all passthrough — the gate forwards the body and returns the 1C response
as-is, so fields can be added on the 1C side without touching Go:

| Endpoint | Description |
|----------|-------------|
| `POST /mcp/reports/specification` | Bill of materials as of a date |
| `POST /mcp/reports/specification_cost` | Same, valued at material prices |
| `POST /mcp/reports/specification_explode` | Multi-level explosion down to raw materials |
| `POST /mcp/reports/specification_where_used` | Reverse explosion (material → products) |
| `POST /mcp/reports/specification_versions` | Change history of a composition, with diffs |
| `POST /mcp/reports/specification_list` | Compositions inventory / products produced without one |
| `POST /mcp/reports/production_output` | Manufacturing output turnover (ТЧ Продукция) |
| `POST /mcp/reports/production_consumption` | Material consumption turnover (ТЧ Материалы) |
| `POST /mcp/reports/production_document` | One production document with its actual movements |

See `api.md` for the argument reference; the 1C side lives in `CommonModules/MCP` (region
`Production`).

## Filters: never ignore one silently

A filter the gate forwards must be either **applied** or **rejected with 400**. Silently
ignoring an unsupported key is the one outcome that must not happen: 1C returns numbers for
the whole database, the agent believes the sample was narrowed, and nothing in the response
reveals the substitution. A refusal costs one retry; a silently ignored filter is a wrong
answer that looks right.

This is not hypothetical. `sales_report` accepted `product_ids` in the body for months —
the key passed schema validation, was dropped when the gate decoded the request into its
typed filter struct, never reached 1C, and every "sales of this SKU" question was answered
with company-wide totals.

Two rules follow, one per side:

**Gate side.** Every `filters` object in a tool schema is closed
(`additionalProperties: false`), and `handleToolsCall` rejects a call whose `filters`
carries a key the tool's own `InputSchema` does not declare, naming the offending and the
supported keys. The schema sent in `tools/list` is the single source of truth, so the check
cannot drift from the advertised contract.

**1C side.** Whatever the gate declares for a report, the base must handle:

- implement the filter, or
- answer 400 explaining why this database cannot support it.

`product_status` in УПП is the model case for the second branch: the item catalog has no
lifecycle attribute there, so the report answers
`product_status is not supported: Номенклатура has no lifecycle status in this database`
instead of quietly returning unfiltered rows.

A filter whose ids resolve to nothing deserves the same treatment — see
`product_ids: no existing product resolved`. An empty filter set is not "no filter": a typo
in a UUID would otherwise return the full database as an honest-looking zero-filtered answer.

Echo what you applied. Every report response carries `applied_filters` with one key per
supported filter; the reader confirms the sample was narrowed by looking there, so a missing
key is itself a signal.

## Firms (multi-company) and the `X-MCP-Sub` header

A **firm** is the legal entity a document is issued by: `Справочник.Фирмы` in rior-cf,
`Справочник.Организации` in УПП. Every report accepts `filters.firm_ids` and offers `firm` as a
`group_by` dimension, and `POST /mcp/resolve/firm` resolves a firm by name or code.

### Endpoint: Resolve Firm

```
POST {base_url}/mcp/resolve/firm
{"query": "ТОВ Ромашка", "limit": 10}
```

Response — the usual candidate shape:

```json
{"candidates": [{"id": "…-uuid", "label": "ТОВ «Ромашка»", "code": "001", "archived": false}]}
```

### Per-key firm restriction

Alongside `X-MCP-Scopes` the gate sends **`X-MCP-Sub`** — the UUID of the MCP account, exactly the
`sub` value 1C itself returned from `/mcp/auth/verify`. This lets 1C restrict which firms a given
key may see **without the gate knowing anything about firms**.

Expected behaviour on the 1C side:

1. read `X-MCP-Sub`, find the account, take its list of allowed firms;
2. an empty list means **no restriction** — data is not sliced by firm at all. The restriction is
   switched on by the mere presence of rows on the account: one row, one firm; several rows,
   several firms. There is no "default firm" setting anywhere — the restriction lives on the
   account and nowhere else;
3. if `filters.firm_ids` is absent, substitute the allowed list (report is aggregated over the
   firms the key may see, not over the whole database);
4. if `filters.firm_ids` is present, intersect it with the allowed list;
5. if the ids in `filters.firm_ids` resolve to no existing firm at all, return **400** — silently
   replacing them with the allowed list would answer about firms nobody asked for;
6. an empty intersection must return **403** `{"error":"FORBIDDEN","message":"firm not allowed"}` —
   not an empty report, which an LLM would read as "there were no sales";
7. `resolve/firm` returns only allowed firms, so the list itself does not disclose the group
   structure;
8. no `X-MCP-Sub` (legacy static-token mode, OAuth off) — no restriction either, for the same
   reason as an empty list: the restriction lives on the account, and without one there is
   nothing to restrict by.

Trust model is the same as for `X-MCP-Scopes`: the header is trusted because 1C is published only
for the gateway behind basic auth. The **request body is not trusted** — never take the list of
allowed firms from it.

Where firms are not used to separate access, implement step 1 as a stub returning an empty list;
everything else then degrades to a no-op.

## Authentication

The Go service authenticates to 1C using one of two methods (configured via `onec.auth`):

### Basic Auth
```
Authorization: Basic base64(username:password)
```

### Bearer Token
```
Authorization: Bearer <token>
```

## Tenant Header

If configured, the service sends a tenant header with each request:
```
X-Tenant: main
```

Header name and value are configured via `onec.tenant_header` and `onec.default_tenant`.

---

## Endpoint: Resolve Customer

```
POST {base_url}/mcp/resolve/customer
Content-Type: application/json
```

### Request

```json
{
  "query": "Shatokhin",
  "limit": 10
}
```

| Field | Type | Description |
|-------|------|-------------|
| `query` | string | Search query (name, phone, etc.) |
| `limit` | integer | Maximum results to return |

### Response

```json
{
  "candidates": [
    {
      "id": "e5d7a8b2-1234-5678-9abc-def012345678",
      "label": "Shatokhin Andriy Petrovych",
      "phone": "+380501234567",
      "city": "Madrid",
      "archived": false
    },
    {
      "id": "f6e8b9c3-2345-6789-abcd-ef0123456789",
      "label": "Shatokhin Ivan Sergiyovych",
      "phone": "+380509876543",
      "city": "Kyiv",
      "archived": false
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `candidates` | array | Yes | List of matching customers |
| `candidates[].id` | string | Yes | Customer GUID (unique identifier) |
| `candidates[].label` | string | Yes | Human-readable name for display |
| `candidates[].phone` | string | No | Phone number for disambiguation |
| `candidates[].city` | string | No | City for disambiguation |
| `candidates[].archived` | boolean | No | Whether customer is archived |

### Notes

- `label` should be human-readable for AI disambiguation
- Include distinguishing fields (phone, city, type) when available
- Return empty `candidates` array if no matches found
- Respect the `limit` parameter

---

## Endpoint: Resolve Warehouse

```
POST {base_url}/mcp/resolve/warehouse
Content-Type: application/json
```

### Request

```json
{
  "query": "Office",
  "limit": 10
}
```

| Field | Type | Description |
|-------|------|-------------|
| `query` | string | Search query (name or code) |
| `limit` | integer | Maximum results to return |

### Response

```json
{
  "candidates": [
    {
      "id": "a1b2c3d4-5678-90ab-cdef-123456789012",
      "label": "Office Warehouse",
      "code": "WH-OFFICE",
      "archived": false
    },
    {
      "id": "b2c3d4e5-6789-01bc-def0-234567890123",
      "label": "Office Storage Room",
      "code": "WH-OFFICE-2",
      "archived": false
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `candidates` | array | Yes | List of matching warehouses |
| `candidates[].id` | string | Yes | Warehouse GUID |
| `candidates[].label` | string | Yes | Human-readable name |
| `candidates[].code` | string | No | Warehouse code |
| `candidates[].archived` | boolean | No | Whether warehouse is archived |

---

## Endpoint: Resolve Product

```
POST {base_url}/mcp/resolve/product
Content-Type: application/json
```

### Request

```json
{
  "query": "gel polish",
  "limit": 10
}
```

| Field | Type | Description |
|-------|------|-------------|
| `query` | string | Search query (product name or артикул) |
| `limit` | integer | Maximum results to return |

### Response

```json
{
  "candidates": [
    {
      "id": "p1q2r3s4-5678-90ab-cdef-123456789012",
      "label": "Gel polish No.42",
      "code": "GP-042",
      "archived": false,
      "status": { "code": "active", "label": "Активна" },
      "status_changed_at": "2026-05-06",
      "markets": ["UA", "EU", "OTHER"],
      "eu_certification": { "code": "certified", "label": "Є" }
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `candidates` | array | Yes | List of matching products |
| `candidates[].id` | string | Yes | Product GUID |
| `candidates[].label` | string | Yes | Human-readable name |
| `candidates[].code` | string | No | Артикул (product code) |
| `candidates[].archived` | boolean | No | Whether product is archived |
| `candidates[].status` | object | No | Жизненный цикл: `{code,label}`; code ∈ `new` \| `active` \| `phasing_out` \| `excluded` |
| `candidates[].status_changed_at` | string | No | Дата последней смены статуса (YYYY-MM-DD); пусто, если статус не менялся с момента внедрения истории |
| `candidates[].markets` | array | No | Разрешённые рынки: подмножество `UA` \| `EU` \| `OTHER` |
| `candidates[].eu_certification` | object | No | Сертификация EU: `{code,label}`; code ∈ `certified` \| `in_process` \| `not_required` |

### Notes

- Если `query` похож на UUID и соответствует существующему товару — возвращается один кандидат с этим UUID, без полнотекстового поиска.
- Товары с пометкой `ДляПроизводства` исключаются на стороне 1С (внутренние/производственные позиции).
- Статусные поля (`status` / `status_changed_at` / `markets` / `eu_certification`) вычисляются на стороне 1С из реквизитов карточки (хелперы `CommonModules/MCP`) — гейт их только пробрасывает. Старые сборки 1С поля не отдают (все `omitempty`).
- Временная логика `markets` (до согласования точной): `ДоставкаЗаГраницуЗапрещена=Истина → ["UA"]`, иначе `["UA","EU","OTHER"]`.

---

## Endpoint: Sales Report

```
POST {base_url}/mcp/reports/sales
Content-Type: application/json
```

### Request

```json
{
  "period": {
    "from": "2025-12-01",
    "to": "2025-12-31"
  },
  "filters": {
    "customer_ids": ["e5d7a8b2-1234-5678-9abc-def012345678"],
    "product_ids": ["p1q2r3s4-5678-90ab-cdef-123456789012"],
    "warehouse_ids": ["a1b2c3d4-5678-90ab-cdef-123456789012"]
  },
  "group_by": ["warehouse", "customer"],
  "measures": ["amount", "qty"],
  "top": 100,
  "sort": [
    {"field": "amount", "dir": "desc"}
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `period.from` | string | Yes | Start date (YYYY-MM-DD) |
| `period.to` | string | Yes | End date (YYYY-MM-DD) |
| `filters.customer_ids` | array | No | Filter by customer GUIDs. Leaf or group UUID — apply via `IN HIERARCHY` so a group matches everyone inside it. |
| `filters.product_ids` | array | No | Filter by product GUIDs. Leaf or product-group UUID, also `IN HIERARCHY`. This is what answers "sales of one SKU over time" — without it the report can only rank products, never restrict to one. |
| `filters.warehouse_ids` | array | No | Filter by warehouse GUIDs |
| `filters.firm_ids` | array | No | Filter by firm GUIDs (from `/mcp/resolve/firm`). Absent = all firms the key may see — see the firms section above. |
| `group_by` | array | No | Grouping dimensions |
| `measures` | array | No | Measures to calculate |
| `top` | integer | No | Max rows to return |
| `sort` | array | No | Sort specification |
| `sort[].field` | string | - | Field to sort by |
| `sort[].dir` | string | - | `asc` or `desc` |

### Supported Values

**group_by:**
- `customer` - group by customer
- `warehouse` - group by warehouse

**measures:**
- `amount` - sales amount (sum)
- `qty` - quantity (sum)

### Response

```json
{
  "columns": [
    {"name": "warehouse", "type": "ref"},
    {"name": "customer", "type": "ref"},
    {"name": "qty", "type": "number"},
    {"name": "amount", "type": "number"}
  ],
  "rows": [
    ["a1b2c3d4-5678-90ab-cdef-123456789012", "e5d7a8b2-1234-5678-9abc-def012345678", 15, 450.00],
    ["a1b2c3d4-5678-90ab-cdef-123456789012", "f6e8b9c3-2345-6789-abcd-ef0123456789", 8, 240.50]
  ],
  "totals": {
    "qty": 23,
    "amount": 690.50
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `columns` | array | Yes | Column definitions |
| `columns[].name` | string | Yes | Column name |
| `columns[].type` | string | Yes | Column type |
| `rows` | array | Yes | Data rows |
| `totals` | object | No | Totals for measures |

### Column Types

- `ref` - reference (GUID)
- `number` - numeric value
- `string` - text value
- `date` - date value

### Notes

- Row values must match column order
- If `group_by` is empty, return aggregated totals only
- If `measures` is empty, include all available measures
- Apply `top` limit after sorting
- `totals` should contain sums for numeric measures
- Echo the applied filters in `applied_filters` — one key per supported filter
  (`customers`, `products`, `warehouses`, `firms`, …), each an array of `{id, label}`.
  This is how the caller tells a narrowed sample from a full one; see the filters section above

---

## Endpoint: Stock Report

```
POST {base_url}/mcp/reports/stock
Content-Type: application/json
```

### Request

```json
{
  "date": "2025-12-31",
  "filters": {
    "product_ids": ["p1q2r3s4-5678-90ab-cdef-123456789012"],
    "warehouse_ids": ["a1b2c3d4-5678-90ab-cdef-123456789012"],
    "product_status": ["excluded"]
  },
  "group_by": ["warehouse", "product"],
  "measures": ["qty", "amount"],
  "top": 100,
  "sort": [
    {"field": "qty", "dir": "desc"}
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `date` | string | No | Balance date (YYYY-MM-DD). Defaults to current moment on the 1C side. |
| `filters.product_ids` | array | No | Filter by product GUIDs |
| `filters.warehouse_ids` | array | No | Filter by warehouse GUIDs |
| `filters.firm_ids` | array | No | Filter by firm GUIDs (from `/mcp/resolve/firm`). Absent = all firms the key may see — see the firms section above. |
| `filters.product_status` | array | No | Фильтр по статусу ЖЦ: подмножество `new` \| `active` \| `phasing_out` \| `excluded`. На 1С разворачивается в пред-резолв ссылок по статусу (не условие на `Balance()`). |
| `group_by` | array | No | Grouping dimensions |
| `measures` | array | No | Measures to calculate |
| `top` | integer | No | Max rows to return |
| `sort` | array | No | Sort specification |

### Supported Values

**group_by:** `warehouse`, `product`
**measures:** `qty` (Количество), `amount` (Сумма)

### Response

Same shape as Sales Report — `{columns, rows, totals}`.

### Notes

- Items with the `ДляПроизводства` flag (both `Товар` and `Склад`) are excluded on the 1C side regardless of filters.
- Default group_by is both `warehouse` and `product`; default measure is `qty` only.
- Resource fields map to virtual table balance fields: `qty` → `КоличествоBalance`, `amount` → `СуммаBalance`.

---

## Admin Endpoints

Administrative tools for event-log analysis, backing the `event_log`, `object_history` and
`find_document` MCP tools. All three are gated by the **`mcp:admin:eventlog`** scope: the gate
hides the tools from users without it (filtered out of `tools/list`) and forwards the resolved
scopes to 1C via the `X-MCP-Scopes` header, where the `/admin/{action}` handler re-checks the
scope (defense in depth). The event log contains PII — grant this scope only to trusted accounts.

On the 1C side the export and document lookup run in **privileged mode**, so the service's
infobase user does **not** need the "event log" administrative right.

---

## Endpoint: Health & capabilities profile

```
GET {base_url}/mcp/health
```

Liveness plus — optionally — the **capabilities profile**: a machine-readable description of
how this particular database differs from the common contract. The gate reads it and edits
tool schemas before showing them to the model.

Why the profile lives in 1C and not in the tenant record: the differences are produced by the
accounting model of the database, and the code that relies on that model is the code that
knows about them. A list kept in the gate's settings would silently go stale on every change
made on the 1C side.

```json
{
  "status": "ok",
  "time": "2026-09-03T10:00:00",
  "capabilities": {
    "profile": "upp-1.3",
    "version": 1,
    "unsupported": {
      "cash_flow": { "filters": ["cost_article_ids"] },
      "stock_balance": { "filters": ["firm_ids", "product_status"], "group_by": ["firm"] },
      "specification_explode": { "params": ["matrix_id", "composition_type_id"] }
    },
    "extra": {
      "production_consumption": { "group_by": ["cost_article"] }
    },
    "tools": { "unavailable": ["availability_report", "goods_in_transit"] },
    "resolvers": { "always_empty": ["sales_channel", "material"] }
  }
}
```

| Field | Meaning |
|-------|---------|
| `profile` | Human-readable database identifier; logging only. |
| `version` | Version of the profile **structure**, not its contents. A version the gate does not know is ignored whole — applying it half-way is worse than not applying it. |
| `unsupported.<tool>` | Facets to remove from that tool's schema: `params`, `filters`, `group_by`, `measures`. Keys are **gate tool names** (`product_specification`), not 1C report types (`specification`). |
| `extra.<tool>` | Facets to add: things this database supports that the common schema does not declare. A silently hidden capability is the same mistake as a promised missing one, only quieter. |
| `tools.unavailable` | Tools to drop from `tools/list` entirely. |
| `resolvers.always_empty` | Entity names whose resolver always returns an empty list here. The tool is **kept** — it works, it just finds nothing — and its description gains a note. |

Both sides must move together: the profile and the 400s in 1C describe the same thing. The
refusals stay as the last line of defence — the gate may be an older build, or `health` may be
unreachable — and in that case the call must hit a clear error, not silence.

Gate behaviour is **fail-open**: no profile, an unknown version, or an unreachable 1C all mean
"show the schemas as before". The profile sharpens the tool surface; it is not an access
control. Access is held by scopes and by the checks inside 1C. The profile is cached per
tenant for 5 minutes (`onec.CapabilitiesTTL`), failures included — otherwise every
`tools/list` against a down 1C would pay a network timeout.

---

## Endpoint: Event Log

Backs both `event_log` and `object_history` — they POST to the same endpoint; 1C reads
whichever filter fields are present. `event_log` leads with `level` / `events` filtering
('errors today', 'postings today'), with `user` / `session` as optional filters; `object_history`
is the object-centric framing. Events are returned in chronological order (oldest first),
which is what you want when reconstructing a session trace up to an error.

```
POST {base_url}/mcp/admin/eventlog
Content-Type: application/json
```

### Request

```json
{
  "user": "Ivanov",
  "session": 1234,
  "level": ["error", "warning"],
  "events": ["_$Data$_.Post"],
  "object_type": "Document.ДокументОтгрузки",
  "object_id": "e5d7a8b2-1234-5678-9abc-def012345678",
  "period": { "from": "2026-06-01", "to": "2026-06-19" },
  "limit": 100
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `user` | string | No | Substring of the infobase user login / full name; resolved to all matching users' UUIDs. If none match, an empty result with a `note` is returned (not the whole log). |
| `session` | integer | No | Session number — pull the full action trace of one session (e.g. the one in which an error occurred). |
| `level` | array | No | `error` / `warning` / `information` / `note`. Omit for all levels. |
| `events` | array | No | Technical event names: `_$Data$_.New`, `_$Data$_.Update`, `_$Data$_.Post`, `_$Data$_.Unpost`, `_$Data$_.Delete`, `_$Session$_.Start`, … |
| `object_type` | string | No | Full metadata name (`Document.<Name>` / `Catalog.<Name>`). With `object_id` → the specific object; alone → all objects of that type. |
| `object_id` | string | No | UUID of a specific object (used together with `object_type`). |
| `period.from` / `period.to` | string | No | Window (YYYY-MM-DD). Defaults to the current day. |
| `limit` | integer | No | Max events (default 100, max 500). If exceeded, the earliest events in the window are returned — narrow the period or add filters. |

### Response

```json
{
  "period": { "from": "2026-06-01T00:00:00", "to": "2026-06-19T23:59:59" },
  "count": 1,
  "events": [
    {
      "date": "2026-06-18T14:03:21",
      "level": "error",
      "user": "Ivanov Petro",
      "event": "_$Data$_.Post",
      "event_presentation": "Проведение",
      "comment": "Не удалось провести документ: ...",
      "metadata": "Документ отгрузки",
      "object": "Отгрузка 00-000123 от 18.06.2026",
      "session": 1234,
      "transaction_status": "RolledBack",
      "computer": "POS-01"
    }
  ],
  "matched_users": ["Ivanov Petro"]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `period` | object | Effective window (ISO 8601). |
| `count` | integer | Number of events returned. |
| `events` | array | Log records, chronological (oldest first). |
| `events[].event` | string | Technical event name (e.g. `_$Data$_.Post`). |
| `events[].event_presentation` | string | Human-readable action (Создание / Изменение / Проведение / Отмена проведения / Удаление). |
| `events[].object` | string | Presentation of the affected data. |
| `matched_users` | array | Present only when `user` was given — the users the filter matched. |
| `note` | string | Present when nothing matched (e.g. unknown user). |

---

## Endpoint: Find Document

Backs `find_document` — resolves a document to its UUID so it can be audited via the event-log
endpoint (`object_type` + `object_id`).

```
POST {base_url}/mcp/admin/find_document
Content-Type: application/json
```

### Request

```json
{
  "doc_type": "ДокументОтгрузки",
  "number": "000123",
  "period": { "from": "2026-06-01", "to": "2026-06-19" },
  "limit": 20
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `doc_type` | string | Yes | Document metadata name (`ДокументОтгрузки`); the `Document.` prefix is optional. Validated against `Metadata.Documents` on the 1C side. |
| `number` | string | No\* | Document number or a substring of it. |
| `period.from` / `period.to` | string | No\* | Date window (YYYY-MM-DD). |
| `limit` | integer | No | Max candidates (default 20, max 100). |

\* At least one of `number` or `period` is required — otherwise the whole document flow would be returned.

### Response

```json
{
  "doc_type": "Document.ДокументОтгрузки",
  "candidates": [
    {
      "id": "e5d7a8b2-1234-5678-9abc-def012345678",
      "object_type": "Document.ДокументОтгрузки",
      "number": "00-000123",
      "date": "2026-06-18T14:03:00",
      "posted": true,
      "deletion_mark": false,
      "presentation": "Отгрузка 00-000123 от 18.06.2026"
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `doc_type` | string | Normalized full type name. |
| `candidates[].id` | string | Document UUID — pass as `object_id` to the event-log endpoint. |
| `candidates[].object_type` | string | Ready-to-use `object_type` for the event-log endpoint. |
| `candidates[].number` / `date` | string | Document number and date (ISO 8601). |
| `candidates[].posted` | boolean | Whether the document is posted (Проведен). |
| `candidates[].deletion_mark` | boolean | Deletion mark. |

---

## Error Handling

1C should return appropriate HTTP status codes:

| Status | Description |
|--------|-------------|
| 200 | Success |
| 400 | Bad request (invalid parameters) |
| 401 | Unauthorized (invalid credentials) |
| 500 | Internal server error |

Error response format (optional):
```json
{
  "error": "error_code",
  "message": "Human-readable error description"
}
```

---

## Example 1C Implementation (Pseudocode)

```bsl
// HTTP Service: mcp

// Method: POST /mcp/resolve/customer
Function ResolveCustomer(Request)
    Query = Request.Body.query;
    Limit = Request.Body.limit;

    Selection = Catalogs.Customers.Select();
    Selection.Filter("Description LIKE %Query%");
    Selection.Top(Limit);

    Candidates = New Array;
    While Selection.Next() Do
        Candidate = New Structure;
        Candidate.Insert("id", String(Selection.Ref.UUID()));
        Candidate.Insert("label", Selection.Description);
        Candidate.Insert("phone", Selection.Phone);
        Candidate.Insert("city", Selection.City);
        Candidate.Insert("archived", Selection.DeletionMark);
        Candidates.Add(Candidate);
    EndDo;

    Response = New Structure("candidates", Candidates);
    Return HTTPResponse(200, JSON(Response));
EndFunction

// Method: POST /mcp/resolve/warehouse
Function ResolveWarehouse(Request)
    // Similar to ResolveCustomer
EndFunction

// Method: POST /mcp/reports/sales
Function SalesReport(Request)
    Period = Request.Body.period;
    Filters = Request.Body.filters;
    GroupBy = Request.Body.group_by;

    // Build and execute query based on parameters
    // Return columns, rows, totals
EndFunction
```

---

## Configuration Example

```yaml
onec:
  base_url: "https://1c.example.com/api"
  timeout_ms: 8000
  auth:
    type: "basic"
    username: "mcp_user"
    password: "secret"
  tenant_header: "X-Tenant"
  default_tenant: "main"
```
