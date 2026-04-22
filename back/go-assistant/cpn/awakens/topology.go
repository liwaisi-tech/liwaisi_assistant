package awakens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
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
	// PlaceAwakenPlan carries the AwakeningProbePlan JSON emitted by the
	// bootstrap LLM. The probe-compose transition consumes it.
	PlaceAwakenPlan = "p-awaken-plan"
	// PlaceAwakenSubnetSpec carries the composed *cpn.CPN describing the
	// probe-fanout sub-net. The probe-instantiate transition consumes it.
	PlaceAwakenSubnetSpec = "p-awaken-subnet-spec"
	// PlaceAwakeningReport holds the reducer-assembled AwakeningReport
	// emitted by the fanout sub-CPN (NOT raw LLM output).
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
	// it is enabled immediately on Run(). Emits an AwakeningProbePlan JSON
	// on p-awaken-plan (probe-fanout v0).
	TransitionAwakenLLMBootstrap = "t-awaken-llm-bootstrap"
	// TransitionAwakenProbeCompose materialises the probe-fanout sub-CPN
	// from the plan. NodeKindTool so we can plug the composer directly.
	TransitionAwakenProbeCompose = "t-awaken-probe-compose"
	// TransitionAwakenProbeInstantiate spawns the composed sub-CPN and
	// bridges the aggregated AwakeningReport back onto
	// p-awakening-report. Implemented as a NodeKindTool rather than
	// NodeKindInstantiate because fireInstantiate round-trips through a
	// FlowRepository (persist.CPNTopology JSON) and probe-fanout v0 skips
	// that persistence hop by design (CON-006 of
	// spec-architecture-brae-awakening-probe-fanout.md).
	TransitionAwakenProbeInstantiate = "t-awaken-probe-instantiate"
	TransitionAwakenReport           = "t-awaken-report"
	TransitionAwakenPersist          = "t-awaken-persist"
	TransitionAwakenRegisterTools    = "t-awaken-register-tools"
	TransitionAwakenEmitMessage      = "t-awaken-emit-message"
)

