package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/mcp-sales-mvp/internal/store"
)

// Тесты гоняют полный flow против настоящего SQLite во временном файле: подменять Storage
// моком бессмысленно — половина проверяемых инвариантов (одноразовость кода, фильтр по tenant,
// ротация refresh) живёт именно в SQL.

const (
	testPublicURL   = "https://gw.example.com"
	testRedirectURI = "https://client.example.com/callback"
	testDevKey      = "dev-key-for-tests"
)

var testScopes = []string{"mcp:resolve", "mcp:report:sales", "mcp:report:cost"}

func testStorage(t *testing.T) *Storage {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "oauth_test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("init storage: %v", err)
	}
	return st
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer поднимает AS+RS одной базы. verify — стратегия проверки ключа; nil означает
// «принимать только dev-ключ».
func newTestServer(t *testing.T, st *Storage, tenant string, verify VerifyKeyFunc) *Server {
	t.Helper()
	srv, err := NewServer(Config{
		Tenant:          tenant,
		TenantName:      strings.ToUpper(tenant),
		PublicURL:       testPublicURL,
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		AuthCodeTTL:     10 * time.Minute,
		DefaultScopes:   testScopes,
		SupportedScopes: testScopes,
		DevAccessKey:    testDevKey,
		VerifyKey:       verify,
	}, st, testLogger())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

// pkcePair возвращает (verifier, S256 challenge).
func pkcePair(verifier string) (string, string) {
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// registerClient прогоняет DCR и возвращает выданный client_id.
func registerClient(t *testing.T, srv *Server) string {
	t.Helper()
	body := `{"client_name":"Test Client","redirect_uris":["` + testRedirectURI + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp ClientRegistrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("register: decode: %v", err)
	}
	if resp.ClientID == "" {
		t.Fatal("register: empty client_id")
	}
	return resp.ClientID
}

// authorizeParams — query/form набор для /oauth/authorize с валидными значениями по умолчанию.
func authorizeParams(clientID, challenge string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"scope":                 {strings.Join(testScopes, " ")},
		"state":                 {"opaque-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
}

// postAuthorize отправляет форму логина и возвращает recorder.
func postAuthorize(srv *Server, v url.Values, accessKey string) *httptest.ResponseRecorder {
	form := url.Values{}
	for k, vals := range v {
		form[k] = vals
	}
	form.Set("access_key", accessKey)

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.HandleAuthorize(rec, req)
	return rec
}

// codeFromRedirect достаёт code из Location успешного POST /authorize.
func codeFromRedirect(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize: status = %d, want 302 (body: %s)", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("authorize: parse Location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("authorize: no code in %s", rec.Header().Get("Location"))
	}
	return code
}

// exchangeCode обменивает код на токены.
func exchangeCode(srv *Server, code, clientID, verifier string) *httptest.ResponseRecorder {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.HandleToken(rec, req)
	return rec
}

func decodeTokens(t *testing.T, rec *httptest.ResponseRecorder) TokenResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("token: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("token: decode: %v", err)
	}
	return resp
}

// fullFlow прогоняет register → authorize → token и возвращает пару токенов и client_id.
func fullFlow(t *testing.T, srv *Server) (tokens TokenResponse, clientID string) {
	t.Helper()
	verifier, challenge := pkcePair("verifier-" + srv.Tenant() + "-0123456789abcdefghij")
	clientID = registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))
	return decodeTokens(t, exchangeCode(srv, code, clientID, verifier)), clientID
}

// protectedHandler — заглушка ресурса за Middleware: пишет sub и scope, которые до неё дошли.
func protectedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := FromContext(r.Context())
		if auth == nil {
			http.Error(w, "no auth in context", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, auth.Sub+"|"+auth.Scope)
	})
}

// callProtected дёргает защищённый ресурс с указанным bearer (пустой — без заголовка).
func callProtected(srv *Server, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Middleware(protectedHandler()).ServeHTTP(rec, req)
	return rec
}

// --- happy path ---

func TestFullAuthorizationCodeFlow(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)

	tokens, _ := fullFlow(t, srv)

	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("expected both access and refresh tokens")
	}
	if tokens.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tokens.TokenType)
	}
	if tokens.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want > 0", tokens.ExpiresIn)
	}

	rec := callProtected(srv, tokens.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("protected: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.HasPrefix(got, "dev-user|") {
		t.Errorf("protected: body = %q, want sub dev-user", got)
	}
}

