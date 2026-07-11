# Deploy checklist (OAuth)

Краткий чек-лист полного развёртывания гейта с OAuth. Детали — в [oauth-setup.md](oauth-setup.md).

## 1. 1С (разово, для каждой базы)

- [ ] Перечисление `MCPScopes`, справочник `MCP_Accounts`, URL-шаблон `/auth/verify` в HTTPService `MCP` — создано в Конфигураторе.
- [ ] Отдельный пользователь 1С под гейт (Basic).
- [ ] `GET /mcp/health` отвечает.

## 2. DNS + TLS

- [ ] A-запись `onec.nomadus.net` → сервер гейта. Одного хоста хватает на все базы — они разделяются слагом в пути.
- [ ] Reverse-proxy (nginx/Caddy) с HTTPS на `127.0.0.1:8088`. **HTTPS обязателен** (ChatGPT не примет HTTP).
- [ ] Прокси проксирует `/` целиком: под корнем живут и `/health`, и `/.well-known/*`, и все `/{slug}/*`.

## 3. Установка

Бинарники собираются автоматически в [GitHub Releases](https://github.com/ruslan-hut/onec-mcp/releases) на каждый тег `v*` — собирать локально не нужно.

- [ ] Скачать `onec-mcp-linux-amd64` из последнего релиза → `/opt/onec-mcp/onec-mcp`, `chmod +x`.
- [ ] Конфиг в `/etc/conf/onec-mcp.yml` из `configs/config.prod.yml.tpl`.
- [ ] `deploy/onec-mcp.service` → `/etc/systemd/system/`, `systemctl enable --now onec-mcp`.

Через GitHub Actions конфиг генерируется автоматически; нужны секреты `ADMIN_USERNAME`,
`ADMIN_PASSWORD`, `OAUTH_PUBLIC_URL` (остальные опциональны).

## 4. Конфиг (ключевое)

Базы 1С в конфиге не описываются — они в SQLite и заводятся через `/admin`.

- [ ] `admin.username` / `admin.password` заданы. Без них гейт не стартует: базы негде завести.
- [ ] `database.path` = постоянный путь (`/var/lib/onec-mcp/onec-mcp.db`), директория писмо-доступна процессу.
- [ ] `oauth.enabled: true`.
- [ ] `oauth.public_url` = внешний HTTPS URL, **корневой, без слага и без trailing slash**.
- [ ] Файл БД включён в бэкап: там и настройки баз (с паролями 1С), и активные токены.

## 5. Заведение баз

- [ ] `https://onec.nomadus.net/admin` открывается и просит логин.
- [ ] Для каждой базы: слаг (`[a-z0-9-]`, не `health` / `admin` / `mcp` / `oauth` / `.well-known`), имя, адрес 1С без `/mcp` в конце, Basic-логин и пароль.
- [ ] Dev-ключ пустой (в проде вход только по ключу из 1С).
- [ ] Статический токен MCP пустой (при OAuth не используется).

## 6. Проверка

- [ ] `GET /health` → `{"status":"ok"}`.
- [ ] На каждый слаг: `GET /.well-known/oauth-authorization-server/<slug>` отдаёт endpoint'ы с issuer `https://onec.nomadus.net/<slug>`.
- [ ] На каждый слаг: `GET /.well-known/oauth-protected-resource/<slug>/mcp` отдаёт RS с resource `https://onec.nomadus.net/<slug>/mcp`.
- [ ] В логе — `tenant registry reloaded` с ожидаемым `count`.

## 7. Доступ пользователю

- [ ] Запись в `MCP_Accounts` **нужной базы**: сотрудник, `КлючДоступа` (`openssl rand -base64 32`), `Активен`, нужные скоупы.
- [ ] Ключ передан по защищённому каналу.
- [ ] Пользователь добавил connector с URL `https://onec.nomadus.net/<slug>/mcp` и ввёл ключ. Доступ к нескольким базам = отдельный коннектор на каждую.
