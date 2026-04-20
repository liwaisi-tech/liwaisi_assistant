package gate

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestSessionHITLHandler_ApproveAndRemember_PersistsPattern proves
// AC-BE-002 + AC-BE-003: a single approve-and-remember resolution causes
// the next identical gate.Check to return nil without emitting another
// ApprovalCard, because rememberApproval is invoked with the exact
// literal-prefix regex.
func TestSessionHITLHandler_ApproveAndRemember_PersistsPattern(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, dec, NewBudgetTracker(), sb, nil)
	g.HostID = "host-test"

	op := cpn.GateOp{Kind: "exec", Command: "gcc hello.c"}

	// First check: caution policy → require HITL.
	first := g.Check(context.Background(), op)
	if _, ok := IsRequiresHITL(first); !ok {
		t.Fatalf("first Check = %v, want ErrRequiresHITL", first)
	}

	// Build a CPN + transition with a preloaded inject channel.
	inject := make(chan cpn.Token, 1)
	tr := &cpn.Transition{
		ID:         "t-bash",
		Kind:       cpn.NodeKindBash,
		HITLConfig: &cpn.HITLConfig{Channel: inject},
	}
	c := cpn.NewCPN("cpn-test", "test", 0, cpn.ModeMAS, "sess-1",
		map[string]*cpn.Place{}, map[string]*cpn.Transition{tr.ID: tr})
	c.EventEmitter = make(chan cpn.Event, 16)

	// Preload the inject channel with an approve-and-remember response.
	inject <- cpn.Token{
		Color: cpn.ColorHuman,
		Payload: cpn.HITLResponse{
			Action:  cpn.HITLApprove,
			Content: `{"action":"approve-and-remember"}`,
		},
	}

	handler := &SessionHITLHandler{Gate: g}
	if err := handler.HandleHITL(context.Background(), tr, c, op, first); err != nil {
		t.Fatalf("HandleHITL: %v", err)
	}

	// Pattern must have been persisted to the in-memory safe list.
	wantPattern := "^" + regexpQuote(CommandKey(op.Command)) + "($|\\s)"
	if !slices.Contains(holder.policy.SafePat, wantPattern) {
		t.Fatalf("pattern %q not persisted; SafePat=%v", wantPattern, holder.policy.SafePat)
	}

	// Second Check with the same op now returns nil — AC-BE-003.
	if err := g.Check(context.Background(), op); err != nil {
		t.Fatalf("second Check = %v, want nil (remembered)", err)
	}
}

// TestSessionHITLHandler_Deny_ReturnsGateDenied_NoPersistence covers
// REQ-004/SEC-003 for the bash path.
func TestSessionHITLHandler_Deny_ReturnsGateDenied_NoPersistence(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	initialSafe := append([]string{}, holder.policy.SafePat...)
	initialForbidden := append([]string{}, holder.policy.ForbiddenPat...)

	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, dec, NewBudgetTracker(), sb, nil)
	g.HostID = "host-test"
	op := cpn.GateOp{Kind: "exec", Command: "gcc hello.c"}

	first := g.Check(context.Background(), op)
	if _, ok := IsRequiresHITL(first); !ok {
		t.Fatalf("first Check = %v", first)
	}

	inject := make(chan cpn.Token, 1)
	tr := &cpn.Transition{
		ID:         "t-bash",
		Kind:       cpn.NodeKindBash,
		HITLConfig: &cpn.HITLConfig{Channel: inject},
	}
	c := cpn.NewCPN("cpn-test", "test", 0, cpn.ModeMAS, "sess-1",
		map[string]*cpn.Place{}, map[string]*cpn.Transition{tr.ID: tr})
	c.EventEmitter = make(chan cpn.Event, 16)
	inject <- cpn.Token{
		Color: cpn.ColorHuman,
		Payload: cpn.HITLResponse{
			Action:  cpn.HITLReject,
			Content: `{"action":"deny"}`,
		},
	}

	handler := &SessionHITLHandler{Gate: g}
	err := handler.HandleHITL(context.Background(), tr, c, op, first)
	if err == nil {
		t.Fatalf("expected deny error")
	}
	var he *cpn.HostError
	if !errors.As(err, &he) || he.Code != cpn.HostErrCodeGateDenied {
		t.Fatalf("err = %v, want HostError{gate_denied}", err)
	}

	// Learned overlay untouched (SEC-003).
	if !sliceEqual(initialSafe, holder.policy.SafePat) {
		t.Fatalf("SafePat changed: before=%v after=%v", initialSafe, holder.policy.SafePat)
	}
	if !sliceEqual(initialForbidden, holder.policy.ForbiddenPat) {
		t.Fatalf("ForbiddenPat changed on deny (expected: only deny-and-blacklist mutates it): %v", holder.policy.ForbiddenPat)
	}
}

// TestSessionHITLHandler_NonHITLError_Propagates ensures the handler is a
// pure pass-through for errors that are not the require-HITL sentinel.
func TestSessionHITLHandler_NonHITLError_Propagates(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	h := &SessionHITLHandler{Gate: g}
	raw := errors.New("boom")
	err := h.HandleHITL(context.Background(), nil, nil, cpn.GateOp{}, raw)
	if !errors.Is(err, raw) {
		t.Fatalf("err = %v, want %v", err, raw)
	}
}

// TestCommandKey_SlashBinSh_Canonical covers AC-BE-006: repeated
// invocations of the canonical tool shape yield byte-equal CommandKeys.
// REQ-008 Option A hardening.
func TestCommandKey_SlashBinSh_Canonical(t *testing.T) {
	a := CommandKey("/bin/sh -c \"uname -a && uptime\"")
	b := CommandKey("/bin/sh -c \"uname -a && uptime\"")
	if a != b {
		t.Fatalf("non-deterministic CommandKey: %q vs %q", a, b)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