func TestAuthorizeRedirectPreservesState(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-state-0123456789abcdefghij")
	clientID := registerClient(t, srv)

	rec := postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if got := loc.Query().Get("state"); got != "opaque-state" {
		t.Errorf("state = %q, want opaque-state — клиент не сможет сматчить свой запрос", got)
	}
}

// --- PKCE ---

func TestPKCEWrongVerifierRejected(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-real-0123456789abcdefghij")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))

	rec := exchangeCode(srv, code, clientID, "verifier-attacker-0123456789abcdef")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — перехваченный код не должен обмениваться без verifier", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("body = %s, want invalid_grant", rec.Body.String())
	}
}

func TestAuthorizeRejectsNonS256Challenge(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	clientID := registerClient(t, srv)

	for _, method := range []string{"plain", "", "S512"} {
		v := authorizeParams(clientID, "some-challenge")
		v.Set("code_challenge_method", method)

		rec := postAuthorize(srv, v, testDevKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("method=%q: status = %d, want 400", method, rec.Code)
		}
	}
}

func TestAuthorizeRequiresCodeChallenge(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	clientID := registerClient(t, srv)
	v := authorizeParams(clientID, "")

	rec := postAuthorize(srv, v, testDevKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — PKCE обязателен", rec.Code)
	}
}

func TestVerifyPKCE(t *testing.T) {
	verifier, challenge := pkcePair("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")

	if !verifyPKCE(verifier, challenge, "S256") {
		t.Error("valid S256 pair rejected")
	}
	if verifyPKCE("wrong", challenge, "S256") {
		t.Error("wrong verifier accepted")
	}
	if verifyPKCE(verifier, verifier, "plain") {
		t.Error("plain method accepted — поддерживается только S256")
	}
}

// --- одноразовость и срок жизни кода ---

func TestAuthCodeIsSingleUse(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	verifier, challenge := pkcePair("verifier-single-0123456789abcdefghij")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))

	if rec := exchangeCode(srv, code, clientID, verifier); rec.Code != http.StatusOK {
		t.Fatalf("first exchange: status = %d, want 200", rec.Code)
	}

	rec := exchangeCode(srv, code, clientID, verifier)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second exchange: status = %d, want 400 — код обязан быть одноразовым", rec.Code)
	}
}

func TestExpiredAuthCodeRejected(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", nil)
	verifier, challenge := pkcePair("verifier-expired-0123456789abcdefgh")
	clientID := registerClient(t, srv)

	// Код кладём напрямую — прокрутить время в flow иначе нечем.
	code := &AuthCode{
		Code:                "expired-code",
		Tenant:              "tenant1",
		ClientID:            clientID,
		RedirectURI:         testRedirectURI,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Sub:                 "dev-user",
		Scope:               strings.Join(testScopes, " "),
		Resource:            testPublicURL + "/tenant1/mcp",
		ExpiresAt:           time.Now().Add(-time.Minute),
	}
	if err := st.CreateAuthCode(context.Background(), code); err != nil {
		t.Fatalf("seed code: %v", err)
	}

	rec := exchangeCode(srv, "expired-code", clientID, verifier)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — просроченный код не должен обмениваться", rec.Code)
	}
}

// --- redirect_uri / open redirect ---

func TestAuthorizeRejectsUnregisteredRedirectURI(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-redir-0123456789abcdefghij")
	clientID := registerClient(t, srv)

	v := authorizeParams(clientID, challenge)
	v.Set("redirect_uri", "https://attacker.example.com/steal")

	rec := postAuthorize(srv, v, testDevKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — редирект на незарегистрированный URI = open redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, ошибка не должна редиректить вовсе", loc)
	}
}

