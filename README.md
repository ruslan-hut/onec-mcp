# OneC MCP

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-2024--11--05-blue?style=flat)](https://modelcontextprotocol.io/)
[![JSON-RPC](https://img.shields.io/badge/JSON--RPC-2.0-orange?style=flat)](https://www.jsonrpc.org/)
[![1C](https://img.shields.io/badge/1C-Enterprise-yellow?style=flat)](https://1c.ru/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)

A Go service that acts as a gateway between an LLM/MCP client and 1C ERP via HTTP.

## Features

- **Customer resolution** - search customers by free-text query with disambiguation support
- **Warehouse resolution** - search warehouses by free-text query with disambiguation support
- **Sales reporting** - fetch sales data with filters, grouping, and sorting
- **MCP protocol support** - JSON-RPC 2.0 endpoint for LLM integration

## Requirements

- Go 1.23+
- 1C HTTP service (or mock server for development)

## Quick Start

```bash
# Run the server
go run ./cmd/server

# Test health endpoint
curl http://localhost:8088/health

# Test MCP endpoint
curl -X POST http://localhost:8088/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-secret-token' \
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
| `mcp.bearer_token` | Bearer token for MCP auth | - |
| `api.bearer_token` | Bearer token for REST API auth | - |

## Documentation

| Document | Description |
|----------|-------------|
| [API Reference](docs/api.md) | REST and MCP endpoint specifications |
| [Testing Guide](docs/testing.md) | What can be tested without 1C backend |
| [1C Integration](docs/onec-integration.md) | Expected 1C endpoints and formats |

## Project Structure

```
├── cmd/server/          # Application entry point
├── configs/             # Configuration files
├── docs/                # Documentation
├── internal/
│   ├── api/             # HTTP handlers and router
│   ├── config/          # Configuration loader
│   ├── mcp/             # MCP JSON-RPC handler
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
| `/reports/sales` | POST | Sales report |
| `/mcp` | POST | MCP JSON-RPC 2.0 |

## MCP Tools

| Tool | Description |
|------|-------------|
| `resolve_customer` | Search customers by name, INN, etc. |
| `resolve_warehouse` | Search warehouses by name or code |
| `sales_report` | Get sales with filters and grouping |
