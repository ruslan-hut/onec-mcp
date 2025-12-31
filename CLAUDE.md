# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

OneC MCP is a Go service that acts as a gateway between LLM/MCP clients and 1C ERP systems via HTTP. It exposes both REST API and MCP (Model Context Protocol) JSON-RPC 2.0 endpoints for customer/warehouse resolution and sales reporting.

## Common Commands

```bash
# Run the server
go run ./cmd/server

# Run with custom config
CONFIG_PATH=configs/config.local.yml go run ./cmd/server

# Build
go build -o bin/server ./cmd/server

# Test health endpoint
curl http://localhost:8088/health

# Test MCP endpoint
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## Architecture

The service follows a layered architecture:

```
cmd/server/main.go          Entry point, wires dependencies, handles graceful shutdown
    ↓
internal/api/               HTTP layer (chi router)
├── router.go               Route definitions, middleware chain
├── handlers.go             REST endpoint handlers
├── middleware.go           Bearer auth middleware
└── models.go               Request/response DTOs

internal/mcp/               MCP protocol layer
├── handler.go              JSON-RPC request router, tool dispatcher
├── protocol.go             JSON-RPC types (Request, Response, errors)
├── jsonrpc.go              Error constructors (-32700, -32600, etc.)
└── tools.go                Tool definitions with JSON schemas

internal/onec/              1C integration layer
├── client.go               HTTP client for 1C backend
└── models.go               1C request/response types

internal/config/            Configuration (cleanenv + YAML)
```

### Key Integration Points

- **MCP Handler** (`internal/mcp/handler.go`): Routes `initialize`, `tools/list`, `tools/call` methods. Authentication via Bearer token configured in `mcp.bearer_token`.
- **1C Client** (`internal/onec/client.go`): Calls 1C endpoints at `/mcp/resolve/customer`, `/mcp/resolve/warehouse`, `/mcp/reports/sales`. Supports basic and bearer auth.
- **Dual Auth**: REST API uses `api.bearer_token`, MCP endpoint uses `mcp.bearer_token`. Both are optional.

### MCP Tools

Three tools are exposed via `tools/list`:
- `resolve_customer` - Search customers by query string
- `resolve_warehouse` - Search warehouses by query string
- `sales_report` - Generate sales report with period, filters, grouping

## Configuration

Config loaded from `configs/config.yml` (override with `CONFIG_PATH` env var). Create `configs/config.local.yml` for local development.

Key settings:
- `onec.base_url` - 1C backend URL (required for actual data)
- `onec.auth.type` - `basic` or `bearer`
- `mcp.bearer_token` / `api.bearer_token` - Auth tokens (empty = disabled)
- `limits.resolve_limit` - Max candidates returned (default: 10)
- `limits.max_rows` - Max report rows (default: 5000)
