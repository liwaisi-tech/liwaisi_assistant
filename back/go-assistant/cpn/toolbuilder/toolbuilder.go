package toolbuilder

// toolbuilder.go — BuildToolCreatorTopology constructs the "tool-creator"
// CPN. Shape and REQ-* references live in the architecture spec (see
// doc.go for the current pointer).
//
// Scope notes:
//   - The profile catalog is collapsed to 3 roles (arch, go-eng, devops).
//   - Installation runs `make install` via a NodeKindBash transition; the
//     legacy NodeKindRegisterTool path is intentionally not wired.
//   - A dedicated t-handle-error transition drains PlaceErrors into
//     PlaceFailed so a validator/gate rejection terminates the flow
//     explicitly instead of deadlocking silently.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ToolCreatorDeps captures external collaborators. Kept as a struct so
// follow-up tasks can add HostAdapter, ToolRegistry, etc. without
// breaking the constructor signature.
type ToolCreatorDeps struct {
	// TDDRunner drives the red-green loop inside t-tdd-loop. A nil
	// runner is replaced with NoopTDDRunner so the topology stays
	// structurally valid even in unit tests.
	TDDRunner TDDRunner
}

// BuildToolCreatorTopology constructs the tool-creator CPN for one
// session. The returned *cpn.CPN is ready to run once the caller
// attaches an LLMClient (required for the LLM transitions) and a
// HostRuntime (required for the bash transitions).
func BuildToolCreatorTopology(sessionID string, deps ToolCreatorDeps) *cpn.CPN {
	places := buildPlaces()
	transitions := buildTransitions(deps)

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-%s", sessionID, FlowName),
		FlowName,
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10

	// Wire the error-handler AFTER construction so the ToolHandler
	// closure can reference the CPN itself and publish a structured
	// EventExecutionFailed event alongside the PlaceFailed token.
	if t, ok := c.Transitions[TrHandleError]; ok {
		t.ToolHandler = handleErrorHandler(c)
	}
	return c
}

// buildPlaces declares all typed buffers in topological order. Grouped
// visually so reviewers can cross-check against the spec table.
func buildPlaces() map[string]*cpn.Place {
	p := func(id string, col cpn.ColorSet) *cpn.Place { return cpn.NewPlace(id, col, cpn.SpaceComputation) }
	return map[string]*cpn.Place{
		// intake + triage
		PlaceRequest:        p(PlaceRequest, cpn.ColorJSON),
		PlaceTriaged:        p(PlaceTriaged, cpn.ColorJSON),
		PlaceReqInvestigate: p(PlaceReqInvestigate, cpn.ColorJSON),
		PlaceReqScaffold:    p(PlaceReqScaffold, cpn.ColorJSON),
		PlaceReqDraft:       p(PlaceReqDraft, cpn.ColorJSON),

		// investigate + scaffold + draft-v0
		PlaceExistingTools:  p(PlaceExistingTools, cpn.ColorJSON),
		PlaceWorkspaceReady: p(PlaceWorkspaceReady, cpn.ColorArtifact),
		PlaceSpecV0:         p(PlaceSpecV0, cpn.ColorJSON),

		// spec draft → reviewer fan-out (3 reviewers: arch, go-eng, devops)
		PlaceSpecDraft:     p(PlaceSpecDraft, cpn.ColorJSON),
		PlaceSpecForGo:     p(PlaceSpecForGo, cpn.ColorJSON),
		PlaceSpecForDevOps: p(PlaceSpecForDevOps, cpn.ColorJSON),
		PlaceSpecForArch:   p(PlaceSpecForArch, cpn.ColorJSON),

		// reviewer outputs
		PlaceReviewGo:     p(PlaceReviewGo, cpn.ColorJSON),
		PlaceReviewDevOps: p(PlaceReviewDevOps, cpn.ColorJSON),
		PlaceReviewArch:   p(PlaceReviewArch, cpn.ColorJSON),

		// aggregate → approve / refine
		PlaceSpecApproved:  p(PlaceSpecApproved, cpn.ColorJSON),
		PlaceRefineRequest: p(PlaceRefineRequest, cpn.ColorJSON),

		// totals → doit → gate → ready → subtasks → tested
		PlaceTotals:     p(PlaceTotals, cpn.ColorJSON),
		PlaceDoIt:       p(PlaceDoIt, cpn.ColorJSON),
		PlaceTotalsEcho: p(PlaceTotalsEcho, cpn.ColorJSON),
		PlaceTotalReady: p(PlaceTotalReady, cpn.ColorJSON),
		PlaceSubtasks:   p(PlaceSubtasks, cpn.ColorJSON),
		PlaceTested:     p(PlaceTested, cpn.ColorArtifact),

		// terminal
		PlacePackaged:      p(PlacePackaged, cpn.ColorToolManifest),
		PlaceInstallResult: p(PlaceInstallResult, cpn.ColorShellResult),
		PlaceRegistered:    p(PlaceRegistered, cpn.ColorArtifact),
		PlaceErrors:        p(PlaceErrors, cpn.ColorError),
		PlaceFailed:        p(PlaceFailed, cpn.ColorError),
	}
}

