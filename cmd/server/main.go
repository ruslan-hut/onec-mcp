package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/mcp-sales-mvp/internal/admin"
	"example.com/mcp-sales-mvp/internal/api"
	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/logger"
	"example.com/mcp-sales-mvp/internal/mcp"
	"example.com/mcp-sales-mvp/internal/oauth"
	"example.com/mcp-sales-mvp/internal/onec"
	"example.com/mcp-sales-mvp/internal/store"
	"example.com/mcp-sales-mvp/internal/tenant"
)

func main() {
	log := logger.New()

	configPath := "configs/config.yml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Один SQLite-файл на всё: настройки баз 1С и OAuth-клиенты/токены.
	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		log.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	tenantStore, err := tenant.NewStore(db)
	if err != nil {
		log.Error("failed to init tenant store", "error", err)
		os.Exit(1)
	}

	// OAuth AS+RS опционален и общий по настройкам, но экземпляр Server — свой на каждую базу
	// (у каждой свой issuer, свой audience и свой верификатор ключей). Storage один на всех,
	// изоляция внутри — по колонке tenant.
	var oauthStorage *oauth.Storage
	var oauthLimiters *api.OAuthLimiters
	if cfg.OAuth.Enabled {
		oauthStorage, err = oauth.NewStorage(db)
		if err != nil {
			log.Error("failed to init oauth storage", "error", err)
			os.Exit(1)
		}

		// Per-IP лимитеры на чувствительные endpoint-ы, общие для всех баз. Окно — 1 минута.
		oauthLimiters = &api.OAuthLimiters{
			Authorize: oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.AuthorizePerMinute, time.Minute),
			Register:  oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.RegisterPerMinute, time.Minute),
			Token:     oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.TokenPerMinute, time.Minute),
		}
		log.Info("OAuth AS+RS enabled", "public_url", cfg.OAuth.PublicURL)
	}

	registry := api.NewRegistry(buildTenants(cfg, tenantStore, oauthStorage, log), log)
	if err := registry.Reload(context.Background()); err != nil {
		log.Error("failed to load tenants", "error", err)
		os.Exit(1)
	}

	var adminHandler http.Handler
	if cfg.Admin.Enabled {
		adminHandler = admin.New(admin.Config{
			Username:     cfg.Admin.Username,
			Password:     cfg.Admin.Password,
			PublicURL:    cfg.OAuth.PublicURL,
			OAuthEnabled: cfg.OAuth.Enabled,
			GlobalScopes: cfg.OAuth.SupportedScopes,
		}, tenantStore, registry, log).Routes()
		log.Info("admin UI enabled", "path", "/admin")
	} else {
		log.Warn("admin UI disabled — databases can only be changed directly in SQLite")
	}

	if cfg.OAuth.Enabled {
		// Фоновая чистка просроченных кодов/токенов раз в час — освобождает место в SQLite.
		// Параллельно — чистка кэшей верификации (по одному на базу) и бакетов лимитеров.
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				if err := oauthStorage.CleanupExpired(context.Background()); err != nil {
					log.Warn("oauth storage cleanup failed", "error", err)
				}
				for _, t := range registry.All() {
					if t.Verifier != nil {
						t.Verifier.Cleanup()
					}
				}
				oauthLimiters.Authorize.Cleanup()
				oauthLimiters.Register.Cleanup()
				oauthLimiters.Token.Cleanup()
			}
		}()
	}

	router := api.NewRouter(registry, adminHandler, oauthLimiters, log)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("starting server", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	log.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}

