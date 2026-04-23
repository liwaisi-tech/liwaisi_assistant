package cpn

import (
	"strings"
	"testing"
)

func TestClassifyBashCommand(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		args       []string
		wantVerb   LedgerVerb
		wantTarget string
		wantOK     bool
	}{
		{
			name: "go build with -o",
			cmd:  "go", args: []string{"build", "-o", "~/bin/sql_client", "./cmd/sql_client"},
			wantVerb: LedgerVerbBuild, wantTarget: "~/bin/sql_client", wantOK: true,
		},
		{
			name: "go build no -o",
			cmd:  "go", args: []string{"build", "./cmd/foo"},
			wantVerb: LedgerVerbBuild, wantTarget: "./cmd/foo", wantOK: true,
		},
		{
			name: "go vet is not state-changing",
			cmd:  "go", args: []string{"vet", "./..."},
			wantOK: false,
		},
		{
			name: "mkdir -p",
			cmd:  "mkdir", args: []string{"-p", "~/workspace/x"},
			wantVerb: LedgerVerbMkdir, wantTarget: "~/workspace/x", wantOK: true,
		},
		{
			name: "chmod +x",
			cmd:  "chmod", args: []string{"+x", "~/bin/foo"},
			wantVerb: LedgerVerbChmod, wantTarget: "~/bin/foo", wantOK: true,
		},
		{
			name: "cp src dst",
			cmd:  "cp", args: []string{"src.txt", "dst.txt"},
			wantVerb: LedgerVerbCp, wantTarget: "dst.txt", wantOK: true,
		},
		{
			name: "mv with flag",
			cmd:  "mv", args: []string{"-f", "old", "new"},
			wantVerb: LedgerVerbMv, wantTarget: "new", wantOK: true,
		},
		{
			name:   "ls is read-only",
			cmd:    "ls", args: []string{"-la"},
			wantOK: false,
		},
		{
			name:   "unknown command",
			cmd:    "frobnicate", args: []string{"x"},
			wantOK: false,
		},
		{
			name: "make default target",
			cmd:  "make", args: nil,
			wantVerb: LedgerVerbBuild, wantTarget: "default", wantOK: true,
		},
		{
			name: "tee writes to file",
			cmd:  "tee", args: []string{"-a", "log.txt"},
			wantVerb: LedgerVerbWrite, wantTarget: "log.txt", wantOK: true,
		},
		{
			name: "go build with -o= form",
			cmd:  "go", args: []string{"build", "-o=bin/x", "./cmd/x"},
			wantVerb: LedgerVerbBuild, wantTarget: "bin/x", wantOK: true,
		},
		{
			name: "absolute path command",
			cmd:  "/usr/bin/mkdir", args: []string{"foo"},
			wantVerb: LedgerVerbMkdir, wantTarget: "foo", wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, tgt, ok := classifyBashCommand(tc.cmd, tc.args)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v (verb=%q target=%q)", ok, tc.wantOK, v, tgt)
			}
			if !ok {
				return
			}
			if v != tc.wantVerb {
				t.Errorf("verb=%q want %q", v, tc.wantVerb)
			}
			if tgt != tc.wantTarget {
				t.Errorf("target=%q want %q", tgt, tc.wantTarget)
			}
		})
	}
}

func TestEmitBashLedger_AppendsOnSuccess(t *testing.T) {
	c := &CPN{}
	emitBashLedger(c, "mkdir", []string{"-p", "/tmp/x"}, 0, "")
	if len(c.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(c.History))
	}
	got := c.History[0]
	if !IsLedgerEntry(got) {
		t.Errorf("appended message is not a ledger entry: %+v", got)
	}
	if !strings.HasPrefix(got.Content, "[ok] mkdir ") {
		t.Errorf("unexpected ledger content: %q", got.Content)
	}
}

func TestEmitBashLedger_AppendsOnFailure(t *testing.T) {
	c := &CPN{}
	emitBashLedger(c, "go", []string{"build", "./cmd/x"}, 1, "main.go:5: undefined: foo")
	if len(c.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(c.History))
	}
	got := c.History[0].Content
	if !strings.HasPrefix(got, "[fail] build ") {
		t.Errorf("unexpected ledger content: %q", got)
	}
	if !strings.Contains(got, "undefined: foo") {
		t.Errorf("cause missing from ledger: %q", got)
	}
}

func TestEmitBashLedger_NoOpForReadOnly(t *testing.T) {
	c := &CPN{}
	emitBashLedger(c, "ls", []string{"-la"}, 0, "")
	if len(c.History) != 0 {
		t.Errorf("read-only command should not append to history; got %d entries", len(c.History))
	}
}

func TestEmitBashLedger_NilCPNSafe(t *testing.T) {
	emitBashLedger(nil, "mkdir", []string{"x"}, 0, "")
	// Just must not panic.
}

func TestTransitionRoleFromHint(t *testing.T) {
	cases := []struct {
		hint string
		want TransitionRole
	}{
		{"", RoleAssistantTransition},
		{"classifier", RoleClassifyTransition},
		{"router", RoleClassifyTransition},
		{"intent_label", RoleClassifyTransition},
		{"validator", RoleValidateTransition},
		{"lint_check", RoleValidateTransition},
		{"synthesizer", RoleSynthesizeTransition},
		{"architect", RoleArchitectTransition},
		{"planner", RolePlanTransition},
		{"unknown", RoleAssistantTransition},
		{"CLASSIFIER", RoleClassifyTransition}, // case-insensitive
	}
	for _, tc := range cases {
		got := TransitionRoleFromHint(tc.hint)
		if got != tc.want {
			t.Errorf("TransitionRoleFromHint(%q)=%q want %q", tc.hint, got, tc.want)
		}
	}
}
