package cpn

import (
	"context"
	"testing"
	"time"
)

// TestCPN_BashE2E_AC001 exercises a minimal CPN: input → NodeKindBash → output.
// The mock HostAdapter returns a canned ExecResult so the test stays
// deterministic and does not touch the real OS. This is the end-to-end
// proof for AC-001 (all the way through Run/Validate).
func TestCPN_BashE2E_AC001(t *testing.T) {
	pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
	pOut := NewPlace("p-out", ColorShellResult, SpaceComputation)
	_ = pIn.Deposit(&Token{
		Color:   ColorShellCmd,
		Space:   SpaceComputation,
		Payload: "echo hello",
	})

	tr := &Transition{
		ID:           "t-bash",
		Kind:         NodeKindBash,
		InputPlaces:  []string{"p-in"},
		OutputPlaces: []string{"p-out"},
		BashConfig: &BashConfig{
			Command: "echo",
			Args:    []string{"hello"},
			Timeout: 500 * time.Millisecond,
		},
	}
	places := map[string]*Place{"p-in": pIn, "p-out": pOut}
	transitions := map[string]*Transition{tr.ID: tr}
	c := NewCPN("cpn-e2e", "root", 0, ModeMAS, "sess-e2e", places, transitions)
	c.HostRuntime = &HostRuntime{
		Adapter: &mockHostAdapter{execResult: ExecResult{ExitCode: 0, Stdout: []byte("hello\n")}},
		Gate:    denyGate{allow: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("state = %s, want completed", c.State)
	}
	if pOut.Len() != 1 {
		t.Fatalf("p-out.Len = %d, want 1", pOut.Len())
	}
	toks, _ := pOut.Peek()
	payload, ok := toks[0].Payload.(ShellResultPayload)
	if !ok {
		t.Fatalf("payload type = %T", toks[0].Payload)
	}
	if payload.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", payload.ExitCode)
	}
	if payload.Stdout != "hello\n" {
		t.Fatalf("stdout = %q, want hello\\n", payload.Stdout)
	}
}

// TestCPN_BashE2E_ErrorPlaceRouting_AC002 builds a topology where the bash
// transition times out and the error token lands on the ErrorPlace. The
// ErrorPlace is also a terminal place, so the CPN completes.
func TestCPN_BashE2E_ErrorPlaceRouting_AC002(t *testing.T) {
	pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
	pErr := NewPlace("p-err", ColorError, SpaceComputation)
	_ = pIn.Deposit(&Token{Color: ColorShellCmd, Space: SpaceComputation, Payload: "go"})

	tr := &Transition{
		ID:           "t-bash",
		Kind:         NodeKindBash,
		InputPlaces:  []string{"p-in"},
		OutputPlaces: []string{"p-err"}, // Route deposits to an error-colored terminal.
		ErrorPlace:   "p-err",
		BashConfig: &BashConfig{
			Command: "sleep",
			Timeout: 10 * time.Millisecond,
		},
	}
	places := map[string]*Place{"p-in": pIn, "p-err": pErr}
	transitions := map[string]*Transition{tr.ID: tr}
	c := NewCPN("cpn-err", "err", 0, ModeMAS, "sess", places, transitions)
	c.HostRuntime = &HostRuntime{
		Adapter: &mockHostAdapter{
			execErr: NewHostError(HostErrCodeTimeout, "timed out", nil),
		},
		Gate: denyGate{allow: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if pErr.Len() != 1 {
		t.Fatalf("expected 1 error token on p-err, got %d", pErr.Len())
	}
}