// buildTenants — билдер для реестра: читает включённые базы из БД и собирает на каждую полную
// рантайм-обвязку. Вызывается на старте и после каждой правки в /admin, поэтому обязан быть
// идемпотентным и не держать состояние между вызовами.
//
// Всё, что несёт данные конкретной базы, создаётся здесь заново и не переиспользуется между
// базами — в первую очередь кэш верификации ключей: общий на все базы, он позволил бы ключу
// из одной 1С открыть доступ к другой.
func buildTenants(
	cfg *config.Config,
	tenantStore *tenant.Store,
	oauthStorage *oauth.Storage,
	log *slog.Logger,
) api.BuildFunc {
	return func(ctx context.Context) ([]*api.Tenant, error) {
		records, err := tenantStore.List(ctx)
		if err != nil {
			return nil, err
		}

		out := make([]*api.Tenant, 0, len(records))
		for _, rec := range records {
			if !rec.Enabled {
				continue
			}

			tlog := log.With("tenant", rec.Slug)
			onecClient := onec.NewClient(onec.Settings{
				BaseURL:         rec.BaseURL,
				Username:        rec.Username,
				Password:        rec.Password,
				Timeout:         rec.Timeout(),
				ReportTimeout:   rec.ReportTimeout(),
				TenantHeader:    rec.TenantHeader,
				DefaultTenant:   rec.DefaultTenant,
				ResolveCacheTTL: rec.ResolveCacheTTL(),
			}, tlog)

			t := &api.Tenant{
				Slug:     rec.Slug,
				Name:     rec.Name,
				Handler:  api.NewHandler(onecClient, cfg, tlog),
				APIToken: rec.APIToken,
				Client:   onecClient,
			}

			if cfg.OAuth.Enabled {
				// TTL=5min, чтобы каждое обращение к /mcp не било в 1С (ротация ключа
				// подхватится при следующей проверке после истечения кэша).
				verifier := oauth.NewCachedVerifier(func(ctx context.Context, key string) (*oauth.UserInfo, error) {
					resp, err := onecClient.VerifyMCPKey(ctx, key)
					if err != nil {
						return nil, err
					}
					return &oauth.UserInfo{Sub: resp.Sub, Name: resp.Name, Scopes: resp.Scopes}, nil
				}, 5*time.Minute)

				defaults, supported := scopesFor(cfg, rec)

				srv, err := oauth.NewServer(oauth.Config{
					Tenant:          rec.Slug,
					TenantName:      rec.Name,
					PublicURL:       cfg.OAuth.PublicURL,
					AccessTokenTTL:  cfg.OAuth.AccessTokenTTL,
					RefreshTokenTTL: cfg.OAuth.RefreshTokenTTL,
					AuthCodeTTL:     cfg.OAuth.AuthCodeTTL,
					DefaultScopes:   defaults,
					SupportedScopes: supported,
					DevAccessKey:    rec.DevAccessKey,
					VerifyKey:       verifier.Verify,
				}, oauthStorage, log)
				if err != nil {
					return nil, fmt.Errorf("tenant %s: %w", rec.Slug, err)
				}

				t.OAuth = srv
				t.Verifier = verifier
			}

			// mcp.Handler с пустым токеном никого не аутентифицирует. Это правильно при OAuth
			// (проверка уже сделана внешним middleware) и недопустимо без него — поэтому база
			// без mcp_token и без OAuth не получает MCP-маршрута вовсе. Открытый /mcp хуже
			// отсутствующего.
			switch {
			case !cfg.MCP.Enabled:
			case t.OAuth != nil:
				t.MCP = mcp.NewHandler(onecClient, cfg, "", tlog)
			case rec.MCPToken != "":
				t.MCP = mcp.NewHandler(onecClient, cfg, rec.MCPToken, tlog)
			default:
				tlog.Warn("mcp endpoint not mounted: oauth is disabled and mcp_token is empty")
			}

			out = append(out, t)
		}
		return out, nil
	}
}

// scopesFor — эффективные списки scope базы: свои, если заданы, иначе глобальные из конфига.
func scopesFor(cfg *config.Config, rec *tenant.Tenant) (defaults, supported []string) {
	defaults = rec.DefaultScopes
	if len(defaults) == 0 {
		defaults = cfg.OAuth.DefaultScopes
	}
	supported = rec.SupportedScopes
	if len(supported) == 0 {
		supported = cfg.OAuth.SupportedScopes
	}
	if len(supported) == 0 {
		supported = defaults
	}
	return defaults, supported
}
