// Package main is the entry point for the go-liwaisi-os service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/configs"
	appservice "github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/application/service"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/infrastructure/driven/health"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/infrastructure/driving/http/handler"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/infrastructure/driving/http/router"
)

// Application version; can be overriden at build time with -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := configs.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Wire dependencies
	healthRepo := health.NewMockRepository()
	healthSvc := appservice.NewHealthService(healthRepo, version)
	healthHandler := handler.NewHealthHandler(healthSvc)

	e := router.New(healthHandler, "go-liwaisi-os")

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           e,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		slog.Info("starting server", "addr", addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server stopped unexpectedly", "error", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	slog.Info("server exited gracefully")
	return nil
}
