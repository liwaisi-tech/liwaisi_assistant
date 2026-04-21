package synthesis

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// Bootstrap installs the synthesis package's hook implementations on the
// cpn package. Call once at server start (and in any test that exercises
// fire_synthesize / fire_instantiate end-to-end).
//
// The hooks are JSON-opaque by contract so cpn/ never needs to import
// persist/ (Axiom A13). This function is the only place synthesis/ and
// cpn/ cross via the hook surface.
func Bootstrap(safe *SafeRegistry) {
	cpn.SetLintTopology(func(raw json.RawMessage, sr cpn.SafeRegistryPort, cap cpn.SizeCap) cpn.LintResultPort {
		topo, err := decodeTopology(raw)
		if err != nil {
			return LintResult{
				PassedFlag: false,
				Other:      []string{"topology decode error: " + err.Error()},
			}
		}
		return Lint(topo, sr, cap)
	})

	cpn.SetCanonicaliseTopology(func(raw json.RawMessage) ([]byte, error) {
		topo, err := decodeTopology(raw)
		if err != nil {
			return nil, err
		}
		return CanonicaliseTopology(topo)
	})

	cpn.SetMaterialiseTopology(func(_ context.Context, raw json.RawMessage, _ cpn.SafeRegistryPort) (*cpn.CPN, error) {
		topo, err := decodeTopology(raw)
		if err != nil {
			return nil, err
		}
		return Materialise(topo, safe)
	})

	cpn.SetTopologyDigest(func(raw json.RawMessage) cpn.TopologyDigest {
		topo, err := decodeTopology(raw)
		if err != nil {
			return cpn.TopologyDigest{}
		}
		names := make(map[string]struct{})
		for _, tr := range topo.Transitions {
			for _, n := range []string{tr.GuardFunc, tr.ExecutorFunc, tr.FactoryFunc} {
				if n != "" {
					names[n] = struct{}{}
				}
			}
		}
		ref := make([]string, 0, len(names))
		for n := range names {
			ref = append(ref, n)
		}
		sort.Strings(ref)
		return cpn.TopologyDigest{
			Name:                 topo.ID,
			Role:                 topo.Role,
			SizePlaces:           len(topo.Places),
			SizeTransitions:      len(topo.Transitions),
			ReferencedPrimitives: ref,
		}
	})
}

// LintRaw decodes the raw persist.CPNTopology JSON blob and runs Lint on
// it with the supplied SafeRegistry and cap. Exposed for callers that sit
// on the cpn side of Axiom A13 and cannot import persist directly (e.g.
// cpn/synthesis/jit). Returns a LintResult with PassedFlag=false and a
// decode-error note when the blob is unparseable.
func LintRaw(raw json.RawMessage, safe cpn.SafeRegistryPort, cap cpn.SizeCap) LintResult {
	topo, err := decodeTopology(raw)
	if err != nil {
		return LintResult{
			PassedFlag: false,
			Other:      []string{"topology decode error: " + err.Error()},
		}
	}
	return Lint(topo, safe, cap)
}

func decodeTopology(raw json.RawMessage) (*persist.CPNTopology, error) {
	if len(raw) == 0 {
		return &persist.CPNTopology{}, nil
	}
	var t persist.CPNTopology
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
