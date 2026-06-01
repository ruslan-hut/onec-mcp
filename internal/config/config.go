package config

import (
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	OneC   OneCConfig   `yaml:"onec"`
	Limits LimitsConfig `yaml:"limits"`
	MCP    MCPConfig    `yaml:"mcp"`
	API    APIConfig    `yaml:"api"`
	OAuth  OAuthConfig  `yaml:"oauth"`
}

// OAuthConfig — настройки встроенного Authorization+Resource Server.
// Включается через oauth.enabled=true; при включении статический Bearer на /mcp отключается.
type OAuthConfig struct {
	Enabled         bool          `yaml:"enabled" env-default:"false"`
	PublicURL       string        `yaml:"public_url"`
	Resource        string        `yaml:"resource"`
	DBPath          string        `yaml:"db_path" env-default:"data/oauth.db"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env-default:"1h"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env-default:"720h"`
	AuthCodeTTL     time.Duration `yaml:"auth_code_ttl" env-default:"10m"`
	DefaultScopes   []string      `yaml:"default_scopes"`
	// SupportedScopes — полный список scope, анонсируемых в OAuth metadata и допустимых при
	// DCR-регистрации. Должен включать все scope из ToolScopes, иначе соответствующие инструменты
	// будут вырезаны ещё до выпуска токена. Пусто = берётся из DefaultScopes.
	SupportedScopes []string         `yaml:"supported_scopes"`
	RateLimit       RateLimitsConfig `yaml:"rate_limit"`
	// DevAccessKey — временный единый ключ для проверки логина на этапе 1.
	// На этапе 2 заменяется на вызов 1С /mcp/auth/verify.
	DevAccessKey string `yaml:"dev_access_key"`
}

// RateLimitsConfig — пер-IP лимиты на чувствительные OAuth-эндпоинты.
// 0 = эндпоинт без ограничения. Окно — одна минута (фиксированное).
type RateLimitsConfig struct {
	AuthorizePerMinute int `yaml:"authorize_per_minute" env-default:"10"`
	RegisterPerMinute  int `yaml:"register_per_minute" env-default:"30"`
	TokenPerMinute     int `yaml:"token_per_minute" env-default:"120"`
}

type MCPConfig struct {
	Enabled     bool   `yaml:"enabled" env-default:"true"`
	BearerToken string `yaml:"bearer_token"`
}

type APIConfig struct {
	BearerToken string `yaml:"bearer_token"`
}

type ServerConfig struct {
	Host string `yaml:"host" env-default:"0.0.0.0"`
	Port int    `yaml:"port" env-default:"8088"`
}

type OneCConfig struct {
	BaseURL            string     `yaml:"base_url"`
	TimeoutMs          int        `yaml:"timeout_ms" env-default:"8000"`
	Auth               AuthConfig `yaml:"auth"`
	TenantHeader       string     `yaml:"tenant_header"`
	DefaultTenant      string     `yaml:"default_tenant"`
	ResolveCacheTTLSec int        `yaml:"resolve_cache_ttl_sec" env-default:"600"`
}

type AuthConfig struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type LimitsConfig struct {
	ResolveLimit int `yaml:"resolve_limit" env-default:"10"`
	MaxRows      int `yaml:"max_rows" env-default:"5000"`
}

func (c *OneCConfig) Timeout() time.Duration {
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

// ResolveCacheTTL — TTL для кэша resolve_* ответов. 0 (или отрицательное) отключает кэш.
func (c *OneCConfig) ResolveCacheTTL() time.Duration {
	return time.Duration(c.ResolveCacheTTLSec) * time.Second
}

func Load(configPath string) (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
