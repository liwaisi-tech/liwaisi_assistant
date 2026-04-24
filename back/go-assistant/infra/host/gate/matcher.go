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
	// Preprocess idiomatic opaque constructs BEFORE unwrap so that a
	// stderr-suppressing `bash -c "cmd 2>/dev/null"` can still unwrap
	// to its safe inner. Without this step the `>` inside the redirect
	// makes unwrap refuse and the whole wrapper drops to Unknown.
	s, preprocessBand := p.preprocessShellSubstitutions(norm)
	if bandSeverity(preprocessBand) > bandSeverity(RiskSafe) {
		return preprocessBand, -1
	}
	// Shell-wrapper unwrap: when the command is `sh -c <inner>` (or bash,
	// any -c / -lc variant), classify the INNER script's intent — not the
	// literal wrapper. A `which python3` wrapped in `sh -c` should be as
	// safe as `which python3` directly. Wrappers like `sh -c "rm -rf /"`
	// still fire the forbidden short-circuit because the inner matches
	// forbidden_patterns after unwrap.
	if inner, ok := unwrapShellWrapper(s); ok {
		s = inner
	}
	return p.classifyMaybeCompound(s)
}

// classifyMaybeCompound classifies s, splitting on safe compound
// operators (&&, ||, ;, |) so `touch a && rm a && echo ok` can stay
// safe instead of HITL-ing the whole pipeline. The band returned is the
// MAX severity across all segments — one dangerous segment taints the
// compound. Redirects (>, <, >>) and command substitution ($(, `) still
// refuse to split and fall through as a single literal: their targets
// aren't tokenised here so we cannot reason about them statically.
func (p *HostPolicy) classifyMaybeCompound(s string) (RiskBand, int) {
	// Whole-string forbidden short-circuit. Some forbidden_patterns span
	// compound operators (e.g. `curl ... | sh`, `wget ... | bash`) — they
	// describe an IDIOM, not two independent segments. Evaluating those
	// against the full literal preserves their semantics even though
	// splitting on `|` would rate each half more leniently.
	if ok, idx := p.forbidden.matches(s); ok {
		return RiskForbidden, idx
	}
	// Classify is the sole entry point that runs preprocessing; recursive
	// calls from the substitution resolver also flow through Classify so
	// by the time we land here the null-redirects and safe $(…) subs are
	// already stripped. Any remaining opaque composition (unresolved $(…),
	// backticks, or genuine write-redirects) forces single-literal
	// classification.
	if containsOpaqueComposition(s) {
		return p.classifySingle(s)
	}
	segs := splitCompoundSegments(s)
	if len(segs) <= 1 {
		return p.classifySingle(s)
	}
	maxBand, maxIdx := RiskSafe, -1
	sawAny := false
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		sawAny = true
		// Recurse so nested `bash -c "x && y"` segments still unwrap.
		if inner, ok := unwrapShellWrapper(seg); ok {
			seg = inner
		}
		b, i := p.classifySingle(seg)
		if bandSeverity(b) > bandSeverity(maxBand) {
			maxBand, maxIdx = b, i
		}
	}
	if !sawAny {
		return RiskUnknown, -1
	}
	return maxBand, maxIdx
}