func TestTokenRejectsMismatchedRedirectURI(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	verifier, challenge := pkcePair("verifier-mismatch-0123456789abcdef")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {"https://client.example.com/other"},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRegisterRejectsNonHTTPSRedirectURI(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)

	for _, uri := range []string{"http://attacker.example.com/cb", "ftp://x/y", "custom-scheme://cb"} {
		body := `{"redirect_uris":["` + uri + `"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.HandleRegister(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("uri=%s: status = %d, want 400", uri, rec.Code)
		}
	}

	// localhost по http разрешён — без него не поднять локальный коннектор.
	body := `{"redirect_uris":["http://localhost:8080/cb"]}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("localhost: status = %d, want 201", rec.Code)
	}
}

// --- изоляция баз ---

// TestTokenOfOneTenantRejectedByAnother — главный инвариант мультибазовости: токен, выпущенный
// для одной 1С, не должен открывать /mcp другой. Обе базы живут в одном Storage, так что тест
// ловит и дырку в фильтре по tenant, и дырку в audience-проверке.
func TestTokenOfOneTenantRejectedByAnother(t *testing.T) {
	st := testStorage(t)
	srv1 := newTestServer(t, st, "tenant1", nil)
	srv2 := newTestServer(t, st, "tenant2", nil)

	tokens1, _ := fullFlow(t, srv1)

	if rec := callProtected(srv1, tokens1.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("own tenant: status = %d, want 200", rec.Code)
	}

	rec := callProtected(srv2, tokens1.AccessToken)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign tenant: status = %d, want 401 — ключ одной базы не должен открывать другую", rec.Code)
	}
}

func TestClientOfOneTenantUnknownToAnother(t *testing.T) {
	st := testStorage(t)
	srv1 := newTestServer(t, st, "tenant1", nil)
	srv2 := newTestServer(t, st, "tenant2", nil)

	clientID := registerClient(t, srv1)
	_, challenge := pkcePair("verifier-cross-0123456789abcdefghij")

	rec := postAuthorize(srv2, authorizeParams(clientID, challenge), testDevKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — client_id чужой базы должен быть неизвестен", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown client_id") {
		t.Errorf("body = %s, want 'unknown client_id'", rec.Body.String())
	}
}

func TestAuthCodeOfOneTenantNotRedeemableByAnother(t *testing.T) {
	st := testStorage(t)
	srv1 := newTestServer(t, st, "tenant1", nil)
	srv2 := newTestServer(t, st, "tenant2", nil)

	verifier, challenge := pkcePair("verifier-code-cross-0123456789abcd")
	clientID := registerClient(t, srv1)
	code := codeFromRedirect(t, postAuthorize(srv1, authorizeParams(clientID, challenge), testDevKey))

	if rec := exchangeCode(srv2, code, clientID, verifier); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — код одной базы не должен обмениваться на другой", rec.Code)
	}
}

// --- audience (RFC 8707) ---

func TestAuthorizeRejectsForeignResource(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-res-0123456789abcdefghijk")
	clientID := registerClient(t, srv)

	v := authorizeParams(clientID, challenge)
	v.Set("resource", testPublicURL+"/tenant2/mcp")

	rec := postAuthorize(srv, v, testDevKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — нельзя просить токен с чужим audience", rec.Code)
	}
}

