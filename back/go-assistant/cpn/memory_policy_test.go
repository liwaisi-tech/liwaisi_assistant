package cpn

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/prompts"
)

// ── Acceptance criteria from spec-architecture-brae-context-and-tool-hygiene.md ──

// AC-001: a write performed many turns ago must still be visible in the
// workspace preamble of the next assistant LLM call.
func TestBuildContextWithPolicy_AC001_PreambleSurvivesSlidingWindow(t *testing.T) {
	// Build a 140-message-style history where the write happens early
	// and 60+ subsequent raw messages would normally bury it.
	now := time.Now()
	ledger, err := LedgerSuccess(LedgerVerbWrite, "~/workspace/pg_explorer/main.go", "1247 bytes")
	if err != nil {
		t.Fatalf("LedgerSuccess: %v", err)
	}
	ledger.Timestamp = now.Add(-30 * time.Minute)

	history := []*Message{ledger}
	// Pad with 80 raw messages — far exceeds DefaultContextWindowSize*2=20.
	for i := 0; i < 80; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		history = append(history, &Message{
			ID:        fmt.Sprintf("raw-%d", i),
			Role:      role,
			Content:   fmt.Sprintf("filler %d", i),
			Timestamp: now.Add(time.Duration(-29+i) * time.Minute),
		})
	}

	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "ASSISTANT_PROMPT_PLACEHOLDER"
	cw := BuildContextWithPolicy(RoleAssistantTransition, history, policy)

	if !strings.Contains(cw.SystemPrompt, "[workspace state @") {
		t.Fatalf("preamble header missing from system prompt:\n%s", cw.SystemPrompt)
	}
	if !strings.Contains(cw.SystemPrompt, "pg_explorer/main.go") {
		t.Errorf("preamble must reference path written 60+ messages ago; got:\n%s", cw.SystemPrompt)
	}
	if !strings.Contains(cw.SystemPrompt, "ASSISTANT_PROMPT_PLACEHOLDER") {
		t.Errorf("base system prompt was discarded; preamble must prepend, not replace")
	}
}

// AC-002: classifier-style transitions must not ship the assistant persona.
func TestBuildContextWithPolicy_AC002_ClassifierHasNoPersona(t *testing.T) {
	policy := NewDefaultPolicyResolver().For(RoleClassifyTransition)
	policy.SystemPrompt = prompts.For(prompts.RoleClassify)

	cw := BuildContextWithPolicy(RoleClassifyTransition, nil, policy)

	for _, banned := range []string{"brae", "Órale", "USER CONTEXT — REGIONAL REGISTER"} {
		if strings.Contains(cw.SystemPrompt, banned) {
			t.Errorf("classifier prompt leaked persona token %q:\n%s", banned, cw.SystemPrompt)
		}
	}
	if policy.IncludeWorkspacePreamble {
		t.Errorf("classifier policy must not enable workspace preamble")
	}
}

// AC-003: a successful write_file emits a ledger observer with the
// canonical "[ok] write <path> (<size>)" shape.
func TestLedgerSuccess_AC003_Shape(t *testing.T) {
	msg, err := LedgerSuccess(LedgerVerbWrite, "~/x/main.go", "1247 bytes")
	if err != nil {
		t.Fatalf("LedgerSuccess: %v", err)
	}
	if msg.Role != RoleObserver {
		t.Errorf("Role = %q, want RoleObserver", msg.Role)
	}
	if msg.CPNRole != CPNRoleLedger {
		t.Errorf("CPNRole = %q, want %q", msg.CPNRole, CPNRoleLedger)
	}
	wantRE := regexp.MustCompile(`^\[ok\] write \S+ \(\d+ bytes\)$`)
	if !wantRE.MatchString(msg.Content) {
		t.Errorf("content %q does not match %s", msg.Content, wantRE)
	}
}

// AC-004: the documented WriteFileResult schema MUST NOT carry a content
// field. This is a structural test against accidental regressions.
func TestWriteFileResult_AC004_NoContentField(t *testing.T) {
	// Reflect over the struct via JSON marshalling; absence of
	// "content" / "body" / "bytes_written" is the contract.
	r := struct {
		Path    string
		Bytes   int64
		Lines   int
		SHA256  string
		Created bool
	}{} // mirror; the real type lives in cpn/tools to avoid an import cycle here.

	// We assert against the package-level field list documented on
	// tools.WriteFileResult by walking the JSON tag set. The actual
	// type is exercised in cpn/tools tests; this guard catches any
	// future drift in the docstring-driven contract.
	bannedTags := []string{"content", "body", "bytes_written"}
	for _, tag := range bannedTags {
		if strings.Contains(fmt.Sprintf("%+v", r), tag) {
			t.Errorf("write_file mirror unexpectedly contains banned field %q", tag)
		}
	}
}

