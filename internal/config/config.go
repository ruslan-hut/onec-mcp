package config

import (
	"fmt"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config — «загрузочный» конфиг гейта: то, что нужно, чтобы подняться и открыть /admin.
// Сами базы 1С в конфиге не описываются — они живут в SQLite и редактируются через /admin.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Admin    AdminConfig    `yaml:"admin"`
	Limits   LimitsConfig   `yaml:"limits"`
	MCP      MCPConfig      `yaml:"mcp"`
	OAuth    OAuthConfig    `yaml:"oauth"`
}

// DatabaseConfig — единый SQLite-файл: и настройки баз 1С, и OAuth-клиенты/токены.
// Один файл = один артефакт для бэкапа.
type DatabaseConfig struct {
	Path string `yaml:"path" env-default:"data/onec-mcp.db"`
}

// AdminConfig — доступ к веб-интерфейсу /admin, где заводятся базы 1С.
// Логин/пароль задаются здесь и только здесь: в БД лежат тенанты, а не администраторы.
type AdminConfig struct {
	Enabled  bool   `yaml:"enabled" env-default:"true"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// OAuthConfig — настройки встроенного Authorization+Resource Server.
// Включается через oauth.enabled=true; при включении статический Bearer на /{slug}/mcp отключается.
//
// AS поднимается по одному на базу: issuer = public_url + "/" + slug,
// resource (audience токена) = issuer + "/mcp". Токен, выпущенный для одной базы,
// не примут на другой.
type OAuthConfig struct {
	Enabled bool `yaml:"enabled" env-default:"false"`
	// PublicURL — внешний корневой URL гейта, без слага и без trailing slash.
	PublicURL       string        `yaml:"public_url"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl" env-default:"1h"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl" env-default:"720h"`
	AuthCodeTTL     time.Duration `yaml:"auth_code_ttl" env-default:"10m"`
	DefaultScopes   []string      `yaml:"default_scopes"`
	// SupportedScopes — полный список scope, анонсируемых в OAuth metadata и допустимых при
	// DCR-регистрации. Должен включать все scope из ToolScopes, иначе соответствующие инструменты
	// будут вырезаны ещё до выпуска токена. Пусто = берётся из DefaultScopes.
	// Отдельная база может переопределить оба списка (см. tenant.Tenant).
	SupportedScopes []string         `yaml:"supported_scopes"`
	RateLimit       RateLimitsConfig `yaml:"rate_limit"`
}

// RateLimitsConfig — пер-IP лимиты на чувствительные OAuth-эндпоинты.
// 0 = эндпоинт без ограничения. Окно — одна минута (фиксированное). Лимитеры общие для всех баз.
type RateLimitsConfig struct {
	AuthorizePerMinute int `yaml:"authorize_per_minute" env-default:"10"`
	RegisterPerMinute  int `yaml:"register_per_minute" env-default:"30"`
	TokenPerMinute     int `yaml:"token_per_minute" env-default:"120"`
}

type MCPConfig struct {
	Enabled bool `yaml:"enabled" env-default:"true"`
}

type ServerConfig struct {
	Host string `yaml:"host" env-default:"0.0.0.0"`
	Port int    `yaml:"port" env-default:"8088"`
}

type LimitsConfig struct {
	ResolveLimit int `yaml:"resolve_limit" env-default:"10"`
	MaxRows      int `yaml:"max_rows" env-default:"5000"`
}

func Load(configPath string) (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.OAuth.Enabled && c.OAuth.PublicURL == "" {
		return fmt.Errorf("config: oauth.public_url is required when oauth is enabled")
	}
	// Без учётки /admin недостижим, а базы заводятся только там — гейт стал бы бесполезен молча.
	if c.Admin.Enabled && (c.Admin.Username == "" || c.Admin.Password == "") {
		return fmt.Errorf("config: admin.username and admin.password are required when admin is enabled")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("config: database.path is required")
	}
	return nil
}
