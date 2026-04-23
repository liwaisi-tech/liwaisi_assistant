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
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/architect"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis/jit"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/billing"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/googleauth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host/gate"
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

	// Load the controlled-vocabulary lexicon (spec-architecture-brae-toolbox-
	// taxonomy.md REQ-006, REQ-LEX-001). BRAE_LEXICON_PATH overrides the
	// embedded default; invalid overrides fail-fast here per AC-013.
	lex, err := tools.LoadLexicon(context.Background())
	if err != nil {
		logger.Error("load lexicon failed", slog.Any("error", err))
		os.Exit(1)
	}
	toolReg := tools.NewRegistry(tools.WithLexicon(lex))
	// Plumb the lexicon through to SessionService so the brae-awakens
	// bootstrap prompt gets the deterministic 32-entry excerpt + few-shot
	// anchors (spec-architecture-brae-awakening-toolbox-extension.md
	// REQ-002, AC-003). Must be appended BEFORE the awakening factory wires
	// up so Deps.Lexicon flows end-to-end.
	serviceOpts = append(serviceOpts, app.WithLexicon(lex))
	var configProvider *config.Provider

	// ── GAP-4 Safe primitive catalogue ──────────────────────────────────
	// Built before SessionService so every spawned CPN can dispatch
	// synthesize / instantiate transitions. The Bootstrap call installs
	// the linter, canonicaliser, materialiser, and digest hooks on cpn/.
	safeRegistry := synthesis.NewSafeRegistry()
	synthesis.RegisterDefaults(safeRegistry)
	// Contribute jit-fanout / jit-aggregate so JIT-composed topologies lint
	// (spec-architecture-brae-jit-cpn-builder.md §4.6). Must run BEFORE Seal.
	jit.Register(safeRegistry)
	if err := safeRegistry.LoadBashSnippetsFile(envOr("BASH_SNIPPETS_PATH", "cpn/synthesis/bash_snippets.yaml")); err != nil {
		logger.Warn("synthesis: load bash snippets failed", slog.Any("error", err))
	}
	safeRegistry.Seal()
	synthesis.Bootstrap(safeRegistry)
	// Plan i-need-you-make-playful-dongarra.md Phase 3: install the
	// TaskSpec→topology shortcut so parallel-fanout TaskSpecs bypass the
	// LLM authoring loop and use the deterministic jit template instead.
	jit.RegisterComposeHook()
	logger.Info("synthesis safe registry sealed", "primitives", len(safeRegistry.Names()))

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

		// ── Tool registry bootstrap (GAP-3) ────────────────────────────
		// Bootstrap reconciles Postgres-persisted tools into memory first
		// so builtin registrations run with awareness of any prior
		// agent-authored rows (REQ-050).
		toolRegistryRepo := storepostgres.NewToolRegistryStore(store.Pool())
		if err := toolReg.Bootstrap(context.Background(), toolRegistryRepo); err != nil {
			logger.Error("tool registry bootstrap failed", slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("tool registry bootstrapped from postgres")

		// Register personality tools (requires persistence for personality repo).
		persDeps := &tools.PersonalityToolDeps{
			Repo:        store.Personalities(),
			DefaultPers: cpn.DefaultPersonality(),
		}
		if err := tools.RegisterPersonalityTools(toolReg, persDeps); err != nil {
			logger.Error("register personality tools failed", slog.Any("error", err))
			os.Exit(1)
		}
		// Do NOT seal here — system tools (bash_exec, file_read, file_write)
		// are registered after hostAdapter/hostGateImpl are created below.

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
			app.WithSafeRegistry(safeRegistry),
			app.WithAuthoredFlowRepository(storepostgres.NewAuthoredFlowRepository(store.Pool())),
			app.WithApprovalStore(storepostgres.NewApprovalRepository(store.Pool())),
			app.WithPendingToolStore(storepostgres.NewPendingToolRepository(store.Pool())),
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
		// Dev-mode: use an in-memory AuthoredFlowRepository so the
		// synthesize / instantiate transitions still work end-to-end.
		serviceOpts = append(serviceOpts,
			app.WithSafeRegistry(safeRegistry),
			app.WithAuthoredFlowRepository(synthesis.NewMemoryAuthoredFlowRepository()),
		)
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
	// removed; ProductDefaultModel is the only default, unconditionally.
	logger.Info("using product default model", "model", openrouter.ProductDefaultModel)

	// ── Driven adapters ─────────────────────────────────────────────────
	llmClient := openrouter.NewClient(apiKey, openrouter.ProductDefaultModel)
	// Per-call audit recorder (writes one row per LLM invocation to llm_calls).
	// Only enabled when persistence is configured.
	var callRecorder openrouter.CallRecorder
	if store != nil {
		callRecorder = &llmCallRecorderAdapter{repo: store.LLMCalls(), logger: logger}
		llmClient.CallRecorder = callRecorder
	}
	holder := config.NewLLMClientHolder(llmClient)
	costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

	// Inject the product-default model from the infra layer at the composition
	// root so the app layer never imports infra/openrouter directly.
	serviceOpts = append(serviceOpts, app.WithDefaultModel(openrouter.ProductDefaultModel))

	if store != nil {
		serviceOpts = append(serviceOpts,
			app.WithTokenLedger(&tokenLedgerAdapter{ledger: llmClient.TokenLedger}),
			app.WithToolRegistry(toolReg),
			// REQ-GATE-001: validate every resolved model against the DB-backed
			// registry at session-creation time. Falls back to product default
			// with a WARN log when the gate fails (REQ-OBS-004).
			app.WithModelRegistry(store.ModelRegistry()),
			// Spec awakening-toolbox-extension REQ-006/007: surface the
			// toolbox aggregate to the personality injector on turn ≥ 2.
			app.WithToolboxLister(toolReg),
		)
	}

	// ── Hot-reload: swap LLM client when config changes ─────────────────
	// REQ-CFG-002: default_model is no longer a platform config key; only
	// the API key triggers a client swap. Model selection is resolved
	// per-session from user preferences with ProductDefaultModel as the
	// floor (see internal/app/session_service.go::applyUserModelPreferences).
	if configProvider != nil {
		configProvider.OnChange(func(key, _ string) {
			if key == "openrouter_api_key" {
				newAPIKey := configProvider.Get("openrouter_api_key")
				newClient := openrouter.NewClient(newAPIKey, openrouter.ProductDefaultModel)
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

	// ── Authored-artefact ledger (GAP-10) ───────────────────────────────
	// Wired ahead of the HostAdapter so we can pass it as a constructor
	// option. When persistence is disabled we fall back to the in-memory
	// ledger so local dev still exercises the write/rollback/restore
	// paths end-to-end.
	var artefactLedger persist.AuthoredArtefactLedger
	if store != nil {
		artefactLedger = storepostgres.NewAuthoredArtefactStore(store.Pool())
		logger.Info("authored artefact ledger enabled (postgres)")
	} else {
		artefactLedger = persist.NewMemoryArtefactLedger()
		logger.Info("authored artefact ledger enabled (in-memory)")
	}

	// ── Host runtime (GAP-1) ────────────────────────────────────────────
	// Single instance shared across all sessions: HostAdapter is stateless
	// per-call, BashSessionManager keeps its own sync, HostGate is pure.
	hostAdapter := host.NewOSHostAdapter(logger, host.WithArtefactLedger(artefactLedger))
	hostSessions := host.NewInMemoryBashSessionManager(logger)

	// ── HostGate (GAP-6) ────────────────────────────────────────────────
	// Load the declarative HostPolicy and construct the PolicyHostGate
	// with the first-run ledger, audit-log, budget tracker, and sandbox
	// capability. Falls back to AllowAllHostGate when the policy file is
	// missing or malformed — with a loud warning so operators notice.
	var hostPolicyHolder *gate.Holder
	var firstRunRepo persist.FirstRunRepository
	var gateDecisionsRepo persist.GateDecisionRepository
	var hostGateImpl cpn.HostGate = host.NewAllowAllHostGate()

	policyPath := envOr("HOST_POLICY_PATH", "infra/host/policies/default.yaml")
	learnedPath := envOr("HOST_POLICY_LEARNED_PATH", "infra/host/policies/learned.yaml")
	if policy, err := gate.LoadFromFile(policyPath); err != nil {
		logger.Warn("host gate: policy load failed, using allow-all gate", "path", policyPath, "error", err)
	} else {
		if err := gate.MergeLearnedFromFile(policy, learnedPath); err != nil {
			logger.Warn("host gate: learned overlay load failed", "path", learnedPath, "error", err)
		}
		hostPolicyHolder = gate.NewHolder(policy).WithLearnedPath(learnedPath)
		if store != nil {
			firstRunRepo = storepostgres.NewFirstRunStore(store.Pool())
			gateDecisionsRepo = storepostgres.NewGateDecisionsStore(store.Pool())
		}
		policyGate := gate.NewPolicyHostGate(
			hostPolicyHolder,
			firstRunRepo,
			gateDecisionsRepo,
			gate.NewBudgetTracker(),
			gate.NewStaticSandboxCapability(),
			logger,
		)
		hostGateImpl = policyGate
		logger.Info("host policy gate enabled", "path", policyPath)
	}

	hostRuntime := &cpn.HostRuntime{
		Adapter:  hostAdapter,
		Gate:     hostGateImpl,
		Sessions: hostSessions,
	}
	// Wire the HOST·HITL router so fire_bash can route ErrRequiresHITL
	// through the policy gate's HandleRequiresHITL helper. Only active when
	// the policy gate is installed — the allow-all fallback never raises
	// the sentinel.
	var awakeningModeReg *gate.AwakeningModeRegistry
	if pg, ok := hostGateImpl.(*gate.PolicyHostGate); ok {
		hostRuntime.HITLHandler = &gate.SessionHITLHandler{Gate: pg}
		// Share an awakening-mode registry between the gate and the
		// SessionService so silent-deny (CON-003 / AC-005) activates
		// for the right session while `brae-awakens` is driving it.
		awakeningModeReg = gate.NewAwakeningModeRegistry()
		pg.AwakeningMode = awakeningModeReg
		// Resolve the session id from ctx so the gate can check the
		// awakening-mode flag per-session.
		pg.SessionIDResolver = cpnSessionIDFromContext
	}
	serviceOpts = append(serviceOpts, app.WithHostRuntime(hostRuntime))
	logger.Info("host runtime initialized", "gate", gateKind(hostGateImpl))

	// ── Host capability repository + discovery factory (GAP-2) ──────────
	var hostCapRepo persist.HostCapabilityRepository
	if store != nil {
		hostCapRepo = store.HostCapability()
		serviceOpts = append(serviceOpts,
			app.WithHostCapabilityRepo(hostCapRepo),
			app.WithHostDiscoveryFactory(func(sid string) *cpn.CPN {
				return hostDiscoveryTopologyFactory(sid, HostDiscoveryDeps{
					Repository: hostCapRepo,
					Source:     persist.HostSnapshotSourceSession,
				})
			}),
		)
		logger.Info("host capability registry enabled")

		// ── brae-awakens topology (REQ-001, CON-001, AC-001) ──────────
		// The factory matches the signature SessionService expects; the
		// repo + host-id are closed over.
		serviceOpts = append(serviceOpts,
			app.WithAwakensFactory(func(sid string, deps awakens.Deps) *cpn.CPN {
				if deps.Repository == nil {
					deps.Repository = hostCapRepo
				}
				if deps.Source == "" {
					deps.Source = awakens.SourceAwakening
				}
				return awakens.TopologyFactory(sid, deps)
			}),
		)
		if awakeningModeReg != nil {
			serviceOpts = append(serviceOpts, app.WithAwakeningMode(awakeningModeReg))
		}
		// Wire the probe-fanout composer — fanout.Compose adapted into the
		// ComposerFunc signature the topology depends on.
		serviceOpts = append(serviceOpts, app.WithAwakeningComposer(newAwakeningComposerAdapter()))
		logger.Info("brae-awakens topology enabled")
	}

	// ── Mutation audit log (GAP-7 REQ-005) ─────────────────────────────
	// When persistence is enabled, every topology mutation attempt is logged
	// to cpn_mutations via the bridge adapter. When persistence is disabled
	// the log is nil and mutations are silently unaduited (dev-mode only).
	if store != nil {
		serviceOpts = append(serviceOpts,
			app.WithMutationLog(&mutationAuditAdapter{repo: store.Mutations()}),
		)
		logger.Info("topology mutation audit log enabled")
	}

	// ── System tools (GAP-11 REQ-001–REQ-006) ───────────────────────────
	// Register bash_exec, file_read, file_write BEFORE sealing so the
	// system namespace accepts them. hostAdapter and hostGateImpl are
	// available here; the tool registry is still open.
	if err := registerSystemTools(toolReg, hostAdapter, hostGateImpl); err != nil {
		logger.Error("system tools registration failed", slog.Any("error", err))
		os.Exit(1)
	}
	toolReg.Seal()
	toolReg.InjectIntoFuncRegistry(registry)
	logger.Info("system tools registered and registry sealed")

	// Wrap the topology factory to inject HostContextFormatter on every new
	// CPN (REQ-012). The formatter reads p-host-capabilities and formats a
	// preamble; it lives here so it can import cpn/persist without cycles.
	wrappedFactory := topologyFactory
	topologyFactory = func(sid string) *cpn.CPN {
		c := wrappedFactory(sid)
		c.HostContextFormatter = buildHostContextPreamble
		return c
	}

	// ── CLI flag --bootstrap-discovery ──────────────────────────────────
	if hasBootstrapDiscoveryFlag(os.Args) {
		if hostCapRepo == nil {
			logger.Error("--bootstrap-discovery requires LIWAISI_DB_DSN")
			os.Exit(1)
		}
		if err := runBootstrapDiscovery(logger, hostRuntime, hostCapRepo); err != nil {
			logger.Error("bootstrap discovery failed", slog.Any("error", err))
			os.Exit(1)
		}
		os.Exit(0)
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
			httpapi.WithToolboxLister(toolReg),
			httpapi.WithWaitlistRepo(store.Waitlist()),
			httpapi.WithModelRegistry(store.ModelRegistry()),
		)
	}
	// GAP-6 admin endpoints — wire the HostPolicy holder + stores. Each
	// option is a no-op when the dependency is nil so the endpoint
	// degrades to 503 instead of panicking.
	if hostPolicyHolder != nil {
		serverOpts = append(serverOpts, httpapi.WithHostPolicy(hostPolicyHolder))
	}
	if gateDecisionsRepo != nil {
		serverOpts = append(serverOpts, httpapi.WithGateDecisions(gateDecisionsRepo))
	}
	if firstRunRepo != nil {
		serverOpts = append(serverOpts, httpapi.WithFirstRun(firstRunRepo))
	}
	// GAP-2 host capability admin endpoints.
	if hostCapRepo != nil {
		serverOpts = append(serverOpts,
			httpapi.WithHostCapabilityAccessor(hostCapRepo),
			httpapi.WithHostIDResolver(&hostIDResolverAdapter{runtime: hostRuntime}),
			httpapi.WithHostDiscoveryRunner(&hostDiscoveryRunnerAdapter{
				repo:    hostCapRepo,
				runtime: hostRuntime,
			}),
		)
	}

	// ── GAP-10 rollback / purge services + admin endpoints ──────────────
	// ToolDeprecator bridges the rollback service to the cpn/tools
	// registry so binaries backing a registered tool get flagged
	// "artefact_rolled_back". Pure adapter — keeps cpn/persist
	// storage-agnostic.
	toolDeprecator := &toolRegistryDeprecator{registry: toolReg}
	rollbackSvc := persist.NewRollbackService(artefactLedger, hostAdapter.AllowedRoot, toolDeprecator, logger)
	purgeSvc := persist.NewPurgeService(artefactLedger, envInt("ARTEFACT_GRACE_DAYS", 7), logger)

	serverOpts = append(serverOpts,
		httpapi.WithArtefactLedger(artefactLedger),
		httpapi.WithArtefactRollback(rollbackSvc),
		httpapi.WithArtefactPurge(purgeSvc),
	)

	// GAP-4 admin flows surface. In prod we wire the postgres-backed
	// repo; dev mode stays 503 because no authored flows exist.
	if store != nil {
		serverOpts = append(serverOpts,
			httpapi.WithAuthoredFlows(storepostgres.NewAuthoredFlowRepository(store.Pool())),
		)
	}

	// GAP-8 skill manifest — aggregates builtins, tools, and host caps into
	// a single read-only endpoint + compact form for LLM classifier injection.
	{
		resolvedHostID, _ := os.Hostname()
		if resolvedHostID == "" {
			resolvedHostID = "localhost"
		}
		skillManifest := app.NewSkillManifestService(
			toolReg.Repository(),
			hostCapRepo,
			resolvedHostID,
			[]string{"classifier", "host-discovery", "tool-forge", "tool-atelier"},
		)
		serverOpts = append(serverOpts, httpapi.WithSkillManifest(skillManifest))
	}

	// ── CPN Agent Architect planner (spec-architecture-cpn-agent-architect
	// slices 1–4). Deterministic, LLM-free: library lookup + hashtag retriever
	// over the sealed tool registry, falling back to the JIT composer. Powers
	// POST /api/v1/flows so the frontend can forge flows independently of a
	// chat turn.
	flowLibrary := cpn.NewFlowLibrary()
	// Register built-in topologies so the architect planner can select them.
	registerToolAtelier(flowLibrary)
	flowPlanner := &architect.Planner{
		Library:      flowLibrary,
		Retriever:    architect.NewHashtagRetriever(toolReg),
		SafeRegistry: safeRegistry,
	}
	serverOpts = append(serverOpts, httpapi.WithFlowPlanner(flowPlanner))

	srv := httpapi.NewServer(cfg, appService, logger, billingClient, serverOpts...)

	// ── SC-13 synthesized-tool HITL broker ─────────────────────────────
	// Built after the HTTP server because it needs the SSE broker owned
	// by Server.Broker(). Attach to both the handler struct (so the POST
	// endpoint can resolve prompts) and the SessionService (so
	// toolapproval.Gate can emit them).
	toolApprovalBroker := httpapi.NewToolApprovalBroker(srv.Broker(), logger)
	srv.AttachToolApprovalBroker(toolApprovalBroker)
	appService.SetToolApprovalPrompter(toolApprovalBroker)

	// ── brae-awakens startup primer ────────────────────────────────────
	// Prime the host-capability snapshot + toolbox catalogue at boot so the
	// first interactive session finds a populated registry and the LLM's
	// "## Environment awareness" block lists every toolbox/tool. Without
	// this, awakening would run async AFTER injectPersonality has baked the
	// system prompt — the LLM would then answer "no tools" for the first
	// few turns. Bounded at 60s; a failure is logged but not fatal (dev
	// setups without an LLM provider still boot).
	primeCtx, cancelPrime := context.WithTimeout(context.Background(), 60*time.Second)
	if err := appService.PrimeHostCapabilities(primeCtx, true); err != nil {
		logger.Warn("brae-awakens startup primer failed", slog.Any("error", err))
	} else {
		logger.Info("brae-awakens startup primer complete")
	}
	cancelPrime()

	// Periodic refresh so newly-installed host binaries / updated personality
	// caches are picked up without a restart. Interval matches the 24h cache
	// TTL by default; override via LIWAISI_AWAKENING_REFRESH_INTERVAL.
	refreshInterval := parseDuration("LIWAISI_AWAKENING_REFRESH_INTERVAL", 24*time.Hour)
	if refreshInterval > 0 {
		refreshCtx, cancelRefresh := context.WithCancel(context.Background())
		defer cancelRefresh()
		go func() {
			ticker := time.NewTicker(refreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-refreshCtx.Done():
					return
				case <-ticker.C:
					cycleCtx, cancel := context.WithTimeout(refreshCtx, 60*time.Second)
					if err := appService.PrimeHostCapabilities(cycleCtx, true); err != nil {
						logger.Warn("brae-awakens refresh failed", slog.Any("error", err))
					} else {
						logger.Info("brae-awakens refresh complete")
					}
					cancel()
				}
			}
		}()
		logger.Info("brae-awakens refresh loop enabled", slog.Duration("interval", refreshInterval))
	}

	// ── Hourly purge goroutine ──────────────────────────────────────────
	// Runs under the process lifetime context so it exits with the
	// server. First tick fires at startup to clear any backlog.
	purgeCtx, cancelPurge := context.WithCancel(context.Background())
	go purgeSvc.Loop(purgeCtx, time.Hour)
	defer cancelPurge()

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

// toolRegistryDeprecator adapts *tools.Registry to the
// persist.ToolDeprecator interface consumed by the rollback service. The
// LookupByBinaryPath resolver scans every registered tool — fine at our
// scale, cheap enough to recompute on every rollback.
type toolRegistryDeprecator struct {
	registry *tools.Registry
}

func (t *toolRegistryDeprecator) Deprecate(ctx context.Context, qualifiedName, reason string) error {
	if t == nil || t.registry == nil {
		return nil
	}
	return t.registry.Deprecate(ctx, qualifiedName, reason)
}

func (t *toolRegistryDeprecator) LookupByBinaryPath(_ context.Context, binaryPath string) (string, bool) {
	if t == nil || t.registry == nil || binaryPath == "" {
		return "", false
	}
	for _, entry := range t.registry.ListFiltered(context.Background(), tools.ToolFilter{}) {
		if entry.BinaryPath == binaryPath {
			return entry.QualifiedName(), true
		}
	}
	return "", false
}

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

// mutationAuditAdapter bridges persist.MutationRepository to cpn.MutationAuditLog (GAP-7).
type mutationAuditAdapter struct {
	repo persist.MutationRepository
}

func (a *mutationAuditAdapter) LogMutation(ctx context.Context, cpnID, sessionID string, m cpn.Mutation, approved bool, rejectedReason string) error {
	return a.repo.Insert(ctx, &persist.MutationRecord{
		CPNID:          cpnID,
		SessionID:      sessionID,
		MutationKind:   string(m.Kind),
		RequestedBy:    m.RequestedBy,
		Reason:         m.Reason,
		Approved:       approved,
		RejectedReason: rejectedReason,
	})
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

// cpnSessionIDFromContext is the bridge the PolicyHostGate uses to recover
// the owning session ID from a CPN fire's ctx. The cpn executor threads the
// ID via cpn.WithSessionID before calling gate.Check (see fire_bash.go), so
// a thin shim is enough here — avoids leaking ctx keys into infra/host/gate.
func cpnSessionIDFromContext(ctx context.Context) string {
	return cpn.SessionIDFromContext(ctx)
}

// gateKind reports a short name for the active HostGate implementation
// so the boot log makes the choice visible.
func gateKind(g cpn.HostGate) string {
	switch g.(type) {
	case *gate.PolicyHostGate:
		return "policy"
	default:
		return "allow-all"
	}
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
