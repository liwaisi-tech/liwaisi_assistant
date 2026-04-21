package awakens

import (
	"slices"
	"strings"
)

// IntrospectionClassName is the Host-gate policy class identifier (§4.5,
// SEC-001). The gate auto-approves commands in this class without HITL.
const IntrospectionClassName = "introspection"

// introspectionAllowCommands is the verbatim commands the gate auto-approves.
// Kept in-sync with default.yaml's `introspection.allow_commands` list.
var introspectionAllowCommands = []string{
	"uname",
	"whoami",
	"id",
	"env",
	"printenv",
	"hostname",
	"arch",
}

// introspectionAllowPrefixes are the prefix rules. A command matches when the
// (normalised) input starts with one of these strings.
var introspectionAllowPrefixes = []string{
	"command -v ",
	"which ",
	"ls ",
	"cat /etc/",
	"cat /proc/",
	"head -n ",
	"tail -n ",
	"busybox --list",
	"getconf ",
}

// introspectionDenyTokens are substrings that disqualify an otherwise-
// allowed command (SEC-003). Matches default.yaml deny_if_args_contain.
var introspectionDenyTokens = []string{
	">",
	">>",
	"|",
	"&&",
	"||",
	";",
	"$(",
	"`",
	"/data/secrets",
	"/.ssh",
	".env",
	"..",
}

// IsIntrospection reports whether command is an auto-approved introspection
// invocation. The check is intentionally strict: any composition primitive
// (pipe, redirect, command substitution) disqualifies the command even if
// its head matches an allowed binary. Non-introspection commands during
// awakening are SILENTLY denied (CON-003/AC-005).
//
// The input is normalised in the same way as the Host-gate matcher: leading
// whitespace stripped, runs of whitespace collapsed.
func IsIntrospection(command string) bool {
	norm := normalise(command)
	if norm == "" {
		return false
	}
	for _, tok := range introspectionDenyTokens {
		if strings.Contains(norm, tok) {
			return false
		}
	}
	// Bare-command case: exact match OR "<cmd>" followed by whitespace/arg.
	head, _, _ := strings.Cut(norm, " ")
	if slices.Contains(introspectionAllowCommands, head) {
		return true
	}
	for _, prefix := range introspectionAllowPrefixes {
		if strings.HasPrefix(norm, prefix) {
			return true
		}
	}
	return false
}

// normalise trims/collapses whitespace so the comparison is stable.
func normalise(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return b.String()
}
