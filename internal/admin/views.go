package admin

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"example.com/mcp-sales-mvp/internal/tenant"
)

type listRow struct {
	Tenant *tenant.Tenant
	MCPURL string
}

type listData struct {
	Tenants []listRow
	Notice  string
}

type formData struct {
	T *tenant.Tenant
	// IsNew различает создание и редактирование: у существующей базы слаг не редактируется.
	IsNew bool
	Error string
	// ScopesPlaceholder — глобальный список scope, применяемый когда у базы свой не задан.
	ScopesPlaceholder string
	OAuthEnabled      bool
}

func (h *Handler) formData(t *tenant.Tenant, isNew bool, errMsg string) formData {
	return formData{
		T:                 t,
		IsNew:             isNew,
		Error:             errMsg,
		ScopesPlaceholder: strings.Join(h.cfg.GlobalScopes, " "),
		OAuthEnabled:      h.cfg.OAuthEnabled,
	}
}

func (h *Handler) render(w http.ResponseWriter, tpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.Execute(w, data); err != nil {
		h.logger.Error("admin.render.failed", "error", err)
	}
}

func (h *Handler) notFound(w http.ResponseWriter, err error) {
	if !errors.Is(err, tenant.ErrNotFound) {
		h.logger.Error("admin.lookup.failed", "error", err)
	}
	http.Error(w, "база не найдена", http.StatusNotFound)
}

func (h *Handler) serverError(w http.ResponseWriter, msg string, err error) {
	h.logger.Error("admin.error", "msg", msg, "error", err)
	http.Error(w, msg, http.StatusInternalServerError)
}

const styles = `
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 900px;
       margin: 40px auto; padding: 0 20px; color: #1a1a1a; background: #fff; }
h1 { font-size: 22px; margin: 0 0 4px; }
p.sub { color: #666; font-size: 14px; margin: 0 0 24px; }
a { color: #0b5fff; text-decoration: none; }
a:hover { text-decoration: underline; }
table { width: 100%; border-collapse: collapse; font-size: 14px; }
th, td { text-align: left; padding: 10px 12px; border-bottom: 1px solid #e5e5e5; vertical-align: top; }
th { font-weight: 600; color: #555; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
code { font-family: ui-monospace, SFMono-Regular, monospace; font-size: 13px; background: #f4f4f5;
       padding: 2px 5px; border-radius: 4px; }
.badge { font-size: 12px; padding: 2px 8px; border-radius: 999px; }
.on  { background: #e6f4ea; color: #137333; }
.off { background: #fce8e6; color: #a50e0e; }
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.btn { display: inline-block; padding: 8px 14px; border-radius: 6px; border: 0; font-size: 14px;
       cursor: pointer; background: #111; color: #fff; }
.btn:hover { background: #333; text-decoration: none; }
.btn.danger { background: #fff; color: #a50e0e; border: 1px solid #f0c3bf; }
.btn.danger:hover { background: #fce8e6; }
.btn.ghost { background: #fff; color: #1a1a1a; border: 1px solid #d5d5d5; }
.btn.ghost:hover { background: #f4f4f5; }
form.editor { display: grid; gap: 18px; margin-top: 24px; }
fieldset { border: 1px solid #e5e5e5; border-radius: 8px; padding: 16px 18px; }
legend { font-size: 12px; font-weight: 600; color: #555; text-transform: uppercase;
         letter-spacing: .04em; padding: 0 6px; }
.field { display: grid; gap: 5px; margin-bottom: 14px; }
.field:last-child { margin-bottom: 0; }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
label { font-size: 13px; font-weight: 500; }
.hint { font-size: 12px; color: #777; }
input[type=text], input[type=password], input[type=number], textarea {
  padding: 8px 10px; font-size: 14px; border: 1px solid #ccc; border-radius: 6px;
  font-family: inherit; width: 100%; background: #fff; color: inherit; }
input:disabled { background: #f4f4f5; color: #777; }
textarea { font-family: ui-monospace, SFMono-Regular, monospace; font-size: 13px; }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: auto; }
.err { background: #fce8e6; color: #a50e0e; padding: 10px 12px; border-radius: 6px; font-size: 14px; }
.notice { background: #e6f4ea; color: #137333; padding: 10px 12px; border-radius: 6px;
          font-size: 14px; margin-bottom: 16px; }
.actions { display: flex; gap: 10px; align-items: center; }
.empty { padding: 40px; text-align: center; color: #777; border: 1px dashed #ddd; border-radius: 8px; }
@media (prefers-color-scheme: dark) {
  body { background: #16181c; color: #e8e8e8; }
  th { color: #999; }
  th, td { border-color: #2c2f36; }
  code { background: #24272e; }
  fieldset { border-color: #2c2f36; }
  input[type=text], input[type=password], input[type=number], textarea {
    background: #1e2127; border-color: #3a3f48; }
  input:disabled { background: #24272e; color: #888; }
  .btn.ghost { background: #1e2127; color: #e8e8e8; border-color: #3a3f48; }
  .btn.danger { background: #1e2127; border-color: #5a2a26; }
}
`

