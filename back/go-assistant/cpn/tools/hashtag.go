package tools

import (
	"errors"
	"regexp"
	"strings"
)

// MaxHashtagsPerTool caps the stored hashtag set per entry
// (taxonomy spec CON-004).
const MaxHashtagsPerTool = 12

// ErrInvalidHashtag signals that a token failed hashtag validation and the
// caller explicitly requested registration with it (REQ-011, AC-005). The
// canonical normalization procedure (NormalizeSet) silently drops invalid
// tokens without raising this error; use ValidateExplicit to enforce.
var ErrInvalidHashtag = errors.New("tools: invalid hashtag")

// ErrTooManyHashtags signals that a registration exceeds MaxHashtagsPerTool
// (REQ-011 / AC-006).
var ErrTooManyHashtags = errors.New("tools: too many hashtags")

// hashtagPattern enforces REQ-NORM-001 step (4): first char a..z, then
// 0..31 additional chars in [a-z0-9-].
var hashtagPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// NormalizeHashtag applies the REQ-NORM-001 ordered procedure to a single
// token and returns (normalised, true) on success or ("", false) when the
// token fails validation (ok=false means "drop silently" per step 4).
//
// Steps: (1) strip single leading '#'; (2) trim surrounding whitespace;
// (3) ASCII lowercase; (4) validate against hashtagPattern.
func NormalizeHashtag(tok string) (string, bool) {
	t := strings.TrimSpace(tok)
	t = strings.TrimPrefix(t, "#")
	t = strings.TrimSpace(t)
	t = strings.ToLower(t)
	if !hashtagPattern.MatchString(t) {
		return "", false
	}
	return t, true
}

// NormalizeSet applies REQ-NORM-001 to every token, preserving first-seen
// order and deduplicating after normalisation (step 5). Invalid tokens are
// dropped silently. The returned slice is always non-nil (empty when the
// input contains no valid tokens).
func NormalizeSet(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, raw := range tokens {
		tag, ok := NormalizeHashtag(raw)
		if !ok {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

// ValidateExplicit is the strict form used on public RegisterEntry /
// RegisterManifest paths that want to surface user-supplied formatting
// errors (AC-005) instead of silently dropping. It runs normalisation and
// returns ErrInvalidHashtag on the first token that fails the pattern.
// Empty input returns (nil, nil).
func ValidateExplicit(tokens []string) ([]string, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	if len(tokens) > MaxHashtagsPerTool {
		return nil, ErrTooManyHashtags
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, raw := range tokens {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, ErrInvalidHashtag
		}
		tag, ok := NormalizeHashtag(raw)
		if !ok {
			return nil, ErrInvalidHashtag
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > MaxHashtagsPerTool {
		return nil, ErrTooManyHashtags
	}
	return out, nil
}
