package persist

import (
	"encoding/json"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// previewMaxLen caps the rendered sidebar preview length. Matches the
// historical truncation point in both Postgres and in-memory backends.
const previewMaxLen = 120

// RenderablePreview walks message contents from newest to oldest and returns
// the first user-renderable summary, truncated to previewMaxLen characters.
// Returns "" when the slice is empty or contains only non-renderable rows
// (raw routing JSON, unparseable A2UI envelopes, blank strings).
//
// The contract is shared by every session-list backend (REQ-202): the
// $$a2ui: marker MUST never leak into the returned string (INV-302), and
// known routing-metadata JSON shapes (e.g. t-ask raw output) MUST be
// skipped (REQ-204) as defense in depth against legacy rows persisted
// before REQ-104 took effect.
//
// See spec-process-bugfix-a2ui-rehydration-completion.md §4.2.
func RenderablePreview(contentsNewestFirst []string) string {
	for _, raw := range contentsNewestFirst {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		if strings.HasPrefix(s, cpn.A2UIMarker) {
			if title := extractA2UITitle(s[len(cpn.A2UIMarker):]); title != "" {
				return truncatePreview(title)
			}
			// Marker present but title not extractable — skip and try older message.
			continue
		}

		if looksLikeRoutingJSON(s) {
			continue
		}

		return truncatePreview(s)
	}
	return ""
}

// a2uiEnvelope is a minimal projection of the A2UI payload used solely to
// extract a human-readable title for the sidebar preview. Unknown fields
// are ignored — this MUST NOT be used as a parser for rendering.
type a2uiEnvelope struct {
	Components []struct {
		Props    map[string]any `json:"props"`
		Children []struct {
			Props map[string]any `json:"props"`
		} `json:"children"`
	} `json:"components"`
}

// extractA2UITitle returns the first user-facing label found in the payload:
// (1) components[0].props.title, (2) components[0].children[0].props.label.
// Returns "" on parse failure or when neither field is present and non-empty.
func extractA2UITitle(jsonStr string) string {
	var env a2uiEnvelope
	if err := json.Unmarshal([]byte(jsonStr), &env); err != nil {
		return ""
	}
	if len(env.Components) == 0 {
		return ""
	}
	c0 := env.Components[0]
	if t, ok := c0.Props["title"].(string); ok {
		if t = strings.TrimSpace(t); t != "" {
			return t
		}
	}
	if len(c0.Children) > 0 {
		if l, ok := c0.Children[0].Props["label"].(string); ok {
			if l = strings.TrimSpace(l); l != "" {
				return l
			}
		}
	}
	return ""
}

// looksLikeRoutingJSON narrowly matches JSON envelopes known to be routing
// metadata (e.g. t-ask raw output, t-classify output) so legacy rows that
// pre-date REQ-104 are skipped in the preview lookup. Intentionally
// over-conservative: only matches if the string starts with `{` AND
// contains a known routing key. Plain JSON objects that happen to be
// user-authored content remain renderable.
func looksLikeRoutingJSON(s string) bool {
	if !strings.HasPrefix(s, "{") {
		return false
	}
	return strings.Contains(s, `"restated_goal"`) ||
		strings.Contains(s, `"classification"`)
}

// truncatePreview clips the string to previewMaxLen bytes. Operates on bytes
// rather than runes to match the historical behavior of both backends; UTF-8
// boundaries inside multi-byte characters are tolerated by downstream
// consumers (the value is rendered as text, not re-decoded).
func truncatePreview(s string) string {
	if len(s) > previewMaxLen {
		return s[:previewMaxLen]
	}
	return s
}
