# Deploy checklist (OAuth)

Краткий чек-лист полного развёртывания гейта с OAuth. Детали — в [oauth-setup.md](oauth-setup.md).

## 1. 1С (разово)

- [ ] Перечисление `MCPScopes`, справочник `MCP_Accounts`, URL-шаблон `/auth/verify` в HTTPService `MCP` — создано в Конфигураторе.
- [ ] Отдельный пользователь 1С под гейт (Basic).
- [ ] `GET /mcp/health` отвечает.

## 2. DNS + TLS

- [ ] A-запись `mcp.example.com` → сервер гейта.
- [ ] Reverse-proxy (nginx/Caddy) с HTTPS на `127.0.0.1:8088`. **HTTPS обязателен** (ChatGPT не примет HTTP).

## 3. Сборка и установка

- [ ] `go build -o /opt/onec-mcp/onec-mcp ./cmd/server`.
- [ ] Конфиг в `/etc/conf/onec-mcp.yml` из `configs/config.prod.yml.tpl`.
- [ ] `deploy/onec-mcp.service` → `/etc/systemd/system/`, `systemctl enable --now onec-mcp`.

## 4. Конфиг OAuth (ключевое)

- [ ] `oauth.enabled: true`.
- [ ] `oauth.public_url` = внешний HTTPS URL (без trailing slash).
- [ ] `onec.base_url` + Basic creds (`onec.auth`).
- [ ] `oauth.dev_access_key: ""` (пусто в проде).
- [ ] `mcp.bearer_token: ""` (при OAuth не нужен).
- [ ] `data/` писмо-доступна процессу.

## 5. Проверка

- [ ] `GET /health` → `{"status":"ok"}`.
- [ ] `GET /.well-known/oauth-authorization-server` отдаёт endpoint'ы.
- [ ] `GET /.well-known/oauth-protected-resource` отдаёт RS.

## 6. Доступ пользователю

- [ ] Запись в `MCP_Accounts`: сотрудник, `КлючДоступа` (`openssl rand -base64 32`), `Активен`, нужные скоупы.
- [ ] Ключ передан по защищённому каналу.
- [ ] Пользователь добавил connector с URL `https://mcp.example.com/mcp` и ввёл ключ.
