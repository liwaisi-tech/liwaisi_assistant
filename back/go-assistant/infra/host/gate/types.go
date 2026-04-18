// Package gate is the GAP-6 policy-backed HostGate implementation.
//
// It replaces the allow-all stub from GAP-1 with a declarative policy
// engine, a first-run ledger, a session-scoped budget tracker, and a
// sandbox-runtime wrapper. Every call to HostAdapter.Exec / SpawnPTY /
// WriteFile already flows through cpn.HostGate.Check; gate.PolicyHostGate
// is a drop-in replacement that evaluates a HostPolicy and escalates to
// HITL where appropriate.
//
// Spec: spec/spec-architecture-host-gate-security-policy.md.
package gate

import (
	"errors"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// RiskBand classifies a command's pattern-matched risk level.
type RiskBand string

// Risk bands map directly to the spec §3 policy buckets.
const (
	RiskSafe      RiskBand = "safe"
	RiskCaution   RiskBand = "caution"
	RiskDangerous RiskBand = "dangerous"
	RiskForbidden RiskBand = "forbidden"
	RiskUnknown   RiskBand = "unknown"
)

// Verdict is the gate's decision for a single GateOp.
type Verdict string

const (
	// VerdictAllow lets the call proceed.
	VerdictAllow Verdict = "allow"
	// VerdictDeny terminates the call with ErrGateDenied.
	VerdictDeny Verdict = "deny"
	// VerdictRequireHITL pauses the call and routes a host-approval prompt.
	VerdictRequireHITL Verdict = "require-hitl"
)

// BudgetEstimate is the predicted resource consumption of a GateOp.
// Callers can leave any field at zero — the gate defaults to a
// conservative estimate (1 CPU-second, 0 bytes, 0 outbound requests).
type BudgetEstimate struct {
	CPUSeconds       int
	BytesWritten     int64
	OutboundRequests int
}

// Decision is the full structured outcome of a Check. The verdict is the
// primary field; the ancillary fields carry audit context and the HITL
// prompt payload.
type Decision struct {
	Verdict   Verdict
	RiskBand  RiskBand
	Reason    string
	Sandbox   cpn.SandboxProfile
	Budget    BudgetEstimate
	Prompt    *HostApprovalPrompt // non-nil when Verdict == VerdictRequireHITL.
	FirstRun  bool                // true when first-run ledger triggered the HITL.
	SessionID string
}

// HostApprovalPrompt is the payload delivered to the frontend as a
// "host.approval" HITL question. Mirrors the spec §3 REQ-040 contract.
type HostApprovalPrompt struct {
	Schema       string   `json:"schema"`        // Always "host.approval".
	Operation    string   `json:"operation"`     // "exec" | "spawn_pty" | "write_file" | "kill".
	Command      string   `json:"command"`       // Full command line (or write_file path).
	RiskBand     RiskBand `json:"risk_band"`     // "safe" | "caution" | "dangerous" | "forbidden" | "unknown".
	Rationale    string   `json:"rationale"`     // Why the gate paused (e.g. "first-run unknown binary").
	Alternatives []string `json:"alternatives,omitempty"`
}

// ErrRequiresHITL is a structured error signalling that the gate cannot
// approve the op without a human decision. Callers inspect the Decision
// field to route the Prompt through the session's HITL channel. Use
// errors.As/errors.Is to detect it.
type ErrRequiresHITL struct {
	Decision Decision
}

// Error implements the error interface.
func (e *ErrRequiresHITL) Error() string {
	if e == nil {
		return "require-hitl"
	}
	return fmt.Sprintf("host gate requires HITL: %s (band=%s, reason=%s)", e.Decision.Verdict, e.Decision.RiskBand, e.Decision.Reason)
}

// Is matches any *ErrRequiresHITL, so errors.Is(err, &ErrRequiresHITL{})
// works regardless of the embedded decision.
func (e *ErrRequiresHITL) Is(target error) bool {
	_, ok := target.(*ErrRequiresHITL)
	return ok
}

// IsRequiresHITL returns the embedded Decision and true if err is a
// require-hitl sentinel (or wraps one); otherwise the zero Decision and
// false.
func IsRequiresHITL(err error) (Decision, bool) {
	if err == nil {
		return Decision{}, false
	}
	var rh *ErrRequiresHITL
	if errors.As(err, &rh) && rh != nil {
		return rh.Decision, true
	}
	return Decision{}, false
}

// ErrSandboxUnavailable is returned when a profile demands a sandbox
// runtime that is not installed on the host and no fallback exists.
var ErrSandboxUnavailable = cpn.NewHostError(cpn.HostErrCodeGateDenied, "sandbox runtime unavailable (bwrap/firejail missing)", nil)

// HostApprovalSchema is the stable HITL prompt schema identifier.
const HostApprovalSchema = "host.approval"
