// Package toolapproval implements SC-13: the first-run HITL gate for
// synthesized tools. It blocks invocation of manifests with
// Kind="synthesized" behind an A2UI hitl component and records per-session
// approvals keyed by (session_id, tool_name, provenance_sha256). SHA256
// drift re-prompts. Non-synthesized tools pass through unchanged.
//
// SC-13 is library-only: Gate.CheckSynthesized is the integration seam a
// later wiring chunk threads into the tool-invocation runtime.
package toolapproval

import "time"

// Approval is a single recorded HITL decision. The composite key is
// (SessionID, ToolName, ProvenanceSHA256) — drift in any component misses
// the lookup and re-triggers HITL (REQ-1303).
type Approval struct {
	SessionID        string    `json:"session_id"`
	ToolName         string    `json:"tool_name"`
	ProvenanceSHA256 string    `json:"provenance_sha256"`
	ApprovedAt       time.Time `json:"approved_at"`
}

// Decision is the outcome of a gate check.
type Decision int

const (
	DecisionUnknown Decision = iota
	DecisionApproved
	DecisionDenied
)

func (d Decision) String() string {
	switch d {
	case DecisionApproved:
		return "approved"
	case DecisionDenied:
		return "denied"
	default:
		return "unknown"
	}
}
