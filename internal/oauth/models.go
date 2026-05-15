package oauth

import "time"

// Client — зарегистрированный OAuth-клиент (Claude, ChatGPT, кастомный).
// Регистрируется через RFC 7591 (DCR) на /oauth/register.
// Public client (PKCE), без client_secret — token_endpoint_auth_method=none.
type Client struct {
	ClientID                string
	RedirectURIs            []string
	ClientName              string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
	Scope                   string
	CreatedAt               time.Time
}

// AuthCode — одноразовый код, выдаваемый /oauth/authorize и обмениваемый на токены в /oauth/token.
// Хранит PKCE-вызов и привязку к пользователю/scope для последующего выпуска токенов.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Sub                 string
	Scope               string
	Resource            string
	ExpiresAt           time.Time
}

// AccessToken — opaque-токен. Lookup по PK на каждом запросе к /mcp.
// Audience-привязка через Resource — токен валиден только для своего MCP-сервера.
type AccessToken struct {
	Token     string
	ClientID  string
	Sub       string
	Scope     string
	Resource  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// RefreshToken — с ротацией: при обмене старый помечается revoked, выпускается новый,
// RotatedFrom указывает на предыдущий для детекции replay-атак.
type RefreshToken struct {
	Token       string
	ClientID    string
	Sub         string
	Scope       string
	Resource    string
	RotatedFrom string
	Revoked     bool
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// UserInfo — результат верификации access key (пока через dev_access_key,
// позже — через 1С /mcp/auth/verify). Кэшируется по sub.
type UserInfo struct {
	Sub    string   `json:"sub"`
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scopes"`
}
