package synthesis

import (
	"bytes"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// TestCanonicalise_MapIterationOrderStable proves AC-008: two topologies
// with different insertion / iteration orders produce the same canonical
// bytes (and thus the same flow_id hash). We simulate map-iteration
// variance by feeding the canonicaliser two topologies whose places and
// transitions are equal-valued but keyed differently.
func TestCanonicalise_MapIterationOrderStable(t *testing.T) {
	topoA := &persist.CPNTopology{
		ID:   "demo",
		Role: "echo",
		Places: map[string]persist.PlaceTopology{
			"p-a": {ID: "p-a", Color: "STRING", Space: "computation"},
			"p-b": {ID: "p-b", Color: "STRING", Space: "computation"},
			"p-c": {ID: "p-c", Color: "ARTIFACT", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-1": {ID: "t-1", Kind: "tool", InputPlaces: []string{"p-a"}, OutputPlaces: []string{"p-b"}, ExecutorFunc: "exec-noop"},
			"t-2": {ID: "t-2", Kind: "tool", InputPlaces: []string{"p-b"}, OutputPlaces: []string{"p-c"}, ExecutorFunc: "exec-noop"},
		},
	}
	// Rebuild topoB with maps populated in reversed insertion order — this
	// is the only lever we have to push Go's map randomisation hard.
	topoB := &persist.CPNTopology{ID: "demo", Role: "echo"}
	topoB.Places = make(map[string]persist.PlaceTopology, 3)
	topoB.Places["p-c"] = topoA.Places["p-c"]
	topoB.Places["p-b"] = topoA.Places["p-b"]
	topoB.Places["p-a"] = topoA.Places["p-a"]
	topoB.Transitions = make(map[string]persist.TransitionTopology, 2)
	topoB.Transitions["t-2"] = topoA.Transitions["t-2"]
	topoB.Transitions["t-1"] = topoA.Transitions["t-1"]

	bytesA, err := CanonicaliseTopology(topoA)
	if err != nil {
		t.Fatalf("canonicalise A: %v", err)
	}
	bytesB, err := CanonicaliseTopology(topoB)
	if err != nil {
		t.Fatalf("canonicalise B: %v", err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Fatalf("canonical bytes differ:\nA=%s\nB=%s", bytesA, bytesB)
	}
	// Hash parity (AC-004 idempotency).
	hashA, _ := CanonicalHash(topoA)
	hashB, _ := CanonicalHash(topoB)
	if hashA != hashB {
		t.Fatalf("hash mismatch: %s vs %s", hashA, hashB)
	}
}

func TestCanonicalise_NestedMapsAreSorted(t *testing.T) {
	topoA := &persist.CPNTopology{
		ID: "cfg-demo",
		Places: map[string]persist.PlaceTopology{
			"p-in": {ID: "p-in", Color: "STRING", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-x": {ID: "t-x", Kind: "tool", InputPlaces: []string{"p-in"}, OutputPlaces: []string{"p-in"},
				ToolParameters: []byte(`{"b":1,"a":2}`),
			},
		},
	}
	topoB := &persist.CPNTopology{
		ID: "cfg-demo",
		Places: map[string]persist.PlaceTopology{
			"p-in": {ID: "p-in", Color: "STRING", Space: "computation"},
		},
		Transitions: map[string]persist.TransitionTopology{
			"t-x": {ID: "t-x", Kind: "tool", InputPlaces: []string{"p-in"}, OutputPlaces: []string{"p-in"},
				ToolParameters: []byte(`{"a":2,"b":1}`),
			},
		},
	}
	ba, _ := CanonicaliseTopology(topoA)
	bb, _ := CanonicaliseTopology(topoB)
	if !bytes.Equal(ba, bb) {
		t.Fatalf("nested JSON not canonicalised:\nA=%s\nB=%s", ba, bb)
	}
}
