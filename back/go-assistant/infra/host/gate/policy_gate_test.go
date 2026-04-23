package gate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// memoryFirstRun is a tiny in-memory FirstRunRepository for tests.
type memoryFirstRun struct {
	mu      sync.Mutex
	entries map[string]*persist.FirstRunLedgerEntry // keyed by hostID|sha
}

func newMemoryFirstRun() *memoryFirstRun {
	return &memoryFirstRun{entries: make(map[string]*persist.FirstRunLedgerEntry)}
}

func key(hostID, sha string) string { return hostID + "|" + sha }

func (m *memoryFirstRun) Record(_ context.Context, hostID, path, sha string) (*persist.FirstRunLedgerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(hostID, sha)
	if e, ok := m.entries[k]; ok {
		return e, nil
	}
	e := &persist.FirstRunLedgerEntry{
		ID: "id-" + sha, HostID: hostID, BinaryPath: path, BinarySHA256: sha,
		FirstSeen: time.Now(),
	}
	m.entries[k] = e
	return e, nil
}

func (m *memoryFirstRun) GetBySHA(_ context.Context, hostID, sha string) (*persist.FirstRunLedgerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries[key(hostID, sha)], nil
}

func (m *memoryFirstRun) Approve(_ context.Context, hostID, sha, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key(hostID, sha)]
	if !ok {
		return errors.New("not found")
	}
	now := time.Now()
	e.FirstApprovedAt = &now
	e.FirstApprovedBy = userID
	e.Revoked = false
	return nil
}

func (m *memoryFirstRun) Revoke(_ context.Context, hostID, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key(hostID, sha)]
	if !ok {
		return errors.New("not found")
	}
	e.Revoked = true
	return nil
}

func (m *memoryFirstRun) List(_ context.Context, _ int) ([]*persist.FirstRunLedgerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*persist.FirstRunLedgerEntry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e)
	}
	return out, nil
}

// memoryDecisions is an in-memory GateDecisionRepository.
type memoryDecisions struct {
	mu      sync.Mutex
	records []*persist.GateDecisionRecord
}

func (m *memoryDecisions) Insert(_ context.Context, rec *persist.GateDecisionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

func (m *memoryDecisions) List(_ context.Context, _ int) ([]*persist.GateDecisionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*persist.GateDecisionRecord, len(m.records))
	copy(out, m.records)
	return out, nil
}

// fakeSandbox is a Capability-stub whose answer is dictated per-runtime.
type fakeSandbox struct {
	avail map[string]bool
}

func (f fakeSandbox) Available(runtime string) bool { return f.avail[runtime] }

// newGateForTest constructs a PolicyHostGate with sandbox present.
func newGateForTest(t *testing.T, policy *HostPolicy) (*PolicyHostGate, *memoryDecisions, *memoryFirstRun) {
	t.Helper()
	holder := NewHolder(policy)
	fr := newMemoryFirstRun()
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, fr, dec, NewBudgetTracker(), sb, nil)
	g.HostID = "host-test"
	return g, dec, fr
}

// TestForbiddenIsDeny covers AC-001.
func TestForbiddenIsDeny(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, _ := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "rm -rf /", Sandbox: cpn.SandboxNone}
	err := g.Check(context.Background(), op)
	if err == nil {
		t.Fatal("expected deny for rm -rf /")
	}
	var he *cpn.HostError
	if !errors.As(err, &he) || he.Code != cpn.HostErrCodeGateDenied {
		t.Fatalf("expected gate_denied, got %v", err)
	}
}

// TestDangerousRequiresHITL covers AC-007.
func TestDangerousRequiresHITL(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, _ := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "chmod +x /tmp/x", Sandbox: cpn.SandboxNone}
	err := g.Check(context.Background(), op)
	dec, ok := IsRequiresHITL(err)
	if !ok {
		t.Fatalf("expected require-hitl, got %v", err)
	}
	if dec.RiskBand != RiskDangerous {
		t.Errorf("expected dangerous band, got %s", dec.RiskBand)
	}
}

// TestBudgetExhausted covers AC-003 (writes over budget -> require-hitl).
func TestBudgetExhausted(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, _ := newGateForTest(t, p)
	g.BudgetEstimator = func(op cpn.GateOp) BudgetEstimate {
		// AC-003: 512 MiB write estimate vs 256 MiB default budget.
		return BudgetEstimate{BytesWritten: 512 << 20}
	}
	op := cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxNone}
	err := g.Check(context.Background(), op)
	dec, ok := IsRequiresHITL(err)
	if !ok {
		t.Fatalf("expected require-hitl from budget, got %v", err)
	}
	if dec.Reason != "bytes_budget_exceeded" {
		t.Errorf("expected bytes_budget_exceeded, got %q", dec.Reason)
	}
}

// TestSandboxUnavailable covers AC-004.
func TestSandboxUnavailable(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{}} // neither bwrap nor firejail
	g := NewPolicyHostGate(holder, newMemoryFirstRun(), dec, NewBudgetTracker(), sb, nil)
	op := cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxReadonly}
	err := g.Check(context.Background(), op)
	var he *cpn.HostError
	if !errors.As(err, &he) || he.Code != cpn.HostErrCodeGateDenied {
		t.Fatalf("expected gate_denied on missing sandbox, got %v", err)
	}
}

