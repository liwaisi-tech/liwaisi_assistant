package acl

import "testing"

const shaA = "1111111111111111111111111111111111111111111111111111111111111111"
const shaB = "2222222222222222222222222222222222222222222222222222222222222222"

func mkACL(rules ...Rule) *ACL { return &ACL{Rules: rules} }

func TestMatch_BinarySHA256Mismatch(t *testing.T) {
	a := mkACL(Rule{Binary: "git", BinarySHA256: shaA, Decision: "allow"})
	if _, ok := a.Match(shaB, nil); ok {
		t.Fatal("expected miss on sha mismatch")
	}
}

func TestMatch_EmptyFlagsWildcard(t *testing.T) {
	a := mkACL(Rule{Binary: "git", BinarySHA256: shaA, Decision: "allow"})
	d, ok := a.Match(shaA, []string{"anything", "--goes"})
	if !ok || d.Kind != "allow" {
		t.Fatalf("expected allow wildcard, got %+v ok=%v", d, ok)
	}
}

func TestMatch_FlagPrefixMatch(t *testing.T) {
	a := mkACL(Rule{Binary: "git", BinarySHA256: shaA, Flags: []string{"--version"}, Decision: "allow"})
	d, ok := a.Match(shaA, []string{"--version", "--long"})
	if !ok || d.Kind != "allow" {
		t.Fatalf("prefix match failed: %+v ok=%v", d, ok)
	}
}

func TestMatch_FlagMismatch(t *testing.T) {
	a := mkACL(Rule{Binary: "git", BinarySHA256: shaA, Flags: []string{"--version"}, Decision: "allow"})
	if _, ok := a.Match(shaA, []string{"--delete"}); ok {
		t.Fatal("expected miss on flag mismatch")
	}
	if _, ok := a.Match(shaA, []string{}); ok {
		t.Fatal("expected miss: rule needs --version but argv is empty")
	}
}

func TestMatch_FirstMatchWins(t *testing.T) {
	a := mkACL(
		Rule{Binary: "git", BinarySHA256: shaA, Decision: "allow", Reason: "first"},
		Rule{Binary: "git", BinarySHA256: shaA, Decision: "deny", Reason: "second"},
	)
	d, ok := a.Match(shaA, []string{"x"})
	if !ok || d.Kind != "allow" || d.Reason != "first" {
		t.Fatalf("first-match-wins violated: %+v", d)
	}
}

func TestMatch_AllowDenyHitl_PropagateReason(t *testing.T) {
	cases := []struct {
		kind, reason string
	}{
		{"allow", "safe introspection"},
		{"deny", "dangerous op"},
		{"hitl", "needs approval"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			a := mkACL(Rule{Binary: "b", BinarySHA256: shaA, Decision: c.kind, Reason: c.reason})
			d, ok := a.Match(shaA, nil)
			if !ok || d.Kind != c.kind || d.Reason != c.reason {
				t.Fatalf("reason/decision not propagated: %+v", d)
			}
			if d.RuleID == "" {
				t.Fatal("empty RuleID")
			}
		})
	}
}

func TestMatch_NilACL(t *testing.T) {
	var a *ACL
	if _, ok := a.Match(shaA, nil); ok {
		t.Fatal("nil ACL should miss")
	}
}
