package host

import (
	"testing"
)

// TestGuard_DenyList_ResistsWhitespaceBypass covers SEC-FIX-007 / AC-007.
//
// The old substring-on-joined check treated args with leading whitespace
// as a bypass: args=[" -rf", " /"] joined as "rm  -rf /" (double space),
// which does NOT contain the literal deny rule "rm -rf /". The
// canonicalising guard must still reject these.
func TestGuard_DenyList_ResistsWhitespaceBypass(t *testing.T) {
	a := NewOSHostAdapter(nil)
	cases := []struct {
		name string
		cmd  string
		args []string
	}{
		{"plain rm -rf /", "rm", []string{"-rf", "/"}},
		{"leading-space args", "rm", []string{" -rf", " /"}},
		{"padded args", "rm", []string{"-rf   ", "   /"}},
		{"path-qualified command", "/bin/rm", []string{"-rf", "/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := a.guard(tc.cmd, tc.args); err == nil {
				t.Fatalf("expected deny-list rejection for %s %v, got nil", tc.cmd, tc.args)
			}
		})
	}
}

// TestGuard_DenyList_AllowsBenign makes sure the canonicalisation does not
// over-match: "rm -rf foo" (not "/") must still succeed.
func TestGuard_DenyList_AllowsBenign(t *testing.T) {
	a := NewOSHostAdapter(nil)
	if err := a.guard("rm", []string{"-rf", "foo"}); err != nil {
		t.Fatalf("unexpected deny for rm -rf foo: %v", err)
	}
	if err := a.guard("echo", []string{"hello", "world"}); err != nil {
		t.Fatalf("unexpected deny for echo: %v", err)
	}
}
