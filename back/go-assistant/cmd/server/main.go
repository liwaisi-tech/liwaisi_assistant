// Package main is the entry point for the go-assistant microservice.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	appservice "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/service"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/persistence/sqlite"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/handler"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/router"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/telemetry"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := configs.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	telemetryShutdown, err := telemetry.Init(ctx, cfg, version.Version)
	if err != nil {
		slog.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}

	if cfg.Telemetry.Enabled {
		metricInterval := time.Duration(cfg.Telemetry.MetricInterval) * time.Second
		if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(metricInterval)); err != nil {
			slog.Error("failed to start runtime metrics", "error", err)
		}
	}

	// Ensure database directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		slog.Error("failed to create database directory", "error", err)
		os.Exit(1)
	}

	db, err := sqlite.NewConnection(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Wire dependencies (inward): infrastructure -> application -> domain.
	healthRepo := sqlite.NewHealthRepository(db)
	healthSvc := appservice.NewHealthService(healthRepo, version.Version)
	healthHandler := handler.NewHealthHandler(healthSvc)

	e := router.New(healthHandler, cfg.Telemetry.ServiceName)

	// Start server in a goroutine.
	go func() {
		addr := fmt.Sprintf(":%d", cfg.ServerPort)
		slog.Info("starting server", "addr", addr, "version", version.Version)
		if err := e.Start(addr); err != nil {
			slog.Info("server stopped", "error", err)
		}
	}()

	// Graceful shutdown on SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	if err := telemetryShutdown.Execute(shutdownCtx); err != nil {
		slog.Error("telemetry shutdown error", "error", err)
	}

	slog.Info("server exited gracefully")
}
