# OAuth setup & admin guide

Гид по подключению гейта к Claude/ChatGPT через OAuth 2.1 и по администрированию доступа пользователей.

## Кратко об архитектуре

```
[Claude / ChatGPT]
       │
       │  OAuth 2.1 (DCR + PKCE)
       ▼
[Gateway: AS + RS]  ◄── SQLite (clients, codes, tokens)
       │
       │  Basic auth (server credentials)
       ▼
[1С HTTPService.MCP]
       │
       ▼
[Catalog.MCP_Accounts]  ←── per-user access key + scopes
```

Ключевые свойства:

- **Гейт совмещает Authorization Server и Resource Server.** Внешний IdP не нужен.
- **1С общается только с гейтом** под одним Basic-логином. Токены MCP-клиента в 1С не пробрасываются.
- **Управление пользователями MCP — в 1С**: справочник `MCP_Accounts` хранит ключ доступа и набор разрешённых инструментов на каждого сотрудника.
- **OAuth-токены — opaque**, хранятся в локальной SQLite гейта, истекают по TTL, refresh с ротацией.

---

## 1С: предварительные требования

Структура должна быть создана в Конфигураторе (это разовая операция).

### Перечисление `MCPScopes`

| Имя значения | Синоним (ru) | OAuth scope |
|---|---|---|
| `Resolve` | Поиск справочников | `mcp:resolve` |
| `SalesReport` | Отчёт продаж | `mcp:report:sales` |
| `StockReport` | Отчёт остатков | `mcp:report:stock` |

### Справочник `MCP_Accounts`

| Что | Тип | Примечание |
|---|---|---|
| Стандартное `Description` | Строка(150) | Название учётки (например, «Claude на ноутбуке Иванова») |
| Реквизит `Сотрудник` | СправочникСсылка.Сотрудники | Обязательное, `Indexing=Index` |
| Реквизит `КлючДоступа` | Строка(128), переменной | Plain-секрет; на форме `PasswordMode=True`; `Indexing=Index` |
| Реквизит `Активен` | Булево | Default = Истина |
| Реквизит `Комментарий` | Строка(500), переменной | Заметки админа |
| Табличная часть `Скоупы.Скоуп` | ПеречислениеСсылка.MCPScopes | Список разрешённых инструментов |

### HTTPService `MCP` — URL-шаблон `/auth/verify`

В существующем сервисе `MCP` добавить:

| Что | Значение |
|---|---|
| URL-шаблон → Имя | `AuthVerify` |
| URL-шаблон → Шаблон | `/auth/verify` |
| Метод → Имя | `Post` |
| Метод → HTTPMethod | `POST` |
| Метод → Handler | `AuthVerify` |

Код (`CommonModule.MCP` + handler `AuthVerify` в HTTPService) уже в репозитории — после создания структуры он сразу заработает.

---

## Гейт: конфигурация

Минимальная рабочая конфигурация в `configs/config.yml`:

```yaml
server:
  host: 0.0.0.0
  port: 8088

onec:
  base_url: "https://1c.example.com"        # БЕЗ /mcp в конце — добавится автоматически
  timeout_ms: 8000
  auth:
    type: "basic"
    username: "<1c-service-user>"            # отдельный пользователь 1С под гейт
    password: "<1c-service-password>"

mcp:
  enabled: true
  bearer_token: ""                            # пустой — при OAuth не нужен

oauth:
  enabled: true
  public_url: "https://mcp.example.com"      # внешний HTTPS URL гейта
  resource: ""                                # пусто = public_url + "/mcp"
  db_path: "data/oauth.db"
  access_token_ttl: "1h"
  refresh_token_ttl: "720h"                  # 30 дней
  auth_code_ttl: "10m"
  default_scopes:
    - "mcp:resolve"
    - "mcp:report:sales"
    - "mcp:report:stock"
  dev_access_key: ""                          # пусто в проде
  rate_limit:
    authorize_per_minute: 10
    register_per_minute: 30
    token_per_minute: 120
```

### Что проверить перед стартом

