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
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
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
				Users:        store.Users(),
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
	// REQ-MIG-003: record the single product default at startup. The legacy
	// "default_model" config key and its DEFAULT_MODEL env variable were
	// removed; PRODUCT_DEFAULT_MODEL is the only default, unconditionally.
	logger.Info("using product default model", "model", openrouter.PRODUCT_DEFAULT_MODEL)

	// ── Driven adapters ─────────────────────────────────────────────────
	llmClient := openrouter.NewClient(apiKey, openrouter.PRODUCT_DEFAULT_MODEL)
	// Per-call audit recorder (writes one row per LLM invocation to llm_calls).
	// Only enabled when persistence is configured.
	var callRecorder openrouter.CallRecorder
	if store != nil {
		callRecorder = &llmCallRecorderAdapter{repo: store.LLMCalls(), logger: logger}
		llmClient.CallRecorder = callRecorder
	}
	holder := config.NewLLMClientHolder(llmClient)
	costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

	if store != nil {
		serviceOpts = append(serviceOpts,
			app.WithTokenLedger(&tokenLedgerAdapter{ledger: llmClient.TokenLedger}),
			app.WithToolRegistry(toolReg),
			// REQ-GATE-001: validate every resolved model against the DB-backed
			// registry at session-creation time. Falls back to product default
			// with a WARN log when the gate fails (REQ-OBS-004).
			app.WithModelRegistry(store.ModelRegistry()),
		)
	}

	// ── Hot-reload: swap LLM client when config changes ─────────────────
	// REQ-CFG-002: default_model is no longer a platform config key; only
	// the API key triggers a client swap. Model selection is resolved
	// per-session from user preferences with PRODUCT_DEFAULT_MODEL as the
	// floor (see internal/app/session_service.go::applyUserModelPreferences).
	if configProvider != nil {
		configProvider.OnChange(func(key, _ string) {
			if key == "openrouter_api_key" {
				newAPIKey := configProvider.Get("openrouter_api_key")
				newClient := openrouter.NewClient(newAPIKey, openrouter.PRODUCT_DEFAULT_MODEL)
				newClient.CallRecorder = callRecorder
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
	case "manage-models":
		// manage-models-flow fragment — REQ-GAP-CPN-001. Selectable via env
		// for tests and targeted dev runs; the production session service
		// integrates the fragment into its classifier routing in a follow-up
		// slice (see spec-process-model-admin-in-chat-gap-closure.md §12).
		topologyFactory = manageModelsTopologyFactoryForSession
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
	if configProvider != nil {
		serverOpts = append(serverOpts, httpapi.WithConfigProvider(configProvider))
	}
	adminEmails := resolveAdminEmails(logger)
	serverOpts = append(serverOpts, httpapi.WithAdminEmails(adminEmails))
	logger.Info("admin emails configured", slog.Int("count", len(adminEmails)))

	// ── Audit repo ─────────────────────────────────────────────────────
	if store != nil {
		auditRepo := storepostgres.NewAuditRepository(store.Pool())
		serverOpts = append(serverOpts, httpapi.WithAuditRepo(auditRepo))
	}

	// ── Driving adapter (HTTP server) ───────────────────────────────────
	if store != nil {
		serverOpts = append(serverOpts,
			httpapi.WithRepos(store.Flows(), store.Intelligence(), store.Events()),
			httpapi.WithUserRepo(store.Users()),
			httpapi.WithPersonalityRepo(store.Personalities()),
			httpapi.WithToolRegistry(toolReg),
			httpapi.WithWaitlistRepo(store.Waitlist()),
			httpapi.WithModelRegistry(store.ModelRegistry()),
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

		// For HITL requests, the backend drives the UI by emitting an A2UI
		// review card as a stream chunk. Because the surface is already
		// server-owned, we must mark the event with CustomSurface:true
		// BEFORE publishing it, so the frontend's onHITLRequested handler
		// suppresses its own default bubble (otherwise the reducer creates
		// a redundant empty assistant bubble alongside the review card).
		//
		// When the transition itself already published an A2UI surface
		// (e.g. t-clarify via A2UIPayloadBuilder), it signals via
		// HITLRequestedPayload{CustomSurface:true} and we skip emitting
		// the default card — two "$$a2ui:" chunks would concatenate into
		// one streaming assistant message, breaking JSON.parse on the
		// frontend and falling through to raw markdown.
		if evt.Type == cpn.EventHITLRequested {
			prompt := "Please review and confirm."
			transitionOwnsSurface := false
			switch p := evt.Payload.(type) {
			case string:
				if p != "" {
					prompt = p
				}
			case cpn.HITLRequestedPayload:
				if p.CustomSurface {
					transitionOwnsSurface = true
				}
				if p.Prompt != "" {
					prompt = p.Prompt
				}
			}

			if !transitionOwnsSurface {
				// Rewrite the payload so the frontend knows the surface
				// is already on the wire and doesn't render a duplicate.
				evt.Payload = cpn.HITLRequestedPayload{Prompt: prompt, CustomSurface: true}
			}

			srv.Broker().PublishEvent(sessionID, &evt)

			if !transitionOwnsSurface {
				// REQ-PAR-003: when the A2UI questionnaire builder on
				// t-clarify fails (e.g. the selected model emitted
				// undecodable JSON), fireHITL falls back to
				// CustomSurface:false and the integration layer emits the
				// generic Review Required card above. That alone strands
				// the user — they see a card with no context. Here we
				// also publish a plain-text banner (no $$a2ui: prefix, so
				// it renders as a regular assistant bubble) explaining
				// what happened and how to recover.
				//
				// Design note: we use transition identity (t-clarify) as
				// the signal rather than a new side-channel field on
				// HITLRequestedPayload, because the invariant "t-clarify
				// always owns its surface on success" is already baked
				// into the topology. A CustomSurface:false for t-clarify
				// is therefore a reliable proxy for an A2UIPayloadBuilder
				// failure without touching cpn/hitl.go.
				if evt.TransitionID == "t-clarify" {
					const banner = "Your selected model did not return a valid questionnaire. You can approve the request as-is, reject it, or change the model in Settings."
					srv.Broker().PublishStreamChunk(sessionID, cpn.StreamChunk{
						SessionID: sessionID,
						CPNID:     evt.CPNID,
						CPNRole:   "review",
						Content:   banner,
						Done:      false,
					})
					srv.Broker().PublishStreamChunk(sessionID, cpn.StreamChunk{
						SessionID: sessionID,
						CPNID:     evt.CPNID,
						CPNRole:   "review",
						Content:   "",
						Done:      true,
					})
				}

				// REQ-BE-003 / INV-005: reaching this branch indicates a
				// misconfigured t-review topology (missing A2UIPayloadBuilder
				// on the transition). The surface is still emitted so the
				// user is not stranded, but NO history row is persisted here
				// — only the transition-owned A2UIPayloadBuilder path in
				// fireHITL feeds c.History. A WARN line is the regression
				// signal on-call greps for. PAT-003. No request-scoped ctx
				// is available inside the event-subscriber callback, so we
				// use context.Background() — trace correlation happens via
				// the session_id / transition_id fields below.
				slog.WarnContext(context.Background(), "legacy review-card emit path used (NOT persisted) — t-review topology may be misconfigured",
					"session_id", sessionID,
					"cpn_id", evt.CPNID,
					"transition_id", evt.TransitionID,
					"prompt_len", len(prompt),
				)

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
			return
		}

		// Emit the raw event for any SSE listeners (monitor, execution trace, etc.)
		srv.Broker().PublishEvent(sessionID, &evt)
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
			switch r.URL.Path {
			case "/.well-known/agent-card.json", "/a2a":
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

// llmCallRecorderAdapter bridges openrouter.CallRecorder (the LLM hot path)
// to persist.LLMCallRepository (the Postgres audit log). Each RecordCall
// dispatches a background goroutine with a fresh context so the LLM caller
// never blocks on the database.
type llmCallRecorderAdapter struct {
	repo   persist.LLMCallRepository
	logger *slog.Logger
}

func (a *llmCallRecorderAdapter) RecordCall(_ context.Context, rec openrouter.CallRecord) {
	if a == nil || a.repo == nil {
		return
	}

	msgsJSON, err := json.Marshal(rec.RequestMessages)
	if err != nil {
		// Fall back to an empty array so the JSONB column stays valid.
		msgsJSON = []byte("[]")
	}

	pr := &persist.LLMCallRecord{
		SessionID:           rec.SessionID,
		TransitionID:        rec.TransitionID,
		CPNID:               rec.CPNID,
		ModelRequested:      rec.ModelRequested,
		ModelResolved:       rec.ModelResolved,
		Endpoint:            rec.Endpoint,
		Streamed:            rec.Streamed,
		InputTokens:         rec.InputTokens,
		OutputTokens:        rec.OutputTokens,
		CacheReadTokens:     rec.CacheReadTokens,
		CacheCreationTokens: rec.CacheCreationTokens,
		ReasoningTokens:     rec.ReasoningTokens,
		CostUSD:             rec.CostUSD,
		RequestMessages:     msgsJSON,
		ResponseText:        rec.ResponseText,
		FinishReason:        rec.FinishReason,
		Error:               rec.Error,
		DurationMs:          rec.Duration.Milliseconds(),
		CreatedAt:           rec.CreatedAt,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.repo.Record(ctx, pr); err != nil {
			a.logger.Warn("llm call audit record failed",
				slog.String("session_id", pr.SessionID),
				slog.String("model", pr.ModelResolved),
				slog.Any("error", err),
			)
		}
	}()
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

// resolveAdminEmails reads ADMIN_EMAILS (or legacy ADMIN_EMAIL), parses it,
// and validates per the truth table in spec §4.3. Fails fatally on the
// forbidden non-dev cases. Logs a warning when dev defaults are in play.
func resolveAdminEmails(logger *slog.Logger) []string {
	devMode := strings.EqualFold(os.Getenv("APP_ENV"), "dev")

	raw := os.Getenv("ADMIN_EMAILS")
	if raw == "" {
		raw = os.Getenv("ADMIN_EMAIL") // legacy fallback
	}

	emails := httpapi.ParseAdminEmails(raw)

	if len(emails) == 0 {
		if devMode {
			logger.Warn("ADMIN_EMAILS unset; defaulting to dev@localhost (DEV ONLY)")
			return []string{"dev@localhost"}
		}
		logger.Error("ADMIN_EMAILS is required outside dev mode")
		os.Exit(1)
	}

	hasForbidden := false
	for _, e := range emails {
		if e == "" || e == "dev@localhost" {
			hasForbidden = true
			break
		}
	}
	if hasForbidden {
		if devMode {
			logger.Warn("ADMIN_EMAILS contains dev@localhost (DEV ONLY)")
			return emails
		}
		logger.Error("ADMIN_EMAILS contains a forbidden entry (empty or dev@localhost) outside dev mode")
		os.Exit(1)
	}
	return emails
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
//
// This is a thin wrapper over reviewCardPayload (topologies.go). The
// transition-owned A2UIPayloadBuilder on t-review uses reviewCardPayload
// directly via buildReviewA2UIPayload. This legacy-branch wrapper exists so
// cmd/server/main.go's defensive WARN-and-emit fallback (PAT-003) produces
// a byte-identical card shape if a future t-review ships without its
// A2UIPayloadBuilder.
//
// The `prompt` argument is retained for call-site compatibility but is
// ignored — the card's primary text is fixed in reviewCardPayload so live
// and rehydrated renders converge. If a custom prompt is ever needed in
// this branch, it should be threaded through reviewCardPayload instead.
func buildHITLReviewCard(_, transitionID string) string {
	payload := reviewCardPayload(transitionID)
	data, err := json.Marshal(payload)
	if err != nil {
		return "" // caller's WARN already fired; swallow to avoid stranding the SSE stream
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
