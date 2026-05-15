package oauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientRegistrationRequest — тело POST /oauth/register (RFC 7591 §2).
// Используется Claude и ChatGPT для автоматической регистрации без предварительного шара client_id.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// ClientRegistrationResponse — ответ RFC 7591 §3.2.1.
// client_secret не выпускаем — поддерживаем только public clients (PKCE).
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// HandleRegister — POST /oauth/register (Dynamic Client Registration).
// Минимально-достаточная реализация для интеграции с Claude/ChatGPT:
// принимаем только public clients (auth method=none), фиксируем PKCE-flow.
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
		return
	}

	var req ClientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "failed to parse body")
		return
	}

	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}

	// Все redirect_uris должны быть либо https, либо localhost — иначе клиент уязвим к перехвату кода
	for _, u := range req.RedirectURIs {
		if !isAllowedRedirectURI(u) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
				"redirect_uri must be https or localhost: "+u)
			return
		}
	}

	// Дефолты по спеке: authorization_code grant + code response
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}

	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata",
			"only token_endpoint_auth_method=none is supported (public clients with PKCE)")
		return
	}

	// Все запрошенные grant types должны быть в нашем списке поддерживаемых
	for _, gt := range grantTypes {
		if gt != "authorization_code" && gt != "refresh_token" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata",
				"unsupported grant_type: "+gt)
			return
		}
	}
	for _, rt := range responseTypes {
		if rt != "code" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata",
				"unsupported response_type: "+rt)
			return
		}
	}

	// Сужаем scope до пересечения с поддерживаемыми — клиент не сможет выпросить лишнего
	scope := intersectScopes(req.Scope, s.cfg.SupportedScopes)

	id, err := randomToken()
	if err != nil {
		s.logger.Error("oauth.client.generate_id_failed", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate client_id")
		return
	}

	client := &Client{
		ClientID:                id,
		RedirectURIs:            req.RedirectURIs,
		ClientName:              req.ClientName,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		Scope:                   scope,
		CreatedAt:               time.Now(),
	}
	if err := s.storage.CreateClient(r.Context(), client); err != nil {
		s.logger.Error("oauth.client.persist_failed", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to store client")
		return
	}

	s.logger.Info("oauth.client.registered", "client_id", id, "client_name", req.ClientName)

	resp := ClientRegistrationResponse{
		ClientID:                client.ClientID,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientName:              client.ClientName,
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              client.GrantTypes,
		ResponseTypes:           client.ResponseTypes,
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		Scope:                   client.Scope,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func isAllowedRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return false
}

// intersectScopes — оставляет только те scope, что есть в supported.
// Если клиент ничего не запросил — возвращает все поддерживаемые.
func intersectScopes(requested string, supported []string) string {
	supSet := make(map[string]struct{}, len(supported))
	for _, s := range supported {
		supSet[s] = struct{}{}
	}
	if requested == "" {
		return strings.Join(supported, " ")
	}
	var out []string
	for _, s := range strings.Fields(requested) {
		if _, ok := supSet[s]; ok {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}

// OAuthError — стандартный формат ошибок RFC 6749 §5.2 и RFC 7591 §3.2.2.
type OAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, OAuthError{Error: code, ErrorDescription: description})
}
