# 1C Integration Guide

This document describes the HTTP endpoints that must be implemented on the 1C side.

## Overview

The Go service acts as a gateway and expects 1C to provide three HTTP endpoints:

| Endpoint | Description |
|----------|-------------|
| `POST /mcp/resolve/customer` | Search customers |
| `POST /mcp/resolve/warehouse` | Search warehouses |
| `POST /mcp/resolve/product` | Search products |
| `POST /mcp/reports/sales` | Generate sales report |
| `POST /mcp/reports/stock` | Stock balance report |
| `POST /mcp/admin/eventlog` | Read the event log (журнал регистрации) — gated by `mcp:admin:eventlog` |
| `POST /mcp/admin/find_document` | Resolve a document to its UUID — gated by `mcp:admin:eventlog` |

Base URL is configured via `onec.base_url` in config.

> Note: this list shows the core endpoints. The gate also calls the resolve endpoints for
> `sales_channel` / `cash` / `cost_article` / `operation`, the report endpoints
> `top_products` / `customer_summary` / `cash_balance` / `cash_flow`, plus `POST /mcp/auth/verify`
> and `GET /mcp/health`. The admin endpoints below were the latest addition.

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
      "archived": false
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

### Notes

- Если `query` похож на UUID и соответствует существующему товару — возвращается один кандидат с этим UUID, без полнотекстового поиска.
- Товары с пометкой `ДляПроизводства` исключаются на стороне 1С (внутренние/производственные позиции).

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
| `filters.customer_ids` | array | No | Filter by customer GUIDs |
| `filters.warehouse_ids` | array | No | Filter by warehouse GUIDs |
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
    "warehouse_ids": ["a1b2c3d4-5678-90ab-cdef-123456789012"]
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
