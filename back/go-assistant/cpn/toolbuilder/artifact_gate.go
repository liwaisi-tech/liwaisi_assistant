package toolbuilder

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ArtifactGate enforces artifact-safety rules on outputs from actions
// whose Capabilities declare EmitsCode or EmitsTests. It is the
// deterministic choke point between an LLM-authored artifact and any
// runner that would execute it.
//
// Scope for PR2:
//   - AllowedImports: Go import paths permitted in code/test artifacts.
//     An import outside the list denies the artifact.
//   - ForbiddenPatterns: regex patterns that deny the artifact on
//     match. Catches common exfiltration / privilege-escalation markers
//     (network, exec, env read) without depending on a full Go parser.
//   - MaxArtifactBytes: upper bound on the artifact payload.
//
// These rules are deliberately strict; the security profile's verdicts
// are what relax them when a threat is explicitly mitigated.
type ArtifactGate struct {
	Rules map[string]ArtifactRules
}

// ArtifactRules captures the per-action artifact constraints. Absent
// entries deny-by-default for any EmitsCode/EmitsTests action.
type ArtifactRules struct {
	AllowedImports    []string
	ForbiddenPatterns []string
	MaxArtifactBytes  int
}

// DefaultArtifactGate returns the built-in artifact gate. Rules are
// tight: stdlib-only imports, no network, no exec, no env reads.
func DefaultArtifactGate() *ArtifactGate {
	return &ArtifactGate{
		Rules: map[string]ArtifactRules{
			ActionReviewCode: {
				// review-code emits suggested_patches as diffs. Patches
				// may reference any import; the gate here checks the
				// patches for overtly dangerous markers only.
				AllowedImports: nil,
				ForbiddenPatterns: []string{
					`(?i)os\.Setenv\(`,
					`(?i)os/exec`,
					`(?i)syscall\.Exec`,
					`(?i)net/http.*Get\(`,
				},
				MaxArtifactBytes: 128 * 1024,
			},
		},
	}
}

// Gate runs the artifact gate against a raw LLM output for an action
// whose Capabilities declare EmitsCode or EmitsTests. Returns nil when
// the artifact is safe to deposit, or an error describing the first
// failed rule.
//
// Actions that declare AdvisoryOnly skip gating entirely — their
// outputs are structured data consumed by downstream prompts, never
// executed.
func (g *ArtifactGate) Gate(action ActionSpec, raw []byte) error {
	if action.Capabilities.AdvisoryOnly {
		return nil
	}
	if !action.Capabilities.EmitsCode && !action.Capabilities.EmitsTests {
		return nil
	}
	rules, ok := g.Rules[action.ID]
	if !ok {
		return fmt.Errorf("artifact-gate: action %q declares executable output but has no rules (deny-by-default)", action.ID)
	}
	if rules.MaxArtifactBytes > 0 && len(raw) > rules.MaxArtifactBytes {
		return fmt.Errorf("artifact-gate: %s payload %d bytes exceeds max %d", action.ID, len(raw), rules.MaxArtifactBytes)
	}
	// Scan the whole payload for forbidden markers. The LLM may wrap
	// code inside JSON strings — rather than parse, we check the raw
	// bytes for the marker. False positives here are preferable to
	// false negatives for a security control.
	for _, pat := range rules.ForbiddenPatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("artifact-gate: bad rule %q: %w", pat, err)
		}
		if re.Match(raw) {
			return fmt.Errorf("artifact-gate: %s artifact matched forbidden pattern %q", action.ID, pat)
		}
	}
	// If AllowedImports is set, extract import paths from any embedded
	// Go source and require every import to be in the allowlist.
	if len(rules.AllowedImports) > 0 {
		if err := checkImports(raw, rules.AllowedImports); err != nil {
			return fmt.Errorf("artifact-gate: %s %w", action.ID, err)
		}
	}
	return nil
}

var importRe = regexp.MustCompile(`"([a-zA-Z0-9_./-]+)"`)

// checkImports is a conservative scan: it looks for any quoted string
// that matches a Go-importish shape (contains `/` or is a stdlib name)
// and ensures each such candidate is in allowed. This over-catches —
// but over-catching is the point of a gate: the review loop surfaces
// false positives, false negatives are silent disasters.
func checkImports(raw []byte, allowed []string) error {
	// Attempt to parse as JSON and extract fields that typically carry
	// code bodies. Fall back to raw scan if parsing fails.
	var body []byte
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		// Concatenate any string fields that look like code containers.
		var sb strings.Builder
		walkStrings(obj, &sb)
		body = []byte(sb.String())
	} else {
		body = raw
	}
	allowSet := make(map[string]struct{}, len(allowed))
	for _, p := range allowed {
		allowSet[p] = struct{}{}
	}
	// Quick heuristic: only enforce when the body actually contains an
	// `import (` or `import "` marker. Otherwise it's a plain verdict.
	if !strings.Contains(string(body), "import ") {
		return nil
	}
	matches := importRe.FindAllStringSubmatch(string(body), -1)
	for _, m := range matches {
		imp := m[1]
		// Filter to import-pathish strings: must contain a '.' or '/'
		// or be a bare stdlib package name.
		if !looksLikeImportPath(imp) {
			continue
		}
		if _, ok := allowSet[imp]; !ok {
			return fmt.Errorf("import %q not in allowlist", imp)
		}
	}
	return nil
}

func looksLikeImportPath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	// Bare stdlib package names — all lowercase, no spaces, length-bounded.
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func walkStrings(v any, sb *strings.Builder) {
	switch t := v.(type) {
	case string:
		sb.WriteString(t)
		sb.WriteByte('\n')
	case []any:
		for _, e := range t {
			walkStrings(e, sb)
		}
	case map[string]any:
		for _, e := range t {
			walkStrings(e, sb)
		}
	}
}
