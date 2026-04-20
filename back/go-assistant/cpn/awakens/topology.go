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
	TransitionAwakenLLM           = "t-awaken-llm"
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
	Source     string // SourceAwakening or SourceAwakeningFallback
	Clock      Clock
}

// TopologyFactory constructs the `brae-awakens` CPN. The factory is designed
// to be composition-friendly: callers wire LLMClient, HostRuntime, and
// ToolRegistry on the returned CPN just as they do for any other flow.
//
// Wiring in production (see session_service.go):
//  1. Call TopologyFactory(sessionID, deps).
//  2. Set root.LLMClient, root.HostRuntime, root.ToolRegistry.
//  3. root.Run(ctx). The deadline is enforced by the caller (CON-001: 30s).
//
// When the LLM is unreachable, callers invoke RunFallback instead; the
// factory still compiles so legacy compat (CON-006) holds.
func TopologyFactory(sessionID string, deps Deps) *cpn.CPN {
	places := map[string]*cpn.Place{
		PlaceAwakenTrigger:      cpn.NewPlace(PlaceAwakenTrigger, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakenSystemPrompt: cpn.NewPlace(PlaceAwakenSystemPrompt, cpn.ColorString, cpn.SpaceComputation),
		PlaceAwakenShellCall:    cpn.NewPlace(PlaceAwakenShellCall, cpn.ColorShellCmd, cpn.SpaceComputation),
		PlaceAwakenShellResult:  cpn.NewPlace(PlaceAwakenShellResult, cpn.ColorShellResult, cpn.SpaceComputation),
		PlaceAwakeningReport:    cpn.NewPlace(PlaceAwakeningReport, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceHostCapabilitiesWK: cpn.NewPlace(PlaceHostCapabilitiesWK, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningSnapshot:  cpn.NewPlace(PlaceAwakeningSnapshot, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningMessage:   cpn.NewPlace(PlaceAwakeningMessage, cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningToolBatch:             cpn.NewPlace(PlaceAwakeningToolBatch, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceAwakeningToolBatch + "-done":   cpn.NewPlace(PlaceAwakeningToolBatch+"-done", cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningMessage + "-emitted":  cpn.NewPlace(PlaceAwakeningMessage+"-emitted", cpn.ColorEvent, cpn.SpaceComputation),
	}

	transitions := map[string]*cpn.Transition{
		TransitionAwakenLLM:           newLLMTransition(),
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
// system-prompt token on p-awaken-system-prompt so the LLM transition fires
// immediately on Run().
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

// newLLMTransition is the `t-awaken-llm` transition. It consumes the trigger
// + system prompt + any shell results and emits either a shell call or the
// terminal report. Wired as NodeKindLLM so fire_llm.go drives the model.
//
// Callers configure LLMClient/LLMConfig on the returned CPN before Run().
func newLLMTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenLLM,
		cpn.NodeKindLLM,
		[]string{PlaceAwakenTrigger, PlaceAwakenSystemPrompt, PlaceAwakenShellResult},
		[]string{PlaceAwakenShellCall, PlaceAwakeningReport},
	)
	t.SystemPrompt = SystemPrompt
	t.LLMTools = []string{TransitionAwakenShell}
	return t
}

// newShellTransition is `t-awaken-shell`. The executor wires it to the
// Host-adapter through the standard fire_bash path; the `introspection`
// policy class silently denies non-introspection commands per CON-003.
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
		Timeout:          2 * time.Second,        // CON-002: hostDiscoveryProbeTimeout
		AllowNonZeroExit: true,
	}
	return t
}

// newReportTransition validates the LLM structured output into an
// AwakeningReport token (§9.4). A validation failure returns a structured
// error; the caller retries up to 2 times (handled by executor retry
// policy) then falls through to awakening-fallback.
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
// ALSO seeds the well-known p-host-capabilities place so later CPNs in the
// session see the fresh snapshot (REQ-007, PAT-002 consumer #1).
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

// newRegisterToolsTransition is a ColorArtifact Tool that wraps
// RegisterBatch — bound to ≤MaxToolsToRegister registrations (CON-004) and
// idempotent (CON-005).
func newRegisterToolsTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenRegisterTools,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningToolBatch},
		[]string{PlaceAwakeningToolBatch + "-done"},
	)
	// Ensure the output place exists.
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		// The ToolRegistry is attached on the owning CPN; we read it via
		// the CPN field indirection. Because Transition.ToolHandler does
		// not receive *CPN, we stash the registry on the CPN and late-bind
		// via a closure set by SetToolRegistry.
		registry := toolRegistryFromContext(ctx)
		if registry == nil {
			// No registry wired — treat as no-op (dev/test mode).
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
// cpn_role = "awakening" (AC-001).
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
