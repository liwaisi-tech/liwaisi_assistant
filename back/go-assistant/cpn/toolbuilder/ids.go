package toolbuilder

// FlowName is the FlowRegistry name for the tool-creator topology.
// Used by main-conversation routers and by the architect planner to
// instantiate tool-creator as a sub-CPN.
const FlowName = "tool-creator"

// Place IDs. Grouped in topological order (intake → triage → investigate-&-scaffold-&-draft →
// review → totals → TDD → package → install → terminal).
const (
	PlaceRequest        = "p-request"
	PlaceTriaged        = "p-triaged"
	PlaceReqInvestigate = "p-req-investigate"
	PlaceReqScaffold    = "p-req-scaffold"
	PlaceReqDraft       = "p-req-draft"
	PlaceExistingTools  = "p-existing-tools"
	PlaceWorkspaceReady = "p-workspace-ready"
	PlaceSpecV0         = "p-spec-v0"
	PlaceSpecDraft      = "p-spec-draft"

	// tool-creator reviewers: arch, go-eng, devops only.
	PlaceSpecForGo     = "p-spec-for-go"
	PlaceSpecForDevOps = "p-spec-for-devops"
	PlaceSpecForArch   = "p-spec-for-arch"

	PlaceReviewGo     = "p-review-go"
	PlaceReviewDevOps = "p-review-devops"
	PlaceReviewArch   = "p-review-arch"

	PlaceSpecApproved  = "p-spec-approved"
	PlaceRefineRequest = "p-refine-request"

	PlaceTotals     = "p-totals"
	PlaceDoIt       = "p-doit"
	PlaceTotalsEcho = "p-totals-echo"
	PlaceTotalReady = "p-total-ready"
	PlaceSubtasks   = "p-subtasks"
	PlaceTested     = "p-tested"
	PlacePackaged   = "p-packaged"

	// PlaceInstallResult holds the ColorShellResult token emitted by the
	// `make install` bash transition. A follow-up Tool transition converts
	// it into the ColorArtifact manifest token on PlaceRegistered.
	PlaceInstallResult = "p-install-result"
	PlaceRegistered    = "p-registered"

	// PlaceErrors is the central error sink; every transition's
	// ErrorPlace routes here. PlaceFailed is the terminal failure place
	// fed by t-handle-error so the CPN has an explicit end-state when a
	// sub-agent validator/gate rejects output (fixes the silent
	// mid-flow halt where PlaceErrors had no consumer).
	PlaceErrors = "p-errors"
	PlaceFailed = "p-failed"
)

// Transition IDs.
const (
	TrTriage            = "t-triage"
	TrDispatch          = "t-dispatch"
	TrInvestigate       = "t-investigate"
	TrScaffoldWorkspace = "t-scaffold-workspace"
	TrDraftSpecV0       = "t-draft-spec-v0"
	TrEnrichSpec        = "t-enrich-spec"
	TrFanoutReviewers   = "t-fanout-reviewers"
	// Reviewer transition IDs follow t-{action}-{profile}. These values
	// equal TransitionID(ActionReviewSpec, Profile*) — asserted by
	// TestTransitionIDConvention.
	TrReviewGo         = "t-review-spec-go-eng"
	TrReviewDevOps     = "t-review-spec-devops"
	TrReviewArch       = "t-review-spec-arch"
	TrAggregateApprove = "t-aggregate-approve"
	TrAggregateRefine  = "t-aggregate-refine"
	// Arch-only sub-agent transitions. IDs equal
	// TransitionID(action, ProfileArch) — asserted at build time by
	// buildSubAgentLLM.
	TrRefineSpec      = "t-refine-spec-arch"
	TrDecomposeTotals = "t-decompose-totals-arch"
	TrAuthorizeTotals = "t-authorize-totals"
	TrGateTotal       = "t-gate-total"
	TrPlanSubtasks    = "t-plan-subtasks-arch"
	TrTDDLoop         = "t-tdd-loop"
	TrPackage         = "t-package"
	// Install via Makefile: a NodeKindBash transition runs
	// `make install` in the scaffolded tool directory, followed by a
	// Tool transition that finalises the registry manifest token.
	// The legacy NodeKindRegisterTool path is intentionally not wired.
	TrInstall         = "t-install"
	TrFinalizeInstall = "t-finalize-install"

	// TrHandleError consumes PlaceErrors tokens (typically from
	// validator/gate failures deposited by fire_llm's
	// depositSubAgentError) and routes them to PlaceFailed so the CPN
	// has an explicit terminal failure instead of a silent deadlock
	// caused by a missing consumer on PlaceErrors.
	TrHandleError = "t-handle-error"
)

// MaxRefineCount caps the spec-refinement loop per REQ-A06.
const MaxRefineCount = 2

// MaxTotals caps the totals decomposition per REQ-A07.
const MaxTotals = 8

// MaxTDDIterations caps the per-subtask TDD loop.
const MaxTDDIterations = 4

// CoverageFloor is the minimum package-level coverage percent enforced by
// the generated Makefile's test-coverage target AND by tool-creator's
// t-package transition per REQ-A09/REQ-A10.
const CoverageFloor = 85.0