- `public_url` — это адрес, по которому MCP-клиент (Claude/ChatGPT) ходит на гейт; **обязательно HTTPS** в проде (ChatGPT не принимает HTTP).
- 1С `base_url` доступен из гейта; гейт-к-1С — Basic auth.
- `data/` — директория должна быть писмо-доступна процессу гейта; гейт создаст её сам.
- Резервная копия `data/oauth.db` имеет смысл (там зарегистрированные клиенты и активные токены), но потеря её просто заставит пользователей пере-логиниться.

### Запуск

```bash
go build -o /usr/local/bin/onec-mcp ./cmd/server
CONFIG_PATH=/etc/onec-mcp/config.yml onec-mcp
```

или через systemd / docker — на ваше усмотрение.

---

## Выдача доступа пользователю (админ в 1С)

1. Открыть справочник «Учётные записи MCP» (`MCP_Accounts`), создать новый элемент.
2. **Description**: говорящее имя, например `Claude / Иванов И.И.`.
3. **Сотрудник**: ссылка на сотрудника.
4. **КлючДоступа**: сгенерировать случайную строку, например:

   ```bash
   openssl rand -base64 32
   ```

   Результат вида `0bF7c1ck...` — это и есть «ключ MCP».
5. **Активен**: установить флажок.
6. **Табличная часть Скоупы**: добавить нужные значения перечисления MCPScopes. Минимум — `Resolve`, если пользователь должен только искать справочники; добавьте `SalesReport`/`StockReport` под нужные отчёты.
7. Сохранить.
8. Передать ключ пользователю **по защищённому каналу** (Bitwarden, лично, корпоративный мессенджер с e2e). Не отсылайте по email и не пишите в общий чат.

### Отзыв доступа

- Снять флажок `Активен` или удалить запись.
- Кэш гейта живёт 5 минут — после снятия флажка пользователь сможет ещё до 5 минут пользоваться текущим access token, плюс access token истечёт по TTL (1 час по умолчанию). Если нужно немедленно — перезапустить гейт.
- Для гарантированного отзыва конкретного токена прямо сейчас можно очистить вручную:

  ```bash
  sqlite3 data/oauth.db "DELETE FROM access_tokens WHERE sub = '<UUID учётки>';"
  sqlite3 data/oauth.db "UPDATE refresh_tokens SET revoked = 1 WHERE sub = '<UUID учётки>';"
  ```

### Ротация ключа

Просто сгенерируйте новый ключ в `КлючДоступа` и передайте пользователю. Старый перестанет работать в течение 5 минут (TTL кэша верификации).

---

## Подключение пользователя

### Claude (Pro / Max)

1. Settings → Connectors → **Add custom connector**.
2. **Remote MCP server URL**: `https://mcp.example.com/mcp` (точное значение даст администратор).
3. Нажать `Add`. Откроется браузерное окно с формой гейта.
4. В поле «MCP access key» вставить полученный от админа ключ. Нажать `Authorize`.
5. Connector активен — Claude автоматически зарегистрировался через DCR и получил токен.

### ChatGPT (Apps / Connectors)

1. Settings → Apps & Connectors → **Add custom connector**.
2. Указать URL гейта.
3. ChatGPT проведёт DCR + PKCE flow.
4. Откроется форма гейта — вставить ключ, авторизоваться.

### Что увидит пользователь после подключения

`tools/list` отфильтрован под скоупы пользователя — пользователь, у которого только `Resolve`, не увидит инструментов отчётов. Это снижает шанс, что LLM попробует вызвать запрещённое.

При попытке вызвать инструмент без нужного скоупа (если LLM всё-таки попытается) — придёт ответ типа `permission denied: tool "X" requires scope "Y"`, который LLM покажет пользователю.

---

## Эксплуатация

### Audit-лог

Все события audit пишутся в общий лог гейта со специальными msg-именами в slog-формате. Полная цепочка действий одного пользователя выбирается через:

```bash
grep -E 'msg=(oauth\.|mcp\.tool\.)' /var/log/onec-mcp.log | grep 'sub=<UUID учётки>'
```

