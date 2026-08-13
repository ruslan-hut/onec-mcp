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
- **Settlements & purchases** - receivables/payables balances (ДЗ/КЗ, expanded by sign) and goods-purchase turnover; together with stock they cover the full Cash Conversion Cycle (DIO/DSO/DPO)
- **MCP protocol support** - JSON-RPC 2.0 endpoint for LLM integration
- **OAuth 2.0** - per-user keys, scope-based tool access, and audit logging
- **Multi-database** - several 1C databases behind one gateway, each under its own path slug and its own OAuth authorization server
- **Admin UI** - databases are stored in SQLite and managed at `/admin`, no restart and no config edit required

## Requirements

- Go 1.23+
- 1C HTTP service (or mock server for development)

## Quick Start

```bash
# Run the server
go run ./cmd/server

# Test health endpoint
curl http://localhost:8088/health

# Add a 1C database at http://localhost:8088/admin, then use its slug:
curl -X POST http://localhost:8088/tenant1/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <oauth-access-token>' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

A fresh database has no tenants, so the gateway starts serving nothing but
`/health` and `/admin`. That is expected — add the first 1C database in the
admin UI.

## Configuration

Copy and edit the config file:

```bash
cp configs/config.yml configs/config.local.yml
```

The config holds only what the gateway needs in order to boot and open `/admin`.
**1C databases are not in the config** — they live in SQLite and are managed
through the admin UI.

| Option | Description | Default |
|--------|-------------|---------|
| `server.host` | Server bind address | `0.0.0.0` |
| `server.port` | Server port | `8088` |
| `database.path` | SQLite file: tenants + OAuth clients/tokens | `data/onec-mcp.db` |
| `admin.enabled` | Serve the admin UI at `/admin` | `true` |
| `admin.username` / `admin.password` | Admin credentials (Basic auth) | - |
| `limits.resolve_limit` | Max resolve results | `10` |
| `limits.max_rows` | Max report rows | `5000` |
| `mcp.enabled` | Enable MCP endpoints | `true` |
| `oauth.enabled` | Enable OAuth 2.0 (primary auth for `/{slug}/mcp`) | `false` |
| `oauth.public_url` | External **root** URL of the gateway, no slug | - |

## Managing databases

Everything about a 1C database — its slug, 1C address and Basic credentials,
timeouts, tokens, scope overrides — is edited at `/admin`, behind Basic auth with
the credentials from the config. Changes take effect immediately: the router
resolves slugs through a registry that is rebuilt on every save, so no restart is
needed.

Each database gets a slug, and the slug is the first path segment: `/tenant1/mcp`,
`/tenant1/oauth/*`, `/tenant1/resolve/*`. Slugs must match `[a-z0-9-]` and cannot
be `health`, `admin`, `mcp`, `oauth`, or `.well-known`. A slug cannot be renamed
after creation — it is baked into connector URLs, the OAuth issuer, and the
audience of every token already issued.

**Authentication.** OAuth 2.0 is the primary auth for the `/{slug}/mcp`
endpoint: LLM clients register dynamically, obtain a per-user token, and the
token's granted scopes drive tool access. Every database runs its own
authorization server — issuer is `<public_url>/<slug>` and the token audience is
`<public_url>/<slug>/mcp` — so a token minted for one database is rejected on
every other. A database's static MCP token is only a fallback for local
development and `curl` tests when `oauth.enabled = false`; a database with
neither OAuth nor a static token gets no MCP route at all, rather than an
unauthenticated one. REST endpoints (`/{slug}/resolve/*`, `/{slug}/reports/*`)
are a separate server-side integration surface guarded by the database's own API
token, independent of OAuth. See the
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
│   ├── admin/           # /admin web UI for managing 1C databases
│   ├── api/             # HTTP handlers, router, tenant registry
│   ├── config/          # Configuration loader
│   ├── mcp/             # MCP JSON-RPC handler
│   ├── oauth/           # OAuth 2.0 server, scopes, audit
│   ├── onec/            # 1C HTTP client
│   ├── store/           # Shared SQLite handle
│   └── tenant/          # 1C database records and their storage
├── go.mod
└── go.sum
```

## Endpoints

`{slug}` is the slug of a database created in the admin UI. Only `/health`,
`/admin/*` and the canonical `.well-known/*` paths live at the root.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/admin/*` | GET/POST | Admin UI: manage 1C databases (Basic auth) |
| `/{slug}/resolve/customer` | POST | Search customers |
| `/{slug}/resolve/warehouse` | POST | Search warehouses |
| `/{slug}/resolve/product` | POST | Search products |
| `/{slug}/resolve/sales_channel` | POST | Search sales channels |
| `/{slug}/reports/sales` | POST | Sales report |
| `/{slug}/reports/stock` | POST | Stock balance report |
| `/{slug}/reports/cash_balance` | POST | Cash-on-hand balance |
| `/{slug}/reports/cash_flow` | POST | Cash flow turnovers |
| `/{slug}/reports/receivables` | POST | Customer receivables balance (ДЗ + advances) |
| `/{slug}/reports/payables` | POST | Supplier payables balance (КЗ + advances) |
| `/{slug}/reports/purchases` | POST | Goods-purchase turnover |
| `/{slug}/mcp` | POST | MCP JSON-RPC 2.0 |
| `/.well-known/oauth-protected-resource/{slug}/mcp` | GET | OAuth resource metadata (RFC 9728) |
| `/.well-known/oauth-authorization-server/{slug}` | GET | OAuth server metadata (RFC 8414) |
| `/{slug}/oauth/register` | POST | Dynamic client registration |
| `/{slug}/oauth/authorize` | GET/POST | Authorization endpoint |
| `/{slug}/oauth/token` | POST | Token endpoint |

Discovery metadata is also served under `/{slug}/.well-known/...` for clients
that look for it beneath the resource prefix instead of the canonical
path-insertion form.

REST endpoints are mounted only for databases that set an API token; the OAuth
and discovery endpoints only when `oauth.enabled` is true. A disabled database
serves no routes at all.

## MCP Tools

| Tool | Scope | Description |
|------|-------|-------------|
| `resolve_customer` | `mcp:resolve` | Search customers by name, phone, etc. (optional catalog groups) |
| `resolve_warehouse` | `mcp:resolve` | Search warehouses by name or code (production warehouses only with `mcp:report:cost`) |
| `resolve_product` | `mcp:resolve` | Search products by name or артикул (optional catalog groups) |
| `resolve_material` | `mcp:report:cost` | Search raw materials and components — the production-only half of the same item catalog |
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
| `receivables_balance` | `mcp:report:money` | Customer receivables (ДЗ) and advances received, expanded by sign |
| `payables_balance` | `mcp:report:money` | Supplier payables (КЗ) and advances issued, expanded by sign |
| `purchases_report` | `mcp:report:money` | Goods-purchase turnover by supplier/month (net of returns) |

### Scopes

Each tool requires a scope; `tools/list` is filtered to the caller's granted
scopes and `tools/call` is rejected without the matching scope. The
`mcp:report:cost` scope works on three levels at once: it unlocks the `cost`,
`profit`, and `margin` measures in `sales_report` and `customer_summary`; it
gates the production tools (`resolve_material`, `product_specification`,
`specification_*`, `production_*`); and it widens what the shared tools return —
`resolve_warehouse` starts listing production warehouses, `stock_balance` and
`availability_report` start covering materials on them. Scopes are enforced
again on the 1C side via the `X-MCP-Scopes` header (defense in depth). See the
[OAuth Setup & Admin Guide](docs/oauth-setup.md) for issuing per-user keys.
