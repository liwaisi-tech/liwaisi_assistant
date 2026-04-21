package awakens

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// FlowName is the registry name used by session service + tests.
const FlowName = "brae-awakens"

// SourceAwakening is the `source` value written to host_capability_snapshots
// on the happy path (REQ-004). First-boot awakening MUST succeed via this
// path — there is no fallback source after the bootstrap-fix.
const SourceAwakening = "awakening"

// ── Place ids (exported for tests and wiring). ───────────────────────────────

const (
	PlaceAwakenTrigger      = "p-awaken-trigger"
	PlaceAwakenSystemPrompt = "p-awaken-system-prompt"
	PlaceAwakenShellCall    = "p-awaken-shell-call"
	PlaceAwakenShellResult  = "p-awaken-shell-result"
	PlaceAwakeningReport    = "p-awakening-report"
	PlaceHostCapabilitiesWK = cpn.WellKnownHostCapabilitiesPlace
	PlaceAwakeningSnapshot  = "p-awakening-snapshot"
	PlaceAwakeningMessage   = "p-awakening-message"
	PlaceAwakeningToolBatch = "p-awakening-toolbatch"
)

// ── Transition ids. ──────────────────────────────────────────────────────────

const (
	// TransitionAwakenLLMBootstrap is the first-turn LLM call. Its inputs
	// are (trigger, system-prompt) — both seeded at topology creation — so
	// it is enabled immediately on Run(). Eliminates the deadlock that the
	// pre-split single `t-awaken-llm` suffered (no initial shell-result).
	TransitionAwakenLLMBootstrap = "t-awaken-llm-bootstrap"
	// TransitionAwakenLLMFollowup handles any LLM iteration after a shell
	// probe has produced a p-awaken-shell-result token. fire_llm already
	// runs the tool-call loop in-process; this transition is vestigial in
	// the common path but preserved for CPN-level symmetry with future
	// places/flows that may route shell results through tokens.
	TransitionAwakenLLMFollowup   = "t-awaken-llm-followup"
	TransitionAwakenShell         = "t-awaken-shell"
	TransitionAwakenReport        = "t-awaken-report"
	TransitionAwakenPersist       = "t-awaken-persist"
	TransitionAwakenRegisterTools = "t-awaken-register-tools"
	TransitionAwakenEmitMessage   = "t-awaken-emit-message"
)

// Deps groups the collaborators the topology needs. All fields are optional;
// nil fields degrade to safe no-ops so the topology can be exercised in
// isolation without a live DB or LLM.
type Deps struct {
	Repository persist.HostCapabilityRepository
	HostID     string
	Source     string // Defaults to SourceAwakening when empty.
	Clock      Clock
}

