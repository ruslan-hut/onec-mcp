# API Reference

## Authentication

### REST API (`/resolve/*`, `/reports/*`)

Protected endpoints require Bearer token authentication:

```
Authorization: Bearer <api-token>
```

Token is configured via `api.bearer_token` in config. If token is not configured, authentication is disabled.

### MCP Endpoint (`/mcp`)

Requires separate Bearer token:

```
Authorization: Bearer <mcp-token>
```

Token is configured via `mcp.bearer_token` in config.

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
> `sales_channel`/`cash`/`cost_article`/`operation`, reports `top_products`/`customer_summary`/`cash_balance`/`cash_flow`).

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