// Deps groups the collaborators the topology needs. All fields are optional;
// nil fields degrade to safe no-ops so the topology can be exercised in
// isolation without a live DB or LLM.
type Deps struct {
	Repository persist.HostCapabilityRepository
	HostID     string
	Source     string // Defaults to SourceAwakening when empty.
	Clock      Clock
	// HostAdapter + HostGate are forwarded to the probe-fanout composer so
	// every spawned branch runs introspection commands through the same
	// gate policy class the old inline shell used.
	HostAdapter cpn.HostAdapter
	HostGate    cpn.HostGate
	// Sandbox is the detected probe-wrapper (SC-10 / REQ-1001/1002). Wired
	// by the session service via DetectSandbox() before the topology runs.
	// Plumbed to the composer so every probe subprocess is wrapped.
	Sandbox fanout.Sandbox
	// Composer builds the probe-fanout sub-CPN from the LLM's plan. When
	// nil, t-awaken-probe-compose returns a diagnostic error so the
	// awakening surfaces a clear configuration failure instead of
	// silently stalling.
	Composer ComposerFunc
	// Lexicon is the controlled-vocabulary port used by the awakening
	// prompt builder (spec-architecture-brae-awakening-toolbox-extension.md
	// REQ-002). When non-nil, the bootstrap LLM transition receives a
	// prompt with an embedded 32-entry lexicon excerpt plus few-shot
	// hashtag anchors. When nil, the legacy SystemPromptPlan constant is
	// used — backwards-compat for callers that pre-date the extension.
	Lexicon cpn.Lexicon
	// Emitter is the SC-08 observability spine. A nil emitter degrades to
	// a no-op so callers without telemetry wiring behave unchanged.
	Emitter *Emitter
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
		PlaceAwakenPlan:         cpn.NewPlace(PlaceAwakenPlan, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceAwakenSubnetSpec:   cpn.NewPlace(PlaceAwakenSubnetSpec, cpn.ColorArtifact, cpn.SpaceComputation),
		// PlaceAwakeningReport now receives the reducer-assembled report
		// from the fanout sub-CPN (ColorArtifact payload).
		PlaceAwakeningReport:               cpn.NewPlace(PlaceAwakeningReport, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceHostCapabilitiesWK:            cpn.NewPlace(PlaceHostCapabilitiesWK, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningSnapshot:             cpn.NewPlace(PlaceAwakeningSnapshot, cpn.ColorHostFact, cpn.SpaceComputation),
		PlaceAwakeningMessage:              cpn.NewPlace(PlaceAwakeningMessage, cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningToolBatch:            cpn.NewPlace(PlaceAwakeningToolBatch, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceAwakeningToolBatch + "-done":  cpn.NewPlace(PlaceAwakeningToolBatch+"-done", cpn.ColorEvent, cpn.SpaceComputation),
		PlaceAwakeningMessage + "-emitted": cpn.NewPlace(PlaceAwakeningMessage+"-emitted", cpn.ColorEvent, cpn.SpaceComputation),
	}

	// Resolve the awakening system prompt once per factory invocation so
	// the seeded token + the LLM transition's SystemPrompt stay in sync.
	// When a Lexicon is wired, the builder embeds the deterministic 32-
	// entry excerpt and few-shot anchors; otherwise we fall back to the
	// legacy constant for backwards-compat.
	awakenPrompt := SystemPromptPlan
	if deps.Lexicon != nil {
		awakenPrompt = BuildSystemPromptPlan(deps.Lexicon)
	}

	transitions := map[string]*cpn.Transition{
		TransitionAwakenLLMBootstrap:     newLLMBootstrapTransition(awakenPrompt),
		TransitionAwakenProbeCompose:     newProbeComposeTransition(sessionID, deps),
		TransitionAwakenProbeInstantiate: newProbeInstantiateTransition(),
		TransitionAwakenReport:           newReportTransition(deps),
		TransitionAwakenPersist:          newPersistTransition(deps),
		TransitionAwakenRegisterTools:    newRegisterToolsTransition(deps.Emitter),
		TransitionAwakenEmitMessage:      newEmitMessageTransition(deps.Emitter),
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

	seed := makeAwakenSeeder(awakenPrompt)
	c.SeedFunc = seed
	seed(c)
	return c
}

// makeAwakenSeeder returns the seed function that places a single start-
// token on p-awaken-trigger and the system-prompt token on
// p-awaken-system-prompt so the bootstrap LLM transition fires immediately
// on Run(). The prompt string is closed over at factory time so a
// Lexicon-aware prompt (REQ-002) survives the re-seed that CPN.Run() does
// when it rewinds to the initial marking.
func makeAwakenSeeder(prompt string) func(*cpn.CPN) {
	return func(c *cpn.CPN) {
		if trigger, ok := c.Places[PlaceAwakenTrigger]; ok {
			_ = trigger.Deposit(&cpn.Token{
				Color:   cpn.ColorString,
				Space:   cpn.SpaceComputation,
				Payload: "awaken",
			})
		}
		if p, ok := c.Places[PlaceAwakenSystemPrompt]; ok {
			_ = p.Deposit(&cpn.Token{
				Color:   cpn.ColorString,
				Space:   cpn.SpaceComputation,
				Payload: prompt,
			})
		}
	}
}

// seedAwakenTrigger is the legacy exported seeder retained for tests and
// callers that expect the pre-Lexicon behaviour. New code should route
// through TopologyFactory, which composes the Lexicon-aware prompt.
func seedAwakenTrigger(c *cpn.CPN) {
	makeAwakenSeeder(SystemPromptPlan)(c)
}

// newLLMBootstrapTransition is the first-turn LLM transition. Its inputs are
// the trigger + system-prompt tokens seeded at topology creation, so it is
// enabled immediately on Run().
//
// Output place: p-awaken-plan — the LLM emits a strict-JSON
// AwakeningProbePlan describing which probes to run. The compose +
// instantiate transitions downstream turn that plan into parallel host
// probes, and the fanout reducer assembles the final AwakeningReport.
//
// RequireJSON is enabled: the plan-emitting system prompt no longer
// references tools, so the OpenRouter/Gemini restriction (can't combine
// response_format=json_object with tools[]) no longer applies.
func newLLMBootstrapTransition(systemPrompt string) *cpn.Transition {
	if systemPrompt == "" {
		systemPrompt = SystemPromptPlan
	}
	t := cpn.NewTransition(
		TransitionAwakenLLMBootstrap,
		cpn.NodeKindLLM,
		[]string{PlaceAwakenTrigger, PlaceAwakenSystemPrompt},
		[]string{PlaceAwakenPlan},
	)
	t.SystemPrompt = systemPrompt
	t.LLMConfig = &cpn.LLMConfig{
		Role:        "awakening-plan",
		MaxTokens:   2048,
		Temperature: 0.2,
		RequireJSON: true,
	}
	return t
}

// newProbeComposeTransition builds the probe-fanout sub-CPN from the LLM's
// plan. Implemented as a NodeKindTool so the composer callable is plugged
// directly through Deps without requiring a NodeKind extension.
//
// Input: p-awaken-plan (ColorArtifact). The consumed token's payload can be
// a typed AwakeningProbePlan (when the Composer re-uses its own type), a
// json.RawMessage, []byte, or string — the composer tolerates all four.
//
// Output: p-awaken-subnet-spec (ColorArtifact) carrying the composed
// *cpn.CPN ready for t-awaken-probe-instantiate to spawn.
func newProbeComposeTransition(sessionID string, deps Deps) *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenProbeCompose,
		cpn.NodeKindTool,
		[]string{PlaceAwakenPlan},
		[]string{PlaceAwakenSubnetSpec},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenProbeCompose)
		}
		if deps.Composer == nil {
			return nil, fmt.Errorf("%s: composer not wired (awakening misconfigured)", TransitionAwakenProbeCompose)
		}
		cdeps := ComposerDeps{
			HostAdapter: deps.HostAdapter,
			HostGate:    deps.HostGate,
			Sandbox:     deps.Sandbox,
		}
		if deps.Clock != nil {
			clock := deps.Clock
			cdeps.Clock = func() time.Time { return clock() }
		}
		if deps.Emitter != nil {
			em := deps.Emitter
			cdeps.OnProbeFired = func(ctx context.Context, slug string, duration time.Duration, exitCode int) {
				em.ProbeFired(ctx, slug, duration, exitCode)
			}
			cdeps.OnProbeReduced = func(ctx context.Context, success, fail int) {
				em.ProbeReduced(ctx, success, fail)
			}
		}
		child, err := deps.Composer(ctx, sessionID, consumed[0].Payload, cdeps)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", TransitionAwakenProbeCompose, err)
		}
		if child == nil {
			return nil, fmt.Errorf("%s: composer returned nil *cpn.CPN", TransitionAwakenProbeCompose)
		}
		if deps.Emitter != nil {
			slugs := extractProbeSlugs(child)
			deps.Emitter.ProbeComposed(ctx, slugs)
		}
		return map[string]cpn.Token{
			PlaceAwakenSubnetSpec: {
				Color:   cpn.ColorArtifact,
				Space:   cpn.SpaceComputation,
				Payload: child,
			},
		}, nil
	}
	return t
}

