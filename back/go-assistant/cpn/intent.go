package cpn

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Intent is the natural-language + structured hashtag intent emitted by the
// tool-request transition (spec-architecture-brae-tool-request-node — not yet
// implemented). Introduced here so JIT has a typed input.
type Intent struct {
	// NL is the free-form natural-language text.
	NL string
	// Hashtags is the canonical, normalised tag vector.
	Hashtags []string
	// Lang is a BCP-47 language tag when detected; empty means unspecified.
	Lang string
}

// Digest returns sha256 over (NL || "\x00" || sorted(Hashtags) joined by
// "\x00" || "\x00" || Lang). Stable across calls on the same value.
func (i Intent) Digest() string {
	h := sha256.New()
	h.Write([]byte(i.NL))
	h.Write([]byte{0})
	tags := append([]string(nil), i.Hashtags...)
	sort.Strings(tags)
	for idx, t := range tags {
		if idx > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(t))
	}
	h.Write([]byte{0})
	h.Write([]byte(i.Lang))
	return hex.EncodeToString(h.Sum(nil))
}
