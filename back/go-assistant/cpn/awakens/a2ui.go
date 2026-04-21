package awakens

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A2UICPNRole is the value the first assistant message uses as `cpn_role`
// so the message-persistence layer can identify awakening turns (AC-001).
const A2UICPNRole = "awakening"

// A2UIMessage is the v0.8 envelope we emit as the first-turn assistant
// message (§4.4). Kept as a thin map[string]any container so we do not
// introduce a hard dependency on a specific A2UI rendering package.
type A2UIMessage struct {
	Components []map[string]any `json:"components"`
}

// Marshal encodes the envelope as canonical JSON.
func (m A2UIMessage) Marshal() ([]byte, error) { return json.Marshal(m) }

// BuildFirstTurnMessage renders an AwakeningReport as the §4.4 A2UI envelope.
// The `title` + surrounding copy are fixed English strings; the frontend
// localises at render time via the user's language preference. BEH-003 is
// honoured by the trailing "What are we working on today?" prompt.
func BuildFirstTurnMessage(r AwakeningReport) A2UIMessage {
	var headline string
	switch {
	case r.NarrativeMD != "":
		// Take the first non-empty line as the headline to keep the card
		// compact (GUD-001 ≤8 lines).
		for line := range strings.SplitSeq(r.NarrativeMD, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				headline = line
				break
			}
		}
	default:
		headline = fmt.Sprintf("Woke up on %s (%s).", r.OS.Name, r.OS.Arch)
	}

	presentNames := make([]string, 0, len(r.PresentTools))
	for _, t := range r.PresentTools {
		presentNames = append(presentNames, t.Name)
	}

	stack := []map[string]any{
		{
			"type":  "text",
			"props": map[string]any{"content": "**Available:** " + strings.Join(presentNames, ", ")},
		},
	}
	if len(r.AbsentTools) > 0 {
		stack = append(stack, map[string]any{
			"type":  "text",
			"props": map[string]any{"content": "**Missing:** " + strings.Join(r.AbsentTools, ", ")},
		})
	}

	card := map[string]any{
		"type":  "card",
		"props": map[string]any{"variant": "info", "title": "brae is ready"},
		"children": []map[string]any{
			{"type": "text", "props": map[string]any{"content": headline}},
			{"type": "divider"},
			{"type": "stack", "children": stack},
		},
	}

	return A2UIMessage{
		Components: []map[string]any{
			card,
			{"type": "text", "props": map[string]any{"content": "What are we working on today?"}},
		},
	}
}