// TopologyFactory constructs the `brae-awakens` CPN. The factory is designed
// to be composition-friendly: callers wire LLMClient, HostRuntime, and
// ToolRegistry on the returned CPN just as they do for any other flow.
//
// Wiring in production (see session_service.go):
//  1. Call TopologyFactory(sessionID, deps).
//  2. Set root.LLMClient, root.HostRuntime, root.ToolRegistry.
//  3. root.Run(ctx). The deadline is enforced by the caller.
//
// When root.Run returns an error, callers propagate it. There is no
// fallback — first-boot awakening must succeed via the LLM path.
func TopologyFactory(sessionID string, deps Deps) *cpn.CPN {
	places := map[string]*cpn.Place{
		PlaceAwakenTrigger:      cpn.NewPlace(PlaceAwakenTrigger, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakenSystemPrompt: cpn.NewPlace(PlaceAwakenSystemPrompt, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakenShellCall:    cpn.NewPlace(PlaceAwakenShellCall, cpn.ColorShellCmd, cpn.SpaceComputation),
		PlaceAwakenShellResult:  cpn.NewPlace(PlaceAwakenShellResult, cpn.ColorShellResult, cpn.SpaceComputation),
		// PlaceAwakeningReport holds the LLM's final structured output.
		// Color is ColorJSON because fire_llm deposits a ColorJSON token
		// whenever LLMConfig.RequireJSON is true (inferOutputColor).
		PlaceAwakeningReport:               cpn.NewPlace(PlaceAwakeningReport, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceHostCapabilitiesWK:            cpn.NewPlace(PlaceHostCapabilitiesWK, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningSnapshot:             cpn.NewPlace(PlaceAwakeningSnapshot, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningMessage:              cpn.NewPlace(PlaceAwakeningMessage, cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningToolBatch:            cpn.NewPlace(PlaceAwakeningToolBatch, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceAwakeningToolBatch + "-done":  cpn.NewPlace(PlaceAwakeningToolBatch+"-done", cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningMessage + "-emitted": cpn.NewPlace(PlaceAwakeningMessage+"-emitted", cpn.ColorEvent, cpn.SpaceComputation),
	}

	transitions := map[string]*cpn.Transition{
		TransitionAwakenLLMBootstrap:  newLLMBootstrapTransition(),
		TransitionAwakenLLMFollowup:   newLLMFollowupTransition(),
		TransitionAwakenShell:         newShellTransition(),
		TransitionAwakenReport:        newReportTransition(),
		TransitionAwakenPersist:       newPersistTransition(deps),
		TransitionAwakenRegisterTools: newRegisterToolsTransition(),
		TransitionAwakenEmitMessage:   newEmitMessageTransition(),
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-awakens", sessionID),
		FlowName,
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 8 // short awakening dialogue

	c.SeedFunc = seedAwakenTrigger
	seedAwakenTrigger(c)
	return c
}

// seedAwakenTrigger places a single start-token on p-awaken-trigger and the
// system-prompt token on p-awaken-system-prompt so the bootstrap LLM
// transition fires immediately on Run().
func seedAwakenTrigger(c *cpn.CPN) {
	if trigger, ok := c.Places[PlaceAwakenTrigger]; ok {
		_ = trigger.Deposit(&cpn.Token{
			Color:   cpn.ColorString,
			Space:   cpn.SpaceComputation,
			Payload: "awaken",
		})
	}
	if prompt, ok := c.Places[PlaceAwakenSystemPrompt]; ok {
		_ = prompt.Deposit(&cpn.Token{
			Color:   cpn.ColorString,
			Space:   cpn.SpaceComputation,
			Payload: SystemPrompt,
		})
	}
}

// newLLMBootstrapTransition is the first-turn LLM transition. Its inputs are
// the trigger + system-prompt tokens seeded at topology creation, so it is
// enabled immediately on Run() — no dependency on a not-yet-produced
// shell-result token. This is the core of the deadlock fix.
//
// Callers configure LLMClient on the owning CPN; LLMConfig is set here with
// RequireJSON=true so the LLM is constrained to emit a structured
// AwakeningReport JSON object.
func newLLMBootstrapTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenLLMBootstrap,
		cpn.NodeKindLLM,
		[]string{PlaceAwakenTrigger, PlaceAwakenSystemPrompt},
		[]string{PlaceAwakeningReport},
	)
	t.SystemPrompt = SystemPrompt
	t.LLMTools = []string{TransitionAwakenShell}
	t.LLMConfig = &cpn.LLMConfig{
		Role:        "awakening",
		MaxTokens:   2048,
		Temperature: 0.2,
		RequireJSON: true,
	}
	return t
}

// newLLMFollowupTransition handles any LLM iteration that depends on a
// p-awaken-shell-result token appearing. fire_llm's in-process tool-call
// loop normally produces the final report in a single bootstrap firing, so
// this transition rarely activates in practice — but it models the
// loop-turn input shape explicitly and keeps the topology self-describing.
func newLLMFollowupTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenLLMFollowup,
		cpn.NodeKindLLM,
		[]string{PlaceAwakenShellResult},
		[]string{PlaceAwakeningReport},
	)
	t.SystemPrompt = SystemPrompt
	t.LLMTools = []string{TransitionAwakenShell}
	t.LLMConfig = &cpn.LLMConfig{
		Role:        "awakening",
		MaxTokens:   2048,
		Temperature: 0.2,
		RequireJSON: true,
	}
	return t
}

// newShellTransition is `t-awaken-shell`. The executor wires it to the
// Host-adapter through the standard fire_bash path; the `introspection`
// policy class silently denies non-introspection commands.
func newShellTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenShell,
		cpn.NodeKindBash,
		[]string{PlaceAwakenShellCall},
		[]string{PlaceAwakenShellResult},
	)
	t.BashConfig = &cpn.BashConfig{
		Command:          "sh",
		Args:             []string{"-c", "true"}, // overridden per-call by fire_bash
		Timeout:          2 * time.Second,
		AllowNonZeroExit: true,
	}
	return t
}

// newReportTransition validates the LLM structured output into an
// AwakeningReport token. A validation failure returns a structured error;
// the caller may retry via the executor's retry policy.
func newReportTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenReport,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningReport},
		[]string{PlaceAwakeningSnapshot, PlaceAwakeningToolBatch, PlaceAwakeningMessage},
	)
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenReport)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		return map[string]cpn.Token{
			PlaceAwakeningSnapshot: {
				Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: report,
			},
			PlaceAwakeningToolBatch: {
				Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: report,
			},
			PlaceAwakeningMessage: {
				Color: cpn.ColorEvent, Space: cpn.SpaceComputation, Payload: report,
			},
		}, nil
	}
	return t
}

