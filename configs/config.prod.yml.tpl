server:
  host: 127.0.0.1
  port: 8088

onec:
  base_url: "${ONEC_BASE_URL}"
  timeout_ms: 8000
  auth:
    type: "${ONEC_AUTH_TYPE}"
    username: "${ONEC_AUTH_USERNAME}"
    password: "${ONEC_AUTH_PASSWORD}"
  tenant_header: "${ONEC_TENANT_HEADER}"
  default_tenant: "${ONEC_DEFAULT_TENANT}"

limits:
  resolve_limit: 10
  max_rows: 5000

mcp:
  enabled: true
  bearer_token: "${MCP_BEARER_TOKEN}"

api:
  bearer_token: "${API_BEARER_TOKEN}"
