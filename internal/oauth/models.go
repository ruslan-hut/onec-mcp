package oauth

import "time"

// Все сущности несут Tenant — слаг базы 1С, к которой они относятся. Все lookup'ы в Storage
// фильтруются по нему, поэтому код/токен/клиент одной базы не видны из другой даже при
// совпадении случайной строки. Это второй рубеж поверх audience-проверки в middleware.

// Client — зарегистрированный OAuth-клиент (Claude, ChatGPT, кастомный).
// Регистрируется через RFC 7591 (DCR) на /{tenant}/oauth/register.
// Public client (PKCE), без client_secret — token_endpoint_auth_method=none.
type Client struct {
	ClientID                string
	Tenant                  string
	RedirectURIs            []string
	ClientName              string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
	Scope                   string
	CreatedAt               time.Time
}

// AuthCode — одноразовый код, выдаваемый /{tenant}/oauth/authorize и обмениваемый на токены
// в /{tenant}/oauth/token. Хранит PKCE-вызов и привязку к пользователю/scope.
type AuthCode struct {
	Code                string
	Tenant              string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Sub                 string
	Scope               string
	Resource            string
	ExpiresAt           time.Time
}

// AccessToken — opaque-токен. Lookup по PK на каждом запросе к /{tenant}/mcp.
// Audience-привязка через Resource — токен валиден только для MCP-сервера своей базы.
type AccessToken struct {
	Token     string
	Tenant    string
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
	Tenant      string
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