func TestAuthorizeAcceptsOwnResource(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-own-res-0123456789abcdefg")
	clientID := registerClient(t, srv)

	v := authorizeParams(clientID, challenge)
	v.Set("resource", testPublicURL+"/tenant1/mcp")

	if rec := postAuthorize(srv, v, testDevKey); rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenRejectsForeignResource(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	verifier, challenge := pkcePair("verifier-tok-res-0123456789abcdef")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {verifier},
		"resource":      {testPublicURL + "/tenant2/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestTokenWithStaleAudienceRejected — токен, выпущенный до мультибазовости (пустой resource),
// не должен приниматься: такой клиент обязан пройти авторизацию заново.
func TestTokenWithStaleAudienceRejected(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", nil)

	stale := &AccessToken{
		Token:     "stale-token",
		Tenant:    "tenant1",
		ClientID:  "some-client",
		Sub:       "user1",
		Scope:     "mcp:resolve",
		Resource:  "",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	if err := st.CreateAccessToken(context.Background(), stale); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if rec := callProtected(srv, "stale-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestForeignAudienceRejectedWithinOwnTenant проверяет ИМЕННО audience-рубеж, отдельно от
// фильтра по tenant: строка лежит под tenant1 и находится lookup'ом, но выписана на ресурс
// чужой базы. TestTokenOfOneTenantRejectedByAnother этот слой не покрывает — там запрос
// отсекается ещё в SQL, и проверка audience до дела не доходит.
func TestForeignAudienceRejectedWithinOwnTenant(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", nil)

	misaimed := &AccessToken{
		Token:     "misaimed-token",
		Tenant:    "tenant1",
		ClientID:  "some-client",
		Sub:       "user1",
		Scope:     "mcp:resolve",
		Resource:  testPublicURL + "/tenant2/mcp",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	if err := st.CreateAccessToken(context.Background(), misaimed); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if rec := callProtected(srv, "misaimed-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — audience обязан проверяться независимо от фильтра по tenant", rec.Code)
	}
}

// --- middleware ---

func TestMiddlewareRejectsMissingAndMalformedBearer(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)

	cases := []struct{ name, header string }{
		{"no header", ""},
		{"empty bearer", "Bearer "},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"no scheme", "just-a-token"},
		{"unknown token", "Bearer nonexistent-token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			srv.Middleware(protectedHandler()).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			// RFC 9728 §5.1: клиент по этому заголовку находит метаданные ресурса и строит flow.
			wa := rec.Header().Get("WWW-Authenticate")
			if !strings.Contains(wa, "resource_metadata=") {
				t.Errorf("WWW-Authenticate = %q, want resource_metadata", wa)
			}
		})
	}
}

func TestMiddlewareRejectsExpiredToken(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", nil)

	expired := &AccessToken{
		Token:     "expired-token",
		Tenant:    "tenant1",
		ClientID:  "some-client",
		Sub:       "user1",
		Scope:     "mcp:resolve",
		Resource:  testPublicURL + "/tenant1/mcp",
		ExpiresAt: time.Now().Add(-time.Minute),
		CreatedAt: time.Now().Add(-time.Hour),
	}
	if err := st.CreateAccessToken(context.Background(), expired); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if rec := callProtected(srv, "expired-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddlewarePopulatesAuthContext(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, _ := fullFlow(t, srv)

	rec := callProtected(srv, tokens.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	parts := strings.SplitN(rec.Body.String(), "|", 2)
	if parts[0] != "dev-user" {
		t.Errorf("sub = %q, want dev-user", parts[0])
	}
	if !strings.Contains(parts[1], "mcp:resolve") {
		t.Errorf("scope = %q, want mcp:resolve", parts[1])
	}
}

// --- refresh ---

func TestRefreshRotatesAndInvalidatesOldToken(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, clientID := fullFlow(t, srv)

	rec := postRefresh(srv, tokens.RefreshToken, clientID, "")
	refreshed := decodeTokens(t, rec)

	if refreshed.RefreshToken == tokens.RefreshToken {
		t.Error("refresh token не ротирован — старый остался в обороте")
	}
	if refreshed.AccessToken == tokens.AccessToken {
		t.Error("access token не обновлён")
	}
	if r := callProtected(srv, refreshed.AccessToken); r.Code != http.StatusOK {
		t.Errorf("new access token: status = %d, want 200", r.Code)
	}

	// Повторное использование израсходованного refresh — replay.
	replay := postRefresh(srv, tokens.RefreshToken, clientID, "")
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay: status = %d, want 400 — использованный refresh должен быть мёртв", replay.Code)
	}
}

func TestRefreshCannotWidenScope(t *testing.T) {
	st := testStorage(t)
	// База выдаёт узкий набор прав: пользователь получает только resolve.
	srv := newTestServer(t, st, "tenant1", func(ctx context.Context, key string) (*UserInfo, error) {
		return &UserInfo{Sub: "narrow-user", Scopes: []string{"mcp:resolve"}}, nil
	})

	verifier, challenge := pkcePair("verifier-narrow-0123456789abcdefgh")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), "any-key"))
	tokens := decodeTokens(t, exchangeCode(srv, code, clientID, verifier))

	if tokens.Scope != "mcp:resolve" {
		t.Fatalf("initial scope = %q, want mcp:resolve — scope обязан быть пересечением с правами пользователя", tokens.Scope)
	}

	// Просим на refresh то, чего в исходном гранте не было.
	rec := postRefresh(srv, tokens.RefreshToken, clientID, "mcp:report:cost")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — refresh не должен расширять права", rec.Code)
	}
}

