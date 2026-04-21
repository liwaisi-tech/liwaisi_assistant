package gate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ──────────────────────────────────────────────────────────────────────────
// matcher.go helpers: JoinArgs, BasePath, unquote edge cases
// ──────────────────────────────────────────────────────────────────────────

func TestJoinArgs(t *testing.T) {
	t.Run("no-args", func(t *testing.T) {
		if got := JoinArgs("ls", nil); got != "ls" {
			t.Fatalf("JoinArgs(ls, nil) = %q, want %q", got, "ls")
		}
	})
	t.Run("several-args", func(t *testing.T) {
		if got := JoinArgs("gcc", []string{"-O2", "main.c"}); got != "gcc -O2 main.c" {
			t.Fatalf("JoinArgs with args got %q", got)
		}
	})
}

func TestBasePath(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/gcc": "gcc",
		"   gcc   ":    "gcc",
		"":             ".",
		"./foo":        "foo",
	}
	for in, want := range cases {
		if got := BasePath(in); got != want {
			t.Errorf("BasePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnquote_ShortStringsAndMixed(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{`"`, `"`},
		{`"x`, `"x`},
		{`"abc"`, "abc"},
		{`'abc'`, "abc"},
		{`"mismatch'`, `"mismatch'`},
	}
	for _, tc := range cases {
		if got := unquote(tc.in); got != tc.out {
			t.Errorf("unquote(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────
// types.go: ErrRequiresHITL.Error + Is
// ──────────────────────────────────────────────────────────────────────────

func TestErrRequiresHITL_ErrorAndIs(t *testing.T) {
	var nilErr *ErrRequiresHITL
	if nilErr.Error() != "require-hitl" {
		t.Fatalf("nil Error = %q", nilErr.Error())
	}
	e := &ErrRequiresHITL{Decision: Decision{Verdict: VerdictRequireHITL, RiskBand: RiskCaution, Reason: "caution"}}
	if !strings.Contains(e.Error(), "caution") {
		t.Fatalf("Error missing reason: %q", e.Error())
	}
	if !errors.Is(e, &ErrRequiresHITL{}) {
		t.Fatal("errors.Is with empty sentinel should match")
	}
	if errors.Is(e, errors.New("other")) {
		t.Fatal("should not match unrelated error")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// hitl.go: EncodePrompt, DecodeResponse, blacklist, HandleRequiresHITL branches
// ──────────────────────────────────────────────────────────────────────────

func TestEncodePrompt_StampsSchema(t *testing.T) {
	raw, err := EncodePrompt(HostApprovalPrompt{Operation: "exec", Command: "ls"})
	if err != nil {
		t.Fatalf("EncodePrompt: %v", err)
	}
	if !strings.Contains(string(raw), `"schema":"host.approval"`) {
		t.Fatalf("missing schema: %s", raw)
	}
}

func TestDecodeResponse_RoundTripAndError(t *testing.T) {
	r, err := DecodeResponse([]byte(`{"action":"approve-once"}`))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if r.Action != ActionApproveOnce {
		t.Fatalf("Action = %q", r.Action)
	}
	if _, err := DecodeResponse([]byte("not-json")); err == nil {
		t.Fatal("expected JSON error")
	}
}

// stubRouter implements HITLRouter and lets tests drive each branch of
// HandleRequiresHITL.
type stubRouter struct {
	publishErr error
	awaitErr   error
	response   HostApprovalResponse
	published  []HostApprovalPrompt
}

func (s *stubRouter) Publish(_ context.Context, p HostApprovalPrompt) error {
	s.published = append(s.published, p)
	return s.publishErr
}

func (s *stubRouter) AwaitResponse(_ context.Context) (HostApprovalResponse, error) {
	return s.response, s.awaitErr
}

func TestHandleRequiresHITL_NilRouter_Denies(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	err := g.HandleRequiresHITL(context.Background(), Decision{RiskBand: RiskCaution}, cpn.GateOp{Kind: "exec", Command: "ls"}, nil)
	if err == nil {
		t.Fatal("expected deny when router nil")
	}
	var he *cpn.HostError
	if !errors.As(err, &he) || he.Code != cpn.HostErrCodeGateDenied {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleRequiresHITL_PublishError(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	r := &stubRouter{publishErr: errors.New("boom")}
	err := g.HandleRequiresHITL(context.Background(), Decision{}, cpn.GateOp{Kind: "exec", Command: "ls"}, r)
	if err == nil || !strings.Contains(err.Error(), "publish") {
		t.Fatalf("expected publish err, got %v", err)
	}
}

func TestHandleRequiresHITL_AwaitError(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	r := &stubRouter{awaitErr: errors.New("timeout")}
	err := g.HandleRequiresHITL(context.Background(), Decision{Prompt: &HostApprovalPrompt{}}, cpn.GateOp{Kind: "exec", Command: "ls"}, r)
	if err == nil || !strings.Contains(err.Error(), "await") {
		t.Fatalf("expected await err, got %v", err)
	}
}

func TestHandleRequiresHITL_AllActions(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		wantDeny bool
	}{
		{"approve-once", ActionApproveOnce, false},
		{"approve-and-remember", ActionApproveAndRemember, false},
		{"deny", ActionDeny, true},
		{"empty-treated-as-deny", "", true},
		{"deny-and-blacklist", ActionDenyAndBlacklist, true},
		{"unknown-action", "mystery", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, _, _ := newGateForTest(t, defaultTestPolicy(t))
			r := &stubRouter{response: HostApprovalResponse{Action: tc.action}}
			op := cpn.GateOp{Kind: "exec", Command: "ls"}
			err := g.HandleRequiresHITL(context.Background(), Decision{Prompt: &HostApprovalPrompt{}}, op, r)
			gotDeny := err != nil
			if gotDeny != tc.wantDeny {
				t.Fatalf("action=%q err=%v wantDeny=%v", tc.action, err, tc.wantDeny)
			}
		})
	}
}

func TestHandleRequiresHITL_ApproveAndRemember_NoPoliciesFallback(t *testing.T) {
	// Force rememberApproval to return an error (policies nil) and make
	// sure HandleRequiresHITL still returns nil (approval succeeds).
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	g := NewPolicyHostGate(holder, nil, &memoryDecisions{}, NewBudgetTracker(), fakeSandbox{avail: map[string]bool{"bwrap": true}}, nil)
	// Nil out Policies to make rememberApproval log-and-continue.
	g.Policies = nil
	r := &stubRouter{response: HostApprovalResponse{Action: ActionApproveAndRemember}}
	err := g.HandleRequiresHITL(context.Background(), Decision{Prompt: &HostApprovalPrompt{}}, cpn.GateOp{Kind: "exec", Command: "ls"}, r)
	if err != nil {
		t.Fatalf("approve-and-remember still approves even when remember fails: got %v", err)
	}
}

func TestHandleRequiresHITL_DenyAndBlacklist_AppendsForbidden(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	before := len(g.Policies.policy.ForbiddenPat)
	r := &stubRouter{response: HostApprovalResponse{Action: ActionDenyAndBlacklist}}
	op := cpn.GateOp{Kind: "exec", Command: "mystery --arg"}
	_ = g.HandleRequiresHITL(context.Background(), Decision{Prompt: &HostApprovalPrompt{}}, op, r)
	if after := len(g.Policies.policy.ForbiddenPat); after != before+1 {
		t.Fatalf("ForbiddenPat grew from %d to %d", before, after)
	}
}

func TestBlacklist_NoPolicyHolder(t *testing.T) {
	g := &PolicyHostGate{}
	if err := g.blacklist(cpn.GateOp{Command: "ls"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRememberApproval_NoPolicyHolder(t *testing.T) {
	g := &PolicyHostGate{}
	if err := g.rememberApproval(context.Background(), cpn.GateOp{Command: "ls"}); err == nil {
		t.Fatal("expected error")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// policy_gate.go: NewStaticSandboxCapability, policy() fallback, ApproveFirstRun,
// RevokeFirstRun, RecordActual, sessionIDResolver, kill allow, write_file path
// ──────────────────────────────────────────────────────────────────────────

func TestStaticSandboxCapability(t *testing.T) {
	sc := NewStaticSandboxCapability()
	if sc.Available("") {
		t.Fatal("empty runtime must be false")
	}
	// cache hit for missing tool
	if sc.Available("definitely-not-a-runtime-xyz") {
		t.Fatal("unexpected true")
	}
	// second call hits cache branch
	if sc.Available("definitely-not-a-runtime-xyz") {
		t.Fatal("cache path still false")
	}
	// exec.LookPath should find "sh" on Linux
	if !sc.Available("sh") {
		t.Fatal("sh should be on PATH")
	}
}

func TestPolicyHostGate_NilPolicies_ReturnsEmpty(t *testing.T) {
	g := &PolicyHostGate{}
	p := g.policy()
	if p == nil {
		t.Fatal("policy() should never return nil")
	}
}

func TestApproveRevokeFirstRun_NilRepo_NoOp(t *testing.T) {
	g := &PolicyHostGate{}
	if err := g.ApproveFirstRun(context.Background(), "", "sha", "u"); err != nil {
		t.Fatalf("ApproveFirstRun nil repo: %v", err)
	}
	if err := g.RevokeFirstRun(context.Background(), "", "sha"); err != nil {
		t.Fatalf("RevokeFirstRun nil repo: %v", err)
	}
}

func TestApproveRevokeFirstRun_PassesThrough(t *testing.T) {
	fr := newMemoryFirstRun()
	// Seed one record
	_, _ = fr.Record(context.Background(), "host-test", "/bin/ls", "shaX")
	g := &PolicyHostGate{FirstRun: fr, HostID: "host-test"}

	if err := g.ApproveFirstRun(context.Background(), "", "shaX", "user-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	entry, _ := fr.GetBySHA(context.Background(), "host-test", "shaX")
	if entry == nil || entry.FirstApprovedAt == nil {
		t.Fatal("record should be approved")
	}

	if err := g.RevokeFirstRun(context.Background(), "host-test", "shaX"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	entry, _ = fr.GetBySHA(context.Background(), "host-test", "shaX")
	if !entry.Revoked {
		t.Fatal("record should be revoked")
	}
}

func TestRecordActual_NilBudgets(t *testing.T) {
	g := &PolicyHostGate{}
	if err := g.RecordActual(context.Background(), cpn.GateOp{}, BudgetEstimate{}); err != nil {
		t.Fatalf("RecordActual: %v", err)
	}
}

func TestRecordActual_WithBudgets(t *testing.T) {
	g := &PolicyHostGate{Budgets: NewBudgetTracker()}
	if err := g.RecordActual(context.Background(),
		cpn.GateOp{Kind: "exec", SessionID: "sess-a"},
		BudgetEstimate{CPUSeconds: 3}); err != nil {
		t.Fatal(err)
	}
	cur := g.Budgets.Current("sess-a")
	if cur.CPUSeconds != 3 {
		t.Fatalf("CPU=%d want 3", cur.CPUSeconds)
	}
	// Empty SessionID buckets into "unknown" (REQ-FIX-009) so mis-wired
	// callers cannot drain real sessions.
	if err := g.RecordActual(context.Background(),
		cpn.GateOp{Kind: "exec"},
		BudgetEstimate{CPUSeconds: 7}); err != nil {
		t.Fatal(err)
	}
	if g.Budgets.Current("unknown").CPUSeconds != 7 {
		t.Fatalf("unknown bucket = %+v", g.Budgets.Current("unknown"))
	}
}

func TestSessionIDResolver_Override(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	g.SessionIDResolver = func(ctx context.Context) string { return "custom-sid" }
	dec := g.Evaluate(context.Background(), cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxNone})
	if dec.SessionID != "custom-sid" {
		t.Fatalf("SessionID = %q", dec.SessionID)
	}
	// Resolver returning "" falls back to "default".
	g.SessionIDResolver = func(ctx context.Context) string { return "" }
	dec = g.Evaluate(context.Background(), cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxNone})
	if dec.SessionID != "default" {
		t.Fatalf("fallback SessionID = %q", dec.SessionID)
	}
}

// TestRecordActual_UsesSessionIDFromOp — AC-009 regression. RecordActual
// must bucket usage by op.SessionID; an empty SessionID falls back to
// "unknown" so one mis-wired caller cannot drain a real session's budget.
func TestRecordActual_UsesSessionIDFromOp(t *testing.T) {
	g := &PolicyHostGate{}
	// No Budgets wired — just make sure the call completes without panic.
	if err := g.RecordActual(context.Background(),
		cpn.GateOp{Kind: "exec", SessionID: "sess-123"},
		BudgetEstimate{CPUSeconds: 1}); err != nil {
		t.Fatalf("RecordActual with session id: %v", err)
	}
	if err := g.RecordActual(context.Background(),
		cpn.GateOp{Kind: "exec"},
		BudgetEstimate{CPUSeconds: 1}); err != nil {
		t.Fatalf("RecordActual without session id: %v", err)
	}
}

func TestEvaluate_KillIsAlwaysAllowed(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	dec := g.Evaluate(context.Background(), cpn.GateOp{Kind: "kill", Command: "rm -rf /"})
	if dec.Verdict != VerdictAllow {
		t.Fatalf("kill verdict = %s", dec.Verdict)
	}
}

func TestEvaluate_WriteFileDangerousPath(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	dec := g.Evaluate(context.Background(), cpn.GateOp{Kind: "write_file", Path: "/etc/passwd"})
	if dec.Verdict != VerdictRequireHITL {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	if dec.RiskBand != RiskDangerous {
		t.Fatalf("band = %s", dec.RiskBand)
	}
}

func TestEvaluate_ReadFileUnknownFallsToHITL(t *testing.T) {
	g, _, _ := newGateForTest(t, defaultTestPolicy(t))
	dec := g.Evaluate(context.Background(), cpn.GateOp{Kind: "read_file", Path: "/tmp/x"})
	if dec.Verdict != VerdictRequireHITL {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
}

func TestEvaluate_FirstRunApprovedThenSafe(t *testing.T) {
	// For a safe-band op like "ls", exec+FirstRun should trigger first-run
	// HITL. After Approve, subsequent call should allow.
	p := defaultTestPolicy(t)
	g, _, fr := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxNone}
	// Resolve ls to a real binary; first call should require HITL via first-run.
	dec := g.Evaluate(context.Background(), op)
	if dec.Verdict != VerdictRequireHITL {
		t.Fatalf("initial verdict = %s", dec.Verdict)
	}
	// Approve the recorded SHA
	entries, _ := fr.List(context.Background(), 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 recorded entry, got %d", len(entries))
	}
	if err := fr.Approve(context.Background(), entries[0].HostID, entries[0].BinarySHA256, "u"); err != nil {
		t.Fatal(err)
	}
	dec = g.Evaluate(context.Background(), op)
	if dec.Verdict != VerdictAllow {
		t.Fatalf("post-approval verdict = %s (%s)", dec.Verdict, dec.Reason)
	}
}

func TestEvaluate_FirstRunRevokedRequiresHITL(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, fr := newGateForTest(t, p)
	op := cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxNone}
	_ = g.Evaluate(context.Background(), op)
	entries, _ := fr.List(context.Background(), 10)
	_ = fr.Approve(context.Background(), entries[0].HostID, entries[0].BinarySHA256, "u")
	_ = fr.Revoke(context.Background(), entries[0].HostID, entries[0].BinarySHA256)
	dec := g.Evaluate(context.Background(), op)
	if dec.Verdict != VerdictRequireHITL {
		t.Fatalf("revoked verdict = %s", dec.Verdict)
	}
}

// failingFirstRun returns error from Record to drive the err branch.
type failingFirstRun struct{ memoryFirstRun }

func (f *failingFirstRun) Record(ctx context.Context, hostID, path, sha string) (*persist.FirstRunLedgerEntry, error) {
	return nil, errors.New("boom")
}

func TestFirstRunLookup_BinaryNotFound(t *testing.T) {
	p := defaultTestPolicy(t)
	g, _, _ := newGateForTest(t, p)
	// exec with a binary that doesn't exist — firstRunLookup returns err,
	// and Evaluate's first-run branch is skipped; fall-through classifies.
	op := cpn.GateOp{Kind: "exec", Command: "does-not-exist-xyz", Sandbox: cpn.SandboxNone}
	dec := g.Evaluate(context.Background(), op)
	if dec.Verdict != VerdictRequireHITL {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	if dec.RiskBand != RiskUnknown {
		t.Fatalf("band = %s", dec.RiskBand)
	}
}

func TestResolveBinaryPath_AbsoluteAndEmpty(t *testing.T) {
	if _, err := resolveBinaryPath("   "); err == nil {
		t.Fatal("expected empty error")
	}
	// absolute path branch
	abs := "/usr/bin/does-not-exist-xyz"
	got, err := resolveBinaryPath(abs)
	if err != nil {
		t.Fatalf("abs path err: %v", err)
	}
	if got != abs {
		t.Fatalf("got %q", got)
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "hello.bin")
	if err := os.WriteFile(fp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256File(fp)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("hello")
	if sum != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("sum=%s", sum)
	}
	if _, err := sha256File(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected open err")
	}
}

func TestTruncSHA(t *testing.T) {
	if truncSHA("abc") != "abc" {
		t.Fatal("short passes through")
	}
	if got := truncSHA("0123456789abcdef"); got != "0123456789ab" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayCommand(t *testing.T) {
	if got := displayCommand(cpn.GateOp{Command: "ls"}); got != "ls" {
		t.Fatalf("command branch = %q", got)
	}
	if got := displayCommand(cpn.GateOp{Kind: "write_file", Path: "/tmp/x"}); got != "write_file /tmp/x" {
		t.Fatalf("path branch = %q", got)
	}
	if got := displayCommand(cpn.GateOp{Kind: "kill"}); got != "kill" {
		t.Fatalf("kind-only branch = %q", got)
	}
}

func TestAuditDecision_Paths(t *testing.T) {
	g := &PolicyHostGate{}
	// nil decisions → no-op nil.
	if err := g.AuditDecision(context.Background(), Decision{}, cpn.GateOp{}, ""); err != nil {
		t.Fatalf("nil audit: %v", err)
	}
	// With decisions: non-empty HITL ID triggers pointer branch.
	dec := &memoryDecisions{}
	g.Decisions = dec
	if err := g.AuditDecision(context.Background(), Decision{Verdict: VerdictAllow, SessionID: "s"}, cpn.GateOp{Kind: "exec", Command: "ls"}, "hitl-42"); err != nil {
		t.Fatal(err)
	}
	dec.mu.Lock()
	defer dec.mu.Unlock()
	if len(dec.records) != 1 {
		t.Fatalf("n records = %d", len(dec.records))
	}
	if dec.records[0].HITLResponseID == nil || *dec.records[0].HITLResponseID != "hitl-42" {
		t.Fatalf("HITLResponseID = %+v", dec.records[0].HITLResponseID)
	}
}

func TestEvaluate_SandboxUnavailable_Deny(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	sb := fakeSandbox{avail: map[string]bool{}}
	g := NewPolicyHostGate(holder, nil, &memoryDecisions{}, NewBudgetTracker(), sb, nil)
	op := cpn.GateOp{Kind: "exec", Command: "ls", Sandbox: cpn.SandboxReadonly}
	dec := g.Evaluate(context.Background(), op)
	if dec.Verdict != VerdictDeny {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	if dec.Reason != "sandbox_unavailable" {
		t.Fatalf("reason = %s", dec.Reason)
	}
}

func TestCheckSandboxAvailable_UnknownProfileFallbacks(t *testing.T) {
	// Policy with no sandbox_mappings → checkSandboxAvailable uses default
	// bwrap/firejail. With neither available → err; with firejail → ok.
	p := &HostPolicy{}
	_ = p.finalize()
	sbNone := fakeSandbox{avail: map[string]bool{}}
	g := &PolicyHostGate{SandboxCap: sbNone}
	if err := g.checkSandboxAvailable(cpn.SandboxProfile("custom"), p); err == nil {
		t.Fatal("expected err when neither preferred nor fallback exists")
	}
	g.SandboxCap = fakeSandbox{avail: map[string]bool{"firejail": true}}
	if err := g.checkSandboxAvailable(cpn.SandboxProfile("custom"), p); err != nil {
		t.Fatalf("fallback ok: %v", err)
	}
}

func TestResolveSandbox_OpOverride(t *testing.T) {
	g := &PolicyHostGate{}
	p := &HostPolicy{Defaults: PolicyDefaults{Sandbox: "readonly"}}
	got := g.resolveSandbox(cpn.GateOp{Sandbox: cpn.SandboxNone}, p)
	if got != cpn.SandboxNone {
		t.Fatalf("op override = %q", got)
	}
	got = g.resolveSandbox(cpn.GateOp{}, p)
	if got != cpn.SandboxProfile("readonly") {
		t.Fatalf("default = %q", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Check: Verdict-switch branches (allow, deny, hitl) fully exercised
// ──────────────────────────────────────────────────────────────────────────

func TestCheck_AllowBranch(t *testing.T) {
	p := defaultTestPolicy(t)
	holder := NewHolder(p)
	g := NewPolicyHostGate(holder, nil, &memoryDecisions{}, NewBudgetTracker(), fakeSandbox{avail: map[string]bool{"bwrap": true}}, nil)
	if err := g.Check(context.Background(), cpn.GateOp{Kind: "kill"}); err != nil {
		t.Fatalf("Check kill: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// policy.go: Put, WithLearnedPath, LearnedPath, ToYAML, ValidatePattern,
// LoadFromBytes, LoadFromFile, MergeLearnedFromFile, append overlay dedup.
// ──────────────────────────────────────────────────────────────────────────

func TestValidatePattern(t *testing.T) {
	if err := ValidatePattern(`^ls$`); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := ValidatePattern(`[`); err == nil {
		t.Fatal("invalid pattern should error")
	}
}

func TestHolder_PutGet_WithLearnedPath(t *testing.T) {
	h := NewHolder(&HostPolicy{})
	_ = h.Get()
	h.WithLearnedPath("/tmp/learned.yaml")
	if h.LearnedPath() != "/tmp/learned.yaml" {
		t.Fatalf("LearnedPath = %q", h.LearnedPath())
	}
	h.Put(&HostPolicy{Version: 9})
	if h.Get().Version != 9 {
		t.Fatalf("Put didn't swap")
	}
}

func TestHolder_AppendLearnedSafePattern_NoPolicyAndInvalid(t *testing.T) {
	h := &Holder{}
	if err := h.AppendLearnedSafePattern("^x$"); err == nil {
		t.Fatal("expected no-policy err")
	}
	h = NewHolder(&HostPolicy{})
	if err := h.AppendLearnedSafePattern("["); err == nil {
		t.Fatal("expected invalid regex err")
	}
}

func TestHolder_AppendLearnedSafePattern_PersistsToDisk(t *testing.T) {
	p := &HostPolicy{}
	_ = p.finalize()
	dir := t.TempDir()
	fp := filepath.Join(dir, "learned.yaml")
	h := NewHolder(p).WithLearnedPath(fp)
	if err := h.AppendLearnedSafePattern(`^magic-xyz$`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "magic-xyz") {
		t.Fatalf("file missing pattern: %s", raw)
	}
	// Dedup branch: re-append same pattern — file unchanged, no error.
	if err := h.AppendLearnedSafePattern(`^magic-xyz$`); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromBytes_AndToYAML(t *testing.T) {
	y := []byte(`
version: 1
defaults:
  sandbox: readonly
safe_patterns:
  - "^ls$"
`)
	p, err := LoadFromBytes(y)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.SafePat, `^ls$`) {
		t.Fatalf("SafePat = %v", p.SafePat)
	}
	out, err := p.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("ToYAML empty")
	}
	// nil policy ToYAML
	var nilp *HostPolicy
	if _, err := nilp.ToYAML(); err == nil {
		t.Fatal("nil policy ToYAML must err")
	}
}

func TestLoadFromBytes_InvalidYAMLAndPatternError(t *testing.T) {
	if _, err := LoadFromBytes([]byte("safe_patterns: 123\n")); err == nil {
		t.Fatal("expected parse err (type mismatch)")
	}
	bad := []byte("safe_patterns:\n  - \"[\"\n")
	if _, err := LoadFromBytes(bad); err == nil {
		t.Fatal("expected compile err")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(fp, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(fp); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("expected read err")
	}
	// Size cap
	big := filepath.Join(dir, "big.yaml")
	data := make([]byte, maxYAMLBytes+1)
	if err := os.WriteFile(big, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(big); err == nil {
		t.Fatal("expected cap err")
	}
}

func TestFinalize_BucketCapAndDefaultsFilled(t *testing.T) {
	p := &HostPolicy{}
	big := make([]string, maxPatternsPerBucket+1)
	for i := range big {
		big[i] = "^x$"
	}
	p.SafePat = big
	if err := p.finalize(); err == nil {
		t.Fatal("expected cap err")
	}
	// Empty policy gets defaults.
	p2 := &HostPolicy{}
	if err := p2.finalize(); err != nil {
		t.Fatal(err)
	}
	if p2.Defaults.Sandbox != "readonly" || p2.Defaults.CPUSecondsBudget == 0 {
		t.Fatalf("defaults not filled: %+v", p2.Defaults)
	}
}

func TestMergeLearnedFromFile(t *testing.T) {
	p := &HostPolicy{}
	_ = p.finalize()
	// Missing file → no-op nil.
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")
	if err := MergeLearnedFromFile(p, missing); err != nil {
		t.Fatal(err)
	}
	// Nil policy
	if err := MergeLearnedFromFile(nil, missing); err == nil {
		t.Fatal("expected nil policy err")
	}
	// Empty overlay file → no-op.
	empty := filepath.Join(dir, "empty.yaml")
	_ = os.WriteFile(empty, []byte("safe_patterns: []\n"), 0o644)
	if err := MergeLearnedFromFile(p, empty); err != nil {
		t.Fatal(err)
	}
	// Good overlay
	good := filepath.Join(dir, "good.yaml")
	_ = os.WriteFile(good, []byte("safe_patterns:\n  - ^custom$\n"), 0o644)
	if err := MergeLearnedFromFile(p, good); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.SafePat, "^custom$") {
		t.Fatalf("SafePat = %v", p.SafePat)
	}
	// Parse error (type mismatch)
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("safe_patterns: 42\n"), 0o644)
	if err := MergeLearnedFromFile(p, bad); err == nil {
		t.Fatal("expected parse err")
	}
	// Compile error in patterns inside overlay
	compileBad := filepath.Join(dir, "compile_bad.yaml")
	_ = os.WriteFile(compileBad, []byte("safe_patterns:\n  - \"[\"\n"), 0o644)
	if err := MergeLearnedFromFile(p, compileBad); err == nil {
		t.Fatal("expected compile err")
	}
}

// ──────────────────────────────────────────────────────────────────────────
// budget.go: Reset + Current error paths
// ──────────────────────────────────────────────────────────────────────────

func TestBudgetTracker_Reset(t *testing.T) {
	bt := NewBudgetTracker()
	bt.RecordActual("s", BudgetEstimate{CPUSeconds: 5})
	if bt.Current("s").CPUSeconds != 5 {
		t.Fatal("pre-reset")
	}
	bt.Reset("s")
	if bt.Current("s").CPUSeconds != 0 {
		t.Fatal("post-reset")
	}
	// default return for unseen session
	if bt.Current("ghost").CPUSeconds != 0 {
		t.Fatal("ghost returns zero")
	}
}

func TestBudgetCheckEstimate_AllBranches(t *testing.T) {
	bt := NewBudgetTracker()
	def := PolicyDefaults{CPUSecondsBudget: 10, BytesWrittenBudget: 100, OutboundRequestsBudget: 2}
	// headroom
	if _, ok := bt.CheckEstimate("s", def, BudgetEstimate{CPUSeconds: 1}); !ok {
		t.Fatal("should have headroom")
	}
	// cpu exceeded
	if r, ok := bt.CheckEstimate("s", def, BudgetEstimate{CPUSeconds: 11}); ok || r != "cpu_budget_exceeded" {
		t.Fatalf("cpu branch: %q", r)
	}
	// bytes exceeded
	if r, ok := bt.CheckEstimate("s", def, BudgetEstimate{BytesWritten: 101}); ok || r != "bytes_budget_exceeded" {
		t.Fatalf("bytes: %q", r)
	}
	// outbound exceeded
	if r, ok := bt.CheckEstimate("s", def, BudgetEstimate{OutboundRequests: 3}); ok || r != "outbound_budget_exceeded" {
		t.Fatalf("outbound: %q", r)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// session_router: transitionIDOrEmpty, logger default, decode pure HITLResponse
// with default Action empty
// ──────────────────────────────────────────────────────────────────────────

func TestTransitionIDOrEmpty(t *testing.T) {
	if transitionIDOrEmpty(nil) != "" {
		t.Fatal("nil should be empty")
	}
	if transitionIDOrEmpty(&cpn.Transition{ID: "t1"}) != "t1" {
		t.Fatal("got id")
	}
}

func TestSessionHITLRouter_LoggerDefault(t *testing.T) {
	r := &SessionHITLRouter{}
	if r.logger() == nil {
		t.Fatal("logger must not be nil")
	}
}

func TestSessionHITLHandler_NilErrAndNilGate(t *testing.T) {
	h := &SessionHITLHandler{}
	if err := h.HandleHITL(context.Background(), nil, nil, cpn.GateOp{}, nil); err != nil {
		t.Fatalf("nil err passthrough: %v", err)
	}
	// HITL err but nil Gate on handler → returns the raw err.
	dec := Decision{RiskBand: RiskCaution}
	hitlErr := &ErrRequiresHITL{Decision: dec}
	if err := h.HandleHITL(context.Background(), nil, nil, cpn.GateOp{}, hitlErr); !errors.Is(err, hitlErr) {
		t.Fatalf("should return raw hitl err, got %v", err)
	}
}

// Compile-time use to silence unused-import warnings if branches change.
var _ = time.Now
var _ = json.Marshal
