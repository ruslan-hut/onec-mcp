package oauth

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// VerifyKeyFunc — стратегия проверки введённого пользователем MCP-ключа.
// Реализации: dev (сравнение с конфиг-значением), 1C (вызов /mcp/auth/verify через гейт).
// Возвращает nil, error если ключ невалиден или произошла ошибка инфраструктуры —
// со стороны вызывающего обе ситуации трактуются как «доступ запрещён», чтобы не утекало
// различие «ключ не существует» / «БД упала».
type VerifyKeyFunc func(ctx context.Context, key string) (*UserInfo, error)

// Config — настройки AS/RS гейта. Загружается из YAML (см. config.OAuthConfig).
type Config struct {
	// PublicURL — внешний URL гейта (например, https://mcp.rior.example).
	// От него производятся все endpoint-ы в .well-known/* и issuer.
	PublicURL string

	// Resource — канонический URL MCP-сервера (audience для токенов).
	// Если пусто — берётся PublicURL + "/mcp".
	Resource string

	// DBPath — путь к файлу SQLite, относительный или абсолютный.
	DBPath string

	// AccessTokenTTL — время жизни access token, по умолчанию 1 час.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL — время жизни refresh token, по умолчанию 30 дней.
	RefreshTokenTTL time.Duration

	// AuthCodeTTL — время жизни одноразового auth code, по умолчанию 10 минут.
	AuthCodeTTL time.Duration

	// DefaultScopes — выдаются клиенту, если он не запросил конкретные.
	DefaultScopes []string

	// SupportedScopes — анонсируются в metadata. По умолчанию = DefaultScopes.
	SupportedScopes []string

	// DevAccessKey — fallback-ключ. Если задан и VerifyKey не задана, используется простое сравнение.
	// Удобно для прогона на dev-стенде без 1С.
	DevAccessKey string

	// VerifyKey — основная стратегия проверки. Если задана — используется вместо DevAccessKey.
	// В проде сюда подставляется обёртка над onec.Client.VerifyMCPKey с TTL-кэшем.
	VerifyKey VerifyKeyFunc
}

// Server — корневая обвязка OAuth-функциональности: holds config, storage, logger.
// HTTP-обработчики и middleware висят на нём как методы.
type Server struct {
	cfg     Config
	storage *Storage
	logger  *slog.Logger
}

func NewServer(cfg Config, storage *Storage, logger *slog.Logger) *Server {
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = time.Hour
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 10 * time.Minute
	}
	if cfg.Resource == "" {
		cfg.Resource = strings.TrimRight(cfg.PublicURL, "/") + "/mcp"
	}
	if len(cfg.SupportedScopes) == 0 {
		cfg.SupportedScopes = cfg.DefaultScopes
	}
	return &Server{cfg: cfg, storage: storage, logger: logger}
}

// PublicURL — нормализованный публичный URL без trailing slash.
func (s *Server) publicURL() string {
	return strings.TrimRight(s.cfg.PublicURL, "/")
}