| Событие | Когда пишется | Поля |
|---|---|---|
| `oauth.client.registered` | DCR от Claude/ChatGPT | `client_id`, `client_name` |
| `oauth.login.failed` | Неверный ключ на форме | `remote`, `client_id` |
| `oauth.code.issued` | Юзер ввёл правильный ключ | `client_id`, `sub`, `scope` |
| `oauth.token.issued` | Выпуск access/refresh | `grant_type`, `sub`, `client_id`, `scope`, `resource` |
| `oauth.token.refused` | 401 на /mcp | `reason` (`no_bearer`/`not_found`/`audience_mismatch`), `remote` |
| `oauth.scope.denied` | Tool вне скоупа токена | `tool`, `required`, `sub`, `have` |
| `mcp.tool.list` | Запрос списка инструментов | `sub`, `client_id`, `count` |
| `mcp.tool.call` | Любой вызов tools/call | `sub`, `client_id`, `tool`, `ok`, `duration_ms`, `reason?` |

### Rate-limit

По умолчанию пер-IP в окне 1 минута:

- `/oauth/authorize` POST — 10/мин (защищает от перебора ключей)
- `/oauth/register` — 30/мин
- `/oauth/token` — 120/мин

При превышении — 429 + `Retry-After`. Настраивается через `oauth.rate_limit.*`. `0` отключает лимит.

### TTL и ротация токенов

- Access token — 1 час по умолчанию.
- Refresh token — 30 дней с ротацией (старый помечается revoked при обмене; повторный обмен старым → `invalid_grant`).
- Auth code — 10 минут, одноразовый.

После истечения access token MCP-клиент сам молча обновится через refresh — пользователь ничего не заметит.

### Проверка живости

- Гейт: `GET /health` → `{"status":"ok"}` (публичный).
- 1С со стороны гейта: `GET /mcp/health` через тот же Basic — должен вернуть `status:"ok"` и время сервера.

### Метаданные для отладки

- `GET /.well-known/oauth-protected-resource` — что отдаёт RS.
- `GET /.well-known/oauth-authorization-server` — endpoint'ы AS и поддерживаемые опции.

---

## Troubleshooting

| Симптом | Что проверить |
|---|---|
| «Invalid access key» в форме | `Активен` в `MCP_Accounts`, точное совпадение ключа, не истёк ли `Account.DeletionMark` |
| `tools/list` пустой | Скоупы у пользователя в табличной части. Без скоупов → LLM ничего не видит. |
| `permission denied: tool X requires scope Y` | Добавить нужное значение в `Скоупы` и подождать TTL кэша (5 мин), либо перезапустить гейт |
| 401 на /mcp с `reason=audience_mismatch` | Несовпадение `resource` в токене с `oauth.resource` в конфиге. Обычно проявляется при смене `public_url` — пере-логин решает. |
| 401 с `reason=not_found` после паузы | Access token истёк. Клиент должен сам обновить — если не обновляет, проверить, что DCR прошёл и есть refresh token. |
| 429 | Лимит превышен. Подождать минуту или поднять лимит в конфиге. |
| 502 / `onec_error` | Проблема в 1С: проверить `GET /mcp/health` из гейта, доступность 1С, Basic-логин в `config.yml → onec.auth`. |
| `oauth.token.refused reason=audience_mismatch` без вашей конфигурации | Кто-то пытается использовать чужой токен — повод заглянуть в лог. |

### Откатить всё и начать заново

```bash
# Гейт: очистить хранилище OAuth (сбрасывает зарегистрированных клиентов и токены)
rm data/oauth.db

# 1С: удалить тестовые записи MCP_Accounts через интерфейс
```

После этого все пользователи переподключают connector.

---

## Совместимость

- Если `oauth.enabled = false` — гейт работает по старой схеме: статический `mcp.bearer_token` на `/mcp`, никаких OAuth-endpoint-ов. Удобно для локальной разработки и для тестов через `curl`.
- Когда `oauth.enabled = true`, статический Bearer на `/mcp` **отключается** автоматически — нельзя случайно оставить два пути аутентификации.
- REST-эндпоинты (`/resolve/*`, `/reports/*`) защищены отдельным `api.bearer_token` и НЕ зависят от OAuth — их аудитория это серверная интеграция, а не LLM-клиенты.