func TestRefreshCanNarrowScope(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, clientID := fullFlow(t, srv)

	refreshed := decodeTokens(t, postRefresh(srv, tokens.RefreshToken, clientID, "mcp:resolve"))
	if refreshed.Scope != "mcp:resolve" {
		t.Errorf("scope = %q, want mcp:resolve", refreshed.Scope)
	}
}

func TestRefreshRejectsMismatchedClient(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, _ := fullFlow(t, srv)

	rec := postRefresh(srv, tokens.RefreshToken, "other-client-id", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — refresh чужого клиента", rec.Code)
	}
}

func postRefresh(srv *Server, refreshToken, clientID, scope string) *httptest.ResponseRecorder {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.HandleToken(rec, req)
	return rec
}

// --- проверка ключа на форме логина ---

func TestAuthorizeRejectsBadAccessKey(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	_, challenge := pkcePair("verifier-badkey-0123456789abcdefgh")
	clientID := registerClient(t, srv)

	rec := postAuthorize(srv, authorizeParams(clientID, challenge), "wrong-key")
	// Форма перерисовывается с ошибкой — не редирект и не выдача кода.
	if rec.Code == http.StatusFound {
		t.Fatal("неверный ключ не должен выдавать код")
	}
	if !strings.Contains(rec.Body.String(), "Invalid access key") {
		t.Errorf("body не содержит сообщения об ошибке: %s", rec.Body.String())
	}
}

func TestAuthorizeScopeIsIntersectionWithUserRights(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", func(ctx context.Context, key string) (*UserInfo, error) {
		return &UserInfo{Sub: "sales-user", Scopes: []string{"mcp:resolve", "mcp:report:sales"}}, nil
	})

	verifier, challenge := pkcePair("verifier-intersect-0123456789abcde")
	clientID := registerClient(t, srv)

	// Клиент просит в том числе cost, которого у пользователя нет.
	v := authorizeParams(clientID, challenge)
	v.Set("scope", "mcp:resolve mcp:report:cost")

	code := codeFromRedirect(t, postAuthorize(srv, v, "any-key"))
	tokens := decodeTokens(t, exchangeCode(srv, code, clientID, verifier))

	if strings.Contains(tokens.Scope, "mcp:report:cost") {
		t.Errorf("scope = %q — выдано право, которого у пользователя нет", tokens.Scope)
	}
	if !strings.Contains(tokens.Scope, "mcp:resolve") {
		t.Errorf("scope = %q, want mcp:resolve", tokens.Scope)
	}
}

func TestVerifyKeyErrorDeniesAccess(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", func(ctx context.Context, key string) (*UserInfo, error) {
		return nil, context.DeadlineExceeded // 1С недоступна
	})

	_, challenge := pkcePair("verifier-1cdown-0123456789abcdefgh")
	clientID := registerClient(t, srv)

	// Ключ отличается от dev-ключа, значит уходит в VerifyKey и получает отказ.
	rec := postAuthorize(srv, authorizeParams(clientID, challenge), "some-real-key")
	if rec.Code == http.StatusFound {
		t.Fatal("при недоступной 1С код выдаваться не должен")
	}
}

// --- scope helpers ---

