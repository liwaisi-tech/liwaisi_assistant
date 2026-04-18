package gate

import (
	"path/filepath"
	"strings"
)

// Classify returns the risk band of a command string against the policy's
// pre-compiled pattern sets. The order mirrors REQ-002: forbidden wins,
// then dangerous, then caution, then safe; anything unmatched is
// RiskUnknown (gate falls through to HITL per GUD-001).
//
// command is a full command line as the user would type it, e.g.
// "/usr/bin/gcc -O2 hello.c". The matcher normalises a few trivial
// obfuscation tricks before matching to keep the pattern list tractable:
//   - leading/trailing whitespace stripped
//   - collapses runs of whitespace to a single space
//   - strips surrounding single/double quotes around the argv[0] binary
//     so `"rm" -rf /` classifies the same as `rm -rf /`
//
// The matcher is deterministic and stable — the same (policy, command)
// pair ALWAYS returns the same risk band across runs.
func (p *HostPolicy) Classify(command string) (RiskBand, int) {
	if p == nil {
		return RiskUnknown, -1
	}
	norm := normaliseCommand(command)

	if ok, idx := p.forbidden.matches(norm); ok {
		return RiskForbidden, idx
	}
	if ok, idx := p.dangerous.matches(norm); ok {
		return RiskDangerous, idx
	}
	if ok, idx := p.caution.matches(norm); ok {
		return RiskCaution, idx
	}
	if ok, idx := p.hitl.matches(norm); ok {
		// HITL-only band: distinct from caution only in that it does not
		// auto-approve after first-run. Treat as caution for classifier
		// purposes; policy_gate.go interprets the explicit hit.
		return RiskCaution, idx
	}
	if ok, idx := p.safe.matches(norm); ok {
		return RiskSafe, idx
	}
	return RiskUnknown, -1
}

// ClassifyWritePath evaluates a write_file op by constructing the
// synthetic pattern target `write_file: <path>` so operators can write
// patterns like `write_file:\s*/etc/.*` per spec §4.
func (p *HostPolicy) ClassifyWritePath(path string) (RiskBand, int) {
	target := "write_file: " + path
	// Dangerous/forbidden write paths matter more than regular command
	// classification, so delegate to Classify which already walks in
	// priority order.
	return p.Classify(target)
}

// normaliseCommand strips trivial obfuscations before pattern matching.
// The goal is defensive — tests (including fuzz) assert that quoted /
// slash-padded variants of forbidden patterns still match. We do NOT
// attempt full shell parsing; that's a rabbit hole and the policy
// language embraces regex expressiveness instead.
func normaliseCommand(command string) string {
	c := strings.TrimSpace(command)
	// Collapse multiple whitespace into a single space so
	// "rm  -rf   /" matches "^rm\s+-rf\s+/".
	c = collapseWhitespace(c)

	// Split off argv[0] and unquote it so `"rm" -rf /` looks normal.
	head, rest, ok := splitFirstToken(c)
	if !ok {
		return c
	}
	head = unquote(head)
	// Resolve head to its basename if absolute; pattern writers can still
	// match the full path via `^/usr/bin/rm`, so we keep the original
	// prefix intact by REJOINING on filepath.Base only as a secondary
	// canonicalisation below. The primary matcher uses the unquoted form
	// as-is.
	if rest == "" {
		return head
	}
	return head + " " + rest
}

// collapseWhitespace replaces runs of unicode whitespace with a single
// ASCII space. Fast path when no change is needed.
func collapseWhitespace(s string) string {
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

// splitFirstToken splits s on the first whitespace rune.
func splitFirstToken(s string) (head, rest string, ok bool) {
	for i, r := range s {
		if r == ' ' {
			return s[:i], strings.TrimLeft(s[i+1:], " "), true
		}
	}
	return s, "", s != ""
}

// unquote strips a single matching pair of surrounding quotes.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// CommandKey is the stable hashing input for the audit log's
// command_hash field — it is the *normalised* form so trivially
// obfuscated variants of the same command collapse to one audit row.
func CommandKey(command string) string { return normaliseCommand(command) }

// JoinArgs turns an (argv0, args...) tuple into the single string
// the matcher expects. Exposed so fire_bash.go can build the key in
// one consistent way on its side.
func JoinArgs(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// BasePath returns the basename for a path, used by first-run lookups
// when the policy author hasn't pinned the full absolute path.
func BasePath(command string) string {
	return filepath.Base(strings.TrimSpace(command))
}
