package oauth

import (
	"context"
	"errors"
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

	// Resource (RFC 8707): если клиент его прислал — он обязан указывать на эту базу
	if err := s.checkResource(resource); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Consume — атомарно достаём и удаляем код, чтобы исключить повторное использование.
	// Поиск ограничен своей базой: код, выданный на другом слаге, не найдётся.
	authCode, err := s.storage.ConsumeAuthCode(r.Context(), s.cfg.Tenant, code)
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

	// Audience всегда наш: AS обслуживает ровно один ресурс, а присланный клиентом resource
	// уже проверен на совпадение выше.
	effectiveResource := s.resource()

	// Обмен кода — начало новой цепочки, family заводится внутри issueTokens.
	access, refresh, err := s.issueTokens(r.Context(), clientID, authCode.Sub, authCode.Scope, effectiveResource, "", "")
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

	// Без этой проверки клиент мог бы на refresh подменить audience на чужую базу
	if err := s.checkResource(resource); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Consume — атомарно помечает revoked и возвращает данные.
	oldRefresh, err := s.storage.ConsumeRefreshToken(r.Context(), s.cfg.Tenant, refreshTokenStr)
	if err != nil {
		// Повторное предъявление уже потраченного токена: легитимный клиент свой обменял, значит
		// в обороте копия. Кто именно пришёл вторым — неизвестно, поэтому гасим всю цепочку и
		// заставляем обоих авторизоваться заново (RFC 9700 §4.14.2).
		if errors.Is(err, ErrTokenReplay) {
			revoked, rerr := s.storage.RevokeFamily(r.Context(), s.cfg.Tenant, oldRefresh.Family)
			if rerr != nil {
				s.logger.Error("oauth.token.family_revoke_failed", "error", rerr, "sub", oldRefresh.Sub)
			}
			s.logger.Warn("oauth.token.replay_detected",
				"sub", oldRefresh.Sub,
				"client_id", oldRefresh.ClientID,
				"remote", clientIP(r),
				"revoked", revoked,
			)
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token is invalid or revoked")
			return
		}
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

	// Ротация продолжает ту же цепочку — family наследуется, иначе отзыв при replay
	// доставал бы только до места разрыва.
	access, refresh, err := s.issueTokens(r.Context(), clientID, oldRefresh.Sub, newScope, s.resource(),
		oldRefresh.Token, oldRefresh.Family)
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
		"resource", s.resource(),
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
//
// family связывает всю цепочку, выросшую из одного грант-события: при обмене кода заводится
// новая, при ротации наследуется прежняя. По ней гасится доступ, если всплывёт replay.
func (s *Server) issueTokens(ctx context.Context, clientID, sub, scope, resource, rotatedFrom, family string) (*AccessToken, *RefreshToken, error) {
	accessStr, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	refreshStr, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	if family == "" {
		if family, err = randomToken(); err != nil {
			return nil, nil, err
		}
	}

	now := time.Now()
	access := &AccessToken{
		Token:     accessStr,
		Tenant:    s.cfg.Tenant,
		ClientID:  clientID,
		Sub:       sub,
		Scope:     scope,
		Resource:  resource,
		Family:    family,
		ExpiresAt: now.Add(s.cfg.AccessTokenTTL),
		CreatedAt: now,
	}
	if err := s.storage.CreateAccessToken(ctx, access); err != nil {
		return nil, nil, err
	}

	refresh := &RefreshToken{
		Token:       refreshStr,
		Tenant:      s.cfg.Tenant,
		ClientID:    clientID,
		Sub:         sub,
		Scope:       scope,
		Resource:    resource,
		RotatedFrom: rotatedFrom,
		Family:      family,
		Revoked:     false,
		ExpiresAt:   now.Add(s.cfg.RefreshTokenTTL),
		CreatedAt:   now,
	}
	if err := s.storage.CreateRefreshToken(ctx, refresh); err != nil {
		return nil, nil, err
	}

	return access, refresh, nil
}
