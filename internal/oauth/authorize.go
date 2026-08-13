package oauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authorizeRequest — параметры из query string на GET /oauth/authorize.
// Передаются в форму как hidden inputs и приходят обратно на POST.
type authorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
}

func parseAuthorizeRequest(v url.Values) authorizeRequest {
	return authorizeRequest{
		ResponseType:        v.Get("response_type"),
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		Scope:               v.Get("scope"),
		State:               v.Get("state"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
		Resource:            v.Get("resource"),
	}
}

// HandleAuthorize обслуживает GET (рендер формы) и POST (приём ключа и выпуск кода).
// Перед показом формы валидируем client_id и redirect_uri — если они невалидны,
// пользователю показываем ошибку, НЕ редиректим (защита от open-redirect).
func (s *Server) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.authorizeGET(w, r)
	case http.MethodPost:
		s.authorizePOST(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) authorizeGET(w http.ResponseWriter, r *http.Request) {
	req := parseAuthorizeRequest(r.URL.Query())

	client, err := s.validateAuthorizeParams(r, req)
	if err != nil {
		s.renderError(w, err.Error())
		return
	}

	s.renderLogin(w, req, client, "")
}

func (s *Server) authorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, "failed to parse form")
		return
	}
	req := parseAuthorizeRequest(r.PostForm)

	client, err := s.validateAuthorizeParams(r, req)
	if err != nil {
		s.renderError(w, err.Error())
		return
	}

	accessKey := strings.TrimSpace(r.PostForm.Get("access_key"))
	if accessKey == "" {
		s.renderLogin(w, req, client, "Access key is required")
		return
	}

	user, err := s.verifyAccessKey(r.Context(), accessKey)
	if err != nil {
		// Сообщение специально расплывчатое — не подсказывать атакующему, что не так
		s.logger.Warn("oauth.login.failed", "remote", r.RemoteAddr, "client_id", req.ClientID)
		s.renderLogin(w, req, client, "Invalid access key")
		return
	}

	// Финальный scope — пересечение запрошенного с разрешённым пользователю
	allowedScope := intersectScopes(req.Scope, user.Scopes)
	if allowedScope == "" {
		// Если ничего не пересеклось — выдаём пользовательские дефолты
		allowedScope = strings.Join(user.Scopes, " ")
	}

	code, err := randomToken()
	if err != nil {
		s.renderError(w, "internal error")
		return
	}

	// Resource фиксируем своим — AS обслуживает ровно одну базу, а совпадение с req.Resource
	// (если клиент его прислал) уже проверено в validateAuthorizeParams.
	now := time.Now()
	authCode := &AuthCode{
		Code:                code,
		Tenant:              s.cfg.Tenant,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Sub:                 user.Sub,
		Scope:               allowedScope,
		Resource:            s.resource(),
		ExpiresAt:           now.Add(s.cfg.AuthCodeTTL),
	}
	if err := s.storage.CreateAuthCode(r.Context(), authCode); err != nil {
		s.logger.Error("oauth.code.persist_failed", "error", err)
		s.renderError(w, "internal error")
		return
	}

	// Собираем callback c кодом и state — клиент проверит state у себя
	redirect, _ := url.Parse(req.RedirectURI)
	q := redirect.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	redirect.RawQuery = q.Encode()

	s.logger.Info("oauth.code.issued",
		"client_id", req.ClientID, "sub", user.Sub, "scope", allowedScope,
	)
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// validateAuthorizeParams проверяет client_id/redirect_uri/PKCE-параметры до показа формы
// или приёма ключа. Ошибки тут — пользовательские HTML-страницы, без редиректа.
func (s *Server) validateAuthorizeParams(r *http.Request, req authorizeRequest) (*Client, error) {
	if req.ResponseType != "code" {
		return nil, fmt.Errorf("response_type must be 'code'")
	}
	if req.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if req.RedirectURI == "" {
		return nil, fmt.Errorf("redirect_uri is required")
	}
	if req.CodeChallenge == "" {
		return nil, fmt.Errorf("code_challenge is required (PKCE)")
	}
	if req.CodeChallengeMethod == "" {
		// Дефолт по RFC 7636 — plain, но мы plain не поддерживаем
		return nil, fmt.Errorf("code_challenge_method=S256 is required")
	}
	if req.CodeChallengeMethod != "S256" {
		return nil, fmt.Errorf("unsupported code_challenge_method, only S256")
	}
	// RFC 8707: клиент вправе не прислать resource, но если прислал — он должен указывать
	// на эту базу. Иначе — попытка получить токен с чужим audience.
	if err := s.checkResource(req.Resource); err != nil {
		return nil, err
	}

	// Клиент ищется в пределах базы: client_id, зарегистрированный на другом слаге, здесь неизвестен.
	client, err := s.storage.GetClient(r.Context(), s.cfg.Tenant, req.ClientID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("unknown client_id")
		}
		s.logger.Error("oauth.client.lookup_failed", "error", err)
		return nil, fmt.Errorf("internal error")
	}

	// Точное совпадение redirect_uri с зарегистрированным — защита от open-redirect
	allowed := false
	for _, u := range client.RedirectURIs {
		if u == req.RedirectURI {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("redirect_uri does not match any registered URI for this client")
	}

	return client, nil
}

