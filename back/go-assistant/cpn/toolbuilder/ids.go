package toolbuilder

// FlowName is the FlowRegistry name for the tool-atelier topology.
// Used by main-conversation routers and by the architect planner to
// instantiate the atelier as a sub-CPN.
const FlowName = "tool-atelier"

// Place IDs. Grouped in topological order (intake → triage → investigate-&-scaffold-&-draft →
// review → totals → TDD → package → register).
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

	PlaceSpecForGo     = "p-spec-for-go"
	PlaceSpecForAI     = "p-spec-for-ai"
	PlaceSpecForDevOps = "p-spec-for-devops"
	PlaceSpecForQA     = "p-spec-for-qa"
	PlaceSpecForArch   = "p-spec-for-arch"
	PlaceSpecForPM     = "p-spec-for-pm"

	PlaceReviewGo     = "p-review-go"
	PlaceReviewAI     = "p-review-ai"
	PlaceReviewDevOps = "p-review-devops"
	PlaceReviewQA     = "p-review-qa"
	PlaceReviewArch   = "p-review-arch"
	PlaceReviewPM     = "p-review-pm"

	PlaceSpecApproved  = "p-spec-approved"
	PlaceRefineRequest = "p-refine-request"

	PlaceTotals      = "p-totals"
	PlaceDoIt        = "p-doit"
	PlaceTotalsEcho  = "p-totals-echo"
	PlaceTotalReady  = "p-total-ready"
	PlaceSubtasks    = "p-subtasks"
	PlaceTested      = "p-tested"
	PlacePackaged    = "p-packaged"
	PlaceRegistered  = "p-registered"
	PlaceErrors      = "p-errors"
)

// Transition IDs.
const (
	TrTriage              = "t-triage"
	TrDispatch            = "t-dispatch"
	TrInvestigate         = "t-investigate"
	TrScaffoldWorkspace   = "t-scaffold-workspace"
	TrDraftSpecV0         = "t-draft-spec-v0"
	TrEnrichSpec          = "t-enrich-spec"
	TrFanoutReviewers     = "t-fanout-reviewers"
	// Reviewer transition IDs follow t-{action}-{profile}. These values
	// equal TransitionID(ActionReviewSpec, Profile*) — asserted by
	// TestTransitionIDConvention. Renamed in PR1; no aliases kept.
	TrReviewGo            = "t-review-spec-go-eng"
	TrReviewAI            = "t-review-spec-ai-eng"
	TrReviewDevOps        = "t-review-spec-devops"
	TrReviewQA            = "t-review-spec-qa"
	TrReviewArch          = "t-review-spec-arch"
	TrReviewPM            = "t-review-spec-pm"
	TrAggregateApprove = "t-aggregate-approve"
	TrAggregateRefine  = "t-aggregate-refine"
	// PR4: renamed to t-{action}-{profile}. These equal
	// TransitionID(action, ProfileArch) — asserted at build time by
	// buildSubAgentLLM. No aliases kept.
	TrRefineSpec      = "t-refine-spec-arch"
	TrDecomposeTotals = "t-decompose-totals-arch"
	TrAuthorizeTotals = "t-authorize-totals"
	TrGateTotal       = "t-gate-total"
	TrPlanSubtasks    = "t-plan-subtasks-arch"
	TrTDDLoop             = "t-tdd-loop"
	TrPackage             = "t-package"
	TrRegister            = "t-register"
)

// MaxRefineCount caps the spec-refinement loop per REQ-A06.
const MaxRefineCount = 2

// MaxTotals caps the totals decomposition per REQ-A07.
const MaxTotals = 8

// MaxTDDIterations caps the per-subtask TDD loop.
const MaxTDDIterations = 4

// CoverageFloor is the minimum package-level coverage percent enforced by
// the generated Makefile's test-coverage target AND by the atelier's
// t-package transition per REQ-A09/REQ-A10.
const CoverageFloor = 85.0
