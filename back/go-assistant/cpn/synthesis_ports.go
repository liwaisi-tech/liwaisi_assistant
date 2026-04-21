package cpn

import (
	"context"
	"encoding/json"
)

// ── GAP-4 ports ────────────────────────────────────────────────────────────
//
// These interfaces bridge the cpn/ engine to the synthesis / flow-repo
// collaborators without forcing cpn/ to import the synthesis or persist
// sub-packages (Axiom A13). Infrastructure wires concrete adapters at boot.
//
// Spec: spec-architecture-cpn-synthesis-instantiate.md §3/§4.

// FlowRef is the payload of a ColorFlowRef token. Produced by
// NodeKindSynthesize after a topology is successfully linted + persisted;
// consumed by NodeKindInstantiate.
type FlowRef struct {
	FlowID  string `json:"flow_id"`
	Summary string `json:"summary,omitempty"`
}

// AuthoredTopologySummary carries the minimal shape the instantiate
// transition needs to display a HITL approval prompt. Kept tiny so the
// prompt stays readable.
type AuthoredTopologySummary struct {
	FlowID               string   `json:"flow_id"`
	Name                 string   `json:"name,omitempty"`
	Summary              string   `json:"summary,omitempty"`
	SizePlaces           int      `json:"size_places"`
	SizeTransitions      int      `json:"size_transitions"`
	ReferencedPrimitives []string `json:"referenced_primitives,omitempty"`
}

// AuthoredFlowProvenance captures who asked for the topology. Attached
// to every Save so admins can trace agent-authored flows back to their
// authoring CPN and prompt digest.
type AuthoredFlowProvenance struct {
	AuthoredByCPNID        string `json:"authored_by_cpn_id"`
	AuthoredFromPromptHash string `json:"authored_from_prompt_digest"`
	SessionID              string `json:"session_id,omitempty"`
}

// AuthoredFlowRecord is the loaded twin of a persisted topology row. The
// TopologyJSON is a canonical JSON serialisation of persist.CPNTopology.
type AuthoredFlowRecord struct {
	FlowID          string
	TopologyJSON    json.RawMessage
	Summary         string
	Rejected        bool
	RejectedReason  string
	SafeLintPassed  bool
	SizePlaces      int
	SizeTransitions int
	Provenance      AuthoredFlowProvenance
}

// AuthoredFlowRepository is the port NodeKindSynthesize + NodeKindInstantiate
// use to persist and reload agent-authored topologies. Implementations are
// wired at boot (Postgres in prod, in-memory in tests).
type AuthoredFlowRepository interface {
	// SaveAuthored persists a canonical topology JSON + provenance.
	// Idempotent by SHA-256 over canonicalised JSON — callers that emit
	// the same topology twice MUST receive the same flow_id.
	// Returns (flow_id, created=true when the row is new, error).
	SaveAuthored(ctx context.Context, canonicalJSON json.RawMessage, summary string, sizePlaces, sizeTransitions int, referenced []string, prov AuthoredFlowProvenance) (flowID string, created bool, err error)

	// GetByID returns the persisted record or an error.
	GetByID(ctx context.Context, flowID string) (*AuthoredFlowRecord, error)

	// Reject marks a flow as rejected so future instantiations fail fast
	// with ErrTopologyRejected.
	Reject(ctx context.Context, flowID, reason string) error

	// ListAuthored returns agent-authored flows (optionally filtered). The
	// admin API uses this to surface everything a CPN synthesised.
	ListAuthored(ctx context.Context, limit int) ([]*AuthoredFlowRecord, error)
}

// SafeRegistryPort is the read-side of the safe primitive catalogue
// (REQ-030). The linter consults Lookup to reject unsafe references; the
// instantiate materialiser uses BuildFuncRegistry to get a FuncRegistry
// preloaded with every safe primitive's Go implementation.
//
// The concrete implementation lives in cpn/synthesis. cpn/ defines only
// the port so fire_synthesize / fire_instantiate stay free of that
// sub-package.
type SafeRegistryPort interface {
	// Lookup returns the kind (guard|executor|factory) of the named
	// primitive and true, or "" and false if the name is unknown.
	Lookup(name string) (kind string, ok bool)

	// Catalogue returns a JSON-encoded description of every registered
	// primitive suitable for LLM prompt injection (REQ-020).
	Catalogue() json.RawMessage

	// Names returns every registered primitive name, sorted — used for
	// deterministic prompt rendering and admin introspection.
	Names() []string
}

// TopologyHITLRouter surfaces a "topology.approval" HITL question to the
// user before NodeKindInstantiate spawns the child CPN (REQ-050). Wired
// by SessionService. Nil falls open with a WARN log, matching GAP-3's
// first-run-ledger pattern.
type TopologyHITLRouter interface {
	// ApproveTopology publishes a topology-approval prompt and blocks
	// until the user responds. Returns true if the user approved.
	ApproveTopology(ctx context.Context, summary AuthoredTopologySummary) (approved bool, err error)
}