// verifyAccessKey проверяет введённый на форме ключ. Сначала dev-ключ базы, если он задан:
// это осознанный обход 1С, чтобы можно было поднять базу до того, как настроен /mcp/auth/verify.
// Проверять его вторым бессмысленно — VerifyKey задан всегда, и до fallback дело бы не дошло.
// В проде dev-ключ должен быть пустым (см. подсказку в /admin).
//
// Сравнение constant-time: ключ — секрет, и по времени ответа его не должно быть видно.
func (s *Server) verifyAccessKey(ctx context.Context, key string) (*UserInfo, error) {
	if s.cfg.DevAccessKey != "" &&
		subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.DevAccessKey)) == 1 {
		s.logger.Warn("oauth.login.dev_key_used", "tenant", s.cfg.Tenant)
		return &UserInfo{
			Sub:    "dev-user",
			Name:   "Dev User",
			Scopes: s.cfg.SupportedScopes,
		}, nil
	}
	if s.cfg.VerifyKey != nil {
		return s.cfg.VerifyKey(ctx, key)
	}
	return nil, errors.New("invalid access key")
}

// loginTemplate — форма ввода ключа. Имя базы — главный элемент страницы: пользователь может
// держать коннекторы к нескольким базам, и ключ от одной не подойдёт к другой. Рядом со слагом,
// чтобы можно было сверить его с URL коннектора и заметить, что открылась не та база.
var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.TenantName}} — MCP Authorization</title>
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 440px;
       margin: 72px auto; padding: 0 16px; color: #1a1a1a; background: #fff; }
