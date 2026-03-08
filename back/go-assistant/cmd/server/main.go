// Package main is the entry point for the go-assistant microservice.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"net/http"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"

	agentv1 "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/api/gen/agent/v1"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	appservice "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/service"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/persistence/sqlite"
	agentgrpc "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/grpc"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/grpc/middleware"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/handler"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/router"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/telemetry"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

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

	ctx := context.Background()

	telemetryShutdown, err := telemetry.Init(ctx, cfg, version.Version)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	if cfg.Telemetry.Enabled {
		metricInterval := time.Duration(cfg.Telemetry.MetricInterval) * time.Second
		if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(metricInterval)); err != nil {
			slog.Error("failed to start runtime metrics", "error", err)
		}
	}

	// Ensure database directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sqlite.NewConnection(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Wire dependencies (inward): infrastructure -> application -> domain.
	healthRepo := sqlite.NewHealthRepository(db)
	healthSvc := appservice.NewHealthService(healthRepo, version.Version)
	healthHandler := handler.NewHealthHandler(healthSvc)

	e := router.New(healthHandler, cfg.Telemetry.ServiceName)

	llmCfg, err := configs.LoadLLMConfig()
	if err != nil {
		return fmt.Errorf("failed to load LLM config: %w", err)
	}

	llmClient, err := openrouter.NewClient(&openrouter.ClientConfig{
		APIKey:  llmCfg.APIKey,
		BaseURL: llmCfg.BaseURL,
		Model:   llmCfg.Model,
		Timeout: llmCfg.Timeout,
	})
	if err != nil {
		return fmt.Errorf("failed to create OpenRouter client: %w", err)
	}

	agentCfg := appservice.AgentConfig{
		Model:             llmCfg.Model,
		Temperature:       llmCfg.Temperature,
		MaxTokens:         llmCfg.MaxTokens,
		MaxToolIterations: llmCfg.MaxToolIterations,
		SystemPrompt:      "You are a helpful AI assistant connected via gRPC-Web. You operate in a web environment. Reply concisely.",
	}
	mem := memory.NewConversationMemory()
	registry := tool.NewRegistry()
	agentSvc := appservice.NewAgentService(llmClient, agentCfg, registry, mem)

	grpcServer := grpc.NewServer(
		grpc.StreamInterceptor(middleware.AuthInterceptor()),
	)
	agentServer := agentgrpc.NewAgentServer(agentSvc)
	agentv1.RegisterAgentServiceServer(grpcServer, agentServer)

	wrappedGrpc := grpcweb.WrapServer(grpcServer,
		grpcweb.WithOriginFunc(func(origin string) bool { return true }), // Allow all CORS for demo
	)

	// Multiplex gRPC-Web, standard gRPC, and REST endpoints.
	multiplexer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
			return
		}
		if wrappedGrpc.IsGrpcWebRequest(r) || wrappedGrpc.IsAcceptableGrpcCorsRequest(r) {
			wrappedGrpc.ServeHTTP(w, r)
			return
		}
		e.ServeHTTP(w, r)
	})

	// Start server in a goroutine.
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(multiplexer, &http2.Server{}),
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		slog.Info("starting server", "addr", addr, "version", version.Version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	grpcServer.GracefulStop()

	if err := telemetryShutdown.Execute(shutdownCtx); err != nil {
		slog.Error("telemetry shutdown error", "error", err)
	}

	slog.Info("server exited gracefully")
	return nil
}
