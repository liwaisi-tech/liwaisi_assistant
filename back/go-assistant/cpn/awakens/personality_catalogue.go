// Package awakens — personality catalogue builder (Chunk 2, Track C of
// spec-architecture-brae-awakening-toolbox-extension.md). Produces the
// "## Environment awareness" block for turn ≥ 2 system prompts, merging
// OS/shell lines with a `### Toolboxes` subsection, and computes the
// personality digest used for cache invalidation (REQ-009).
package awakens

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// CatalogueTruncationEvent records a single toolbox dropped while fitting
// the rendered block under the byte cap (REQ-008, AC-005). One event is
// emitted per drop; downstream observers render these as
// `personality.catalogue.truncated` lifecycle events.
type CatalogueTruncationEvent struct {
	Namespace string
	Reason    string
}

// catalogueTruncationReason is the canonical reason code carried on every
// truncation event emitted by BuildToolboxCatalogue.
const catalogueTruncationReason = "toolbox_catalogue_over_cap"

// catalogueTrailer is the closing sentence appended after the toolbox
// bullets. It invites the agent to emit a `tool_request` rather than
// hallucinate a missing tool (spec §4.3).
const catalogueTrailer = "To request tools beyond what is listed, emit a `tool_request` call with the relevant hashtags."

// BuildToolboxCatalogue renders the combined "Environment awareness" block
// merging OS/shell environment lines with a `### Toolboxes` subsection
// (REQ-006/007, AC-004). Deterministic output; truncates toolboxes from the
// end under the sort `(toolbox != "general") DESC, ToolCount DESC, Namespace
// ASC` when the total rendered block exceeds cap bytes (REQ-008, AC-005).
//
// Returns:
//   - block: the rendered Markdown (≤ cap bytes when cap > 0).
//   - digest: sha256(toolbox_catalogue_bytes || os_line || shell_line ||
//     lexicon_excerpt_bytes) hex-encoded (REQ-009).
//   - truncated: one entry per dropped toolbox with {Namespace, Reason}.
//   - err: non-nil only on programmer error; I/O-free.
func BuildToolboxCatalogue(
	osLine string,
	shellLine string,
	presentTools []string,
	absentTools []string,
	toolboxes []tools.ToolboxManifest,
	lexiconExcerpt []byte,
	cap int,
) (block string, digest string, truncated []CatalogueTruncationEvent, err error) {
	// Copy + sort toolboxes by the stable render order (GUD-003 variant:
	// we lead with non-general, highest-count first, namespace asc as
	// deterministic tie-break). This is also the inverse of the drop
	// order defined in REQ-008 / AC-005 — dropping always happens from
	// the tail of the renderOrder slice.
	renderOrder := make([]tools.ToolboxManifest, len(toolboxes))
	copy(renderOrder, toolboxes)
	sortToolboxesForRender(renderOrder)

	// Render once at full size, then iteratively drop the tail until
	// we fit under the cap. This keeps the hot path allocation-free in
	// the common case (no truncation).
	catalogueBytes := renderCatalogueBytes(renderOrder)
	fullBlock := assembleBlock(osLine, shellLine, presentTools, absentTools, catalogueBytes)

	if cap > 0 {
		for len(fullBlock) > cap && len(renderOrder) > 0 {
			dropped := renderOrder[len(renderOrder)-1]
			renderOrder = renderOrder[:len(renderOrder)-1]
			truncated = append(truncated, CatalogueTruncationEvent{
				Namespace: dropped.Namespace,
				Reason:    catalogueTruncationReason,
			})
			catalogueBytes = renderCatalogueBytes(renderOrder)
			fullBlock = assembleBlock(osLine, shellLine, presentTools, absentTools, catalogueBytes)
		}
	}

	digest = PersonalityDigest([]byte(catalogueBytes), []byte(osLine), []byte(shellLine), lexiconExcerpt)
	return fullBlock, digest, truncated, nil
}

