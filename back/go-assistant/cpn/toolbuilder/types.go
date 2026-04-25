package toolbuilder

// Request is the user-/router-supplied input on p-request.
type Request struct {
	// Name is the tool's identifier (becomes the binary name and go.mod path suffix).
	Name string `json:"name"`

	// Module is the go.mod module path (defaults to "brae.tools/<Name>").
	Module string `json:"module"`

	// Description is a one-line summary the user provided or the router inferred.
	Description string `json:"description"`

	// Author is optional; used for README provenance only.
	Author string `json:"author,omitempty"`

	// SessionID threads the originating session through tool-creator for
	// telemetry and event correlation.
	SessionID string `json:"session_id,omitempty"`
}

// TriageVerdict is the payload on p-triaged. Produced by t-triage (LLM).
type TriageVerdict struct {
	Ready             bool    `json:"ready"`
	ClarifyQuestion   string  `json:"clarify_question,omitempty"`
	NormalisedRequest Request `json:"request"`
}

// SpecDraft is the payload circulating through p-spec-v0, p-spec-draft,
// p-spec-for-*, and p-spec-approved. tool-creator's core representation.
type SpecDraft struct {
	Name        string        `json:"name"`
	Purpose     string        `json:"purpose"`
	NonGoals    []string      `json:"non_goals,omitempty"`
	DomainModel string        `json:"domain_model"`
	Ports       []PortSpec    `json:"ports"`
	Adapters    []AdapterSpec `json:"adapters"`
	CLISurface  string        `json:"cli_surface"`
	TestPlan    []string      `json:"test_plan"`
	Risks       []string      `json:"risks,omitempty"`

	// RefineCount tracks how many times this draft has been through the
	// review → refine loop. Capped by MaxRefineCount per REQ-A06.
	RefineCount int `json:"refine_count,omitempty"`

	// WorkspacePath is set by t-enrich-spec after the scaffold lands.
	WorkspacePath string `json:"workspace_path,omitempty"`

	// ExistingTools is the t-investigate summary, embedded so reviewers
	// can flag name collisions and capability overlap.
	ExistingTools []ExistingTool `json:"existing_tools,omitempty"`
}

// PortSpec describes a hexagonal port in the spec.
type PortSpec struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

// AdapterSpec describes an adapter implementing a port.
type AdapterSpec struct {
	Name           string `json:"name"`
	ImplementsPort string `json:"implements_port"`
	Summary        string `json:"summary"`
}

// ExistingTool is what t-investigate emits: a peek at what already lives
// in workspace/tools/src/ so reviewers can flag collisions.
type ExistingTool struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Summary string `json:"summary"`
}

// Review is the output of any t-review-* transition.
type Review struct {
	Role           string   `json:"role"`
	Approved       bool     `json:"approved"`
	BlockingIssues []string `json:"blocking_issues,omitempty"`
	Suggestions    []string `json:"suggestions,omitempty"`
}

// Verdict is the payload on p-spec-approved OR p-refine-request, depending
// on which aggregator transition fired. Always carries the reviews for
// downstream introspection + refinement context.
type Verdict struct {
	Approved       bool      `json:"approved"`
	BlockingIssues []string  `json:"blocking_issues,omitempty"`
	Suggestions    []string  `json:"suggestions,omitempty"`
	Reviews        []Review  `json:"reviews"`
	SpecDraft      SpecDraft `json:"spec_draft"`
}

// TotalSpec is a single element of a totals batch. One total = one
// independent workstream.
type TotalSpec struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Deliverables []string `json:"deliverables"`
	ParallelSafe bool     `json:"parallel_safe"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

// TotalsBatch is the payload on p-totals (and echoed on p-totals-echo).
// Batch-carrier shape: the entire decomposition rides on a single token
// for v1 simplicity. v2 may split into multi-token firings.
type TotalsBatch struct {
	SpecName      string      `json:"spec_name"`
	WorkspacePath string      `json:"workspace_path,omitempty"`
	Totals        []TotalSpec `json:"totals"`
}

// DoItBatch is the payload on p-doit. Produced by t-authorize-totals: one
// DoIt entry per total in the batch, keyed by TotalID.
type DoItBatch struct {
	Entries []DoIt `json:"entries"`
}

// DoIt is the autonomous "go" token for one total. Replaces what would
// have been a per-total human-approval gate (see feedback_minimize_hitl.md).
type DoIt struct {
	TotalID  string `json:"total_id"`
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

// TotalReadyBatch is the payload on p-total-ready: totals paired with
// their DoIt tokens.
type TotalReadyBatch struct {
	SpecName      string       `json:"spec_name"`
	WorkspacePath string       `json:"workspace_path,omitempty"`
	Entries       []TotalReady `json:"entries"`
}

// TotalReady pairs a TotalSpec with the DoIt that authorised it.
type TotalReady struct {
	Total TotalSpec `json:"total"`
	DoIt  DoIt      `json:"doit"`
}

// SubtasksBatch is the payload on p-subtasks: for each ready total, an
// LLM-generated list of subtasks.
type SubtasksBatch struct {
	SpecName      string      `json:"spec_name"`
	WorkspacePath string      `json:"workspace_path,omitempty"`
	ByTotal       []TotalSubs `json:"by_total"`
}

// TotalSubs groups a total's subtasks.
type TotalSubs struct {
	TotalID  string    `json:"total_id"`
	Subtasks []Subtask `json:"subtasks"`
}

// Subtask is the minimum-context work unit fed to the TDD loop.
type Subtask struct {
	ID           string   `json:"id"`
	Purpose      string   `json:"purpose"`
	FilesToTouch []string `json:"files_to_touch,omitempty"`
	TestName     string   `json:"test_name,omitempty"`
	ParallelSafe bool     `json:"parallel_safe"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

// TestedArtifact is the payload on p-tested — one per spec (batch) for v1.
type TestedArtifact struct {
	SpecName      string        `json:"spec_name"`
	WorkspacePath string        `json:"workspace_path"`
	CoveragePct   float64       `json:"coverage_pct"`
	PerTotal      []TotalTested `json:"per_total"`
}

// TotalTested is the per-total outcome of the TDD loop.
type TotalTested struct {
	TotalID    string  `json:"total_id"`
	Passed     bool    `json:"passed"`
	Iterations int     `json:"iterations"`
	Coverage   float64 `json:"coverage"`
	Notes      string  `json:"notes,omitempty"`
}
