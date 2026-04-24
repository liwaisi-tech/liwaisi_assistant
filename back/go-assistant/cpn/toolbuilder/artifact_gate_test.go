package toolbuilder

import (
	"strings"
	"testing"
)

func TestArtifactGate_AdvisoryActionsSkipGate(t *testing.T) {
	g := DefaultArtifactGate()
	action := ActionSpec{ID: ActionSecurityEval, Capabilities: ActionCapabilities{AdvisoryOnly: true}}
	if err := g.Gate(action, []byte(`{"role":"security","threats":[]}`)); err != nil {
		t.Fatalf("advisory action must skip gate: %v", err)
	}
}

func TestArtifactGate_EmitsCodeWithoutRules_Denied(t *testing.T) {
	g := &ArtifactGate{Rules: map[string]ArtifactRules{}}
	action := ActionSpec{ID: "unknown-code-emitter", Capabilities: ActionCapabilities{EmitsCode: true}}
	err := g.Gate(action, []byte(`{"x":1}`))
	if err == nil {
		t.Fatal("code-emitting action with no rules should be denied")
	}
	if !strings.Contains(err.Error(), "no rules") {
		t.Errorf("error should mention missing rules, got: %v", err)
	}
}

func TestArtifactGate_ReviewCode_ForbiddenPatterns(t *testing.T) {
	g := DefaultArtifactGate()
	action := ActionSpec{ID: ActionReviewCode, Capabilities: ActionCapabilities{EmitsCode: true}}
	cases := []struct {
		name     string
		payload  string
		wantFail bool
	}{
		{"clean patch", `{"suggested_patches":[{"path":"main.go","diff":"+fmt.Println(\"hi\")"}]}`, false},
		{"os.Setenv", `{"suggested_patches":[{"path":"main.go","diff":"+os.Setenv(\"k\",\"v\")"}]}`, true},
		{"os/exec", `{"suggested_patches":[{"path":"main.go","diff":"+import \"os/exec\""}]}`, true},
		{"syscall.Exec", `{"suggested_patches":[{"path":"main.go","diff":"+syscall.Exec(\"sh\")"}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := g.Gate(action, []byte(tc.payload))
			if tc.wantFail && err == nil {
				t.Errorf("expected denial, got nil")
			}
			if !tc.wantFail && err != nil {
				t.Errorf("expected accept, got: %v", err)
			}
		})
	}
}

func TestArtifactGate_MaxBytes(t *testing.T) {
	g := DefaultArtifactGate()
	action := ActionSpec{ID: ActionReviewCode, Capabilities: ActionCapabilities{EmitsCode: true}}
	big := make([]byte, 200*1024)
	for i := range big {
		big[i] = '{'
	}
	if err := g.Gate(action, big); err == nil {
		t.Fatal("expected denial for oversized payload")
	}
}

func TestCheckImports_AllowlistEnforced(t *testing.T) {
	allowed := []string{"fmt", "strings"}
	// No import keyword → pass.
	if err := checkImports([]byte(`{"x":"plain text"}`), allowed); err != nil {
		t.Errorf("no imports should pass, got: %v", err)
	}
	// Import inside allowlist → pass.
	ok := `{"diff":"import (\"fmt\")"}`
	if err := checkImports([]byte(ok), allowed); err != nil {
		t.Errorf("allowed import should pass, got: %v", err)
	}
	// Import outside allowlist → fail.
	bad := `{"diff":"import (\"net/http\")"}`
	if err := checkImports([]byte(bad), allowed); err == nil {
		t.Fatal("disallowed import should fail")
	}
}
