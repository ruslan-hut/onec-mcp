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

	var mcpHandler http.Handler
	if cfg.MCP.Enabled {
		mcpHandler = mcp.NewHandler(onecClient, cfg, log)
		log.Info("MCP endpoint enabled")
	}

	if cfg.API.BearerToken == "" {
		log.Warn("no bearer token provided, API disabled")
	} else {
		log.Info("API enabled")
	}

	router := api.NewRouter(handler, mcpHandler, cfg.API.BearerToken, log)

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