// newPersistTransition writes the projected snapshot via the repository and
// seeds the well-known p-host-capabilities place so later CPNs in the
// session see the fresh snapshot.
func newPersistTransition(deps Deps) *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenPersist,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningSnapshot},
		[]string{PlaceHostCapabilitiesWK},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenPersist)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		hostID := deps.HostID
		if hostID == "" {
			hostID = "awakening-" + uuid.NewString()
		}
		source := deps.Source
		if source == "" {
			source = SourceAwakening
		}
		clock := deps.Clock
		if clock == nil {
			clock = SystemClock
		}
		snap := report.Project(hostID, source, clock())
		if snap.ID == "" {
			snap.ID = uuid.NewString()
		}
		if deps.Repository != nil {
			if err := deps.Repository.Save(ctx, snap); err != nil {
				return nil, fmt.Errorf("%s: save snapshot: %w", TransitionAwakenPersist, err)
			}
		}
		return map[string]cpn.Token{
			PlaceHostCapabilitiesWK: {
				Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: snap,
			},
		}, nil
	}
	return t
}

// newRegisterToolsTransition wraps RegisterBatch — bound to
// MaxToolsToRegister registrations and idempotent.
func newRegisterToolsTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenRegisterTools,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningToolBatch},
		[]string{PlaceAwakeningToolBatch + "-done"},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		registry := toolRegistryFromContext(ctx)
		if registry == nil {
			return map[string]cpn.Token{}, nil
		}
		_, regErr := RegisterBatch(ctx, registry, report, nil)
		if regErr != nil {
			return nil, regErr
		}
		return map[string]cpn.Token{}, nil
	}
	return t
}

// newEmitMessageTransition builds the A2UI envelope. Downstream session
// plumbing (emit-first-assistant-message) consumes the resulting Event
// token and persists it as the session's first `messages` row with
// cpn_role = "awakening".
func newEmitMessageTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenEmitMessage,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningMessage},
		[]string{PlaceAwakeningMessage + "-emitted"},
	)
	t.ToolHandler = func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		envelope := BuildFirstTurnMessage(report)
		return map[string]cpn.Token{
			PlaceAwakeningMessage + "-emitted": {
				Color:   cpn.ColorEvent,
				Space:   cpn.SpaceComputation,
				Payload: envelope,
			},
		}, nil
	}
	return t
}

// coerceReport tolerates the three shapes the LLM layer may hand us: a
// typed AwakeningReport, a RawMessage, or a raw string. Parse + validate is
// centralised here so every downstream transition agrees on the contract.
func coerceReport(payload any) (AwakeningReport, error) {
	switch v := payload.(type) {
	case AwakeningReport:
		if err := v.Validate(); err != nil {
			return AwakeningReport{}, err
		}
		return v, nil
	case *AwakeningReport:
		if v == nil {
			return AwakeningReport{}, fmt.Errorf("%w: nil *AwakeningReport", ErrInvalidReport)
		}
		if err := v.Validate(); err != nil {
			return AwakeningReport{}, err
		}
		return *v, nil
	case json.RawMessage:
		return ParseReport(v)
	case []byte:
		return ParseReport(v)
	case string:
		return ParseReport([]byte(v))
	default:
		raw, err := json.Marshal(payload)
		if err != nil {
			return AwakeningReport{}, fmt.Errorf("%w: unsupported payload %T", ErrInvalidReport, payload)
		}
		return ParseReport(raw)
	}
}

// ── Tool-registry context plumbing ──────────────────────────────────────────
//
// The CPN Tool-transition API passes only context + consumed tokens. To
// access the session-scoped ToolRegistry from within a Tool handler, we
// thread it through context.Context. Callers (session_service.go) attach
// the registry via WithToolRegistry before CPN.Run().

type ctxKeyToolRegistry struct{}

// WithToolRegistry embeds a registry reference into ctx for the awakening
// register-tools transition to pick up. Returns ctx unchanged when reg is
// nil so callers can pass through without branching.
func WithToolRegistry(ctx context.Context, reg cpn.ToolRegistry) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyToolRegistry{}, reg)
}

// toolRegistryFromContext retrieves the registry attached by
// WithToolRegistry. Returns nil when absent (the transition no-ops).
func toolRegistryFromContext(ctx context.Context) cpn.ToolRegistry {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(ctxKeyToolRegistry{}).(cpn.ToolRegistry)
	return v
}
