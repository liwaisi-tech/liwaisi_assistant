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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/billing"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/googleauth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/httpapi"
	storepostgres "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres"
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

	// ── Persistence layer (optional) ────────────────────────────────
	var serviceOpts []app.SessionServiceOption
	var store *storepostgres.Store
	registry := newServerFuncRegistry()
	toolReg := tools.NewRegistry()

	if dsn := os.Getenv("LIWAISI_DB_DSN"); dsn != "" {
		redisURL := envOr("LIWAISI_REDIS_URL", "redis://localhost:6379")
		migrationsPath := envOr("MIGRATIONS_PATH", "store/postgres/migrations")

		var err error
		store, err = storepostgres.NewStore(context.Background(), storepostgres.PoolConfig{DSN: dsn}, storepostgres.RedisConfig{URL: redisURL}, migrationsPath)
		if err != nil {
			logger.Error("store initialization failed", slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("persistence enabled", "postgres", "connected", "redis", "connected")

		// Register personality tools (requires persistence for personality repo).
		persDeps := &tools.PersonalityToolDeps{
			Repo:        store.Personalities(),
			DefaultPers: cpn.DefaultPersonality(),
		}
		if err := tools.RegisterPersonalityTools(toolReg, persDeps); err != nil {
			logger.Error("register personality tools failed", slog.Any("error", err))
			os.Exit(1)
		}
		toolReg.Seal()
		toolReg.InjectIntoFuncRegistry(registry)
		logger.Info("personality tools registered and sealed")

		serviceOpts = append(serviceOpts,
			app.WithPersistence(&app.PersistDeps{
				Sessions:     store.Sessions(),
				Events:       store.Events(),
				Ledger:       store.Ledger(),
				Flows:        store.Flows(),
				Intelligence: store.Intelligence(),
				FuncRegistry: registry,
			}),
			app.WithTokenLedger(&tokenLedgerAdapter{ledger: llmClient.TokenLedger}),
			app.WithToolRegistry(toolReg),
		)
	} else {
		logger.Info("persistence disabled (LIWAISI_DB_DSN not set)")
	}

	// ── Application layer ───────────────────────────────────────────
	appService := app.NewSessionService(llmClient, costProvider, logger, topologyFactory, serviceOpts...)

	// ── Billing adapter ────────────────────────────────────────────
	billingClient := billing.NewClient(apiKey, "https://openrouter.ai/api/v1")

	// ── Authentication adapter ──────────────────────────────────────
	var serverOpts []httpapi.ServerOption
	if googleClientID := os.Getenv("GOOGLE_CLIENT_ID"); googleClientID != "" {
		verifier := googleauth.NewGoogleTokenVerifier(googleClientID)
		serverOpts = append(serverOpts, httpapi.WithAuth(verifier))
		logger.Info("google auth enabled", slog.String("client_id", googleClientID[:min(len(googleClientID), 20)]+"..."))
	} else {
		logger.Info("google auth disabled (GOOGLE_CLIENT_ID not set); running in dev-mode")
	}

	// ── Driving adapter (HTTP server) ───────────────────────────────
	if store != nil {
		serverOpts = append(serverOpts,
			httpapi.WithRepos(store.Flows(), store.Intelligence(), store.Events()),
			httpapi.WithUserRepo(store.Users()),
			httpapi.WithPersonalityRepo(store.Personalities()),
			httpapi.WithToolRegistry(toolReg),
		)
	}
	srv := httpapi.NewServer(cfg, appService, logger, billingClient, serverOpts...)

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
	if store != nil {
		if err := store.Close(); err != nil {
			logger.Error("store close error", slog.Any("error", err))
		}
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

// tokenLedgerAdapter adapts openrouter.TokenLedger to app.TokenLedgerReader.
type tokenLedgerAdapter struct {
	ledger *openrouter.TokenLedger
}

func (a *tokenLedgerAdapter) Get(sessionID string) *app.TokenUsage {
	rec := a.ledger.Get(sessionID)
	if rec == nil {
		return nil
	}
	return &app.TokenUsage{
		InputTokens:  rec.InputTokens,
		OutputTokens: rec.OutputTokens,
		Calls:        rec.Calls,
		TotalCostUSD: rec.TotalCostUSD,
	}
}

// envOr returns the environment variable value or the default.
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envInt parses an integer from an environment variable.
func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
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
