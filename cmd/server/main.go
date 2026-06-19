package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/mcp-sales-mvp/internal/api"
	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/logger"
	"example.com/mcp-sales-mvp/internal/mcp"
	"example.com/mcp-sales-mvp/internal/oauth"
	"example.com/mcp-sales-mvp/internal/onec"
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

	onecClient := onec.NewClient(&cfg.OneC, log)

	handler := api.NewHandler(onecClient, cfg, log)

	// OAuth AS+RS опционален. Если включён — статический Bearer на /mcp отключается
	// (mcp.Handler конструируется без bearer_token), его место занимает OAuth middleware.
	var oauthServer *oauth.Server
	var oauthLimiters *api.OAuthLimiters
	if cfg.OAuth.Enabled {
		storage, err := oauth.NewStorage(cfg.OAuth.DBPath)
		if err != nil {
			log.Error("failed to open oauth storage", "error", err)
			os.Exit(1)
		}
		defer func() { _ = storage.Close() }()

		// Проверка ключа идёт в 1С через onec.Client.VerifyMCPKey. Кэш TTL=5min
		// чтобы каждое обращение к /mcp не било в 1С (ротация ключа в 1С подхватится
		// при следующей проверке после истечения кэша).
		verifier := oauth.NewCachedVerifier(func(ctx context.Context, key string) (*oauth.UserInfo, error) {
			resp, err := onecClient.VerifyMCPKey(ctx, key)
			if err != nil {
				return nil, err
			}
			return &oauth.UserInfo{Sub: resp.Sub, Name: resp.Name, Scopes: resp.Scopes}, nil
		}, 5*time.Minute)

		oauthServer = oauth.NewServer(oauth.Config{
			PublicURL:       cfg.OAuth.PublicURL,
			Resource:        cfg.OAuth.Resource,
			AccessTokenTTL:  cfg.OAuth.AccessTokenTTL,
			RefreshTokenTTL: cfg.OAuth.RefreshTokenTTL,
			AuthCodeTTL:     cfg.OAuth.AuthCodeTTL,
			DefaultScopes:   cfg.OAuth.DefaultScopes,
			SupportedScopes: cfg.OAuth.SupportedScopes,
			DevAccessKey:    cfg.OAuth.DevAccessKey,
			VerifyKey:       verifier.Verify,
		}, storage, log)
		log.Info("OAuth AS+RS enabled", "public_url", cfg.OAuth.PublicURL, "resource", cfg.OAuth.Resource)

		// Per-IP лимитеры на чувствительные endpoint-ы. Окно — 1 минута.
		oauthLimiters = &api.OAuthLimiters{
			Authorize: oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.AuthorizePerMinute, time.Minute),
			Register:  oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.RegisterPerMinute, time.Minute),
			Token:     oauth.NewFixedWindowLimiter(cfg.OAuth.RateLimit.TokenPerMinute, time.Minute),
		}

		// Фоновая чистка просроченных кодов/токенов раз в час — освобождает место в SQLite.
		// Параллельно — чистка кэша верификации и бакетов лимитеров.
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := storage.CleanupExpired(context.Background()); err != nil {
						log.Warn("oauth storage cleanup failed", "error", err)
					}
					verifier.Cleanup()
					oauthLimiters.Authorize.Cleanup()
					oauthLimiters.Register.Cleanup()
					oauthLimiters.Token.Cleanup()
				}
			}
		}()
	}

	var mcpHandler http.Handler
	if cfg.MCP.Enabled {
		mcpCfg := *cfg
		if oauthServer != nil {
			// Гасим статический Bearer внутри mcp.Handler, чтобы не было двойной аутентификации:
			// внешний oauth middleware уже всё проверил
			mcpCfg.MCP.BearerToken = ""
		}
		mcpHandler = mcp.NewHandler(onecClient, &mcpCfg, log)
		log.Info("MCP endpoint enabled")
	}

	if cfg.API.BearerToken == "" {
		log.Warn("no bearer token provided, API disabled")
	} else {
		log.Info("API enabled")
	}

	router := api.NewRouter(handler, mcpHandler, cfg.API.BearerToken, oauthServer, oauthLimiters, log)

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