// extractProbeSlugs returns the sorted per-probe transition IDs stripped of
// the fanout prefix so the observability payload matches the probe-IDs the
// LLM emitted in its plan. Best-effort: returns nil when the child has no
// recognisable probe transitions.
func extractProbeSlugs(child *cpn.CPN) []string {
	if child == nil {
		return nil
	}
	const probePrefix = "t-probe-"
	slugs := make([]string, 0, len(child.Transitions))
	for id := range child.Transitions {
		if strings.HasPrefix(id, probePrefix) {
			slugs = append(slugs, strings.TrimPrefix(id, probePrefix))
		}
	}
	sort.Strings(slugs)
	return slugs
}

// newProbeInstantiateTransition spawns the composed sub-CPN in-process and
// forwards the reducer-emitted AwakeningReport onto p-awakening-report.
//
// Per REQ-006 this step SkipHITL — at awakening time no user UI exists to
// approve a topology, and the plan has already been validated by the
// composer (SEC-002, CON-002, CON-003). Since we don't go through
// fireInstantiate (which insists on a FlowRepository round-trip), SkipHITL
// is effectively implicit here: there is no gate to skip because the
// custom handler never consults a TopologyRouter.
func newProbeInstantiateTransition() *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenProbeInstantiate,
		cpn.NodeKindTool,
		[]string{PlaceAwakenSubnetSpec},
		[]string{PlaceAwakeningReport},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenProbeInstantiate)
		}
		child, ok := consumed[0].Payload.(*cpn.CPN)
		if !ok || child == nil {
			return nil, fmt.Errorf("%s: expected *cpn.CPN payload, got %T", TransitionAwakenProbeInstantiate, consumed[0].Payload)
		}
		if err := child.Run(ctx); err != nil {
			return nil, fmt.Errorf("%s: sub-CPN run: %w", TransitionAwakenProbeInstantiate, err)
		}
		report, err := extractFanoutReport(child)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", TransitionAwakenProbeInstantiate, err)
		}
		return map[string]cpn.Token{
			PlaceAwakeningReport: {
				Color:   cpn.ColorArtifact,
				Space:   cpn.SpaceComputation,
				Payload: report,
			},
		}, nil
	}
	return t
}

