package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// VerifyKeyFunc — стратегия проверки введённого пользователем MCP-ключа.
// Реализации: dev (сравнение с конфиг-значением), 1C (вызов /mcp/auth/verify через гейт).
// Возвращает nil, error если ключ невалиден или произошла ошибка инфраструктуры —
// со стороны вызывающего обе ситуации трактуются как «доступ запрещён», чтобы не утекало
// различие «ключ не существует» / «БД упала».
//
// Функция обязана быть привязана к конкретной базе 1С: общий на все базы верификатор
// (особенно с кэшем) означал бы, что ключ одной базы открывает доступ к другой.
type VerifyKeyFunc func(ctx context.Context, key string) (*UserInfo, error)

// Config — настройки AS/RS одной базы 1С. Собирается в main.go из config.TenantConfig + глобального oauth-блока.
type Config struct {
	// Tenant — слаг базы. Определяет и путь (/{tenant}/oauth/*), и issuer, и изоляцию в Storage.
	Tenant string

	// TenantName — человекочитаемое имя базы, показывается на форме логина.
	TenantName string

	// PublicURL — внешний КОРНЕВОЙ URL гейта, без слага (например, https://onec.nomadus.net).
	// Все endpoint-ы и issuer производятся от PublicURL + "/" + Tenant.
	PublicURL string

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
	// В проде сюда подставляется обёртка над onec.Client.VerifyMCPKey этой базы с TTL-кэшем.
	VerifyKey VerifyKeyFunc
}

// Server — AS+RS одной базы 1С. Storage общий на все базы, изоляция — по колонке tenant.
type Server struct {
	cfg     Config
	storage *Storage
	logger  *slog.Logger
}

func NewServer(cfg Config, storage *Storage, logger *slog.Logger) (*Server, error) {
	if cfg.Tenant == "" {
		return nil, fmt.Errorf("oauth: tenant is required")
	}
	if cfg.PublicURL == "" {
		return nil, fmt.Errorf("oauth: public_url is required")
	}
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = time.Hour
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 10 * time.Minute
	}
	if len(cfg.SupportedScopes) == 0 {
		cfg.SupportedScopes = cfg.DefaultScopes
	}
	if cfg.TenantName == "" {
		cfg.TenantName = cfg.Tenant
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")

	return &Server{cfg: cfg, storage: storage, logger: logger.With("tenant", cfg.Tenant)}, nil
}

// Tenant — слаг базы, обслуживаемой этим сервером.
func (s *Server) Tenant() string { return s.cfg.Tenant }

// rootURL — корневой публичный URL гейта, без слага. От него строятся well-known пути.
func (s *Server) rootURL() string { return s.cfg.PublicURL }

// issuer — OAuth issuer этой базы: корневой URL + слаг. Он же база для всех /oauth/* endpoint-ов.
func (s *Server) issuer() string { return s.cfg.PublicURL + "/" + s.cfg.Tenant }

// resource — канонический URL MCP-сервера этой базы, он же audience выпускаемых токенов (RFC 8707).
// Именно он разделяет базы: токен tenant1 не пройдёт audience-проверку на /tenant2/mcp.
func (s *Server) resource() string { return s.issuer() + "/mcp" }

// resourceMetadataURL — канонический (RFC 9728) путь метаданных ресурса: слаг и /mcp
// вставляются ПОСЛЕ well-known сегмента, а не перед ним.
func (s *Server) resourceMetadataURL() string {
	return s.rootURL() + "/.well-known/oauth-protected-resource/" + s.cfg.Tenant + "/mcp"
}

// authorizeEndpoint — абсолютный URL формы логина. Используется как action у HTML-формы:
// абсолютный, чтобы не сломаться, если гейт стоит за прокси с префиксом пути.
func (s *Server) authorizeEndpoint() string { return s.issuer() + "/oauth/authorize" }

// checkResource валидирует параметр resource из /authorize и /token (RFC 8707).
// AS обслуживает ровно один ресурс, поэтому допустимы только пустое значение и точное совпадение —
// иначе клиент мог бы попросить токен с audience чужой базы.
func (s *Server) checkResource(raw string) error {
	if raw == "" {
		return nil
	}
	if strings.TrimRight(raw, "/") != s.resource() {
		return fmt.Errorf("resource must be %s", s.resource())
	}
	return nil
}
