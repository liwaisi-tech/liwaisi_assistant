package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

// stubTopologyRouter counts approvals + optionally rejects.
type stubTopologyRouter struct {
	approvedCount atomic.Int32
	reject        bool
}

func (s *stubTopologyRouter) ApproveTopology(_ context.Context, _ AuthoredTopologySummary) (bool, error) {
	if s.reject {
		return false, nil
	}
	s.approvedCount.Add(1)
	return true, nil
}

// buildEchoChild builds the simplest possible materialised CPN: a tool
// transition that copies its input token to the output place. Used by
// the fake materialiser.
func buildEchoChild() *CPN {
	in := NewPlace("p-in", ColorString, SpaceComputation)
	out := NewPlace("p-out", ColorArtifact, SpaceComputation)
	tr := NewTransition("t-echo", NodeKindTool, []string{"p-in"}, []string{"p-out"})
	tr.Executor = func(_ context.Context, t Token) (Token, error) {
		out := t
		out.Color = ColorArtifact
		return out, nil
	}
	return NewCPN("child", "echo", 0, ModeMAS, "",
		map[string]*Place{"p-in": in, "p-out": out},
		map[string]*Transition{"t-echo": tr},
	)
}

func TestFireInstantiate_HITLApprove_RunsChildAndDepositsArtefact(t *testing.T) {
	resetSynthesisHooks(t)
	SetLintTopology(func(_ json.RawMessage, _ SafeRegistryPort, _ SizeCap) LintResultPort {
		return passThroughLint{}
	})
	SetMaterialiseTopology(func(_ context.Context, _ json.RawMessage, _ SafeRegistryPort) (*CPN, error) {
		return buildEchoChild(), nil
	})
	SetTopologyDigest(func(_ json.RawMessage) TopologyDigest {
		return TopologyDigest{Name: "echo", SizePlaces: 2, SizeTransitions: 1}
	})

	repo := newStubFlowRepo()
	flowID, _, _ := repo.SaveAuthored(context.Background(), json.RawMessage(`{"id":"echo"}`), "echo", 2, 1, nil, AuthoredFlowProvenance{})

	in := NewPlace("p-flowref-in", ColorFlowRef, SpaceComputation)
	out := NewPlace("p-artefact", ColorArtifact, SpaceComputation)
	tr := NewTransition("t-instantiate", NodeKindInstantiate, []string{"p-flowref-in"}, []string{"p-artefact"})
	tr.InstantiateConfig = &InstantiateConfig{}

	c := NewCPN("parent", "parent", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-flowref-in": in, "p-artefact": out},
		map[string]*Transition{"t-instantiate": tr},
	)
	c.FlowRepository = repo
	c.SafeRegistry = stubSafeRegistry{}
	router := &stubTopologyRouter{}
	c.TopologyRouter = router

	tok := Token{Color: ColorFlowRef, Payload: FlowRef{FlowID: flowID, Summary: "echo"}, Space: SpaceComputation}

	_, _, err := fireInstantiate(context.Background(), tr, c, []Token{tok})
	if err != nil {
		t.Fatalf("fireInstantiate: %v", err)
	}
	if router.approvedCount.Load() != 1 {
		t.Errorf("want 1 approval, got %d", router.approvedCount.Load())
	}
	if !c.HasApprovedFlow(flowID) {
		t.Errorf("flow not marked approved on session")
	}
}

func TestFireInstantiate_RejectedFlow_FailsFast(t *testing.T) {
	resetSynthesisHooks(t)
	SetLintTopology(func(_ json.RawMessage, _ SafeRegistryPort, _ SizeCap) LintResultPort {
		return passThroughLint{}
	})

	repo := newStubFlowRepo()
	flowID, _, _ := repo.SaveAuthored(context.Background(), json.RawMessage(`{}`), "", 0, 0, nil, AuthoredFlowProvenance{})
	_ = repo.Reject(context.Background(), flowID, "admin blocked")

	in := NewPlace("p-flowref-in", ColorFlowRef, SpaceComputation)
	out := NewPlace("p-out", ColorArtifact, SpaceComputation)
	tr := NewTransition("t-inst", NodeKindInstantiate, []string{"p-flowref-in"}, []string{"p-out"})
	c := NewCPN("parent", "parent", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-flowref-in": in, "p-out": out},
		map[string]*Transition{"t-inst": tr},
	)
	c.FlowRepository = repo
	c.SafeRegistry = stubSafeRegistry{}

	tok := Token{Color: ColorFlowRef, Payload: FlowRef{FlowID: flowID}}
	_, _, err := fireInstantiate(context.Background(), tr, c, []Token{tok})
	if !errors.Is(err, ErrTopologyRejected) {
		t.Fatalf("expected ErrTopologyRejected, got %v", err)
	}
}

func TestFireInstantiate_ApprovedFlowSkipsHITL(t *testing.T) {
	resetSynthesisHooks(t)
	SetLintTopology(func(_ json.RawMessage, _ SafeRegistryPort, _ SizeCap) LintResultPort {
		return passThroughLint{}
	})
	SetMaterialiseTopology(func(_ context.Context, _ json.RawMessage, _ SafeRegistryPort) (*CPN, error) {
		return buildEchoChild(), nil
	})
	SetTopologyDigest(func(_ json.RawMessage) TopologyDigest { return TopologyDigest{} })

	repo := newStubFlowRepo()
	flowID, _, _ := repo.SaveAuthored(context.Background(), json.RawMessage(`{}`), "", 0, 0, nil, AuthoredFlowProvenance{})

	in := NewPlace("p-in", ColorFlowRef, SpaceComputation)
	out := NewPlace("p-out", ColorArtifact, SpaceComputation)
	tr := NewTransition("t-inst", NodeKindInstantiate, []string{"p-in"}, []string{"p-out"})
	c := NewCPN("parent", "parent", 0, ModeMAS, "sess-1",
		map[string]*Place{"p-in": in, "p-out": out},
		map[string]*Transition{"t-inst": tr},
	)
	c.FlowRepository = repo
	c.SafeRegistry = stubSafeRegistry{}
	router := &stubTopologyRouter{}
	c.TopologyRouter = router

	// Pre-approve so HITL is skipped.
	c.MarkFlowApproved(flowID)

	tok := Token{Color: ColorFlowRef, Payload: FlowRef{FlowID: flowID}}
	_, _, err := fireInstantiate(context.Background(), tr, c, []Token{tok})
	if err != nil {
		t.Fatalf("fireInstantiate: %v", err)
	}
	if router.approvedCount.Load() != 0 {
		t.Errorf("expected 0 approvals on pre-approved flow, got %d", router.approvedCount.Load())
	}
}
