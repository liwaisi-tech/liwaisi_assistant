package fanout

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func samplePlan() AwakeningProbePlan {
	return AwakeningProbePlan{
		Rationale:         "unit-test plan",
		TimeoutPerProbeMs: 1000,
		Probes: []AwakeningProbeEntry{
			{ID: "cmd-sh", Kind: ProbeKindBinary, Target: "sh", Command: "command -v sh"},
			{ID: "cmd-git", Kind: ProbeKindBinary, Target: "git", Command: "command -v git"},
			{ID: "cap-python", Kind: ProbeKindCapability, Target: "python-runtime", Command: "python3 -V"},
		},
	}
}

func TestCompose_3Probes_ShapeInvariants(t *testing.T) {
	t.Parallel()
	plan := samplePlan()
	c, err := Compose("sess-1", plan, Deps{})
	if err != nil {
		t.Fatalf("Compose returned error: %v", err)
	}

	// (a) len(Transitions) == 3 LLM probes + 8 mandatory info probes + 1 reducer = 12.
	expectedProbes := 3 + len(mandatoryInfoProbes())
	if got, want := len(c.Transitions), expectedProbes+1; got != want {
		t.Fatalf("expected %d transitions (%d probes + reducer), got %d", want, expectedProbes, got)
	}

	// (b) Each probe transition has a single output place.
	probeOutputs := map[string]bool{}
	for id, tr := range c.Transitions {
		if id == TransitionReduceID {
			continue
		}
		if tr.Kind != cpn.NodeKindTool {
			t.Fatalf("probe transition %s is kind %s, want Tool", id, tr.Kind)
		}
		if len(tr.OutputPlaces) != 1 {
			t.Fatalf("probe %s has %d outputs, want 1", id, len(tr.OutputPlaces))
		}
		probeOutputs[tr.OutputPlaces[0]] = true
		if tr.Executor == nil {
			t.Fatalf("probe %s missing executor", id)
		}
	}

	// (c) Reducer input places are exactly the probe output places.
	reducer := c.Transitions[TransitionReduceID]
	if reducer == nil {
		t.Fatalf("missing reducer transition")
	}
	reducerInputs := map[string]bool{}
	for _, pid := range reducer.InputPlaces {
		reducerInputs[pid] = true
	}
	if !reflect.DeepEqual(probeOutputs, reducerInputs) {
		t.Fatalf("reducer inputs %v do not equal probe outputs %v",
			reducerInputs, probeOutputs)
	}

	// Reducer has 2 outputs (report + egress).
	if len(reducer.OutputPlaces) != 2 {
		t.Fatalf("reducer has %d outputs, want 2", len(reducer.OutputPlaces))
	}

	// Places include trigger, plan, report, egress, plus per-probe result places.
	for _, id := range []string{PlaceTriggerID, PlacePlanID, PlaceReportID, PlaceEgressID} {
		if _, ok := c.Places[id]; !ok {
			t.Fatalf("missing well-known place %s", id)
		}
	}
}

func TestCompose_Determinism_NFR002(t *testing.T) {
	t.Parallel()
	plan := samplePlan()
	a, err := Compose("sess-x", plan, Deps{})
	if err != nil {
		t.Fatalf("first compose: %v", err)
	}
	b, err := Compose("sess-x", plan, Deps{})
	if err != nil {
		t.Fatalf("second compose: %v", err)
	}

	keysA := sortedKeys(a.Transitions)
	keysB := sortedKeys(b.Transitions)
	if !reflect.DeepEqual(keysA, keysB) {
		t.Fatalf("transition key sets differ:\n a=%v\n b=%v", keysA, keysB)
	}

	placesA := sortedKeys(a.Places)
	placesB := sortedKeys(b.Places)
	if !reflect.DeepEqual(placesA, placesB) {
		t.Fatalf("place key sets differ:\n a=%v\n b=%v", placesA, placesB)
	}

	// Same arc topology: reducer input place list order must match.
	raIn := append([]string(nil), a.Transitions[TransitionReduceID].InputPlaces...)
	rbIn := append([]string(nil), b.Transitions[TransitionReduceID].InputPlaces...)
	if !reflect.DeepEqual(raIn, rbIn) {
		t.Fatalf("reducer input ordering not deterministic:\n a=%v\n b=%v", raIn, rbIn)
	}
}

func TestCompose_RejectsInvalidPlan(t *testing.T) {
	t.Parallel()
	_, err := Compose("s", AwakeningProbePlan{}, Deps{})
	if err == nil {
		t.Fatalf("expected error on empty plan")
	}
}