func TestScopesFromString(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a b c", []string{"a", "b", "c"}},
		{"a  b", []string{"a", "b"}},
		{" a b ", []string{"a", "b"}},
	}

	for _, tc := range cases {
		got := ScopesFromString(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ScopesFromString(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ScopesFromString(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestIntersectScopes(t *testing.T) {
	supported := []string{"mcp:resolve", "mcp:report:sales"}

	cases := []struct {
		requested string
		want      string
	}{
		{"", "mcp:resolve mcp:report:sales"},                             // не запросили — отдаём всё поддерживаемое
		{"mcp:resolve", "mcp:resolve"},                                   // подмножество
		{"mcp:resolve mcp:report:cost", "mcp:resolve"},                   // лишнее отбрасывается
		{"mcp:admin:eventlog", ""},                                       // ничего не пересеклось
		{"mcp:report:sales mcp:resolve", "mcp:report:sales mcp:resolve"}, // порядок запрошенного
	}

	for _, tc := range cases {
		if got := intersectScopes(tc.requested, supported); got != tc.want {
			t.Errorf("intersectScopes(%q) = %q, want %q", tc.requested, got, tc.want)
		}
	}
}

func TestHasScope(t *testing.T) {
	auth := &AuthInfo{Scopes: []string{"mcp:resolve", "mcp:report:sales"}}

	if !auth.HasScope("mcp:resolve") {
		t.Error("HasScope(mcp:resolve) = false")
	}
	if auth.HasScope("mcp:report:cost") {
		t.Error("HasScope(mcp:report:cost) = true — права нет")
	}
	if auth.HasScope("") {
		t.Error("HasScope(\"\") = true")
	}
}

// --- кэш верификации ключей ---

func TestCachedVerifierCachesAndIsolatesKeys(t *testing.T) {
	calls := 0
	cv := NewCachedVerifier(func(ctx context.Context, key string) (*UserInfo, error) {
		calls++
		return &UserInfo{Sub: "user-" + key, Scopes: []string{"mcp:resolve"}}, nil
	}, time.Minute)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		info, err := cv.Verify(ctx, "key1")
		if err != nil || info.Sub != "user-key1" {
			t.Fatalf("Verify(key1) = %v, %v", info, err)
		}
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 — повторные проверки должны идти из кэша", calls)
	}

	// Другой ключ — отдельная запись, а не переиспользование чужого результата.
	info, err := cv.Verify(ctx, "key2")
	if err != nil {
		t.Fatalf("Verify(key2): %v", err)
	}
	if info.Sub != "user-key2" {
		t.Errorf("sub = %q, want user-key2 — кэш перепутал ключи", info.Sub)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestCachedVerifierCleanupDropsExpired(t *testing.T) {
	cv := NewCachedVerifier(func(ctx context.Context, key string) (*UserInfo, error) {
		return &UserInfo{Sub: "u", Scopes: nil}, nil
	}, time.Nanosecond)

	if _, err := cv.Verify(context.Background(), "key1"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	time.Sleep(time.Millisecond)
	cv.Cleanup()

	cv.mu.Lock()
	n := len(cv.entries)
	cv.mu.Unlock()
	if n != 0 {
		t.Errorf("entries = %d, want 0 — иначе map растёт по числу проверенных ключей за uptime", n)
	}
}

// --- storage ---

func TestGetClientIsTenantScoped(t *testing.T) {
	st := testStorage(t)
	ctx := context.Background()

	c := &Client{
		ClientID:                "shared-id",
		Tenant:                  "tenant1",
		RedirectURIs:            []string{testRedirectURI},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		CreatedAt:               time.Now(),
	}
	if err := st.CreateClient(ctx, c); err != nil {
		t.Fatalf("create client: %v", err)
	}

	if _, err := st.GetClient(ctx, "tenant1", "shared-id"); err != nil {
		t.Errorf("own tenant: %v", err)
	}
	if _, err := st.GetClient(ctx, "tenant2", "shared-id"); err == nil {
		t.Error("клиент виден из чужой базы")
	}
}

func TestCleanupExpiredRemovesStaleRows(t *testing.T) {
	st := testStorage(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	if err := st.CreateAuthCode(ctx, &AuthCode{
		Code: "old-code", Tenant: "tenant1", ClientID: "c", RedirectURI: testRedirectURI,
		CodeChallenge: "ch", CodeChallengeMethod: "S256", Sub: "u", Scope: "mcp:resolve",
		ExpiresAt: past,
	}); err != nil {
		t.Fatalf("seed code: %v", err)
	}
	if err := st.CreateAccessToken(ctx, &AccessToken{
		Token: "old-token", Tenant: "tenant1", ClientID: "c", Sub: "u", Scope: "mcp:resolve",
		Resource: testPublicURL + "/tenant1/mcp", ExpiresAt: past, CreatedAt: past,
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := st.CleanupExpired(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := st.ConsumeAuthCode(ctx, "tenant1", "old-code"); err == nil {
		t.Error("просроченный код пережил чистку")
	}
	if _, err := st.GetActiveAccessToken(ctx, "tenant1", "old-token"); err == nil {
		t.Error("просроченный токен пережил чистку")
	}
}