var listTemplate = template.Must(template.New("list").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Базы 1С — onec-mcp</title>
<style>` + styles + `</style>
</head>
<body>
<div class="toolbar">
  <div>
    <h1>Базы 1С</h1>
    <p class="sub">Слаг базы — первый сегмент пути: <code>/{слаг}/mcp</code></p>
  </div>
  <a class="btn" href="/admin/new">Добавить базу</a>
</div>

{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}

{{if .Tenants}}
<table>
  <thead>
    <tr><th>Слаг</th><th>Имя</th><th>1С</th><th>URL коннектора</th><th>Статус</th><th></th></tr>
  </thead>
  <tbody>
  {{range .Tenants}}
    <tr>
      <td><code>{{.Tenant.Slug}}</code></td>
      <td>{{.Tenant.Name}}</td>
      <td>{{.Tenant.BaseURL}}</td>
      <td>{{if .MCPURL}}<code>{{.MCPURL}}</code>{{else}}<span class="hint">задайте oauth.public_url</span>{{end}}</td>
      <td>
        {{if .Tenant.Enabled}}<span class="badge on">включена</span>
        {{else}}<span class="badge off">выключена</span>{{end}}
      </td>
      <td><a href="/admin/{{.Tenant.Slug}}">Изменить</a></td>
    </tr>
  {{end}}
  </tbody>
</table>
{{else}}
<div class="empty">Пока нет ни одной базы. Гейт не обслуживает ни одного маршрута, кроме этого.</div>
{{end}}
</body>
</html>`))

var formTemplate = template.Must(template.New("form").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .IsNew}}Новая база{{else}}{{.T.Slug}}{{end}} — onec-mcp</title>
<style>` + styles + `</style>
</head>
<body>
<h1>{{if .IsNew}}Новая база{{else}}Изменение базы {{.T.Slug}}{{end}}</h1>
<p class="sub"><a href="/admin/">← ко всем базам</a></p>

{{if .Error}}<div class="err">{{.Error}}</div>{{end}}

<form class="editor" method="POST" action="{{if .IsNew}}/admin/new{{else}}/admin/{{.T.Slug}}{{end}}">

  <fieldset>
    <legend>Основное</legend>
    <div class="row">
      <div class="field">
        <label for="slug">Слаг</label>
        <input id="slug" name="slug" type="text" value="{{.T.Slug}}"
               {{if not .IsNew}}disabled{{else}}required{{end}}
               pattern="[a-z0-9][a-z0-9-]*" autocomplete="off">
        <span class="hint">
          {{if .IsNew}}Строчные латинские буквы, цифры, дефис. Попадёт в URL и в OAuth issuer.
          {{else}}Слаг менять нельзя: он зашит в URL коннекторов и в привязку выпущенных токенов.{{end}}
        </span>
      </div>
      <div class="field">
        <label for="name">Имя базы</label>
        <input id="name" name="name" type="text" value="{{.T.Name}}" autocomplete="off">
        <span class="hint">Показывается пользователю на форме авторизации.</span>
      </div>
    </div>
    <div class="field check">
      <input id="enabled" name="enabled" type="checkbox" value="1" {{if .T.Enabled}}checked{{end}}>
      <label for="enabled">Включена</label>
    </div>
    <span class="hint">Выключенная база не отдаёт ни одного маршрута — быстрый способ отрезать доступ, не удаляя настройки.</span>
  </fieldset>

  <fieldset>
    <legend>Подключение к 1С (Basic)</legend>
    <div class="field">
      <label for="base_url">Адрес базы</label>
      <input id="base_url" name="base_url" type="text" value="{{.T.BaseURL}}" required
             placeholder="https://1c.example.com/api" autocomplete="off">
      <span class="hint">Без <code>/mcp</code> в конце — он добавляется автоматически.</span>
    </div>
    <div class="row">
      <div class="field">
        <label for="username">Пользователь</label>
        <input id="username" name="username" type="text" value="{{.T.Username}}" required autocomplete="off">
      </div>
      <div class="field">
        <label for="password">Пароль</label>
        <input id="password" name="password" type="password" autocomplete="new-password"
               {{if .IsNew}}required{{end}}>
        <span class="hint">{{if .IsNew}}Хранится в БД в открытом виде.{{else}}Пусто — оставить текущий пароль.{{end}}</span>
      </div>
    </div>
  </fieldset>

  <fieldset>
    <legend>Таймауты и кэш</legend>
    <div class="row">
      <div class="field">
        <label for="timeout_ms">Таймаут, мс</label>
        <input id="timeout_ms" name="timeout_ms" type="number" value="{{.T.TimeoutMs}}">
        <span class="hint">Резолвы и проверка ключа.</span>
      </div>
      <div class="field">
        <label for="report_timeout_ms">Таймаут отчётов, мс</label>
        <input id="report_timeout_ms" name="report_timeout_ms" type="number" value="{{.T.ReportTimeoutMs}}">
        <span class="hint">Отчёты сканируют регистры и легально дольше.</span>
      </div>
    </div>
    <div class="field">
      <label for="resolve_cache_ttl_sec">TTL кэша резолвов, сек</label>
      <input id="resolve_cache_ttl_sec" name="resolve_cache_ttl_sec" type="number" value="{{.T.ResolveCacheTTLSec}}">
      <span class="hint">Отрицательное значение отключает кэш.</span>
    </div>
  </fieldset>

  <fieldset>
    <legend>Доступ</legend>
    <div class="field">
      <label for="mcp_token">Статический токен MCP</label>
      <input id="mcp_token" name="mcp_token" type="text" value="{{.T.MCPToken}}" autocomplete="off">
      <span class="hint">
        {{if .OAuthEnabled}}OAuth включён — это поле игнорируется.
        {{else}}Bearer для <code>/{слаг}/mcp</code>. Обязателен, пока OAuth выключен.{{end}}
      </span>
    </div>
    <div class="field">
      <label for="api_token">Токен REST API</label>
      <input id="api_token" name="api_token" type="text" value="{{.T.APIToken}}" autocomplete="off">
      <span class="hint">Пусто — REST-маршруты <code>/{слаг}/resolve/*</code> и <code>/{слаг}/reports/*</code> не публикуются.</span>
    </div>
    <div class="field">
      <label for="dev_access_key">Dev-ключ авторизации</label>
      <input id="dev_access_key" name="dev_access_key" type="text" value="{{.T.DevAccessKey}}" autocomplete="off">
      <span class="hint">
        Принимается на форме авторизации <strong>вместо</strong> ключа из 1С — чтобы поднять базу
        до того, как настроен <code>/mcp/auth/verify</code>. Обязательно пусто в проде.
      </span>
    </div>
  </fieldset>

  <fieldset>
    <legend>Scope</legend>
    <div class="field">
      <label for="default_scopes">Выдаваемые по умолчанию</label>
      <textarea id="default_scopes" name="default_scopes" rows="2"
                placeholder="{{.ScopesPlaceholder}}">{{range $i, $s := .T.DefaultScopes}}{{if $i}} {{end}}{{$s}}{{end}}</textarea>
    </div>
    <div class="field">
      <label for="supported_scopes">Допустимые</label>
      <textarea id="supported_scopes" name="supported_scopes" rows="2"
                placeholder="{{.ScopesPlaceholder}}">{{range $i, $s := .T.SupportedScopes}}{{if $i}} {{end}}{{$s}}{{end}}</textarea>
      <span class="hint">Пусто — берутся глобальные из конфига. Через пробел, запятую или с новой строки.</span>
    </div>
  </fieldset>

  <fieldset>
    <legend>Заголовок тенанта 1С</legend>
    <div class="row">
      <div class="field">
        <label for="tenant_header">Имя заголовка</label>
        <input id="tenant_header" name="tenant_header" type="text" value="{{.T.TenantHeader}}" autocomplete="off">
      </div>
      <div class="field">
        <label for="default_tenant">Значение</label>
        <input id="default_tenant" name="default_tenant" type="text" value="{{.T.DefaultTenant}}" autocomplete="off">
      </div>
    </div>
    <span class="hint">Прокидывается в каждый запрос к 1С, если оба поля заданы. Обычно не нужен.</span>
  </fieldset>

  <div class="actions">
    <button class="btn" type="submit">Сохранить</button>
    <a class="btn ghost" href="/admin/">Отмена</a>
  </div>
</form>

{{if not .IsNew}}
<form method="POST" action="/admin/{{.T.Slug}}/delete" style="margin-top:28px"
      onsubmit="return confirm('Удалить базу {{.T.Slug}}? Коннекторы, настроенные на неё, перестанут работать.')">
  <button class="btn danger" type="submit">Удалить базу</button>
</form>
{{end}}

</body>
</html>`))