// buildSubAgentLLM constructs an LLM transition for a (profile ×
// action) sub-agent. Panics if the pair is unknown to the catalog or
// denied by the allowlist matrix — both are build-time programmer
// errors that must not ship. The returned transition has SystemPrompt
// composed from the catalog, Meta populated for the visualizer, and
// ErrorPlace wired for the engine.
func buildSubAgentLLM(
	catalog *SubAgentCatalog,
	transitionID, profileID, actionID string,
	inputPlaces, outputPlaces []string,
	llmCfg *cpn.LLMConfig,
) *cpn.Transition {
	profile, ok := catalog.Profiles.Get(profileID)
	if !ok {
		panic(fmt.Sprintf("tool-creator: sub-agent %s references unknown profile %q", transitionID, profileID))
	}
	action, ok := catalog.Actions.Get(actionID)
	if !ok {
		panic(fmt.Sprintf("tool-creator: sub-agent %s references unknown action %q", transitionID, actionID))
	}
	if want := TransitionID(actionID, profileID); transitionID != want {
		panic(fmt.Sprintf("tool-creator: sub-agent id %q violates convention (want %q)", transitionID, want))
	}
	sysPrompt, err := catalog.Compose(profileID, actionID)
	if err != nil {
		panic(fmt.Sprintf("tool-creator: compose %s×%s: %v", profileID, actionID, err))
	}
	t := cpn.NewTransition(transitionID, cpn.NodeKindLLM, inputPlaces, outputPlaces)
	t.SystemPrompt = sysPrompt
	t.LLMConfig = llmCfg
	t.ErrorPlace = PlaceErrors
	t.Meta = SubAgentMeta(profile, action)
	return t
}