// ErrFanoutNoReport signals that the composed fanout sub-CPN completed but
// did not deposit an AwakeningReport on any terminal place. runAwakening
// treats this as a total-fanout failure per REQ-007.
var ErrFanoutNoReport = errors.New("awakening fanout produced no report")

// extractFanoutReport scans the child sub-CPN's terminal places for an
// AwakeningReport token. The composer is expected to emit the report on a
// single terminal (p-probe-egress by spec §4.3 / REQ-002), but we tolerate
// any terminal place to stay decoupled from composer place-id choices.
func extractFanoutReport(child *cpn.CPN) (AwakeningReport, error) {
	if child == nil {
		return AwakeningReport{}, ErrFanoutNoReport
	}
	for _, p := range child.TerminalPlaces() {
		toks, ok := p.Peek()
		if !ok {
			continue
		}
		for _, tok := range toks {
			report, err := coerceReport(tok.Payload)
			if err == nil {
				return report, nil
			}
		}
	}
	return AwakeningReport{}, ErrFanoutNoReport
}

// newReportTransition validates the fanout-assembled AwakeningReport into
// the downstream snapshot / tool-batch / message tokens. The reducer in
// the fanout sub-CPN always emits a typed AwakeningReport, but coerceReport
// still accepts json.RawMessage / []byte / string for defensive symmetry.
func newReportTransition(deps Deps) *cpn.Transition {
	emitter := deps.Emitter
	t := cpn.NewTransition(
		TransitionAwakenReport,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningReport},
		[]string{PlaceAwakeningSnapshot, PlaceAwakeningToolBatch, PlaceAwakeningMessage},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no input token", TransitionAwakenReport)
		}
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		if !deps.Sandbox.IsZero() {
			report.Host.Sandbox = AwakeningSandbox{
				Tool:    deps.Sandbox.Tool,
				Version: deps.Sandbox.Version,
			}
		}
		if emitter != nil {
			buckets := make([]string, 0, len(report.ToolsRegister))
			seen := make(map[string]struct{}, len(report.ToolsRegister))
			for _, tr := range report.ToolsRegister {
				b := canonicaliseToolbox(tr.Toolbox)
				if _, ok := seen[b]; ok {
					continue
				}
				seen[b] = struct{}{}
				buckets = append(buckets, b)
			}
			sort.Strings(buckets)
			emitter.ReportProjected(ctx, len(report.ToolsRegister), buckets)
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
func newRegisterToolsTransition(emitter *Emitter) *cpn.Transition {
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
		donePlace := PlaceAwakeningToolBatch + "-done"
		registry := toolRegistryFromContext(ctx)
		slog.InfoContext(ctx, "awakens.register_tools.invoked",
			"tools_to_register", len(report.ToolsRegister),
			"registry_attached", registry != nil,
		)
		if registry == nil {
			slog.WarnContext(ctx, "awakens.register_tools.noop_registry_missing",
				"tools_to_register", len(report.ToolsRegister),
			)
			if emitter != nil {
				emitter.Registered(ctx, 0, 0)
			}
			return map[string]cpn.Token{
				donePlace: {
					Color:   cpn.ColorEvent,
					Space:   cpn.SpaceComputation,
					Payload: []string{},
				},
			}, nil
		}
		registered, regErr := RegisterBatch(ctx, registry, report, nil)
		if regErr != nil {
			slog.ErrorContext(ctx, "awakens.register_tools.failed",
				"error", regErr,
				"tools_to_register", len(report.ToolsRegister),
			)
			return nil, regErr
		}
		if registered == nil {
			registered = []string{}
		}
		duplicates := len(report.ToolsRegister) - len(registered)
		if duplicates < 0 {
			duplicates = 0
		}
		slog.InfoContext(ctx, "awakens.register_tools.completed",
			"requested", len(report.ToolsRegister),
			"registered", len(registered),
			"duplicates", duplicates,
			"registered_names", registered,
		)
		if emitter != nil {
			emitter.Registered(ctx, len(registered), duplicates)
		}
		return map[string]cpn.Token{
			donePlace: {
				Color:   cpn.ColorEvent,
				Space:   cpn.SpaceComputation,
				Payload: registered,
			},
		}, nil
	}
	return t
}