// classifySingle is the original single-command classifier: it walks the
// pattern buckets in priority order and returns the first match.
func (p *HostPolicy) classifySingle(norm string) (RiskBand, int) {
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

// bandSeverity orders the risk bands for compound aggregation. Forbidden
// beats everything so `ok && rm -rf /` classifies as forbidden. Unknown
// beats safe so a compound with one unclassified segment still surfaces
// a HITL card — we don't silently upgrade unknown → safe just because
// the other legs happened to match the allowlist.
func bandSeverity(b RiskBand) int {
	switch b {
	case RiskForbidden:
		return 5
	case RiskDangerous:
		return 4
	case RiskCaution:
		return 3
	case RiskUnknown:
		return 2
	case RiskSafe:
		return 1
	}
	return 0
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

// unwrapShellWrapper detects `sh -c <inner>` / `bash -c <inner>` and returns
// the inner script when it is a single command. Returns ("", false) when
// the command is not a recognised wrapper OR when the inner script
// contains shell composition primitives (pipes, redirects, chains, command
// substitution) — the caller then treats the whole command as unknown and
// lets HITL gate it. Keeping the "compound → no unwrap" rule means the
// unwrap never silently escalates privileges: only a literal single
// command passes through, which is the exact intent a pattern-based
// classifier can reason about.
//
// input is expected to already be normalised by normaliseCommand (leading
// whitespace stripped, internal whitespace collapsed, argv[0] unquoted).
func unwrapShellWrapper(normCmd string) (string, bool) {
	head, rest, ok := splitFirstToken(normCmd)
	if !ok || rest == "" {
		return "", false
	}
	if !isShellWrapperBinary(head) {
		return "", false
	}
	flagTok, inner, ok := splitFirstToken(rest)
	if !ok || inner == "" {
		return "", false
	}
	if flagTok != "-c" && flagTok != "-lc" {
		return "", false
	}
	inner = strings.TrimSpace(inner)
	inner = unquote(inner)
	if inner == "" {
		return "", false
	}
	if containsShellComposition(inner) {
		return "", false
	}
	return inner, true
}

// isShellWrapperCommand reports whether the first token of command is a
// recognised shell wrapper (bash/sh/absolute variants). Used by the gate
// to skip the first-run SHA ledger for shell invocations — the binary's
// hash carries no classification signal because the matcher already
// evaluated the INNER script via unwrapShellWrapper.
func isShellWrapperCommand(command string) bool {
	head, _, _ := splitFirstToken(strings.TrimSpace(command))
	return isShellWrapperBinary(unquote(head))
}

// isShellWrapperBinary reports whether head is one of the POSIX shell
// binaries we recognise for unwrap. Kept deliberately narrow: exotic
// shells (zsh, fish, ksh) fall through to literal classification so
// their argument conventions are never assumed.
func isShellWrapperBinary(head string) bool {
	switch head {
	case "sh", "bash",
		"/bin/sh", "/bin/bash",
		"/usr/bin/sh", "/usr/bin/bash":
		return true
	}
	return false
}

// containsShellComposition reports whether s uses any shell composition
// operator that would change classification from the single-command view.
// Historically this included `&&`/`||`/`;`/`|` and refused to unwrap them;
// those are now handled by classifyMaybeCompound (which splits and takes
// the max severity) so the wrapper unwrap only guards against shapes we
// genuinely cannot reason about statically: redirects (target is an
// arbitrary path) and command substitution (inner execution).
func containsShellComposition(s string) bool {
	return containsOpaqueComposition(s)
}

// preprocessShellSubstitutions strips idioms that are statically safe
// (null-redirects, substitutions with all-safe inners) so a compound
// that would otherwise fall to Unknown via the opaque check can still
// classify via the normal segment path.
//
// Returns (cleanedString, worstInnerBand). When worstInnerBand is
// anything above RiskSafe the caller short-circuits — we discovered a
// dangerous substitution inside an otherwise-innocent outer shell.
func (p *HostPolicy) preprocessShellSubstitutions(s string) (string, RiskBand) {
	// 1. Strip well-known null-redirects. Order matters: longer forms
	//    before their shorter prefixes so we don't leave dangling chars.
	for _, idiom := range []string{
		" 2>/dev/null", " 2>&1", " &>/dev/null", " >/dev/null", " 1>/dev/null",
		"2>/dev/null", "2>&1", "&>/dev/null", ">/dev/null", "1>/dev/null",
	} {
		s = strings.ReplaceAll(s, idiom, "")
	}
	// 2. Resolve $(…) recursively (single level of nesting; deeper is rare
	//    in day-to-day shell). Inner is classified via Classify — if it
	//    comes back RiskSafe we drop the substitution; otherwise we bubble
	//    the worst band out so the caller can short-circuit.
	worst := RiskSafe
	for pass := 0; pass < 8; pass++ { // bounded loop; 8 nestings is generous
		start := strings.Index(s, "$(")
		if start < 0 {
			break
		}
		depth := 1
		end := -1
		for i := start + 2; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break // unmatched paren; leave it so opaque check still catches it
		}
		inner := s[start+2 : end]
		innerBand, _ := p.Classify(inner)
		if bandSeverity(innerBand) > bandSeverity(worst) {
			worst = innerBand
		}
		if innerBand != RiskSafe {
			// Leave the substitution in place; the caller will see a
			// non-safe worst band and short-circuit without running the
			// segment classifier on a string that contains $(…).
			break
		}
		s = s[:start] + s[end+1:]
	}
	// 3. Backtick substitution — same treatment as $(…). No nesting support.
	for pass := 0; pass < 4; pass++ {
		start := strings.IndexByte(s, '`')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start+1:], '`')
		if end < 0 {
			break
		}
		end += start + 1
		inner := s[start+1 : end]
		innerBand, _ := p.Classify(inner)
		if bandSeverity(innerBand) > bandSeverity(worst) {
			worst = innerBand
		}
		if innerBand != RiskSafe {
			break
		}
		s = s[:start] + s[end+1:]
	}
	return s, worst
}

// containsOpaqueComposition reports whether s uses shell primitives whose
// effect depends on arguments the regex classifier cannot see: redirect
// targets (`>`, `>>`, `<`), command substitution (`$(...)`), backticks.
// When present we fall back to literal classification — a dangerous
// pattern on the whole string still matches, otherwise the command stays
// Unknown and HITL gates it.
func containsOpaqueComposition(s string) bool {
	for _, tok := range []string{">>", ">", "<", "$(", "`"} {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

// splitCompoundSegments splits s on the top-level shell chain operators
// `&&`, `||`, `;`, `|`. Quoted regions (matching `'` or `"`) are skipped
// so `echo "a && b"` is one segment. This is a minimal shell-aware split,
// not a full parser — it intentionally does not handle escapes or nested
// shells; callers that need those refuse to split via the opaque check
// above.
func splitCompoundSegments(s string) []string {
	if s == "" {
		return nil
	}
	var segs []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			cur.WriteByte(c)
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			cur.WriteByte(c)
			continue
		}
		// Two-char operators first.
		if i+1 < len(s) {
			two := s[i : i+2]
			if two == "&&" || two == "||" {
				segs = append(segs, cur.String())
				cur.Reset()
				i++
				continue
			}
		}
		if c == ';' || c == '|' {
			segs = append(segs, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		segs = append(segs, cur.String())
	}
	return segs
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
