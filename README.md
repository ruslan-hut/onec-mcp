# OneC MCP

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-2024--11--05-blue?style=flat)](https://modelcontextprotocol.io/)
[![JSON-RPC](https://img.shields.io/badge/JSON--RPC-2.0-orange?style=flat)](https://www.jsonrpc.org/)
[![1C](https://img.shields.io/badge/1C-Enterprise-yellow?style=flat)](https://1c.ru/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)

A Go service that acts as a gateway between an LLM/MCP client and 1C ERP via HTTP.

## Features

- **Reference resolution** - search customers, warehouses, products, sales channels, cash desks, cost articles, and operation types by free-text query, with hierarchical group support
- **Sales reporting** - sales, top products, and per-customer summaries with filters, grouping, sorting, and (scoped) cost/profit/margin measures
- **Stock reporting** - product stock balances as of a date
- **Money reporting** - cash-on-hand balances and cash flow turnovers
- **MCP protocol support** - JSON-RPC 2.0 endpoint for LLM integration
- **OAuth 2.0** - per-user keys, scope-based tool access, and audit logging

## Requirements

- Go 1.23+
- 1C HTTP service (or mock server for development)

## Quick Start

```bash
# Run the server
go run ./cmd/server

# Test health endpoint
curl http://localhost:8088/health

# Test MCP endpoint (OAuth token, or the static fallback when oauth.enabled = false)
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <oauth-access-token>' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## Configuration

Copy and edit the config file:

```bash
cp configs/config.yml configs/config.local.yml
```

| Option | Description | Default |
|--------|-------------|---------|
| `server.host` | Server bind address | `0.0.0.0` |
| `server.port` | Server port | `8088` |
| `onec.base_url` | 1C API base URL | - |
| `onec.timeout_ms` | Request timeout in ms | `8000` |
| `onec.auth.type` | Auth type (`basic` / `bearer`) | - |
| `limits.resolve_limit` | Max resolve results | `10` |
| `limits.max_rows` | Max report rows | `5000` |
| `mcp.enabled` | Enable MCP endpoint | `true` |
| `oauth.enabled` | Enable OAuth 2.0 (primary auth for `/mcp`) | `true` |
| `mcp.bearer_token` | Static fallback token for `/mcp` (used only when `oauth.enabled = false`) | - |
| `api.bearer_token` | Bearer token for REST API auth | - |

**Authentication.** OAuth 2.0 is the primary auth for the `/mcp` endpoint: LLM
clients register dynamically, obtain a per-user token, and the token's granted
scopes drive tool access. The static `mcp.bearer_token` is only a fallback for
local development and `curl` tests when `oauth.enabled = false`. REST endpoints
(`/resolve/*`, `/reports/*`) are a separate server-side integration surface
guarded by `api.bearer_token`, independent of OAuth. See the
[OAuth Setup & Admin Guide](docs/oauth-setup.md).

## Documentation

| Document | Description |
|----------|-------------|
| [API Reference](docs/api.md) | REST and MCP endpoint specifications |
| [Testing Guide](docs/testing.md) | What can be tested without 1C backend |
| [1C Integration](docs/onec-integration.md) | Expected 1C endpoints and formats |
| [OAuth Setup & Admin Guide](docs/oauth-setup.md) | Connecting Claude/ChatGPT, issuing per-user keys, audit log, troubleshooting |

## Project Structure

```
├── cmd/server/          # Application entry point
├── configs/             # Configuration files
├── docs/                # Documentation
├── internal/
│   ├── api/             # HTTP handlers and router
│   ├── config/          # Configuration loader
│   ├── mcp/             # MCP JSON-RPC handler
│   ├── oauth/           # OAuth 2.0 server, scopes, audit
│   └── onec/            # 1C HTTP client
├── go.mod
└── go.sum
```

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/resolve/customer` | POST | Search customers |
| `/resolve/warehouse` | POST | Search warehouses |
| `/resolve/product` | POST | Search products |
| `/resolve/sales_channel` | POST | Search sales channels |
| `/reports/sales` | POST | Sales report |
| `/reports/stock` | POST | Stock balance report |
| `/mcp` | POST | MCP JSON-RPC 2.0 |
| `/.well-known/oauth-protected-resource` | GET | OAuth resource metadata |
| `/.well-known/oauth-authorization-server` | GET | OAuth server metadata |
| `/oauth/register` | POST | Dynamic client registration |
| `/oauth/authorize` | GET/POST | Authorization endpoint |
| `/oauth/token` | POST | Token endpoint |

REST endpoints are mounted only when `api.bearer_token` is set; the OAuth and
discovery endpoints only when an OAuth server is configured.

## MCP Tools

| Tool | Scope | Description |
|------|-------|-------------|
| `resolve_customer` | `mcp:resolve` | Search customers by name, phone, etc. (optional catalog groups) |
| `resolve_warehouse` | `mcp:resolve` | Search warehouses by name or code |
| `resolve_product` | `mcp:resolve` | Search products by name or артикул (optional catalog groups) |
| `resolve_sales_channel` | `mcp:resolve` | Search hierarchical sales channels |
| `resolve_cash` | `mcp:report:money` | Search cash desks (кассы) |
| `resolve_cost_article` | `mcp:report:money` | Search cost articles (статьи затрат) |
| `resolve_operation` | `mcp:report:money` | Search cash-flow operation types |
| `sales_report` | `mcp:report:sales` | Sales with filters, grouping, sorting, cohorts |
| `top_products` | `mcp:report:sales` | Top-N best-selling products for a period |
| `customer_summary` | `mcp:report:sales` | Summary card for a single customer |
| `stock_balance` | `mcp:report:stock` | Product stock balance as of a date |
| `cash_balance` | `mcp:report:money` | Cash-on-hand balance per cash desk |
| `cash_flow` | `mcp:report:money` | Cash flow turnovers for a period |

### Scopes

Each tool requires a scope; `tools/list` is filtered to the caller's granted
scopes and `tools/call` is rejected without the matching scope. The
`mcp:report:cost` scope is measure-level — it unlocks the `cost`, `profit`, and
`margin` measures in `sales_report` and `customer_summary`. Scopes are enforced
again on the 1C side via the `X-MCP-Scopes` header (defense in depth). See the
[OAuth Setup & Admin Guide](docs/oauth-setup.md) for issuing per-user keys.
