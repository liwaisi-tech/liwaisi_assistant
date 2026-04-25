package cpn

import (
	"testing"
)

func TestComputeFlowSignature_EmptyCPN(t *testing.T) {
	sig := ComputeFlowSignature(nil, nil)
	if sig.Digest == "" {
		t.Fatal("nil CPN must still yield a stable Digest")
	}
	if len(sig.InputColors) != 0 || len(sig.OutputColors) != 0 {
		t.Errorf("nil CPN yielded non-empty colours: %+v", sig)
	}
}

func TestComputeFlowSignature_ClassifiesSourcesAndSinks(t *testing.T) {
	c := &CPN{
		ID: "c",
		Places: map[string]*Place{
			"in":  {ID: "in", Color: "json", Space: SpaceComputation},
			"mid": {ID: "mid", Color: "artifact", Space: SpaceComputation},
			"out": {ID: "out", Color: "artifact", Space: SpaceComputation},
		},
		Transitions: map[string]*Transition{
			"t1": {
				ID:           "t1",
				Kind:         NodeKindTool,
				ToolName:     "bash_exec",
				InputPlaces:  []string{"in"},
				OutputPlaces: []string{"mid"},
			},
			"t2": {
				ID:           "t2",
				Kind:         NodeKindLLM,
				InputPlaces:  []string{"mid"},
				OutputPlaces: []string{"out"},
			},
		},
	}
	sig := ComputeFlowSignature(c, nil)
	if len(sig.InputColors) != 1 || sig.InputColors[0] != "json" {
		t.Errorf("InputColors = %v, want [json]", sig.InputColors)
	}
	if len(sig.OutputColors) != 1 || sig.OutputColors[0] != "artifact" {
		t.Errorf("OutputColors = %v, want [artifact]", sig.OutputColors)
	}
	if len(sig.RequiredTools) != 1 || sig.RequiredTools[0] != "bash_exec" {
		t.Errorf("RequiredTools = %v, want [bash_exec]", sig.RequiredTools)
	}
	if sig.Template != "custom" {
		t.Errorf("Template = %q, want custom", sig.Template)
	}
	if sig.Digest == "" {
		t.Error("Digest must be non-empty")
	}
}

func TestComputeFlowSignature_QualifiedToolName(t *testing.T) {
	c := &CPN{
		Places: map[string]*Place{
			"in":  {ID: "in", Color: "json", Space: SpaceComputation},
			"out": {ID: "out", Color: "artifact", Space: SpaceComputation},
		},
		Transitions: map[string]*Transition{
			"t1": {
				ID:           "t1",
				Kind:         NodeKindTool,
				ToolName:     "bash_exec",
				ToolMeta:     &ToolMeta{Namespace: "system"},
				InputPlaces:  []string{"in"},
				OutputPlaces: []string{"out"},
			},
		},
	}
	sig := ComputeFlowSignature(c, nil)
	if got := sig.RequiredTools; len(got) != 1 || got[0] != "system/bash_exec" {
		t.Errorf("RequiredTools = %v, want [system/bash_exec]", got)
	}
}

type fakeResolver struct {
	hashtags map[string][]string
	caps     map[string][]string
}

func (r fakeResolver) HashtagsFor(qn string) []string { return r.hashtags[qn] }
func (r fakeResolver) CapsFor(qn string) []string     { return r.caps[qn] }

func TestComputeFlowSignature_UsesResolver(t *testing.T) {
	c := &CPN{
		Places: map[string]*Place{
			"in":  {ID: "in", Color: "json", Space: SpaceComputation},
			"out": {ID: "out", Color: "artifact", Space: SpaceComputation},
		},
		Transitions: map[string]*Transition{
			"t1": {
				ID:           "t1",
				Kind:         NodeKindTool,
				ToolName:     "bash_exec",
				InputPlaces:  []string{"in"},
				OutputPlaces: []string{"out"},
			},
		},
	}
	res := fakeResolver{
		hashtags: map[string][]string{"bash_exec": {"shell", "host", "shell"}},
		caps:     map[string][]string{"bash_exec": {"host.bash"}},
	}
	sig := ComputeFlowSignature(c, res)
	if len(sig.Hashtags) != 2 {
		t.Fatalf("Hashtags = %v, expected dedup to 2", sig.Hashtags)
	}
	if sig.Hashtags[0] != "host" || sig.Hashtags[1] != "shell" {
		t.Errorf("Hashtags = %v, want [host shell] (sorted)", sig.Hashtags)
	}
	if len(sig.RequiredCaps) != 1 || sig.RequiredCaps[0] != "host.bash" {
		t.Errorf("RequiredCaps = %v, want [host.bash]", sig.RequiredCaps)
	}
}