// buildTransitions declares every transition. Each transition's
// configuration (SystemPrompt, BashConfig, ToolHandler) is set inline for
// readability — reviewers should be able to scan one function top-to-bottom
// and understand the whole pipeline.
func buildTransitions(deps ToolCreatorDeps) map[string]*cpn.Transition {
	tr := make(map[string]*cpn.Transition)

	// Sub-agent catalog — seeded once, used for every (profile × action)
	// transition below (reviewers + refine-spec + decompose-totals +
	// plan-subtasks). Seed failure is a build-time error.
	catalog, err := NewSubAgentCatalog()
	if err != nil {
		panic(fmt.Sprintf("tool-creator: sub-agent catalog seed failed: %v", err))
	}

	// ── t-triage (LLM) ────────────────────────────────────────────────
	tTriage := cpn.NewTransition(TrTriage, cpn.NodeKindLLM,
		[]string{PlaceRequest}, []string{PlaceTriaged})
	tTriage.SystemPrompt = `You are a senior Go software architect acting as a request-triage bot for tool-creator.
Read the incoming ToolRequest JSON (field "description" carries the user's intent) and decide if it is specific enough to start design.

DEFAULT TO "ready": true. A request is ready when you can identify at least:
- A plausible tool name (infer one from the verb+noun if the user didn't name it, e.g. "html-to-markdown").
- A one-line purpose you can paraphrase from the user's words.
- At least one verb describing behaviour.

Only return "ready": false when the request is genuinely unworkable (pure nonsense, empty, or "make something useful"-tier vague). Do NOT block on missing optional details like preferred libraries, module grouping, or author — infer sensible defaults and proceed.

ALWAYS echo a fully-populated "request" object back — even when ready is false — inferring name/module/description/author from whatever context you have. The scaffold stage needs those fields.

Output ONLY valid JSON: {"ready":bool,"clarify_question":"string","request":{"name":"...","module":"...","description":"...","author":"..."}}`
	tTriage.LLMConfig = &cpn.LLMConfig{Role: "classifier", MaxTokens: 512, RequireJSON: true}
	tTriage.ErrorPlace = PlaceErrors
	tr[TrTriage] = tTriage

	// ── t-dispatch (Tool) ─────────────────────────────────────────────
	tDispatch := cpn.NewTransition(TrDispatch, cpn.NodeKindTool,
		[]string{PlaceTriaged},
		[]string{PlaceReqInvestigate, PlaceReqScaffold, PlaceReqDraft})
	tDispatch.ToolHandler = dispatchHandler()
	tDispatch.ErrorPlace = PlaceErrors
	tr[TrDispatch] = tDispatch

	// ── t-investigate (Tool, stub) ────────────────────────────────────
	tInvestigate := cpn.NewTransition(TrInvestigate, cpn.NodeKindTool,
		[]string{PlaceReqInvestigate}, []string{PlaceExistingTools})
	tInvestigate.ToolHandler = investigateHandler()
	tInvestigate.ErrorPlace = PlaceErrors
	tr[TrInvestigate] = tInvestigate

	// ── t-scaffold-workspace (Tool, placeholder) ──────────────────────
	// The real implementation renders template files to a temp dir and
	// shells out (git init + go mod init). Kept as a Tool stub for now so
	// it emits a ColorArtifact token — bash transitions are constrained
	// to shell-family output colors and can't target p-workspace-ready
	// directly. Swap to a NodeKindBash + intermediate shell-result place
	// once the real scaffolder lands.
	tScaffold := cpn.NewTransition(TrScaffoldWorkspace, cpn.NodeKindTool,
		[]string{PlaceReqScaffold}, []string{PlaceWorkspaceReady})
	tScaffold.ToolHandler = scaffoldWorkspaceStub()
	tScaffold.ErrorPlace = PlaceErrors
	tr[TrScaffoldWorkspace] = tScaffold

	// ── t-draft-spec-v0 (LLM) ─────────────────────────────────────────
	tDraft := cpn.NewTransition(TrDraftSpecV0, cpn.NodeKindLLM,
		[]string{PlaceReqDraft}, []string{PlaceSpecV0})
	tDraft.SystemPrompt = `You are a senior Go software architect.
Given a ToolRequest JSON, produce a hexagonal + DDD-flavoured spec for a small Go CLI tool.
Constraints: one aggregate, ≤3 ports, ≤3 adapters, stdlib-only, ≤200 LOC total.
Output ONLY valid JSON matching SpecDraft:
{"name":"...","purpose":"...","non_goals":[],"domain_model":"...","ports":[{"name":"...","purpose":"..."}],"adapters":[{"name":"...","implements_port":"...","summary":"..."}],"cli_surface":"...","test_plan":["..."],"risks":[]}`
	tDraft.LLMConfig = &cpn.LLMConfig{Role: "structured", MaxTokens: 2048, RequireJSON: true}
	tDraft.ErrorPlace = PlaceErrors
	tr[TrDraftSpecV0] = tDraft

	// ── t-enrich-spec (Tool) ──────────────────────────────────────────
	tEnrich := cpn.NewTransition(TrEnrichSpec, cpn.NodeKindTool,
		[]string{PlaceSpecV0, PlaceWorkspaceReady, PlaceExistingTools},
		[]string{PlaceSpecDraft})
	tEnrich.ToolHandler = enrichSpecHandler()
	tEnrich.ErrorPlace = PlaceErrors
	tr[TrEnrichSpec] = tEnrich

	// ── t-fanout-reviewers (Tool) ─────────────────────────────────────
	fanOutputs := make([]string, 0, len(reviewerRoles))
	for _, r := range reviewerRoles {
		fanOutputs = append(fanOutputs, r.InputPlace)
	}
	tFanout := cpn.NewTransition(TrFanoutReviewers, cpn.NodeKindTool,
		[]string{PlaceSpecDraft}, fanOutputs)
	tFanout.ToolHandler = fanoutReviewersHandler()
	tFanout.ErrorPlace = PlaceErrors
	tr[TrFanoutReviewers] = tFanout

	// ── t-review-spec-* (LLM sub-agents, one per reviewer role) ──────
	// Each reviewer is a sub-agent composed of (profile × action). The
	// SystemPrompt is rendered deterministically from the catalog so
	// profile text stays free of action coupling. Transition.Meta
	// surfaces the sub-agent identity to the visualizer and event
	// pipeline — see registry.go SubAgentMeta.
	for _, r := range reviewerRoles {
		tr[r.TransitionID] = buildSubAgentLLM(catalog, r.TransitionID,
			r.ProfileID, r.ActionID,
			[]string{r.InputPlace}, []string{r.OutputPlace},
			&cpn.LLMConfig{Role: "structured", MaxTokens: 800, RequireJSON: true})
	}

	// ── t-aggregate-approve / t-aggregate-refine (Tool, mutually exclusive guards) ──
	aggInputs := make([]string, 0, len(reviewerRoles))
	for _, r := range reviewerRoles {
		aggInputs = append(aggInputs, r.OutputPlace)
	}
	tApprove := cpn.NewTransition(TrAggregateApprove, cpn.NodeKindTool,
		aggInputs, []string{PlaceSpecApproved})
	tApprove.ToolHandler = aggregateApproveHandler()
	tApprove.Guard = guardAggregateApprove
	tApprove.ErrorPlace = PlaceErrors
	tr[TrAggregateApprove] = tApprove

	tRefine := cpn.NewTransition(TrAggregateRefine, cpn.NodeKindTool,
		aggInputs, []string{PlaceRefineRequest})
	tRefine.ToolHandler = aggregateRefineHandler()
	tRefine.Guard = guardAggregateRefine
	tRefine.ErrorPlace = PlaceErrors
	tr[TrAggregateRefine] = tRefine

	// ── t-refine-spec-arch (LLM sub-agent) ───────────────────────────
	// Closes the review → refine loop. Arch profile owns the refine
	// action (allowlist matrix: refine-spec is restricted to arch).
	tRefineLLM := buildSubAgentLLM(catalog, TrRefineSpec,
		ProfileArch, ActionRefineSpec,
		[]string{PlaceRefineRequest}, []string{PlaceSpecDraft},
		&cpn.LLMConfig{Role: "structured", MaxTokens: 2048, RequireJSON: true})
	tr[TrRefineSpec] = tRefineLLM

	// ── t-decompose-totals-arch (LLM sub-agent) ──────────────────────
	tDecompose := buildSubAgentLLM(catalog, TrDecomposeTotals,
		ProfileArch, ActionDecomposeTotals,
		[]string{PlaceSpecApproved}, []string{PlaceTotals},
		&cpn.LLMConfig{Role: "structured", MaxTokens: 1024, RequireJSON: true})
	tr[TrDecomposeTotals] = tDecompose

	// ── t-authorize-totals (Tool, deterministic) ──────────────────────
	tAuthorize := cpn.NewTransition(TrAuthorizeTotals, cpn.NodeKindTool,
		[]string{PlaceTotals}, []string{PlaceDoIt, PlaceTotalsEcho})
	tAuthorize.ToolHandler = authorizeTotalsHandler()
	tAuthorize.ErrorPlace = PlaceErrors
	tr[TrAuthorizeTotals] = tAuthorize

	// ── t-gate-total (Tool; join DoIt + echoed Totals) ────────────────
	tGate := cpn.NewTransition(TrGateTotal, cpn.NodeKindTool,
		[]string{PlaceDoIt, PlaceTotalsEcho}, []string{PlaceTotalReady})
	tGate.ToolHandler = gateTotalHandler()
	tGate.ErrorPlace = PlaceErrors
	tr[TrGateTotal] = tGate

	// ── t-plan-subtasks-arch (LLM sub-agent) ─────────────────────────
	tPlan := buildSubAgentLLM(catalog, TrPlanSubtasks,
		ProfileArch, ActionPlanSubtasks,
		[]string{PlaceTotalReady}, []string{PlaceSubtasks},
		&cpn.LLMConfig{Role: "structured", MaxTokens: 2048, RequireJSON: true})
	tr[TrPlanSubtasks] = tPlan

	// ── t-tdd-loop (Tool, v1 stub) ────────────────────────────────────
	tTDD := cpn.NewTransition(TrTDDLoop, cpn.NodeKindTool,
		[]string{PlaceSubtasks}, []string{PlaceTested})
	tTDD.ToolHandler = tddLoopHandler(deps.TDDRunner)
	tTDD.ErrorPlace = PlaceErrors
	tr[TrTDDLoop] = tTDD

	// ── t-package (Tool, placeholder) ─────────────────────────────────
	// Kept as a Tool stub so it emits a ColorToolManifest token — same
	// validator constraint as t-scaffold-workspace. Real implementation
	// will run `go build` + sign + produce the manifest.
	tPackage := cpn.NewTransition(TrPackage, cpn.NodeKindTool,
		[]string{PlaceTested}, []string{PlacePackaged})
	tPackage.ToolHandler = packageStub()
	tPackage.ErrorPlace = PlaceErrors
	tr[TrPackage] = tPackage

	// ── t-install (Bash) ──────────────────────────────────────────────
	// Runs `make install` in the scaffolded tool directory. Makefile
	// shells out to install.sh which copies bin/<name> into
	// $BRAE_TOOLS_BIN. On success the transition deposits a
	// ColorShellResult token on PlaceInstallResult.
	//
	// Command/Args/Cwd are left empty on the template here; the adapter
	// that materialises the scaffold is expected to rewrite BashConfig
	// at session-bind time to point at the real workspace path. Leaving
	// the placeholder ensures the topology remains structurally valid
	// (executor requires a non-nil BashConfig for NodeKindBash).
	tInstall := cpn.NewTransition(TrInstall, cpn.NodeKindBash,
		[]string{PlacePackaged}, []string{PlaceInstallResult})
	tInstall.BashConfig = &cpn.BashConfig{
		Command: "make",
		Args:    []string{"install"},
	}
	tInstall.ErrorPlace = PlaceErrors
	tr[TrInstall] = tInstall

	// ── t-finalize-install (Tool) ─────────────────────────────────────
	// Converts the ColorShellResult from `make install` into a
	// ColorArtifact manifest token on PlaceRegistered. Extracts the
	// installed binary path (and, when available, a sha256) by parsing
	// install.sh's "installed: <path>" line from stdout.
	tFinalize := cpn.NewTransition(TrFinalizeInstall, cpn.NodeKindTool,
		[]string{PlaceInstallResult}, []string{PlaceRegistered})
	tFinalize.ToolHandler = finalizeInstallHandler()
	tFinalize.ErrorPlace = PlaceErrors
	tr[TrFinalizeInstall] = tFinalize

	// ── t-handle-error (Tool) ─────────────────────────────────────────
	// Drains PlaceErrors → PlaceFailed, fixing the silent mid-flow halt
	// where a validator/gate failure deposited via depositSubAgentError
	// had no consumer and sat in PlaceErrors forever. The ToolHandler
	// that also publishes EventExecutionFailed is wired post-build in
	// BuildToolCreatorTopology so it can capture the owning CPN.
	tHandleError := cpn.NewTransition(TrHandleError, cpn.NodeKindTool,
		[]string{PlaceErrors}, []string{PlaceFailed})
	tHandleError.ToolHandler = handleErrorHandler(nil) // overwritten post-build
	// No ErrorPlace: this transition is itself the error sink.
	tr[TrHandleError] = tHandleError

	return tr
}