// sortToolboxesForRender orders toolboxes so the tail matches REQ-008's
// drop order: `general` is dropped first (appears last), then the smallest
// non-general by ToolCount ascending, then by Namespace ascending.
//
// Render order (head → tail):
//  1. non-general toolboxes, ToolCount DESC, Namespace ASC
//  2. `general` toolbox(es), ToolCount DESC, Namespace ASC
//
// When the cap is exceeded we drop from the tail, which yields:
// general first → smallest non-general next → ties by namespace asc.
func sortToolboxesForRender(tb []tools.ToolboxManifest) {
	sort.SliceStable(tb, func(i, j int) bool {
		iGen := tb[i].Namespace == "general"
		jGen := tb[j].Namespace == "general"
		if iGen != jGen {
			// non-general (false) should come BEFORE general (true).
			return !iGen
		}
		if tb[i].ToolCount != tb[j].ToolCount {
			return tb[i].ToolCount > tb[j].ToolCount
		}
		return tb[i].Namespace < tb[j].Namespace
	})
}

// renderCatalogueBytes renders only the `### Toolboxes` subsection (header
// + bullets + trailer) for the supplied toolboxes, or the empty string
// when none are supplied. This is the digest input defined in REQ-009.
func renderCatalogueBytes(tb []tools.ToolboxManifest) string {
	if len(tb) == 0 {
		return ""
	}
	var b strings.Builder
	// Pre-size: ~120 bytes per bullet is a safe upper bound.
	b.Grow(64 + len(tb)*160)
	b.WriteString("### Toolboxes\n\n")
	for _, m := range tb {
		fmt.Fprintf(&b, "- **%s** (%d tools): %s\n", m.Namespace, m.ToolCount, toolboxSummary(m))
		if len(m.Hashtags) > 0 {
			fmt.Fprintf(&b, "  Hashtags: %s.\n", strings.Join(m.Hashtags, ", "))
		}
	}
	b.WriteByte('\n')
	b.WriteString(catalogueTrailer)
	b.WriteByte('\n')
	return b.String()
}

// toolboxSummary returns the spec-§4.3-compliant bullet summary text: the
// manifest's explicit Summary when present, otherwise a derived default
// from the namespace + title so the bullet always ends with a period.
func toolboxSummary(m tools.ToolboxManifest) string {
	s := strings.TrimSpace(m.Summary)
	if s == "" {
		if t := strings.TrimSpace(m.Title); t != "" {
			s = t
		} else {
			s = fmt.Sprintf("Tools grouped under %q", m.Namespace)
		}
	}
	// Normalise trailing punctuation so the sentence closes cleanly.
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// assembleBlock composes the full "## Environment awareness" block from
// its parts. The os/shell lines are rendered verbatim (caller owns
// formatting); the catalogue is appended when non-empty.
func assembleBlock(osLine, shellLine string, presentTools, absentTools []string, catalogueBytes string) string {
	var b strings.Builder
	b.Grow(256 + len(catalogueBytes))
	b.WriteString(EnvironmentAwarenessHeader)
	b.WriteString("\n\n")
	if s := strings.TrimSpace(osLine); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if s := strings.TrimSpace(shellLine); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if len(presentTools) > 0 {
		fmt.Fprintf(&b, "Available: %s.\n", strings.Join(presentTools, ", "))
	}
	if len(absentTools) > 0 {
		fmt.Fprintf(&b, "NOT available: %s.\n", strings.Join(absentTools, ", "))
	}
	b.WriteString("If the user asks you to use a missing tool, state it is absent and propose an alternative.\n")
	if catalogueBytes != "" {
		b.WriteByte('\n')
		b.WriteString(catalogueBytes)
	}
	return b.String()
}

// PersonalityDigest computes the personality digest defined in REQ-009:
// sha256(toolbox_catalogue_bytes || os_line || shell_line || lexicon_excerpt),
// hex-encoded. Exposed as a free function so callers outside awakens can
// reproduce the digest without re-rendering the block.
func PersonalityDigest(catalogueBytes, osLine, shellLine, lexiconExcerpt []byte) string {
	h := sha256.New()
	h.Write(catalogueBytes)
	h.Write(osLine)
	h.Write(shellLine)
	h.Write(lexiconExcerpt)
	return hex.EncodeToString(h.Sum(nil))
}
