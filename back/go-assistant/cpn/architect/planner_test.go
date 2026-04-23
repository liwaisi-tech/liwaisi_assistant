package architect

import (
	"context"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis/jit"
)

// fakeLibrary is a tiny LibraryPort for deterministic tests. We don't touch
// cpn.FlowLibrary directly here — Plan's contract is the LibraryPort shape.
type fakeLibrary struct {
	covering []*cpn.FlowLibraryEntry
	byHash   map[string]*cpn.FlowLibraryEntry
}

func (f *fakeLibrary) FindCovering(_, _, _ []string) []*cpn.FlowLibraryEntry {
	return f.covering
}

func (f *fakeLibrary) GetEntry(hash string) (*cpn.FlowLibraryEntry, bool) {
	e, ok := f.byHash[hash]
	return e, ok
}

type fakeRetriever struct {
	out cpn.ToolMatchSet
	err error
}

func (r *fakeRetriever) Match(_ ArchitectRequest) (cpn.ToolMatchSet, error) {
	return r.out, r.err
}

func TestPlanner_Reuse_OnLibraryHit(t *testing.T) {
	entry := &cpn.FlowLibraryEntry{
		Hash: "h-shell",
		Signature: cpn.FlowSignature{
			Hashtags:     []string{"shell", "host"},
			RequiredCaps: []string{"host.bash"},
		},
	}
	p := &Planner{
		Library:   &fakeLibrary{covering: []*cpn.FlowLibraryEntry{entry}},
		Retriever: &fakeRetriever{},
	}
	draft, err := p.Plan(context.Background(), ArchitectRequest{
		Hashtags:     []string{"shell"},
		RequiredCaps: []string{"host.bash"},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if draft.Strategy != StrategyReuse {
		t.Fatalf("Strategy = %q, want reuse", draft.Strategy)
	}
	if draft.BaseFlowID != "h-shell" {
		t.Errorf("BaseFlowID = %q", draft.BaseFlowID)
	}
	if draft.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", draft.Confidence)
	}
}

func TestPlanner_Reject_WhenNoLibraryAndNoRetrieverMatches(t *testing.T) {
	p := &Planner{
		Library:   &fakeLibrary{},
		Retriever: &fakeRetriever{},
	}
	draft, err := p.Plan(context.Background(), ArchitectRequest{
		Hashtags: []string{"shell"},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if draft.Strategy != StrategyReject {
		t.Errorf("Strategy = %q, want reject", draft.Strategy)
	}
}

func TestPlanner_Compose_WhenRetrieverMatchesAndLibraryMisses(t *testing.T) {
	safe := synthesis.NewSafeRegistry()
	synthesis.RegisterDefaults(safe)
	jit.Register(safe)
	safe.Seal()

	match := cpn.ToolMatchSet{
		Matches: []cpn.ToolMatch{
			{QualifiedName: "pdf/pdf-to-text@0.1.0", Score: 1, Hashtags: []string{"pdf"}},
			{QualifiedName: "pdf/pdf-info@0.1.0", Score: 1, Hashtags: []string{"pdf"}},
		},
		Digest: "d",
	}

	p := &Planner{
		Library:      &fakeLibrary{},
		Retriever:    &fakeRetriever{out: match},
		SafeRegistry: safe,
	}
	draft, err := p.Plan(context.Background(), ArchitectRequest{
		Hashtags: []string{"pdf"},
		Intent:   cpn.Intent{NL: "extract and count pages"},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if draft.Strategy != StrategyCompose {
		t.Fatalf("Strategy = %q, want compose; reason=%s", draft.Strategy, draft.Reason)
	}
	if len(draft.TopologyBlob) == 0 {
		t.Error("TopologyBlob must be populated on Compose")
	}
}

func TestPlanner_RetrieverError_Propagates(t *testing.T) {
	p := &Planner{
		Library:   &fakeLibrary{},
		Retriever: &fakeRetriever{err: context.Canceled},
	}
	if _, err := p.Plan(context.Background(), ArchitectRequest{Hashtags: []string{"x"}}); err == nil {
		t.Error("expected retriever error to propagate")
	}
}

func TestPlanner_ErrorsOnMissingDeps(t *testing.T) {
	p := &Planner{Retriever: &fakeRetriever{}}
	if _, err := p.Plan(context.Background(), ArchitectRequest{}); err == nil {
		t.Error("expected ErrNoLibrary")
	}
	p = &Planner{Library: &fakeLibrary{}}
	if _, err := p.Plan(context.Background(), ArchitectRequest{}); err == nil {
		t.Error("expected ErrNoRetriever")
	}
}

func TestPlanner_MinConfidenceForReuse_FallsThroughToCompose(t *testing.T) {
	// Library has an entry but the request requires two hashtags and the
	// entry only covers one. With MinConfidenceForReuse=0.75 we should
	// skip reuse and go to retriever.
	entry := &cpn.FlowLibraryEntry{
		Hash:      "h-partial",
		Signature: cpn.FlowSignature{Hashtags: []string{"shell"}},
	}
	safe := synthesis.NewSafeRegistry()
	synthesis.RegisterDefaults(safe)
	jit.Register(safe)
	safe.Seal()

	match := cpn.ToolMatchSet{
		Matches: []cpn.ToolMatch{
			{QualifiedName: "pdf/a@0.1.0"}, {QualifiedName: "pdf/b@0.1.0"},
		},
		Digest: "d",
	}
	p := &Planner{
		Library:               &fakeLibrary{covering: []*cpn.FlowLibraryEntry{entry}},
		Retriever:             &fakeRetriever{out: match},
		SafeRegistry:          safe,
		MinConfidenceForReuse: 0.75,
	}
	// FindCovering returned the entry (it covers "shell"), but
	// signatureConfidence for two hashtags with one matched = 0.5 < 0.75
	// — so we expect Compose. However the fake FindCovering doesn't
	// filter; only the planner's confidence check does. That's the point
	// of the test.
	draft, err := p.Plan(context.Background(), ArchitectRequest{
		Hashtags: []string{"shell", "host"},
		Intent:   cpn.Intent{NL: "run stuff"},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if draft.Strategy != StrategyCompose {
		t.Errorf("expected Compose when confidence < threshold, got %q", draft.Strategy)
	}
}
