package oauth

import (
	"context"
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
	_ = client

	s.renderLogin(w, req, "")
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
		s.renderLogin(w, req, "Access key is required")
		return
	}

	user, err := s.verifyAccessKey(r.Context(), accessKey)
	if err != nil {
		// Сообщение специально расплывчатое — не подсказывать атакующему, что не так
		s.logger.Warn("oauth.login.failed", "remote", r.RemoteAddr, "client_id", req.ClientID)
		s.renderLogin(w, req, "Invalid access key")
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

	now := time.Now()
	authCode := &AuthCode{
		Code:                code,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Sub:                 user.Sub,
		Scope:               allowedScope,
		Resource:            req.Resource,
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
	_ = client
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

	client, err := s.storage.GetClient(r.Context(), req.ClientID)
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

// verifyAccessKey выбирает стратегию: cfg.VerifyKey (прод, через 1С с кэшем)
// либо cfg.DevAccessKey (dev fallback). Если ни одна не настроена — всегда отказ.
func (s *Server) verifyAccessKey(ctx context.Context, key string) (*UserInfo, error) {
	if s.cfg.VerifyKey != nil {
		return s.cfg.VerifyKey(ctx, key)
	}
	if s.cfg.DevAccessKey != "" && key == s.cfg.DevAccessKey {
		return &UserInfo{
			Sub:    "dev-user",
			Name:   "Dev User",
			Scopes: s.cfg.SupportedScopes,
		}, nil
	}
	return nil, errors.New("invalid access key")
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>MCP Authorization</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 420px; margin: 80px auto; padding: 0 16px; color: #222; }
h1 { font-size: 20px; margin-bottom: 8px; }
p.sub { color: #666; margin-top: 0; font-size: 14px; }
form { display: flex; flex-direction: column; gap: 12px; margin-top: 24px; }
label { font-size: 13px; color: #444; }
input[type=password] { padding: 10px; font-size: 14px; border: 1px solid #ccc; border-radius: 6px; font-family: ui-monospace, monospace; }
button { padding: 10px; background: #111; color: #fff; border: 0; border-radius: 6px; font-size: 14px; cursor: pointer; }
button:hover { background: #333; }
.err { color: #b00020; font-size: 13px; }
.client { font-size: 13px; color: #555; }
</style>
</head>
<body>
<h1>Authorize MCP access</h1>
<p class="sub">Application <span class="client">{{.ClientName}}</span> wants to access your data via MCP.</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="POST" action="/oauth/authorize">
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

func (s *Server) renderLogin(w http.ResponseWriter, req authorizeRequest, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	clientName := ""
	if client, err := s.storage.GetClient(context.Background(), req.ClientID); err == nil {
		clientName = client.ClientName
		if clientName == "" {
			clientName = client.ClientID
		}
	}

	data := struct {
		Req        authorizeRequest
		Error      string
		ClientName string
	}{
		Req:        req,
		Error:      errMsg,
		ClientName: clientName,
	}
	_ = loginTemplate.Execute(w, data)
}

func (s *Server) renderError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = errorTemplate.Execute(w, msg)
}
