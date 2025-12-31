# 1C Integration Guide

This document describes the HTTP endpoints that must be implemented on the 1C side.

## Overview

The Go service acts as a gateway and expects 1C to provide three HTTP endpoints:

| Endpoint | Description |
|----------|-------------|
| `POST /mcp/resolve/customer` | Search customers |
| `POST /mcp/resolve/warehouse` | Search warehouses |
| `POST /mcp/reports/sales` | Generate sales report |

Base URL is configured via `onec.base_url` in config.

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
