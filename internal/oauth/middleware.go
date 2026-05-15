package oauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type ctxKey struct{}

// AuthInfo — данные о вызывающем пользователе, прокидываются через context.
// Handlers (MCP tools, REST endpoints) могут достать sub/scopes через FromContext.
type AuthInfo struct {
	Sub      string
	ClientID string
	Scope    string
	Scopes   []string
	Resource string
}

// HasScope — true, если у токена есть указанный scope. Используется handlers'ами для per-tool ACL.
func (a *AuthInfo) HasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// FromContext достаёт AuthInfo из request.Context() (ноль, если middleware не отработала).
func FromContext(ctx context.Context) *AuthInfo {
	v, _ := ctx.Value(ctxKey{}).(*AuthInfo)
	return v
}

func withAuth(ctx context.Context, info *AuthInfo) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

// Middleware возвращает chi-совместимый wrapper: проверяет Bearer токен и обогащает context.
// При отсутствии/невалидности токена — 401 с WWW-Authenticate, указывающим MCP-клиенту,
// куда идти за метаданными ресурса (RFC 9728 §5.1). Каждый отказ пишется audit-событием
// oauth.token.refused с reason, чтобы по логу было видно brute-force и неправильно настроенных клиентов.
func (s *Server) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := extractBearer(r)
		if !ok {
			s.logger.Warn("oauth.token.refused", "reason", "no_bearer", "remote", clientIP(r))
			s.unauthorized(w, "invalid_token", "missing bearer token")
			return
		}

		access, err := s.storage.GetActiveAccessToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				s.logger.Warn("oauth.token.refused", "reason", "not_found", "remote", clientIP(r))
				s.unauthorized(w, "invalid_token", "token is invalid or expired")
				return
			}
			s.logger.Error("oauth.token.lookup_failed", "error", err)
			s.unauthorized(w, "server_error", "token lookup failed")
			return
		}

		// Audience-привязка (RFC 8707): токен валиден только для канонического URL этого MCP-сервера
		if access.Resource != "" && access.Resource != s.cfg.Resource {
			s.logger.Warn("oauth.token.refused",
				"reason", "audience_mismatch",
				"remote", clientIP(r),
				"sub", access.Sub,
				"expected", s.cfg.Resource,
				"actual", access.Resource,
			)
			s.unauthorized(w, "invalid_token", "token audience does not match this resource")
			return
		}

		info := &AuthInfo{
			Sub:      access.Sub,
			ClientID: access.ClientID,
			Scope:    access.Scope,
			Scopes:   ScopesFromString(access.Scope),
			Resource: access.Resource,
		}
		next.ServeHTTP(w, r.WithContext(withAuth(r.Context(), info)))
	})
}

// unauthorized отдаёт 401 с правильным WWW-Authenticate. resource_metadata указывает на
// .well-known/oauth-protected-resource — клиент по нему построит OAuth-flow с нуля.
func (s *Server) unauthorized(w http.ResponseWriter, code, description string) {
	metaURL := s.publicURL() + "/.well-known/oauth-protected-resource"
	header := `Bearer error="` + code + `", error_description="` + description + `", resource_metadata="` + metaURL + `"`
	w.Header().Set("WWW-Authenticate", header)
	writeOAuthError(w, http.StatusUnauthorized, code, description)
}

func extractBearer(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", false
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}
