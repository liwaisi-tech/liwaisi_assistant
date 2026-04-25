package toolbuilder

import (
	"strings"
	"testing"
)

func TestLoadArtifactGate_Roundtrip(t *testing.T) {
	yaml := `
version: 1
rules:
  review-code:
    max_artifact_bytes: 65536
    forbidden_patterns:
      - "(?i)os\\.Setenv\\("
    allowed_imports:
      - fmt
`
	g, err := LoadArtifactGate(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("LoadArtifactGate: %v", err)
	}
	r, ok := g.Rules[ActionReviewCode]
	if !ok {
		t.Fatal("review-code rule missing")
	}
	if r.MaxArtifactBytes != 65536 {
		t.Errorf("MaxArtifactBytes = %d, want 65536", r.MaxArtifactBytes)
	}
	if len(r.ForbiddenPatterns) != 1 {
		t.Errorf("forbidden patterns = %d, want 1", len(r.ForbiddenPatterns))
	}
	if len(r.AllowedImports) != 1 || r.AllowedImports[0] != "fmt" {
		t.Errorf("allowed imports = %v", r.AllowedImports)
	}
}

func TestLoadArtifactGate_BadVersion(t *testing.T) {
	yaml := `version: 99
rules: {}
`
	if _, err := LoadArtifactGate(strings.NewReader(yaml)); err == nil {
		t.Error("expected version mismatch to fail")
	}
}

func TestLoadArtifactGate_BadPattern(t *testing.T) {
	yaml := `version: 1
rules:
  review-code:
    forbidden_patterns:
      - "[invalid"
`
	if _, err := LoadArtifactGate(strings.NewReader(yaml)); err == nil {
		t.Error("expected invalid regex to fail at load time")
	}
}

func TestArtifactGate_AdvisoryActionsSkipGate(t *testing.T) {
	g := DefaultArtifactGate()
	// review-spec is advisory-only; gating must be a no-op regardless of
	// payload content.
	action := ActionSpec{ID: ActionReviewSpec, Capabilities: ActionCapabilities{AdvisoryOnly: true}}
	if err := g.Gate(action, []byte(`{"role":"go-eng","approved":true}`)); err != nil {
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
