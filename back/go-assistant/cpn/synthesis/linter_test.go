package synthesis

import (
	"errors"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// newTestSafeRegistry returns a freshly-registered, sealed SafeRegistry
// with the spec §3 REQ-030 catalogue.
func newTestSafeRegistry(t *testing.T) *SafeRegistry {
	t.Helper()
	r := NewSafeRegistry()
	RegisterDefaults(r)
	r.Seal()
	return r
}

// helpers to build minimal topologies.
func goodTopology() *persist.CPNTopology {
	return &persist.CPNTopology{
		ID:   "good",
		Role: "echo",
		Places: map[string]persist.PlaceTopology{
			"p-in":  {ID: "p-in", Color: "STRING", Space: "computation"},
			"p-out": {ID: "p-out", Color: "ARTIFACT", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-1": {
				ID:           "t-1",
				Kind:         "tool",
				InputPlaces:  []string{"p-in"},
				OutputPlaces: []string{"p-out"},
				ExecutorFunc: "exec-noop",
			},
		},
	}
}

func TestLint_GoodTopology_Passes(t *testing.T) {
	safe := newTestSafeRegistry(t)
	res := Lint(goodTopology(), safe, cpn.DefaultSizeCap())
	if !res.PassedFlag {
		t.Fatalf("expected pass, got %#v (err=%v)", res, res.Err())
	}
}

func TestLint_UnsafePrimitive_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := goodTopology()
	tt := topo.Transitions["t-1"]
	tt.ExecutorFunc = "exec-run-any-bash"
	topo.Transitions["t-1"] = tt
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrUnsafePrimitive) {
		t.Fatalf("expected ErrUnsafePrimitive, got %v", res.Err())
	}
	if len(res.UnsafePrimitives) != 1 || res.UnsafePrimitives[0] != "exec-run-any-bash" {
		t.Fatalf("expected unsafe list to contain the bad name, got %v", res.UnsafePrimitives)
	}
}

func TestLint_TooManyTransitions_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := goodTopology()
	for i := 0; i < 51; i++ {
		id := "t-x-" + itoa(i)
		topo.Transitions[id] = persist.TransitionTopology{
			ID:           id,
			Kind:         "tool",
			InputPlaces:  []string{"p-in"},
			OutputPlaces: []string{"p-out"},
			ExecutorFunc: "exec-noop",
		}
	}
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrTopologyTooLarge) {
		t.Fatalf("expected ErrTopologyTooLarge, got %v", res.Err())
	}
}

func TestLint_MissingTerminal_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := &persist.CPNTopology{
		ID: "cycle",
		Places: map[string]persist.PlaceTopology{
			"p-a": {ID: "p-a", Color: "STRING", Space: "computation"},
			"p-b": {ID: "p-b", Color: "STRING", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-a": {ID: "t-a", Kind: "tool", InputPlaces: []string{"p-a"}, OutputPlaces: []string{"p-b"}, ExecutorFunc: "exec-noop"},
			"t-b": {ID: "t-b", Kind: "tool", InputPlaces: []string{"p-b"}, OutputPlaces: []string{"p-a"}, ExecutorFunc: "exec-noop"},
		},
	}
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrNoTerminal) {
		t.Fatalf("expected ErrNoTerminal, got %v", res.Err())
	}
}

func TestLint_DisallowedKind_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := goodTopology()
	topo.Transitions["t-reg"] = persist.TransitionTopology{
		ID: "t-reg", Kind: "register_tool",
		InputPlaces: []string{"p-in"}, OutputPlaces: []string{"p-out"},
	}
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrDisallowedKind) {
		t.Fatalf("expected ErrDisallowedKind, got %v", res.Err())
	}
}

func TestLint_UnresolvedArc_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := goodTopology()
	tt := topo.Transitions["t-1"]
	tt.OutputPlaces = append(tt.OutputPlaces, "p-ghost")
	topo.Transitions["t-1"] = tt
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrUnresolvedArc) {
		t.Fatalf("expected ErrUnresolvedArc, got %v", res.Err())
	}
}

func TestLint_SpaceIsolation_Rejects(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := &persist.CPNTopology{
		ID: "surface-bridge",
		Places: map[string]persist.PlaceTopology{
			"p-surface": {ID: "p-surface", Color: "STRING", Space: "surface"},
			"p-compute": {ID: "p-compute", Color: "STRING", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-bridge": {
				ID: "t-bridge", Kind: "tool",
				InputPlaces: []string{"p-surface"}, OutputPlaces: []string{"p-compute"},
				ExecutorFunc: "exec-noop",
			},
		},
	}
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if res.PassedFlag {
		t.Fatalf("expected failure")
	}
	if !errors.Is(res.Err(), cpn.ErrSpaceIsolationViolated) {
		t.Fatalf("expected ErrSpaceIsolationViolated, got %v", res.Err())
	}
}

func TestLint_HITLException_AllowsSurfaceToCompute(t *testing.T) {
	safe := newTestSafeRegistry(t)
	topo := &persist.CPNTopology{
		ID: "hitl",
		Places: map[string]persist.PlaceTopology{
			"p-surface": {ID: "p-surface", Color: "STRING", Space: "surface"},
			"p-compute": {ID: "p-compute", Color: "HUMAN", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-hitl": {
				ID: "t-hitl", Kind: "hitl",
				InputPlaces: []string{"p-surface"}, OutputPlaces: []string{"p-compute"},
			},
		},
	}
	res := Lint(topo, safe, cpn.DefaultSizeCap())
	if !res.PassedFlag {
		t.Fatalf("expected pass, got %v", res.Err())
	}
}

// FuzzLint_NeverAcceptsSpaceViolation enforces the §10 validation claim:
// no generated topology with a direct surface→computation edge (outside
// HITL) may pass the linter.
func FuzzLint_NeverAcceptsSpaceViolation(f *testing.F) {
	f.Add(true, true)
	f.Add(true, false)
	f.Add(false, true)
	f.Fuzz(func(t *testing.T, surfaceInput bool, computationOutput bool) {
		if !surfaceInput || !computationOutput {
			t.Skip()
		}
		safe := newTestSafeRegistry(t)
		topo := &persist.CPNTopology{
			ID: "fuzz",
			Places: map[string]persist.PlaceTopology{
				"p-surface": {ID: "p-surface", Color: "STRING", Space: "surface"},
				"p-compute": {ID: "p-compute", Color: "STRING", Space: "computation"},
			},
			Transitions: map[string]persist.TransitionTopology{
				"t-bridge": {
					ID: "t-bridge", Kind: "tool",
					InputPlaces:  []string{"p-surface"},
					OutputPlaces: []string{"p-compute"},
					ExecutorFunc: "exec-noop",
				},
			},
		}
		res := Lint(topo, safe, cpn.DefaultSizeCap())
		if res.PassedFlag {
			t.Fatalf("linter accepted space-violating topology")
		}
	})
}

// itoa is a zero-dependency integer-to-string helper used by the test to
// generate many transition IDs without importing strconv in every file.
func itoa(i int) string {
	return strings.Trim(
		func() string {
			if i == 0 {
				return "0"
			}
			digits := []byte{}
			for i > 0 {
				digits = append([]byte{byte('0' + i%10)}, digits...)
				i /= 10
			}
			return string(digits)
		}(), " ")
}