// handleErrorHandler returns a Tool handler that forwards a PlaceErrors
// token to PlaceFailed and, when c is non-nil, publishes an
// EventExecutionFailed envelope with best-effort decoded context.
//
// The nil-c variant is installed at build time so the topology
// validates (ToolHandler must be non-nil for NodeKindTool). The
// CPN-bound variant is installed in BuildToolCreatorTopology after
// NewCPN so the closure captures the constructed CPN.
func handleErrorHandler(c *cpn.CPN) func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no error token", TrHandleError)
		}
		tok := consumed[0]

		// Best-effort decode of the structured error payload deposited
		// by fire_llm.depositSubAgentError.
		payload := cpn.ExecutionFailedPayload{}
		if raw, ok := tok.Payload.(string); ok {
			var decoded struct {
				TransitionID string `json:"transition_id"`
				ProfileID    string `json:"profile_id"`
				ActionID     string `json:"action_id"`
				Stage        string `json:"stage"`
				Error        string `json:"error"`
			}
			if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
				payload = cpn.ExecutionFailedPayload{
					Stage:     decoded.Stage,
					Reason:    decoded.Error,
					ProfileID: decoded.ProfileID,
					ActionID:  decoded.ActionID,
					Source:    decoded.TransitionID,
				}
			} else {
				payload.Reason = raw
			}
		}

		if c != nil {
			c.PublishExecutionFailed(TrHandleError, payload)
		}

		// Echo the failure onto the terminal place so observers (and
		// tests) can assert the CPN has stopped deterministically.
		out := tok
		out.Color = cpn.ColorError
		return map[string]cpn.Token{
			PlaceFailed: out,
		}, nil
	}
}
