// Package main is the composition root for the liwaisi HTTP server.
// It wires all dependencies following Hexagonal Architecture:
// driven adapters -> application layer -> driving adapter (HTTP).
package main

import (
	"context"
	"encoding/json"
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
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/a2a"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/httpapi"
	storepostgres "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel(),
	}))

	// ── Server config from environment (always from env, not DB) ───────
	addr := envOr("LISTEN_ADDR", ":8080")
	corsOrigins := strings.Split(envOr("CORS_ORIGINS", "*"), ",")

	cfg := httpapi.ServerConfig{
		Addr:            addr,
		ReadTimeout:     parseDuration("READ_TIMEOUT", 10*time.Second),
		IdleTimeout:     parseDuration("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: parseDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		AllowedOrigins:  corsOrigins,
	}

	// ── Persistence layer (optional) ────────────────────────────────────
	var serviceOpts []app.SessionServiceOption
	var store *storepostgres.Store
	registry := newServerFuncRegistry()
	toolReg := tools.NewRegistry()
	var configProvider *config.Provider

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
		)

		// ── Config provider (encrypted config in DB) ────────────────────
		masterKeyPath := envOr("LIWAISI_MASTER_KEY_PATH", "/data/secrets/master.key")
		masterKey, err := config.LoadOrGenerateMasterKey(masterKeyPath)
		if err != nil {
			logger.Error("master key initialization failed", slog.Any("error", err))
			os.Exit(1)
		}
		encryptor, err := config.NewEncryptor(masterKey)
		if err != nil {
			logger.Error("encryptor initialization failed", slog.Any("error", err))
			os.Exit(1)
		}
		configRepo := storepostgres.NewConfigRepository(store.Pool(), encryptor)
		configProvider = config.NewProvider(configRepo)
		if err := configProvider.Refresh(context.Background()); err != nil {
			logger.Warn("config refresh failed, starting with env/defaults", slog.Any("error", err))
		}
		logger.Info("config provider initialized")
	} else {
		logger.Info("persistence disabled (LIWAISI_DB_DSN not set)")
	}

	// ── Resolve config values (env > DB > default) ──────────────────────
	resolveConfig := func(key string) string {
		if configProvider != nil {
			return configProvider.Get(key)
		}
		def := config.DefByKey(key)
		if def == nil {
			return ""
		}
		if v := os.Getenv(def.EnvVar); v != "" {
			return v
		}
		return def.Default
	}

	apiKey := resolveConfig("openrouter_api_key")
	if apiKey == "" {
		logger.Warn("OPENROUTER_API_KEY not set; LLM calls will fail")
	}
	defaultModel := resolveConfig("default_model")

	// ── Driven adapters ─────────────────────────────────────────────────
	llmClient := openrouter.NewClient(apiKey, defaultModel)
	holder := config.NewLLMClientHolder(llmClient)
	costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

	if store != nil {
		serviceOpts = append(serviceOpts,
			app.WithTokenLedger(&tokenLedgerAdapter{ledger: llmClient.TokenLedger}),
			app.WithToolRegistry(toolReg),
		)
	}

	// ── Hot-reload: swap LLM client when config changes ─────────────────
	if configProvider != nil {
		configProvider.OnChange(func(key, _ string) {
			if key == "openrouter_api_key" || key == "default_model" {
				newAPIKey := configProvider.Get("openrouter_api_key")
				newModel := configProvider.Get("default_model")
				newClient := openrouter.NewClient(newAPIKey, newModel)
				holder.Swap(newClient)
				logger.Info("LLM client hot-reloaded", "trigger_key", key)
			}
		})
	}

	// ── Topology selection ──────────────────────────────────────────────
	var topologyFactory app.TopologyFactory
	switch envOr("TOPOLOGY", "unified") {
	case "simple":
		topologyFactory = defaultTopologyFactory
	case "hitl":
		topologyFactory = hitlTopologyFactory
	default:
		topologyFactory = unifiedTopologyFactory
	}

	// ── Application layer ───────────────────────────────────────────────
	appService := app.NewSessionService(holder, costProvider, logger, topologyFactory, serviceOpts...)

	// ── Billing adapter ─────────────────────────────────────────────────
	billingClient := billing.NewClient(apiKey, "https://openrouter.ai/api/v1")

	// ── Authentication adapter ──────────────────────────────────────────
	var serverOpts []httpapi.ServerOption
	googleClientID := resolveConfig("google_client_id")
	if googleClientID != "" {
		verifier := googleauth.NewGoogleTokenVerifier(googleClientID)
		serverOpts = append(serverOpts, httpapi.WithAuth(verifier))
		logger.Info("google auth enabled", slog.String("client_id", googleClientID[:min(len(googleClientID), 20)]+"..."))
	} else {
		logger.Info("google auth disabled (GOOGLE_CLIENT_ID not set); running in dev-mode")
	}

	// ── Rate limiting ──────────────────────────────────────────────────
	serverOpts = append(serverOpts, httpapi.WithRateLimiting(httpapi.DefaultRateLimitConfig()))

	// ── Admin config ───────────────────────────────────────────────────
	adminEmail := envOr("ADMIN_EMAIL", "dev@localhost")
	if configProvider != nil {
		serverOpts = append(serverOpts, httpapi.WithConfigProvider(configProvider))
	}
	serverOpts = append(serverOpts, httpapi.WithAdminEmail(adminEmail))
	logger.Info("admin email configured", slog.String("admin", adminEmail))

	// ── Driving adapter (HTTP server) ───────────────────────────────────
	if store != nil {
		serverOpts = append(serverOpts,
			httpapi.WithRepos(store.Flows(), store.Intelligence(), store.Events()),
			httpapi.WithUserRepo(store.Users()),
			httpapi.WithPersonalityRepo(store.Personalities()),
			httpapi.WithToolRegistry(toolReg),
			httpapi.WithWaitlistRepo(store.Waitlist()),
		)
	}
	srv := httpapi.NewServer(cfg, appService, logger, billingClient, serverOpts...)

	// Wire event callback: CPN events -> SSE broker.
	// HITL events are intercepted and re-emitted as A2UI stream chunks
	// so the frontend renders the review card natively through A2UI.
	appService.SetEventCallback(func(sessionID string, evt cpn.Event) {
		if evt.Type == cpn.EventStreamChunk {
			if chunk, ok := evt.Payload.(cpn.StreamChunk); ok {
				srv.Broker().PublishStreamChunk(sessionID, chunk)
				return
			}
		}

		// Emit the raw event for any SSE listeners (monitor, execution trace, etc.)
		srv.Broker().PublishEvent(sessionID, &evt)

		// For HITL requests, also emit an A2UI review card as a stream chunk.
		// The agent drives the UI: the backend decides what interface to show.
		//
		// When the transition already published its own A2UI surface (signalled
		// via HITLRequestedPayload{CustomSurface:true}), the default card is
		// suppressed — emitting both would concatenate two "$$a2ui:" chunks
		// into one streaming assistant message, breaking JSON.parse on the
		// frontend and falling through to raw markdown.
		if evt.Type == cpn.EventHITLRequested {
			prompt := "Please review and confirm."
			switch p := evt.Payload.(type) {
			case string:
				if p != "" {
					prompt = p
				}
			case cpn.HITLRequestedPayload:
				if p.CustomSurface {
					return // transition owns the surface; skip default card
				}
				if p.Prompt != "" {
					prompt = p.Prompt
				}
			}
			a2uiPayload := buildHITLReviewCard(prompt, evt.TransitionID)
			srv.Broker().PublishStreamChunk(sessionID, cpn.StreamChunk{
				SessionID: sessionID,
				CPNID:     evt.CPNID,
				CPNRole:   "review",
				Content:   a2uiPayload,
				Done:      false,
			})
			// Done sentinel for this A2UI message.
			srv.Broker().PublishStreamChunk(sessionID, cpn.StreamChunk{
				SessionID: sessionID,
				CPNID:     evt.CPNID,
				CPNRole:   "review",
				Content:   "",
				Done:      true,
			})
		}
	})

	// ── A2A protocol adapter (optional) ─────────────────────────────────
	// Composites A2A routes with the existing REST handler on the same addr.
	topHandler := srv.Handler()

	a2aCfg := a2a.ConfigFromEnv()
	if a2aCfg.IsEnabled() {
		a2aMapper := a2a.NewMapper()
		a2aExecutor := a2a.NewBRAEExecutor(appService, a2aMapper, logger)
		a2aCard := a2a.BuildAgentCard(a2aCfg, nil, nil, "0.1.0")

		a2aMux := http.NewServeMux()
		a2a.RegisterHandlers(a2aMux, a2aExecutor, a2aCard, nil, logger)

		// Composite handler: A2A paths handled by a2aMux, rest by httpapi.
		restHandler := topHandler
		topHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/.well-known/agent-card.json",
				r.URL.Path == "/a2a":
				a2aMux.ServeHTTP(w, r)
			default:
				restHandler.ServeHTTP(w, r)
			}
		})

		logger.Info("A2A protocol adapter registered",
			slog.String("base_url", a2aCfg.BaseURL),
		)
	}

	// ── Start server with graceful shutdown ─────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create a combined HTTP server so both REST and A2A share the same addr.
	httpSrv := &http.Server{
		Addr:        addr,
		Handler:     topHandler,
		ReadTimeout: cfg.ReadTimeout,
		IdleTimeout: cfg.IdleTimeout,
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	logger.Info("server started", slog.String("addr", addr))
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
	// Also shutdown the REST server's internal resources (SSE broker, etc).
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("rest server shutdown error", slog.Any("error", err))
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

// buildHITLReviewCard constructs an A2UI payload for HITL review.
// The backend drives the UI: approve, request changes, or discard.
func buildHITLReviewCard(prompt, transitionID string) string {
	type comp struct {
		Type     string         `json:"type"`
		Props    map[string]any `json:"props,omitempty"`
		Children []comp         `json:"children,omitempty"`
	}
	type payload struct {
		Components []comp `json:"components"`
	}

	p := payload{
		Components: []comp{
			{Type: "card", Props: map[string]any{"title": "Review Required"}, Children: []comp{
				{Type: "text", Props: map[string]any{"content": prompt}},
				{Type: "divider"},
				{Type: "text", Props: map[string]any{"content": "Choose an action to continue:", "variant": "secondary"}},
			}},
			{Type: "button", Props: map[string]any{
				"label": "✓ Approve", "variant": "success",
				"actionType": "hitl:approve", "id": transitionID,
			}},
			{Type: "button", Props: map[string]any{
				"label": "✎ Request Changes", "variant": "primary",
				"actionType": "hitl:revise", "id": transitionID,
			}},
			{Type: "button", Props: map[string]any{
				"label": "✗ Discard", "variant": "danger",
				"actionType": "hitl:reject", "id": transitionID,
			}},
		},
	}

	data, err := json.Marshal(p)
	if err != nil {
		return prompt // fallback to plain text
	}
	return "$$a2ui:" + string(data)
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
