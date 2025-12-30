# Testing Guide

## Testing Without 1C Backend

The following functionality can be tested without a real 1C backend.

### 1. Start the Server

```bash
go run ./cmd/server
```

**Expected output:**
```
level=INFO msg="MCP endpoint enabled" path=/mcp
level=INFO msg="starting server" addr=0.0.0.0:8088
```

---

### 2. Health Check

```bash
curl http://localhost:8088/health
```

**Expected:**
```json
{"status":"ok"}
```

---

### 3. MCP Initialize

```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"initialize","id":1}'
```

**Expected:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "protocolVersion": "2024-11-05",
    "serverInfo": {"name": "mcp-sales-mvp", "version": "1.0.0"},
    "capabilities": {"tools": {}}
  },
  "id": 1
}
```

---

### 4. MCP List Tools

```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":2}'
```

**Expected:** JSON with 3 tools and their schemas:
- `resolve_customer`
- `resolve_warehouse`
- `sales_report`

---

### 5. MCP Authentication

**Without token (should fail):**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"initialize","id":1}'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32000,"message":"Unauthorized"},"id":null}
```

**With wrong token (should fail):**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer wrong-token' \
  -d '{"jsonrpc":"2.0","method":"initialize","id":1}'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32000,"message":"Unauthorized"},"id":null}
```

---

### 6. JSON-RPC Error Handling

**Invalid JSON:**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d 'not json'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}
```

**Unknown method:**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"unknown","id":1}'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found","data":"unknown"},"id":1}
```

**Invalid jsonrpc version:**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"1.0","method":"initialize","id":1}'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":1}
```

**Unknown tool:**
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"unknown_tool"},"id":1}'
```

**Expected:**
```json
{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":"unknown tool: unknown_tool"},"id":1}
```

---

### 7. Tool Calls (Requires 1C)

Calling `tools/call` will attempt to connect to the configured 1C backend.

```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "resolve_customer",
      "arguments": {"query": "test"}
    },
    "id": 3
  }'
```

**Expected (without 1C):** Error response with connection failure message:
```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": [{"type": "text", "text": "request failed: ..."}],
    "isError": true
  },
  "id": 3
}
```

---

### 8. REST API Authentication

**Without token (should fail):**
```bash
curl -X POST http://localhost:8088/resolve/customer \
  -H 'Content-Type: application/json' \
  -d '{"query": "test"}'
```

**Expected:**
```json
{"error":"unauthorized","message":"Invalid or missing Bearer token"}
```

**With wrong token (should fail):**
```bash
curl -X POST http://localhost:8088/resolve/customer \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer wrong-token' \
  -d '{"query": "test"}'
```

**Expected:**
```json
{"error":"unauthorized","message":"Invalid or missing Bearer token"}
```

---

### 9. REST API Validation

**Missing required field:**
```bash
curl -X POST http://localhost:8088/resolve/customer \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-token' \
  -d '{}'
```

**Expected:**
```json
{"error":"validation_error","message":"Query is required"}
```

**Invalid measure:**
```bash
curl -X POST http://localhost:8088/reports/sales \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-token' \
  -d '{"period":{"from":"2025-01-01","to":"2025-01-31"},"measures":["invalid"]}'
```

**Expected:**
```json
{"error":"validation_error","message":"Invalid measure: invalid. Supported: amount, qty"}
```

---

### 10. Graceful Shutdown

Press `Ctrl+C` in the terminal running the server.

**Expected:**
```
level=INFO msg="shutting down server"
level=INFO msg="server stopped"
```

---

## Testing With 1C Backend

When a 1C backend is available, you can test the full flow:

### Resolve Customer
```bash
curl -X POST http://localhost:8088/resolve/customer \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-token' \
  -d '{"query": "Shatokhin", "limit": 5}'
```

### Resolve Warehouse
```bash
curl -X POST http://localhost:8088/resolve/warehouse \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-token' \
  -d '{"query": "Office", "limit": 5}'
```

### Sales Report
```bash
curl -X POST http://localhost:8088/reports/sales \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-token' \
  -d '{
    "period": {"from": "2025-12-01", "to": "2025-12-31"},
    "group_by": ["customer"],
    "measures": ["amount", "qty"]
  }'
```

### MCP Tool Call
```bash
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "sales_report",
      "arguments": {
        "period": {"from": "2025-12-01", "to": "2025-12-31"},
        "group_by": ["warehouse"]
      }
    },
    "id": 1
  }'
```
