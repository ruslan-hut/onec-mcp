server:
  host: 127.0.0.1
  port: 8088

database:
  path: "/var/lib/onec-mcp/onec-mcp.db"

admin:
  enabled: true
  username: "${ADMIN_USERNAME}"
  password: "${ADMIN_PASSWORD}"

limits:
  resolve_limit: 10
  max_rows: 5000

mcp:
  enabled: true

oauth:
  enabled: true
  public_url: "${OAUTH_PUBLIC_URL}"
  access_token_ttl: "1h"
  refresh_token_ttl: "720h"
  auth_code_ttl: "10m"
  default_scopes:
    - "mcp:resolve"
    - "mcp:report:sales"
    - "mcp:report:stock"
  supported_scopes:
    - "mcp:resolve"
    - "mcp:report:sales"
    - "mcp:report:stock"
    - "mcp:report:money"
    - "mcp:report:cost"
    - "mcp:admin:eventlog"
  rate_limit:
    authorize_per_minute: 10
    register_per_minute: 30
    token_per_minute: 120

# Базы 1С здесь не описываются — они в SQLite по database.path, заводятся через /admin.
