package acl

import (
	"strings"
	"testing"
)

const validYAML = `
rules:
  - binary: git
    binary_sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    flags: ["status"]
    decision: allow
    reason: "introspection"
  - binary: rm
    binary_sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    flags: ["-rf", "/"]
    decision: deny
    reason: "dangerous"
`

func TestLoadACL_ValidRules(t *testing.T) {
	a, err := LoadACLBytes([]byte(validYAML))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(a.Rules) != 2 {
		t.Fatalf("want 2 rules, got %d", len(a.Rules))
	}
	if a.Rules[0].Decision != "allow" {
		t.Fatalf("decision: %s", a.Rules[0].Decision)
	}
}

func TestLoadACL_RejectsMissingSHA256(t *testing.T) {
	y := `rules:
  - binary: git
    decision: allow
`
	_, err := LoadACLBytes([]byte(y))
	if err == nil || !strings.Contains(err.Error(), "missing binary_sha256") {
		t.Fatalf("want missing sha error, got %v", err)
	}
}

func TestLoadACL_RejectsInvalidSHA256(t *testing.T) {
	cases := map[string]string{
		"short":     "abc",
		"uppercase": "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
		"nonhex":    "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	}
	for name, sha := range cases {
		t.Run(name, func(t *testing.T) {
			y := "rules:\n  - binary: git\n    binary_sha256: \"" + sha + "\"\n    decision: allow\n"
			_, err := LoadACLBytes([]byte(y))
			if err == nil || !strings.Contains(err.Error(), "invalid binary_sha256") {
				t.Fatalf("want invalid sha error, got %v", err)
			}
		})
	}
}

func TestLoadACL_RejectsUnknownDecision(t *testing.T) {
	y := `rules:
  - binary: git
    binary_sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    decision: maybe
`
	_, err := LoadACLBytes([]byte(y))
	if err == nil || !strings.Contains(err.Error(), "invalid decision") {
		t.Fatalf("want invalid decision, got %v", err)
	}
}

func TestLoadACL_StrictRejectsUnknownField(t *testing.T) {
	y := `rules:
  - binary: git
    binary_sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    decision: allow
    bogus_field: true
`
	_, err := LoadACLBytes([]byte(y))
	if err == nil {
		t.Fatalf("want strict decode error")
	}
}

func TestLoadACL_FromFile(t *testing.T) {
	// covers LoadACL path via a temp file
	dir := t.TempDir()
	p := dir + "/acl.yaml"
	if err := writeFile(p, []byte(validYAML)); err != nil {
		t.Fatal(err)
	}
	a, err := LoadACL(p)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(a.Rules) != 2 {
		t.Fatalf("want 2, got %d", len(a.Rules))
	}
	_, err = LoadACL(dir + "/missing.yaml")
	if err == nil {
		t.Fatal("want read error")
	}
}
