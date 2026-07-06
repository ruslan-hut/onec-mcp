# Category Watchdog — контракт 1С ↔ гейт

Единый источник истины по доработкам под еженедельный отчёт «Category Watchdog»
(ТЗ BAF: статусные атрибуты номенклатуры + история стокаутов). Обе стороны —
1С (`CommonModules/MCP`, HTTP-сервис `MCP`) и гейт (`internal/mcp`, `internal/onec`) —
реализуют строго по этому документу. Коды статусов/рынков/сертификации стабильны и
не переводятся в отображаемые метки нигде, кроме поля `label`.

## Канонические коды

Вычисляются на стороне 1С из реквизитов карточки `Товары` (хелперы
`ProductStatusInfo` / `ProductMarkets` / `ProductCertificationInfo` в `CommonModules/MCP`).
Гейт коды не интерпретирует — только пробрасывает.

**status** (жизненный цикл):

| code | label | Правило вывода в 1С |
|---|---|---|
| `new` | Новинка | `Статус = ProductStatus.New` |
| `active` | Активна | `Статус = InUse` или не заполнен |
| `phasing_out` | На виводі | `Статус = ToExclude` и `НЕ НеВыгружатьНаСайт` |
| `excluded` | Виведена | `Статус = ToExclude` и `НеВыгружатьНаСайт` |

**markets** (массив; временная логика до согласования точной):

| Условие | markets |
|---|---|
| `ДоставкаЗаГраницуЗапрещена = Истина` | `["UA"]` |
| иначе | `["UA","EU","OTHER"]` |

**eu_certification**:

| code | label | Правило |
|---|---|---|
| `not_required` | Не потрібна | `НеТребуетСертификации = Истина` |
| `certified` | Є | иначе `Сертифицирован = Истина` |
| `in_process` | У процесі | иначе (нужна, не получена; «немає» и «у процесі» ТЗ слиты) |

`status_changed_at` — дата последней смены статуса из регистра `InformationRegister.ProductStatus`
(`SliceLast.Date`, YYYY-MM-DD); пусто, если статус не менялся с момента внедрения истории.

## 1. resolve_product — новые поля

Эндпойнт 1С `/mcp/resolve/product`, каждый элемент `candidates[]` дополнен:
`status {code,label}`, `status_changed_at` (string), `markets` ([]string),
`eu_certification {code,label}`. Все — `omitempty`.

- **Гейт:** ✅ готово — `ProductCandidate` (`internal/onec/models.go`) расширен, описание
  инструмента обновлено, проброс сквозной.
- **1С:** ✅ готово — `FindProducts` наполняет статусные поля в обеих ветках (UUID и основной
  запрос) через `ApplyProductStatusFields`; дата смены статуса — из `ProductStatus` одним срезом.

## 2. Фильтр product_status — stock_balance и sales_report

`filters.product_status` — массив кодов статуса (`new|active|phasing_out|excluded`).

- **Гейт:** ✅ готово — поле в `StockFilters` / `SalesFilters`, enum в схемах обоих инструментов.
- **1С:** ✅ готово — `ProductRefsByStatus` разворачивает коды в набор ссылок (пред-резолв),
  условие `Товар IN (&StatusProducts)` идёт по значению измерения в `Balance()`/`Turnovers()`,
  без условия на реквизит `Товар.Статус` (таблица итогов сохраняется).

## 3. product_details — новый инструмент (✅ обе стороны)

Пакетная выдача статусных атрибутов по списку позиций/групп.

- **1С:** ✅ реализовано — тип `product_details` в `ComposeReport` → `ProductDetails` (+ проекция
  `ProjectFields`). product_ids (позиции/группы) разворачиваются `IN HIERARCHY` до листьев,
  исключаются группы/удалённые/производственные, TOP 500. Статусные поля - через общие хелперы
  (`ApplyProductStatusFields`), дата - из `ProductStatus` одним срезом. Scope `mcp:resolve`.
- **Гейт:** ✅ реализовано — tool `product_details`, `ToolScopes: mcp:resolve`, клиент
  `ProductDetails` → `/mcp/reports/product_details`, ответ 1С проброс as-is (RawMessage).