func TestCompose_SeedsTriggerAndPlan(t *testing.T) {
	t.Parallel()
	plan := samplePlan()
	c, err := Compose("s", plan, Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	// One trigger token per probe — each probe transition consumes from
	// the shared trigger place, so it must be seeded with (len(probes) +
	// mandatoryInfoProbes) tokens to prevent sub-CPN deadlock.
	wantTokens := len(plan.Probes) + len(mandatoryInfoProbes())
	if got := c.Places[PlaceTriggerID].Len(); got != wantTokens {
		t.Fatalf("trigger seed count: got %d want %d", got, wantTokens)
	}
	if c.Places[PlacePlanID].Len() != 1 {
		t.Fatalf("plan place must be seeded with 1 token")
	}
}

func TestCompose_NoSubNetOrInstantiate_CON004(t *testing.T) {
	t.Parallel()
	c, err := Compose("s", samplePlan(), Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	for id, tr := range c.Transitions {
		if tr.Kind == cpn.NodeKindInstantiate || tr.Kind == cpn.NodeKindSubNet {
			t.Fatalf("transition %s has forbidden kind %s", id, tr.Kind)
		}
	}
}

func TestAssertFlatTopology_FiresOnSubNet(t *testing.T) {
	t.Parallel()
	// Drive the CON-004 self-check by injecting a SubNet transition into
	// a composed CPN and asserting the helper rejects it.
	c, err := Compose("s", samplePlan(), Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	badID := "t-sneaky-subnet"
	c.Transitions[badID] = cpn.NewTransition(badID, cpn.NodeKindSubNet, nil, nil)
	err = assertFlatTopology(c)
	if err == nil {
		t.Fatalf("expected CON-004 error from assertFlatTopology, got nil")
	}
	if !strings.Contains(err.Error(), "CON-004") {
		t.Fatalf("expected error to reference CON-004, got %v", err)
	}
}

func TestAssertFlatTopology_FiresOnInstantiate(t *testing.T) {
	t.Parallel()
	c, err := Compose("s", samplePlan(), Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	c.Transitions["t-sneaky-inst"] = cpn.NewTransition("t-sneaky-inst", cpn.NodeKindInstantiate, nil, nil)
	if err := assertFlatTopology(c); err == nil {
		t.Fatalf("expected CON-004 error for Instantiate, got nil")
	}
}

func TestCompose_DedupesDuplicateIDs(t *testing.T) {
	t.Parallel()
	plan := AwakeningProbePlan{
		Probes: []AwakeningProbeEntry{
			{ID: "dup", Kind: ProbeKindBinary, Target: "a", Command: "command -v a"},
			{ID: "dup", Kind: ProbeKindBinary, Target: "b", Command: "command -v b"},
		},
	}
	c, err := Compose("s", plan, Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	// 2 dedup'd LLM probes + 8 mandatory info probes + 1 reducer.
	want := 2 + len(mandatoryInfoProbes()) + 1
	if got := len(c.Transitions); got != want {
		t.Fatalf("expected %d transitions, got %d", want, got)
	}
}

func TestCompose_DerivesIDFromCommand(t *testing.T) {
	t.Parallel()
	plan := AwakeningProbePlan{
		Probes: []AwakeningProbeEntry{
			{Kind: ProbeKindBinary, Target: "sh", Command: "command -v sh"},
		},
	}
	c, err := Compose("s", plan, Deps{})
	if err != nil {
		t.Fatalf("Compose error: %v", err)
	}
	// Find the probe transition (anything that isn't the reducer).
	var probeID string
	for id := range c.Transitions {
		if id != TransitionReduceID {
			probeID = id
			break
		}
	}
	if probeID == "" {
		t.Fatalf("no probe transition found")
	}
	if !strings.HasPrefix(probeID, TransitionProbePrefix) {
		t.Fatalf("transition %q lacks expected prefix", probeID)
	}
}

func TestClampTimeout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   int
		want int64 // want timeout in ms
	}{
		{0, int64(DefaultPerProbeTimeout / 1_000_000)},
		{-1, int64(DefaultPerProbeTimeout / 1_000_000)},
		{5000, int64(DefaultPerProbeTimeout / 1_000_000)},
		{50, int64(MinPerProbeTimeout / 1_000_000)},
		{1500, 1500},
	}
	for _, tc := range cases {
		got := clampTimeout(tc.in).Milliseconds()
		if got != tc.want {
			t.Errorf("clampTimeout(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