func TestComputeFlowSignature_DigestStable(t *testing.T) {
	c := &CPN{
		Places: map[string]*Place{
			"in":  {ID: "in", Color: "json", Space: SpaceComputation},
			"out": {ID: "out", Color: "artifact", Space: SpaceComputation},
		},
		Transitions: map[string]*Transition{
			"t1": {ID: "t1", Kind: NodeKindTool, ToolName: "x",
				InputPlaces: []string{"in"}, OutputPlaces: []string{"out"}},
		},
	}
	s1 := ComputeFlowSignature(c, nil)
	s2 := ComputeFlowSignature(c, nil)
	if s1.Digest != s2.Digest {
		t.Errorf("digest not stable: %s vs %s", s1.Digest, s2.Digest)
	}
}

func TestFlowSignature_WithTemplate_RecomputesDigest(t *testing.T) {
	base := FlowSignature{InputColors: []string{"json"}, Template: "custom"}
	base.Digest = signatureDigest(base)
	promoted := base.WithTemplate("parallel-fanout")
	if promoted.Digest == base.Digest {
		t.Error("digest unchanged after template change")
	}
	if promoted.Template != "parallel-fanout" {
		t.Errorf("Template = %q", promoted.Template)
	}
}

func TestFlowSignature_Covers(t *testing.T) {
	sig := FlowSignature{
		InputColors:  []string{"json"},
		Hashtags:     []string{"shell", "host"},
		RequiredCaps: []string{"host.bash"},
	}
	if !sig.Covers([]string{"shell"}, []string{"host.bash"}, []string{"json"}) {
		t.Error("should cover exact requirement")
	}
	if !sig.Covers(nil, nil, nil) {
		t.Error("empty requirements should always be covered")
	}
	if sig.Covers([]string{"fs"}, nil, nil) {
		t.Error("must reject missing hashtag")
	}
	if sig.Covers(nil, []string{"net.http"}, nil) {
		t.Error("must reject missing capability")
	}
	if sig.Covers(nil, nil, []string{"artifact"}) {
		t.Error("must reject missing input colour")
	}
}

func TestFlowLibrary_RegisterWithSignature_DigestLookup(t *testing.T) {
	fl := NewFlowLibrary()
	c := &CPN{ID: "c1"}
	sig := FlowSignature{
		InputColors:  []string{"json"},
		Hashtags:     []string{"shell"},
		RequiredCaps: []string{"host.bash"},
		Template:     "parallel-fanout",
	}
	sig.Digest = signatureDigest(sig)
	fl.RegisterWithSignature(c, "h1", sig, "jit")

	entry, ok := fl.FindByDigest(sig.Digest)
	if !ok {
		t.Fatal("FindByDigest missed")
	}
	if entry.Hash != "h1" || entry.Origin != "jit" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Signature.Template != "parallel-fanout" {
		t.Errorf("template lost: %q", entry.Signature.Template)
	}
	if entry.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be stamped automatically")
	}
}

func TestFlowLibrary_FindCovering(t *testing.T) {
	fl := NewFlowLibrary()
	shellSig := FlowSignature{
		InputColors:  []string{"json"},
		Hashtags:     []string{"shell", "host"},
		RequiredCaps: []string{"host.bash"},
	}
	shellSig.Digest = signatureDigest(shellSig)
	fl.RegisterWithSignature(&CPN{ID: "shell"}, "h-shell", shellSig, "jit")

	httpSig := FlowSignature{
		InputColors:  []string{"json"},
		Hashtags:     []string{"http", "net"},
		RequiredCaps: []string{"net.http"},
	}
	httpSig.Digest = signatureDigest(httpSig)
	fl.RegisterWithSignature(&CPN{ID: "http"}, "h-http", httpSig, "jit")

	got := fl.FindCovering([]string{"shell"}, []string{"host.bash"}, []string{"json"})
	if len(got) != 1 || got[0].Hash != "h-shell" {
		t.Fatalf("FindCovering = %v", got)
	}

	got = fl.FindCovering(nil, nil, []string{"json"})
	if len(got) != 2 {
		t.Errorf("expected both flows to cover json-only, got %d", len(got))
	}

	got = fl.FindCovering([]string{"grpc"}, nil, nil)
	if len(got) != 0 {
		t.Errorf("expected no matches for grpc, got %v", got)
	}
}

func TestFlowLibrary_RegisterOverwrite_UpdatesDigestIndex(t *testing.T) {
	fl := NewFlowLibrary()
	oldSig := FlowSignature{Hashtags: []string{"a"}}
	oldSig.Digest = signatureDigest(oldSig)
	fl.RegisterWithSignature(&CPN{ID: "old"}, "h1", oldSig, "jit")

	newSig := FlowSignature{Hashtags: []string{"b"}}
	newSig.Digest = signatureDigest(newSig)
	fl.RegisterWithSignature(&CPN{ID: "new"}, "h1", newSig, "jit")

	if _, ok := fl.FindByDigest(oldSig.Digest); ok {
		t.Error("stale digest must be evicted after overwrite")
	}
	got, ok := fl.FindByDigest(newSig.Digest)
	if !ok || got.CPN.ID != "new" {
		t.Errorf("new digest not present: %+v, ok=%v", got, ok)
	}
}
