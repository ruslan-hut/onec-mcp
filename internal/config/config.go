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
	BaseURL       string     `yaml:"base_url"`
	TimeoutMs     int        `yaml:"timeout_ms" env-default:"8000"`
	Auth          AuthConfig `yaml:"auth"`
	TenantHeader  string     `yaml:"tenant_header"`
	DefaultTenant string     `yaml:"default_tenant"`
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

func Load(configPath string) (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