Запрос:
```json
{ "product_ids": ["<uuid|group-uuid>", "..."], "fields": ["status","markets"] }
```
| Параметр | Тип | Описание |
|---|---|---|
| `product_ids` | array[uuid] | Позиции или группы (IN HIERARCHY). До 500 позиций в ответе (TOP). |
| `fields` | array[string] | Опц. Подмножество полей (id всегда). По умолчанию — все. |

Ответ: `{ "products": [ { id, label, code, group{id,label}, status{code,label},
status_changed_at, markets[], eu_certification{code,label} } ] }`.

## 4. availability_report — новый инструмент (✅ обе стороны)

История стокаутов (out-of-stock days) за период.

- **Источник в 1С:** регистр сведений `ОстаткиТоваровПоДням` (периодичность День; заполняется
  регламентом `ЗаполнитьОстаткиПоДням`; ресурс `День` = флаг наличия, `1` если `Остаток > 0`).
- **1С:** ✅ реализовано — тип `availability` в `ComposeReport` → `AvailabilityReport`.
  `days` считается по КАЛЕНДАРЮ (регистр хранит стокаут-дни разреженно, см. аудит регистра),
  `oos_days = days − SUM(День)`, базовый набор = пары Товар×Склад со строками в периоде
  (авто-исключение «мёртвых» SKU). **Ограничение v1:** позиция, стоявшая в нуле весь период
  (падение до окна), строк не имеет и не попадает в выдачу — ловить через resolve_product status.
- **Гейт:** ✅ реализовано — tool `availability_report`, `ToolScopes: mcp:report:stock`,
  клиент `AvailabilityReport` → `/mcp/reports/availability`, ответ 1С пробрасывается as-is (RawMessage).
- **Производительность (важно):** общий таймаут клиента к 1С был 8 с — для скана посуточного
  регистра по всему ассортименту мало (падало `context deadline exceeded`). Исправлено:
  (1) отчётные эндпойнты `/mcp/reports/*` идут через отдельный клиент с таймаутом
  `report_timeout_ms` (по умолч. 45000); (2) в 1С из тяжёлого запроса убран join `Товар.Parent` -
  группа добирается отдельным лёгким запросом только при `group_by=product_group`.

Запрос:
```json
{
  "period": {"from": "2026-06-01", "to": "2026-06-30"},
  "filters": {"product_ids": ["..."], "warehouse_ids": ["..."]},
  "group_by": ["product", "week"],
  "measures": ["oos_days", "days", "availability_pct", "avg_qty"],
  "sort": [{"field": "oos_days", "dir": "desc"}],
  "top": 100
}
```
| Параметр | Тип | Описание |
|---|---|---|
| `period` | {from,to} | Период (YYYY-MM-DD). |
| `filters.product_ids` | array[uuid] | Позиции/группы (IN HIERARCHY). |
| `filters.warehouse_ids` | array[uuid] | Склады. |
| `group_by` | array | `product`, `product_group`, `warehouse`, `week`. По умолчанию `product × warehouse`. |
| `measures` | array | `oos_days`, `days`, `availability_pct` = (days−oos_days)/days, `avg_qty`. |
| `sort`, `top` | — | Как в остальных отчётах. |

Ответ: `{columns, rows, totals}` — та же форма, что stock/sales.

Правила:
- Позиция без движений и остатка за весь период (никогда не была на складе) — в регистр не
  попадает → в отчёт не включается (мёртвые SKU не засоряют выдачу).
- Измерение `Фирма` в регистре не заполняется — фильтр по фирме не предлагать.
- Для контрольных сверок (июнь, 4 недели) убедиться, что история в регистре покрывает период;
  при пробелах — разовый бэкфилл `ЗаполнитьОстаткиПоДням(from, to)`.

## Матрица готовности

| Пункт | Гейт | 1С |
|---|---|---|
| Канонические коды / хелперы | n/a | ✅ |
| resolve_product поля | ✅ | ✅ |
| product_status фильтр (stock + sales) | ✅ | ✅ |
| product_details | ✅ | ✅ |
| availability_report | ✅ | ✅ |

Все пункты контракта реализованы с обеих сторон. Финальная приёмка — тестовый прогон отчёта
«Category Watchdog» на живых данных (после деплоя 1С + гейта).
