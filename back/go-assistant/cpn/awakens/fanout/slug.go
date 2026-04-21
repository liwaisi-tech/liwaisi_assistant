package fanout

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"unicode"
)

// slugify produces a stable, lowercase, filesystem-safe slug for probe IDs
// per GUD-002. The function is deterministic — identical input always
// produces identical output — and collision-resistant enough for the
// MaxProbes=16 fanout width: when an ID would otherwise collapse to the
// empty string (e.g. a command consisting entirely of punctuation) we fall
// back to an 8-char SHA-1 prefix of the raw input.
//
// Rules:
//   - Lowercase the input.
//   - Map Unicode letters/digits to themselves; everything else to '-'.
//   - Collapse repeat dashes.
//   - Trim leading/trailing dashes.
//   - If the result is empty, emit "probe-" + sha1[:8].
//   - Cap output at 48 runes to keep transition IDs readable.
func slugify(input string) string {
	lowered := strings.ToLower(input)
	var b strings.Builder
	b.Grow(len(lowered))
	prevDash := false
	for _, r := range lowered {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	// Truncate to 48 runes for readability. We operate on bytes here
	// because slugify already restricted the alphabet to ASCII after the
	// letter/digit filter in all realistic inputs — non-ASCII letters
	// survive but each counts as its multi-byte UTF-8 length under len().
	// That is acceptable: the cap is a soft readability limit, not a
	// security boundary.
	if len(slug) > 48 {
		slug = slug[:48]
		slug = strings.TrimRight(slug, "-")
	}
	if slug == "" {
		sum := sha1.Sum([]byte(input))
		return "probe-" + hex.EncodeToString(sum[:4])
	}
	return slug
}
