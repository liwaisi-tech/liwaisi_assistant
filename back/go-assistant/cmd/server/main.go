// Package main is the composition root for the liwaisi HTTP server.
// It wires all dependencies following Hexagonal Architecture:
// driven adapters → application layer → driving adapter (HTTP).
package main

import (
	"context"
	"errors"
	"fmt"
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

// defaultTopologyFactory creates a minimal CPN topology for a session:
// p-input → t-llm → p-output.
func defaultTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}
	tLLM := cpn.NewTransition("t-llm", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-output"})
	tLLM.SystemPrompt = "You are a helpful assistant. Be concise."
	tLLM.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    1024,
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-llm": tLLM,
	}
	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// hitlTopologyFactory creates a CPN topology with a human-review gate:
// p-input → t-plan (LLM) → p-plan → t-review (HITL) → p-reviewed → t-execute (LLM) → p-output.
//
// The assistant first presents a plan, waits for user approval, then executes.
// Channel on t-review is left nil — wired by SessionService.CreateSession.
func hitlTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":    cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-plan":     cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed": cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":   cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	tPlan := cpn.NewTransition("t-plan", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-plan"})
	tPlan.SystemPrompt = "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. " +
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\""
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    1024,
		Temperature:  0.7,
		StreamOutput: true,
	}

	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = "You are a helpful assistant. The user approved the following plan. " +
		"Execute it thoroughly and provide the final result."
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    2048,
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-plan":    tPlan,
		"t-review":  tReview,
		"t-execute": tExecute,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// unifiedTopologyFactory creates a CPN topology with an intent classifier router:
//
//	p-input → t-classify (LLM, classifier model, JSON)
//	  → p-classified
//	    ├→ t-direct (guard: conversation) → p-output
//	    └→ t-plan (guard: task) → p-plan → t-review (HITL) → p-reviewed → t-execute → p-output
//
// The classifier routes greetings/questions to a direct response and complex tasks
// to the plan-review-execute HITL flow. Follows the Router Pattern (Arize, BSWEN 2026).
func unifiedTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":      cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-classified": cpn.NewPlace("p-classified", cpn.ColorJSON, cpn.SpaceSurface),
		"p-plan":       cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed":   cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":     cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	// t-classify: fast intent classifier using lightweight model.
	tClassify := cpn.NewTransition("t-classify", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-classified"})
	tClassify.SystemPrompt = `You are an intent classifier. Classify the user's message into exactly one category.
Respond with JSON only, no explanation.

Categories:
- "conversation": greetings, casual chat, simple factual questions, clarifications, thank you messages
- "task": requests requiring planning, multi-step work, code generation, document creation, analysis, implementation

Examples:
User: "Hola, ¿cómo estás?" → {"intent":"conversation"}
User: "What is a Petri net?" → {"intent":"conversation"}
User: "Create a plan to implement a CNN in R" → {"intent":"task"}
User: "Refactor the authentication module" → {"intent":"task"}
User: "Thanks!" → {"intent":"conversation"}

Respond ONLY with the JSON object.`
	tClassify.LLMConfig = &cpn.LLMConfig{
		Model:        "classifier",
		MaxTokens:    64,
		Temperature:  0.0,
		RequireJSON:  true,
		StreamOutput: false,
		SkipHistory:  true, // Classify each message independently, without conversation bias.
	}

	// t-direct: fires for conversation intent — direct streaming response.
	tDirect := cpn.NewTransition("t-direct", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-output"})
	tDirect.SystemPrompt = "You are a helpful, friendly assistant. Respond naturally and concisely."
	tDirect.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    1024,
		Temperature:  0.7,
		StreamOutput: true,
	}
	tDirect.Guard = func(tokens []*cpn.Token) bool {
		for _, tok := range tokens {
			if s, ok := tok.Payload.(string); ok {
				return strings.Contains(strings.ToLower(s), `"conversation"`)
			}
		}
		return false
	}

	// t-plan: fires for task intent — presents a plan for review.
	tPlan := cpn.NewTransition("t-plan", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-plan"})
	tPlan.SystemPrompt = "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. " +
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\""
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    1024,
		Temperature:  0.7,
		StreamOutput: true,
	}
	tPlan.Guard = func(tokens []*cpn.Token) bool {
		for _, tok := range tokens {
			if s, ok := tok.Payload.(string); ok {
				return !strings.Contains(strings.ToLower(s), `"conversation"`)
			}
		}
		return true // ambiguous → default to task (safer)
	}

	// t-review: HITL gate — waits for user approval.
	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	// t-execute: fires after approval — executes the plan.
	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = "You are a helpful assistant. The user approved the following plan. " +
		"Execute it thoroughly and provide the final result."
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    2048,
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-classify": tClassify,
		"t-direct":   tDirect,
		"t-plan":     tPlan,
		"t-review":   tReview,
		"t-execute":  tExecute,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
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
