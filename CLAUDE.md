# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Workflow Settings

- **Always verify after code changes** - run `go build ./...` and `go test ./...` after every change
  and report the results; fix what the build or the tests report before handing work back
- `go vet ./...` is expected to pass too

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

# Test MCP endpoint (tenant1 is a database slug from `tenants` in the config)
curl -X POST http://localhost:8088/tenant1/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## Architecture

The service follows a layered architecture:

```
cmd/server/main.go          Entry point, wires dependencies, builds tenant runtimes
    ↓
internal/api/               HTTP layer (chi router)
├── router.go               Route definitions, middleware chain
├── registry.go             Slug → tenant runtime, rebuilt on every admin edit
├── handlers.go             REST endpoint handlers
├── middleware.go           Bearer auth middleware
└── models.go               Request/response DTOs

internal/admin/             /admin web UI for managing 1C databases
├── admin.go                Basic auth, CSRF guard, CRUD handlers
└── views.go                HTML templates

internal/tenant/            1C database records
├── tenant.go               Record type, defaults, validation
└── store.go                SQLite CRUD

internal/mcp/               MCP protocol layer
├── handler.go              JSON-RPC request router, tool dispatcher
├── protocol.go             JSON-RPC types (Request, Response, errors)
├── jsonrpc.go              Error constructors (-32700, -32600, etc.)
└── tools.go                Tool definitions with JSON schemas

internal/onec/              1C integration layer
├── client.go               HTTP client for 1C backend
└── models.go               1C request/response types

internal/store/             Shared SQLite handle (tenants + OAuth tables)
internal/config/            Configuration (cleanenv + YAML)
```

### Multi-database

The gateway fronts several 1C databases at once. A database is a row in the `tenants` SQLite
table, **not** a config entry — it is created and edited at `/admin` (Basic auth, credentials
from the config). Its slug is the first path segment: `/{slug}/mcp`, `/{slug}/oauth/*`,
`/{slug}/resolve/*`. Only `/health`, `/admin/*` and the canonical `.well-known/*` paths live at
the root.

Because databases change at runtime, routes cannot be registered per tenant at startup. The
router holds `{tenant}` patterns and resolves each request through `api.Registry`, which
`cmd/server/main.go` populates via a build function: one `onec.Client`, one
`oauth.CachedVerifier`, one `oauth.Server`, one `mcp.Handler`, one `api.Handler` per database.
Every admin mutation calls `Registry.Reload`, which rebuilds all runtimes and swaps the map.

Nothing carrying database-specific state may be shared between tenants — in particular the
key-verification cache, since a shared one would let a key from one 1C unlock another.

Isolation rests on three independent mechanisms: a `tenant` column filtering every OAuth
storage lookup, an audience check in `oauth.Server.Middleware` (a token minted for
`<public_url>/<slug>/mcp` is rejected everywhere else), and per-database 1C credentials.

### Key Integration Points

- **MCP Handler** (`internal/mcp/handler.go`): Routes `initialize`, `tools/list`, `tools/call` methods. Its static Bearer is the database's MCP token, and is blanked when OAuth is on. A blank token means *no authentication at all*, so a database with neither OAuth nor a static token gets no MCP route (see the `switch` in `buildTenants`).
- **1C Client** (`internal/onec/client.go`): Calls 1C endpoints at `/mcp/resolve/customer`, `/mcp/resolve/warehouse`, `/mcp/reports/sales`. Basic auth only.
- **Dual Auth**: REST API uses the database's API token, the MCP endpoint uses OAuth (or its static MCP token when `oauth.enabled = false`).

### MCP Tools

Tools live in `internal/mcp/tools.go` (definitions + `ToolScopes`); the dispatch is the `switch`
in `handleToolsCall`. They come in families, each closed by one scope:
- resolvers (`resolve_customer`, `resolve_warehouse`, `resolve_product`, …) — `mcp:resolve`
- sales (`sales_report`, `top_products`, `customer_summary`) — `mcp:report:sales`
- stock (`stock_balance`, `availability_report`, `goods_in_transit`) — `mcp:report:stock`
- money (`cash_*`, `receivables_balance`, `payables_balance`, `purchases_report`) — `mcp:report:money`
- production (`resolve_material`, `product_specification`, `specification_*`, `production_*`) — `mcp:report:cost`
- admin (`event_log`, `object_history`, `find_document`) — `mcp:admin:eventlog`

`mcp:report:cost` is not only a tool-level gate: it also widens what shared tools return
(`resolve_warehouse` lists production warehouses, `stock_balance` / `availability_report` cover
materials on them — 1C decides this from the `X-MCP-Scopes` header). Anything whose *content*
depends on the caller's scopes must carry that fact in its `resolveCache` key — see
`costScoped` in `internal/onec/client.go`, otherwise one cost-scoped call poisons the cache for
everyone else until the TTL expires.

Adding a tool means: a constant, a `ToolScopes` entry (a tool missing from the map is rejected as
unknown), a definition in `GetTools`, a `case` in the dispatch, and the matching branch in the 1C
side's `ScopeForReport` / `ScopeForEntity` — the gate's scope map and 1C's must agree, since 1C
re-checks the `X-MCP-Scopes` header. See `docs/api.md` for the user-facing argument reference.

## Configuration

Config loaded from `configs/config.yml` (override with `CONFIG_PATH` env var). Create `configs/config.local.yml` for local development.

The config holds only what is needed to boot and open `/admin`. Per-database settings (1C URL,
Basic credentials, timeouts, tokens, scope overrides) are **not** here — they are rows in SQLite,
edited through the admin UI.

Key settings:
- `database.path` - single SQLite file: tenants + OAuth clients/tokens
- `admin.username` / `admin.password` - credentials for `/admin`; the gateway refuses to start without them when admin is enabled
- `oauth.public_url` - external **root** URL of the gateway, without the slug
- `limits.resolve_limit` - Max candidates returned (default: 10)
- `limits.max_rows` - Max report rows (default: 5000)