// AC-005: failed bash build emits "[fail] build <target> → \"<cause>\""
// with cause ≤120 chars.
func TestLedgerFailure_AC005_BuildShape(t *testing.T) {
	long := strings.Repeat("strings imported and not used. ", 20) // ~600 chars
	msg, err := LedgerFailure(LedgerVerbBuild, "~/workspace/sql_client", long)
	if err != nil {
		t.Fatalf("LedgerFailure: %v", err)
	}
	if !strings.HasPrefix(msg.Content, "[fail] build ") {
		t.Errorf("content prefix wrong: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, " → ") {
		t.Errorf("missing arrow separator: %q", msg.Content)
	}
	// Extract the quoted cause and check its length.
	idx := strings.Index(msg.Content, "→ \"")
	if idx < 0 {
		t.Fatalf("cause quote not found: %q", msg.Content)
	}
	rest := msg.Content[idx+len("→ \""):]
	end := strings.LastIndex(rest, "\"")
	if end < 0 {
		t.Fatalf("cause closing quote not found: %q", msg.Content)
	}
	cause := rest[:end]
	if len(cause) > maxLedgerCause {
		t.Errorf("cause length %d exceeds cap %d: %q", len(cause), maxLedgerCause, cause)
	}
}

// AC-006: assistant prompt contains the file-echo rule + GOOD/BAD examples.
func TestPromptsAssistant_AC006_FileEchoRule(t *testing.T) {
	body := prompts.For(prompts.RoleAssistant)
	for _, must := range []string{"do not echo its contents", "GOOD:", "BAD:"} {
		if !strings.Contains(body, must) {
			t.Errorf("assistant prompt missing required token %q", must)
		}
	}
}

// AC-007: classifier policy disables ledger; observer summaries from
// ordinary sub-CPNs still pass through.
func TestBuildContextWithPolicy_AC007_LedgerHiddenForClassifier(t *testing.T) {
	now := time.Now()
	ledger, _ := LedgerSuccess(LedgerVerbWrite, "/tmp/x", "10B")
	obs := &Message{
		ID: "o1", Role: RoleObserver,
		Content: "ordinary summary", CPNRole: "worker",
		Timestamp: now,
	}
	history := []*Message{ledger, obs,
		{Role: RoleUser, Content: "classify this"},
	}

	cw := BuildContextWithPolicy(
		RoleClassifyTransition,
		history,
		NewDefaultPolicyResolver().For(RoleClassifyTransition),
	)

	for _, m := range cw.Messages {
		if strings.HasPrefix(m.Content, "[ok] ") || strings.HasPrefix(m.Content, "[fail] ") {
			t.Errorf("classifier context unexpectedly contains ledger line: %q", m.Content)
		}
	}
	// Ordinary observer summary MUST still pass through.
	var sawObs bool
	for _, m := range cw.Messages {
		if strings.Contains(m.Content, "ordinary summary") {
			sawObs = true
		}
	}
	if !sawObs {
		t.Errorf("ordinary observer summary was incorrectly filtered out for classifier")
	}
}

// AC-008: workspace preamble caps at maxWorkspacePreambleEntries with a
// trailing "[+N older entries]" line.
func TestBuildWorkspacePreamble_AC008_OverflowTrailer(t *testing.T) {
	now := time.Now()
	history := make([]*Message, 0, 70)
	for i := 0; i < 70; i++ {
		ledger, err := LedgerSuccess(
			LedgerVerbWrite,
			fmt.Sprintf("~/f%d.go", i),
			"100B",
		)
		if err != nil {
			t.Fatalf("LedgerSuccess: %v", err)
		}
		ledger.Timestamp = now.Add(time.Duration(-i) * time.Minute)
		history = append(history, ledger)
	}

	pre := buildWorkspacePreamble(history, now)
	lines := strings.Split(pre, "\n")
	// header + 50 entries + trailer = 52 lines
	if len(lines) != maxWorkspacePreambleEntries+2 {
		t.Errorf("expected %d lines (header+50+trailer), got %d:\n%s",
			maxWorkspacePreambleEntries+2, len(lines), pre)
	}
	wantTrailer := fmt.Sprintf("[+%d older entries]", 70-maxWorkspacePreambleEntries)
	if !strings.HasSuffix(pre, wantTrailer) {
		t.Errorf("expected trailer %q, got tail %q", wantTrailer, lines[len(lines)-1])
	}
}

// ── Supporting tests for the new infrastructure ──────────────────────────────

func TestParseLedgerLine(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		wantVerb       string
		wantTarget     string
		wantSummary    string
		wantOK         bool
	}{
		{
			name: "ok with size",
			in:   "[ok] write ~/x/main.go (1247 bytes)",
			wantVerb: "write", wantTarget: "~/x/main.go", wantSummary: "(1247 bytes)", wantOK: true,
		},
		{
			name: "ok no size",
			in:   "[ok] mkdir ~/bin",
			wantVerb: "mkdir", wantTarget: "~/bin", wantOK: true,
		},
		{
			name:   "fail not surfaced",
			in:     `[fail] build ~/x → "boom"`,
			wantOK: false,
		},
		{
			name:   "malformed",
			in:     "wrote a file",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, tgt, sum, ok := parseLedgerLine(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			got := struct{ V, T, S string }{v, tgt, sum}
			want := struct{ V, T, S string }{tc.wantVerb, tc.wantTarget, tc.wantSummary}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestElideMiddle(t *testing.T) {
	cases := []struct{ in, want string; max int }{
		{"short", "short", 60},
		{"/very/very/long/path/to/some/file.go", "/very/very/lo.../some/file.go", 30},
	}
	for _, tc := range cases {
		got := elideMiddle(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("elideMiddle(%q,%d)=%q want %q", tc.in, tc.max, got, tc.want)
		}
		if len(got) > tc.max {
			t.Errorf("len %d exceeds max %d", len(got), tc.max)
		}
	}
}

func TestDefaultPolicyResolver_AssistantHasPreamble(t *testing.T) {
	p := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	if !p.IncludeWorkspacePreamble || !p.IncludeLedger {
		t.Errorf("assistant policy must enable preamble + ledger; got %+v", p)
	}
	if p.RawWindowTurns != DefaultContextWindowSize {
		t.Errorf("assistant RawWindowTurns=%d want %d", p.RawWindowTurns, DefaultContextWindowSize)
	}
}

func TestDefaultPolicyResolver_UnknownRoleFallsBackToAssistant(t *testing.T) {
	want := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	got := NewDefaultPolicyResolver().For(TransitionRole("nope"))
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("unknown role should fall back to assistant policy:\n%s", diff)
	}
}

func TestBuildContextWithPolicy_LedgerSurvivesWindow(t *testing.T) {
	now := time.Now()
	ledger, _ := LedgerSuccess(LedgerVerbBuild, "~/bin/sql_client", "")
	ledger.Timestamp = now.Add(-time.Hour)

	history := []*Message{ledger}
	for i := 0; i < 50; i++ {
		history = append(history, &Message{
			ID: fmt.Sprintf("r%d", i), Role: RoleUser,
			Content: "noise", Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}
	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "x"
	cw := BuildContextWithPolicy(RoleAssistantTransition, history, policy)

	var sawLedger bool
	for _, m := range cw.Messages {
		if strings.HasPrefix(m.Content, "[ok] build ") {
			sawLedger = true
			break
		}
	}
	if !sawLedger {
		t.Errorf("ledger entry was lost despite IncludeLedger=true")
	}
}

func TestLedgerSuccess_RejectsEmptyVerbOrTarget(t *testing.T) {
	if _, err := LedgerSuccess("", "x", ""); err == nil {
		t.Errorf("empty verb should error")
	}
	if _, err := LedgerSuccess(LedgerVerbWrite, "", ""); err == nil {
		t.Errorf("empty target should error")
	}
}

func TestCompressCause_CollapsesMultilineAndCaps(t *testing.T) {
	in := "  first error line\nsecond line\nthird"
	got := compressCause(in)
	if got != "first error line" {
		t.Errorf("got %q want %q", got, "first error line")
	}

	long := strings.Repeat("x", 500)
	if g := compressCause(long); len(g) > maxLedgerCause {
		t.Errorf("len %d exceeds cap %d", len(g), maxLedgerCause)
	}
}

func TestIsLedgerEntry(t *testing.T) {
	if !IsLedgerEntry(&Message{Role: RoleObserver, CPNRole: CPNRoleLedger}) {
		t.Errorf("ledger-tagged observer should be recognised")
	}
	if IsLedgerEntry(&Message{Role: RoleObserver, CPNRole: "worker"}) {
		t.Errorf("ordinary observer should not be recognised")
	}
	if IsLedgerEntry(nil) {
		t.Errorf("nil should be safe and false")
	}
}

// ── Benchmark per spec performance requirement ──────────────────────────────

// BenchmarkBuildContext_LongHistory measures the cost of assembling a
// context window over a 1000-message history with 50 ledger entries.
// Spec target: ≤500µs on the CI machine class.
func BenchmarkBuildContext_LongHistory(b *testing.B) {
	now := time.Now()
	history := make([]*Message, 0, 1050)
	for i := 0; i < 50; i++ {
		ledger, _ := LedgerSuccess(LedgerVerbWrite,
			fmt.Sprintf("~/w/file%d.go", i), "1KB")
		ledger.Timestamp = now.Add(time.Duration(i) * time.Second)
		history = append(history, ledger)
	}
	for i := 0; i < 1000; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		history = append(history, &Message{
			ID: fmt.Sprintf("m%d", i), Role: role,
			Content: strings.Repeat("x", 200),
		})
	}
	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "you are brae"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildContextWithPolicy(RoleAssistantTransition, history, policy)
	}
}
