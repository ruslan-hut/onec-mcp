package oauth

import (
	"encoding/json"
	"net/http"
)

// ProtectedResourceMetadata — RFC 9728 ответ для MCP-клиентов,
// указывающий, куда идти за токеном.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

// AuthorizationServerMetadata — RFC 8414, описывает endpoint-ы AS и поддерживаемые опции.
// Включает registration_endpoint, чтобы ChatGPT мог сделать Dynamic Client Registration.
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

// HandleProtectedResourceMetadata — GET /.well-known/oauth-protected-resource.
// MCP-клиент дёргает этот endpoint после получения 401 с WWW-Authenticate.
func (s *Server) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	meta := ProtectedResourceMetadata{
		Resource:               s.cfg.Resource,
		AuthorizationServers:   []string{s.publicURL()},
		ScopesSupported:        s.cfg.SupportedScopes,
		BearerMethodsSupported: []string{"header"},
	}
	writeJSON(w, http.StatusOK, meta)
}

// HandleAuthorizationServerMetadata — GET /.well-known/oauth-authorization-server.
// MCP-клиент использует это для построения OAuth-flow.
func (s *Server) HandleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL()
	meta := AuthorizationServerMetadata{
		Issuer:                            base,
		AuthorizationEndpoint:             base + "/oauth/authorize",
		TokenEndpoint:                     base + "/oauth/token",
		RegistrationEndpoint:              base + "/oauth/register",
		ScopesSupported:                   s.cfg.SupportedScopes,
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	}
	writeJSON(w, http.StatusOK, meta)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