// newEmitMessageTransition builds the A2UI envelope. Downstream session
// plumbing (emit-first-assistant-message) consumes the resulting Event
// token and persists it as the session's first `messages` row with
// cpn_role = "awakening".
func newEmitMessageTransition(emitter *Emitter) *cpn.Transition {
	t := cpn.NewTransition(
		TransitionAwakenEmitMessage,
		cpn.NodeKindTool,
		[]string{PlaceAwakeningMessage},
		[]string{PlaceAwakeningMessage + "-emitted"},
	)
	t.ToolHandler = func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		report, err := coerceReport(consumed[0].Payload)
		if err != nil {
			return nil, err
		}
		envelope := BuildFirstTurnMessage(report)
		if emitter != nil {
			emitter.Emitted(ctx, len(envelope.Components))
		}
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

// coerceReport tolerates the shapes the upstream producers may hand us: a
// typed AwakeningReport (from the legacy single-shot LLM path or a future
// merger), a fanout.AwakeningReport (probe-derived slice from the reducer),
// a RawMessage, or a raw string. Parse + validate is centralised here so
// every downstream transition agrees on the contract.
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
	case fanout.AwakeningReport:
		return reportFromFanout(v), nil
	case *fanout.AwakeningReport:
		if v == nil {
			return AwakeningReport{}, fmt.Errorf("%w: nil *fanout.AwakeningReport", ErrInvalidReport)
		}
		return reportFromFanout(*v), nil
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

// reportFromFanout bridges the probe-fanout reducer's report into the full
// awakens.AwakeningReport the downstream persist / register / emit transitions
// consume. The probe layer now delivers OS / Shell / Identity data (from
// composer-injected info probes) so we copy those through rather than faking
// defaults. Validate() is deliberately NOT called here — the probe-only slice
// is legitimately partial on cold probes; downstream consumers that need
// validation call Validate() themselves on the final report.
func reportFromFanout(r fanout.AwakeningReport) AwakeningReport {
	present := make([]AwakeningTool, 0, len(r.Binaries))
	absent := make([]string, 0)
	for _, b := range r.Binaries {
		if b.Present {
			present = append(present, AwakeningTool{Name: b.Name, Version: b.Version})
		} else {
			absent = append(absent, b.Name)
		}
	}
	caps := make([]AwakeningCapability, 0, len(r.Capabilities))
	for _, c := range r.Capabilities {
		caps = append(caps, AwakeningCapability{
			Name:      c.Name,
			Satisfied: c.Satisfied,
			Evidence:  c.Evidence,
		})
	}
	regs := make([]AwakeningToolRegister, 0, len(r.ToolsRegister))
	for _, tr := range r.ToolsRegister {
		regs = append(regs, AwakeningToolRegister{Name: tr.Name, Basis: tr.Basis})
	}

	osOut := AwakeningOS{
		Name:    strings.TrimSpace(r.OS.Name),
		Version: strings.TrimSpace(r.OS.Version),
		Kernel:  strings.TrimSpace(r.OS.Kernel),
		Arch:    strings.TrimSpace(r.OS.Arch),
	}
	if osOut.Name == "" {
		osOut.Name = "Linux"
	}
	if osOut.Arch == "" {
		osOut.Arch = runtime.GOARCH
	}

	shellOut := AwakeningShell{
		Path:           strings.TrimSpace(r.Shell.Path),
		Implementation: strings.TrimSpace(r.Shell.Implementation),
	}
	if shellOut.Path == "" {
		shellOut.Path = "/bin/sh"
	}

	identityOut := AwakeningIdentity{
		User: strings.TrimSpace(r.Identity.User),
		UID:  r.Identity.UID,
		GID:  r.Identity.GID,
		Home: strings.TrimSpace(r.Identity.Home),
	}

	out := AwakeningReport{
		OS:            osOut,
		Shell:         shellOut,
		Identity:      identityOut,
		PresentTools:  present,
		AbsentTools:   absent,
		Capabilities:  caps,
		ToolsRegister: regs,
		NarrativeMD:   buildNarrative(osOut, shellOut, identityOut, r.Notes),
	}
	return out
}

// buildNarrative composes the short Spanish headline the A2UI first-turn card
// uses. Pulls OS / shell / identity data first so the card never leads with
// "probe notes:" (previous bug) and always tells the user what brae woke up in.
func buildNarrative(os AwakeningOS, shell AwakeningShell, ident AwakeningIdentity, notes []string) string {
	var b strings.Builder
	b.WriteString("Woke up on ")
	b.WriteString(os.Name)
	if os.Version != "" {
		b.WriteByte(' ')
		b.WriteString(os.Version)
	}
	if os.Arch != "" {
		b.WriteString(" (")
		b.WriteString(os.Arch)
		b.WriteByte(')')
	}
	if os.Kernel != "" {
		b.WriteString(", kernel ")
		b.WriteString(os.Kernel)
	}
	b.WriteString(".")
	if shell.Path != "" {
		b.WriteString(" Shell: ")
		b.WriteString(shell.Path)
		if shell.Implementation != "" {
			b.WriteString(" (")
			b.WriteString(shell.Implementation)
			b.WriteByte(')')
		}
		b.WriteByte('.')
	}
	if ident.User != "" {
		b.WriteString(" User: ")
		b.WriteString(ident.User)
		if ident.Home != "" {
			b.WriteString(" (home ")
			b.WriteString(ident.Home)
			b.WriteByte(')')
		}
		b.WriteByte('.')
	}
	if len(notes) > 0 {
		b.WriteString("\nNotes: ")
		b.WriteString(strings.Join(notes, "; "))
		b.WriteByte('.')
	}
	return b.String()
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
