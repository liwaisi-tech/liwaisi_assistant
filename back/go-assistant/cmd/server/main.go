// Package main is the composition root for the liwaisi HTTP server.
// It wires all dependencies following Hexagonal Architecture:
// driven adapters → application layer → driving adapter (HTTP).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/billing"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel(),
	}))

	// ── Configuration from environment ──────────────────────────────
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		logger.Warn("OPENROUTER_API_KEY not set; LLM calls will fail")
	}

	addr := envOr("LISTEN_ADDR", ":8080")
	corsOrigins := strings.Split(envOr("CORS_ORIGINS", "*"), ",")
	defaultModel := envOr("DEFAULT_MODEL", "anthropic/claude-sonnet-4-6")

	cfg := httpapi.ServerConfig{
		Addr:            addr,
		ReadTimeout:     parseDuration("READ_TIMEOUT", 10*time.Second),
		IdleTimeout:     parseDuration("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: parseDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		AllowedOrigins:  corsOrigins,
	}

	// ── Driven adapters ─────────────────────────────────────────────
	llmClient := openrouter.NewClient(apiKey, defaultModel)
	costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

	// ── Topology selection ──────────────────────────────────────────
	var topologyFactory app.TopologyFactory
	switch envOr("TOPOLOGY", "unified") {
	case "simple":
		topologyFactory = defaultTopologyFactory
	case "hitl":
		topologyFactory = hitlTopologyFactory
	default:
		topologyFactory = unifiedTopologyFactory
	}

	// ── Application layer ───────────────────────────────────────────
	appService := app.NewSessionService(llmClient, costProvider, logger, topologyFactory)

	// ── Billing adapter ────────────────────────────────────────────
	billingClient := billing.NewClient(apiKey, "https://openrouter.ai/api/v1")

	// ── Driving adapter (HTTP server) ───────────────────────────────
	srv := httpapi.NewServer(cfg, appService, logger, billingClient)

	// Wire event callback: CPN events → SSE broker.
	appService.SetEventCallback(func(sessionID string, evt cpn.Event) {
		if evt.Type == cpn.EventStreamChunk {
			if chunk, ok := evt.Payload.(cpn.StreamChunk); ok {
				srv.Broker().PublishStreamChunk(sessionID, chunk)
				return
			}
		}
		srv.Broker().PublishEvent(sessionID, &evt)
	})

	// ── Start server with graceful shutdown ─────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	logger.Info("server started", slog.String("addr", addr))
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
	logger.Info("server stopped")
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// ledgerCostAdapter adapts TokenLedger to cpn.CostProvider.
type ledgerCostAdapter struct {
	ledger *openrouter.TokenLedger
}

func (a *ledgerCostAdapter) SessionCostUSD(sessionID string) float64 {
	rec := a.ledger.Get(sessionID)
	if rec == nil {
		return 0
	}
	return rec.TotalCostUSD
}


// envOr returns the environment variable value or the default.
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// parseDuration parses a duration from an environment variable.
func parseDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	return d
}

// slogLevel returns the slog level from LOG_LEVEL env var.
func slogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