h1 { font-size: 19px; margin: 0 0 20px; }
.db { border: 1px solid #e5e5e5; border-left: 4px solid #0b5fff; border-radius: 8px;
      padding: 14px 16px; background: #fafafa; }
.db-label { display: block; font-size: 11px; font-weight: 600; text-transform: uppercase;
            letter-spacing: .06em; color: #777; margin-bottom: 4px; }
.db-name { display: block; font-size: 22px; font-weight: 650; line-height: 1.25;
           word-break: break-word; }
.db-slug { display: inline-block; margin-top: 8px; font-family: ui-monospace, SFMono-Regular, monospace;
           font-size: 12px; color: #555; background: #ececf0; padding: 2px 7px; border-radius: 4px; }
p.sub { color: #666; font-size: 14px; margin: 16px 0 0; }
.client { font-weight: 600; color: #1a1a1a; }
form { display: flex; flex-direction: column; gap: 8px; margin-top: 24px; }
label { font-size: 13px; font-weight: 500; }
input[type=password] { padding: 10px 12px; font-size: 14px; border: 1px solid #ccc; border-radius: 6px;
                       font-family: ui-monospace, SFMono-Regular, monospace; background: #fff;
                       color: inherit; width: 100%; }
input[type=password]:focus { outline: 2px solid #0b5fff; outline-offset: -1px; border-color: #0b5fff; }
button { margin-top: 8px; padding: 11px; background: #111; color: #fff; border: 0; border-radius: 6px;
         font-size: 14px; font-weight: 500; cursor: pointer; }
button:hover { background: #333; }
.err { background: #fce8e6; color: #a50e0e; padding: 10px 12px; border-radius: 6px;
       font-size: 13px; margin: 16px 0 0; }
@media (prefers-color-scheme: dark) {
  body { background: #16181c; color: #e8e8e8; }
  .db { background: #1e2127; border-color: #2c2f36; border-left-color: #4c8dff; }
  .db-label { color: #999; }
  .db-slug { background: #2c2f36; color: #b5b5b5; }
  .client { color: #e8e8e8; }
  input[type=password] { background: #1e2127; border-color: #3a3f48; }
  .err { background: #3a1f1d; color: #f2b8b5; }
}
</style>
</head>
<body>
<h1>Authorize MCP access</h1>

<div class="db">
  <span class="db-label">1C database</span>
  <span class="db-name">{{.TenantName}}</span>
  <span class="db-slug">{{.Tenant}}</span>
</div>

<p class="sub">Application <span class="client">{{.ClientName}}</span> is requesting access to this
database. Enter the MCP key issued for it — a key from another database will not work here.</p>

{{if .Error}}<p class="err">{{.Error}}</p>{{end}}

<form method="POST" action="{{.ActionURL}}">
  <label for="access_key">MCP access key</label>
  <input id="access_key" name="access_key" type="password" autocomplete="off" autofocus required>
  <input type="hidden" name="response_type" value="{{.Req.ResponseType}}">
  <input type="hidden" name="client_id" value="{{.Req.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.Req.RedirectURI}}">
  <input type="hidden" name="scope" value="{{.Req.Scope}}">
  <input type="hidden" name="state" value="{{.Req.State}}">
  <input type="hidden" name="code_challenge" value="{{.Req.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="{{.Req.CodeChallengeMethod}}">
  <input type="hidden" name="resource" value="{{.Req.Resource}}">
  <button type="submit">Authorize</button>
</form>
</body>
</html>`))

var errorTemplate = template.Must(template.New("err").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorization error</title>
<style>body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;max-width:480px;margin:80px auto;padding:0 16px;color:#222}h1{color:#b00020;font-size:18px}p{font-size:14px}</style>
</head>
<body><h1>Authorization error</h1><p>{{.}}</p></body></html>`))

// renderLogin получает клиента параметром, а не читает его из БД сам: вызывающий только что
// достал его в validateAuthorizeParams. Второй запрос был бы лишним походом в SQLite на КАЖДЫЙ
// неаутентифицированный GET /oauth/authorize — а соединение одно на весь гейт, и такой трафик
// сериализуется с админкой и выпуском токенов всех остальных баз.
func (s *Server) renderLogin(w http.ResponseWriter, req authorizeRequest, client *Client, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	clientName := ""
	if client != nil {
		clientName = client.ClientName
		if clientName == "" {
			clientName = client.ClientID
		}
	}

	data := struct {
		Req        authorizeRequest
		Error      string
		ClientName string
		TenantName string
		Tenant     string
		ActionURL  string
	}{
		Req:        req,
		Error:      errMsg,
		ClientName: clientName,
		TenantName: s.cfg.TenantName,
		Tenant:     s.cfg.Tenant,
		ActionURL:  s.authorizeEndpoint(),
	}
	_ = loginTemplate.Execute(w, data)
}

func (s *Server) renderError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = errorTemplate.Execute(w, msg)
}
