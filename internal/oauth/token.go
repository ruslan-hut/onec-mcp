package oauth

import (
	"context"
	"net/http"
	"time"
)

// TokenResponse — стандартный ответ OAuth 2.1 §4.1.4. Кодируется в JSON.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// HandleToken — POST /oauth/token. Принимает application/x-www-form-urlencoded,
// маршрутизирует по grant_type. Никакой кэш на ответы — Cache-Control: no-store.
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "failed to parse body")
		return
	}

	grantType := r.PostForm.Get("grant_type")
	switch grantType {
	case "authorization_code":
		s.tokenAuthorizationCode(w, r)
	case "refresh_token":
		s.tokenRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			"only authorization_code and refresh_token are supported")
	}
}

func (s *Server) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.PostForm.Get("code")
	clientID := r.PostForm.Get("client_id")
	redirectURI := r.PostForm.Get("redirect_uri")
	codeVerifier := r.PostForm.Get("code_verifier")
	resource := r.PostForm.Get("resource")

	if code == "" || clientID == "" || redirectURI == "" || codeVerifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"code, client_id, redirect_uri, code_verifier are required")
		return
	}

	// Consume — атомарно достаём и удаляем код, чтобы исключить повторное использование
	authCode, err := s.storage.ConsumeAuthCode(r.Context(), code)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid or expired")
		return
	}

	if authCode.ClientID != clientID || authCode.RedirectURI != redirectURI {
		// Несовпадение — клиент пытается обменять чужой код или подменить redirect
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code does not match client/redirect")
		return
	}

	// PKCE: только S256, ConstantTimeCompare хешей
	if !verifyPKCE(codeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	// Resource (RFC 8707): если был указан при /authorize — должен совпасть на /token
	if authCode.Resource != "" && resource != "" && authCode.Resource != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource mismatch")
		return
	}
	effectiveResource := authCode.Resource
	if effectiveResource == "" {
		effectiveResource = resource
	}
	if effectiveResource == "" {
		effectiveResource = s.cfg.Resource
	}

	access, refresh, err := s.issueTokens(r.Context(), clientID, authCode.Sub, authCode.Scope, effectiveResource, "")
	if err != nil {
		s.logger.Error("oauth.token.issue_failed", "grant_type", "authorization_code", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue tokens")
		return
	}

	s.logger.Info("oauth.token.issued",
		"grant_type", "authorization_code",
		"sub", authCode.Sub,
		"client_id", clientID,
		"scope", authCode.Scope,
		"resource", effectiveResource,
	)

	writeJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  access.Token,
		TokenType:    "Bearer",
		ExpiresIn:    int(time.Until(access.ExpiresAt).Seconds()),
		RefreshToken: refresh.Token,
		Scope:        authCode.Scope,
	})
}

func (s *Server) tokenRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshTokenStr := r.PostForm.Get("refresh_token")
	clientID := r.PostForm.Get("client_id")
	requestedScope := r.PostForm.Get("scope")
	resource := r.PostForm.Get("resource")

	if refreshTokenStr == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"refresh_token and client_id are required")
		return
	}

	// Consume — атомарно помечает revoked и возвращает данные.
	// Повторный вызов с тем же токеном получит ErrNotFound — это защита от replay.
	oldRefresh, err := s.storage.ConsumeRefreshToken(r.Context(), refreshTokenStr)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token is invalid or revoked")
		return
	}

	if oldRefresh.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token does not match client")
		return
	}

	// scope при refresh может быть только сужен — никаких новых scope клиент не получает
	newScope := oldRefresh.Scope
	if requestedScope != "" {
		newScope = intersectScopes(requestedScope, ScopesFromString(oldRefresh.Scope))
		if newScope == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "requested scope outside original grant")
			return
		}
	}

	effectiveResource := oldRefresh.Resource
	if resource != "" {
		effectiveResource = resource
	}

	access, refresh, err := s.issueTokens(r.Context(), clientID, oldRefresh.Sub, newScope, effectiveResource, oldRefresh.Token)
	if err != nil {
		s.logger.Error("oauth.token.issue_failed", "grant_type", "refresh_token", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue tokens")
		return
	}

	s.logger.Info("oauth.token.issued",
		"grant_type", "refresh_token",
		"sub", oldRefresh.Sub,
		"client_id", clientID,
		"scope", newScope,
		"resource", effectiveResource,
	)

	writeJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  access.Token,
		TokenType:    "Bearer",
		ExpiresIn:    int(time.Until(access.ExpiresAt).Seconds()),
		RefreshToken: refresh.Token,
		Scope:        newScope,
	})
}

// issueTokens создаёт пару access+refresh, привязывает к sub/client/scope/resource.
// rotatedFrom != "" означает выпуск через refresh — сохраняется ссылка на предыдущий токен.
func (s *Server) issueTokens(ctx context.Context, clientID, sub, scope, resource, rotatedFrom string) (*AccessToken, *RefreshToken, error) {
	accessStr, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	refreshStr, err := randomToken()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	access := &AccessToken{
		Token:     accessStr,
		ClientID:  clientID,
		Sub:       sub,
		Scope:     scope,
		Resource:  resource,
		ExpiresAt: now.Add(s.cfg.AccessTokenTTL),
		CreatedAt: now,
	}
	if err := s.storage.CreateAccessToken(ctx, access); err != nil {
		return nil, nil, err
	}

	refresh := &RefreshToken{
		Token:       refreshStr,
		ClientID:    clientID,
		Sub:         sub,
		Scope:       scope,
		Resource:    resource,
		RotatedFrom: rotatedFrom,
		Revoked:     false,
		ExpiresAt:   now.Add(s.cfg.RefreshTokenTTL),
		CreatedAt:   now,
	}
	if err := s.storage.CreateRefreshToken(ctx, refresh); err != nil {
		return nil, nil, err
	}

	return access, refresh, nil
}

