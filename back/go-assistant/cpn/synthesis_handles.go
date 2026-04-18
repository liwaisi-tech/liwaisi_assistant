package cpn

import (
	"context"
	"encoding/json"
)

// The cpn package must not import cpn/synthesis or cpn/persist (A13:
// sub-packages may import cpn, not the reverse). fire_synthesize and
// fire_instantiate need the linter + canonicaliser + materialiser at
// runtime, so we publish tiny opaque-JSON hooks here that the wiring
// layer (cmd/server/main.go) populates at boot.
//
// Nil handles trigger a permissive / no-op fallback so the executor
// never panics in dev mode.

// LintTopologyFunc is the pluggable linter entry point. It takes the
// topology JSON (already parsed through encoding/json into an opaque
// blob), the safe registry, and the size cap.
type LintTopologyFunc func(topologyJSON json.RawMessage, safe SafeRegistryPort, cap SizeCap) LintResultPort

// CanonicaliseTopologyFunc produces a byte-stable JSON serialisation of
// an opaque topology (AC-008). Must be deterministic across Go map
// iteration order.
type CanonicaliseTopologyFunc func(topologyJSON json.RawMessage) ([]byte, error)

// MaterialiseTopologyFunc rehydrates an opaque topology into a runnable
// *CPN using ONLY safe primitives. Errors surface as ErrDeprecatedDependency
// when a referenced primitive has been removed since persist time.
type MaterialiseTopologyFunc func(ctx context.Context, topologyJSON json.RawMessage, safe SafeRegistryPort) (*CPN, error)

// TopologyDigestFunc extracts size + referenced-primitives metadata from an
// opaque topology. Called by fire_synthesize to populate the persistence
// record and the HITL summary.
type TopologyDigestFunc func(topologyJSON json.RawMessage) TopologyDigest

// TopologyDigest summarises the result of TopologyDigestFunc.
type TopologyDigest struct {
	Name                 string
	Role                 string
	SizePlaces           int
	SizeTransitions      int
	ReferencedPrimitives []string
}

// LintResultPort is the shape the executor needs from a lint run.
type LintResultPort interface {
	Passed() bool
	Err() error
}

var (
	lintTopologyHook        LintTopologyFunc
	canonicaliseTopologyFun CanonicaliseTopologyFunc
	materialiseTopologyHook MaterialiseTopologyFunc
	topologyDigestHook      TopologyDigestFunc
)

// SetLintTopology installs the pluggable linter.
func SetLintTopology(fn LintTopologyFunc) { lintTopologyHook = fn }

// SetCanonicaliseTopology installs the pluggable canonicaliser.
func SetCanonicaliseTopology(fn CanonicaliseTopologyFunc) { canonicaliseTopologyFun = fn }

// SetMaterialiseTopology installs the pluggable materialiser.
func SetMaterialiseTopology(fn MaterialiseTopologyFunc) { materialiseTopologyHook = fn }

// SetTopologyDigest installs the pluggable digest helper.
func SetTopologyDigest(fn TopologyDigestFunc) { topologyDigestHook = fn }

// ── internal-use wrappers (nil-safe) ──────────────────────────────────────

func lintTopology(topo json.RawMessage, safe SafeRegistryPort, cap SizeCap) LintResultPort {
	if lintTopologyHook == nil {
		return passThroughLint{}
	}
	return lintTopologyHook(topo, safe, cap)
}

func canonicaliseTopology(topo json.RawMessage) ([]byte, error) {
	if canonicaliseTopologyFun != nil {
		return canonicaliseTopologyFun(topo)
	}
	// Fallback: return the JSON as-is (unstable across processes but
	// round-trippable).
	out := make([]byte, len(topo))
	copy(out, topo)
	return out, nil
}

func materialiseTopology(ctx context.Context, topo json.RawMessage, safe SafeRegistryPort) (*CPN, error) {
	if materialiseTopologyHook == nil {
		return nil, errNoMaterialiser
	}
	return materialiseTopologyHook(ctx, topo, safe)
}

func topologyDigest(topo json.RawMessage) TopologyDigest {
	if topologyDigestHook == nil {
		return TopologyDigest{}
	}
	return topologyDigestHook(topo)
}

type passThroughLint struct{}

func (passThroughLint) Passed() bool { return true }
func (passThroughLint) Err() error   { return nil }
