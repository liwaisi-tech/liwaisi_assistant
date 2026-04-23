package cpn

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── LedgerSink unit tests ───────────────────────────────────────────────────

func TestWithLedgerSink_RoundTrip(t *testing.T) {
	c := &CPN{}
	ctx := WithLedgerSink(context.Background(), c.LedgerSink())
	got, ok := LedgerSinkFromContext(ctx)
	if !ok {
		t.Fatalf("expected sink to be present")
	}
	msg, err := LedgerSuccess(LedgerVerbWrite, "/tmp/x", "10B")
	if err != nil {
		t.Fatalf("LedgerSuccess: %v", err)
	}
	got.Append(msg)

	if len(c.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(c.History))
	}
}

func TestLedgerSinkFromContext_AbsentReturnsFalse(t *testing.T) {
	if _, ok := LedgerSinkFromContext(context.Background()); ok {
		t.Errorf("expected ok=false on bare context")
	}
	if _, ok := LedgerSinkFromContext(nil); ok { //nolint:staticcheck // intentionally testing nil context
		t.Errorf("expected ok=false on nil context")
	}
}

func TestWithLedgerSink_NilSinkPassThrough(t *testing.T) {
	ctx := WithLedgerSink(context.Background(), nil)
	if _, ok := LedgerSinkFromContext(ctx); ok {
		t.Errorf("nil sink should not install anything")
	}
}

func TestCPN_LedgerSink_NilCPN(t *testing.T) {
	var c *CPN
	if c.LedgerSink() != nil {
		t.Errorf("nil CPN should return nil sink")
	}
}

func TestCPNLedgerSink_AppendNilSafe(t *testing.T) {
	c := &CPN{}
	c.LedgerSink().Append(nil) // must not panic
	if len(c.History) != 0 {
		t.Errorf("nil message should not be appended")
	}
}

func TestCPNLedgerSink_ConcurrentAppend(t *testing.T) {
	// Race-detector check: 100 goroutines append concurrently. The
	// CPN.mu lock inside cpnLedgerSink.Append is what makes this
	// safe; without it, -race trips.
	c := &CPN{}
	sink := c.LedgerSink()
	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			msg, _ := LedgerSuccess(LedgerVerbWrite, "/tmp/x", "1B")
			sink.Append(msg)
		}()
	}
	wg.Wait()
	if len(c.History) != N {
		t.Errorf("expected %d history entries, got %d", N, len(c.History))
	}
}

// ── Ephemeral filtering ─────────────────────────────────────────────────────

func TestBuildContextWithPolicy_DropsEphemeralFromT3(t *testing.T) {
	now := time.Now()
	history := []*Message{
		{ID: "u1", Role: RoleUser, Content: "first", Timestamp: now.Add(-3 * time.Minute)},
		{ID: "a1", Role: RoleAssistant, Content: "ok", Timestamp: now.Add(-2 * time.Minute)},
		{ID: "u2", Role: RoleUser, Content: "verbose ls output", CPNRole: CPNRoleEphemeral, Timestamp: now.Add(-1 * time.Minute)},
		{ID: "a2", Role: RoleAssistant, Content: "summary", Timestamp: now},
	}
	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "x"
	cw := BuildContextWithPolicy(RoleAssistantTransition, history, policy)

	for _, m := range cw.Messages {
		if strings.Contains(m.Content, "verbose ls output") {
			t.Errorf("ephemeral message leaked into T3: %q", m.Content)
		}
	}
	// The other three should be present.
	want := []string{"first", "ok", "summary"}
	for _, w := range want {
		var found bool
		for _, m := range cw.Messages {
			if strings.Contains(m.Content, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("non-ephemeral message %q missing from T3", w)
		}
	}
}

func TestIsEphemeral(t *testing.T) {
	if !IsEphemeral(&Message{CPNRole: CPNRoleEphemeral}) {
		t.Errorf("ephemeral-tagged message should be recognised")
	}
	if IsEphemeral(&Message{CPNRole: "worker"}) {
		t.Errorf("ordinary message should not be recognised")
	}
	if IsEphemeral(nil) {
		t.Errorf("nil should be safe and false")
	}
}

// ── End-to-end: write_file via tool exec → ledger → preamble ────────────────

// TestEndToEnd_WriteThenPreambleSeesIt simulates the failure mode that
// motivated this entire spec: an agent writes a file early in a
// session, dozens of turns later the LLM forgets the file exists, and
// re-creates it. With the spec implemented, the workspace preamble on
// the LATER turn must surface the path.
//
// The test wires:
//   - a fake HostAdapter that records writes
//   - a CPN with its real LedgerSink
//   - the file-write executor reached via context-installed sink
//   - 80 noise messages padding the sliding window past its limit
//   - a final BuildContextWithPolicy that must yield a preamble line
//
// No real LLM is involved — the test exercises the memory-and-tool
// machinery directly. A separate integration test in fire_llm_test.go
// can layer in a mock LLMClient if/when desired.
func TestEndToEnd_WriteThenPreambleSeesIt(t *testing.T) {
	c := &CPN{}

	// Step 1: simulate a state-changing tool call early in the session.
	// We invoke the ledger sink directly the way the file_tools
	// executor would — via the context-installed sink.
	ctx := WithLedgerSink(context.Background(), c.LedgerSink())
	EmitLedgerSuccess(ctx, LedgerVerbWrite, "~/workspace/pg_explorer/main.go", "1.2KB")

	if len(c.History) != 1 || !IsLedgerEntry(c.History[0]) {
		t.Fatalf("ledger emission did not append: history=%+v", c.History)
	}
	// Backdate so the workspace preamble shows a stable timestamp.
	c.History[0].Timestamp = time.Now().Add(-30 * time.Minute)

	// Step 2: pad the session with noise that would normally bury the
	// write under the sliding window.
	for i := 0; i < 80; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		c.History = append(c.History, &Message{
			Role: role, Content: "filler",
			Timestamp: time.Now().Add(time.Duration(-29+i) * time.Minute),
		})
	}

	// Step 3: assemble context for the next assistant turn.
	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "ASSISTANT_BODY"
	cw := BuildContextWithPolicy(RoleAssistantTransition, c.History, policy)

	if !strings.Contains(cw.SystemPrompt, "[workspace state @") {
		t.Errorf("preamble header missing:\n%s", cw.SystemPrompt)
	}
	if !strings.Contains(cw.SystemPrompt, "pg_explorer/main.go") {
		t.Errorf("preamble lost the path written 80 messages ago:\n%s", cw.SystemPrompt)
	}
	if !strings.Contains(cw.SystemPrompt, "ASSISTANT_BODY") {
		t.Errorf("base prompt was discarded; preamble must prepend, not replace")
	}
	// The ledger MUST also be visible in T2 for the assistant role.
	var sawLedgerInT2 bool
	for _, m := range cw.Messages {
		if strings.HasPrefix(m.Content, "[ok] write ") {
			sawLedgerInT2 = true
		}
	}
	if !sawLedgerInT2 {
		t.Errorf("ledger entry was not surfaced in T2 messages")
	}
}