// TestSafeAllowed: a safe-band command with no first-run ledger (so the
// first-run gate is skipped by passing a nil FirstRun) is allowed.
func TestSafeAllowed(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, dec, NewBudgetTracker(), sb, nil)
	op := cpn.GateOp{Kind: "exec", Command: "ls -la /tmp", Sandbox: cpn.SandboxNone}
	if err := g.Check(context.Background(), op); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

// TestAuditLogged verifies AC-005 — every decision appears in the audit
// log. Because auditAsync is fire-and-forget we poll briefly.
func TestAuditLogged(t *testing.T) {
	p := defaultTestPolicy(t)
	g, dec, _ := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "rm -rf /", Sandbox: cpn.SandboxNone}
	_ = g.Check(context.Background(), op)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dec.mu.Lock()
		n := len(dec.records)
		dec.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("audit row was not written within 2s")
}

// TestRememberedCautionSafe: after AppendLearnedSafePattern and a
// first-run approval, a caution pattern is allowed without HITL. REQ-002
// keeps first-run BEFORE allow patterns so we test both overrides.
func TestRememberedCautionSafe(t *testing.T) {
	p := defaultTestPolicy(t)
	// Skip first-run gate entirely for this logic test — we isolate the
	// learned-safe-pattern override from the ledger semantics (covered by
	// TestUnknownDefaultsToHITL and sibling first-run tests).
	holder := NewHolder(p)
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, dec, NewBudgetTracker(), sb, nil)

	// Baseline — gcc is caution → require-hitl.
	op := cpn.GateOp{Kind: "exec", Command: "gcc hello.c", Sandbox: cpn.SandboxNone}
	err := g.Check(context.Background(), op)
	if _, ok := IsRequiresHITL(err); !ok {
		t.Fatalf("expected require-hitl for gcc, got %v", err)
	}

	// Remember.
	if err := g.Policies.AppendLearnedSafePattern(`^gcc\s+hello\.c($|\s)`); err != nil {
		t.Fatalf("remember: %v", err)
	}

	// Now it should allow.
	if err := g.Check(context.Background(), op); err != nil {
		t.Fatalf("expected allow after remember, got %v", err)
	}
}

// TestRememberApproval_UnwrapsShellWrapper guards the bug where
// approve-and-remember on a shell-wrapped command stored a pattern against
// the wrapper string (e.g. `^bash -lc "ps aux"($|\s)`) while Classify
// unwraps the wrapper and matches against the inner (`ps aux`). The
// stored pattern never matched the unwrapped form, so every subsequent
// invocation fell through to RiskUnknown → HITL again, even though the
// UI had already shown "Aprobado y recordado".
func TestRememberApproval_UnwrapsShellWrapper(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	dec := &memoryDecisions{}
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, dec, NewBudgetTracker(), sb, nil)

	op := cpn.GateOp{Kind: "exec", Command: `bash -lc "ps aux"`, Sandbox: cpn.SandboxNone}

	// Remember the wrapper form — simulates the user clicking
	// "Aprobado y recordado" on a bash_exec invocation that wrapped the
	// inner command in `bash -lc "..."`.
	if err := g.rememberApproval(context.Background(), op); err != nil {
		t.Fatalf("rememberApproval: %v", err)
	}

	// The exact same invocation must now hit the safe band, not fall
	// through to default-deny HITL.
	if err := g.Check(context.Background(), op); err != nil {
		t.Fatalf("expected allow after remember, got %v", err)
	}

	// And the bare inner command must also be allowed — same intent,
	// different call shape.
	innerOp := cpn.GateOp{Kind: "exec", Command: "ps aux", Sandbox: cpn.SandboxNone}
	if err := g.Check(context.Background(), innerOp); err != nil {
		t.Fatalf("expected allow for inner command after remembering wrapped form, got %v", err)
	}
}

// TestUnknownDefaultsToHITL enforces GUD-001 (when in doubt, deny via HITL).
func TestUnknownDefaultsToHITL(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, _ := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "frobnicate --widget", Sandbox: cpn.SandboxNone}
	err := g.Check(context.Background(), op)
	if _, ok := IsRequiresHITL(err); !ok {
		t.Fatalf("unknown command must escalate to HITL, got %v", err)
	}
}

// BenchmarkGateCheck verifies the < 2ms/op spec target (§6).
func BenchmarkGateCheck(b *testing.B) {
	p := defaultTestPolicy(&testing.T{})
	holder := NewHolder(p)
	sb := fakeSandbox{avail: map[string]bool{"bwrap": true}}
	g := NewPolicyHostGate(holder, nil, nil, NewBudgetTracker(), sb, nil)
	op := cpn.GateOp{Kind: "exec", Command: "ls -la /tmp", Sandbox: cpn.SandboxNone}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Check(ctx, op)
	}
}

// TestBudgetRecordActual: counters accumulate via RecordActual.
func TestBudgetRecordActual(t *testing.T) {
	bt := NewBudgetTracker()
	bt.RecordActual("s1", BudgetEstimate{CPUSeconds: 5, BytesWritten: 1024})
	bt.RecordActual("s1", BudgetEstimate{CPUSeconds: 3})
	cur := bt.Current("s1")
	if cur.CPUSeconds != 8 || cur.BytesWritten != 1024 {
		t.Errorf("Current=%+v, want CPU=8 Bytes=1024", cur)
	}
}
