package toolapproval

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

// A2UICPNRole marks the first-turn role for synthesized-tool approval
// components so the persistence layer can classify the envelope.
const A2UICPNRole = "synthesized-tool-approval"

// BuildHITLPreview renders a PendingTool as a deterministic A2UI v0.8
// `hitl` envelope (REQ-1301). Byte-stable for fixed input: all slices are
// copied + sorted and timestamps are RFC3339Nano-encoded.
func BuildHITLPreview(pt toolsynth.PendingTool) awakens.A2UIMessage {
	flags := extractFlags(pt.Manifest.Schema)
	subs := extractSubcommands(pt.Manifest.Schema)
	examples := extractExamples(pt.Manifest.HelpText)

	preview := map[string]any{
		"tool_name":         pt.Manifest.Name,
		"namespace":         pt.Manifest.Namespace,
		"version":           pt.Manifest.Version,
		"kind":              pt.Manifest.Kind,
		"origin":            pt.Manifest.Origin,
		"source_sha256":     pt.SourceSHA256,
		"provenance_sha256": pt.ProvenanceSHA256,
		"flags":             flags,
		"subcommands":       subs,
		"examples":          examples,
		"created_at":        pt.CreatedAt.UTC().Format(time.RFC3339Nano),
	}

	hitl := map[string]any{
		"type": "hitl",
		"props": map[string]any{
			"cpn_role": A2UICPNRole,
			"title":    "Approve synthesized tool: " + pt.Manifest.Name,
			"actions":  []string{"approve", "deny"},
			"preview":  preview,
		},
	}

	return awakens.A2UIMessage{Components: []map[string]any{hitl}}
}

// extractFlags decodes the synthesized manifest's JSON Schema and returns
// the flag names sorted. "subcommand" (the enum property injected by
// SC-12) is excluded — it's reported separately under "subcommands".
func extractFlags(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return []string{}
	}
	var obj struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &obj); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(obj.Properties))
	for k := range obj.Properties {
		if k == "subcommand" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func extractSubcommands(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return []string{}
	}
	var obj struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &obj); err != nil {
		return []string{}
	}
	sub, ok := obj.Properties["subcommand"]
	if !ok {
		return []string{}
	}
	out := append([]string(nil), sub.Enum...)
	sort.Strings(out)
	if out == nil {
		return []string{}
	}
	return out
}

// extractExamples scans the rendered help text for the Examples section.
// PendingTool doesn't persist the raw example slice so we recover it from
// the deterministic help-text renderer (see toolsynth.renderHelpText).
func extractExamples(helpText string) []string {
	out := []string{}
	inExamples := false
	start := 0
	for i := 0; i <= len(helpText); i++ {
		if i < len(helpText) && helpText[i] != '\n' {
			continue
		}
		line := helpText[start:i]
		start = i + 1
		if line == "Examples:" {
			inExamples = true
			continue
		}
		if !inExamples {
			continue
		}
		if len(line) >= 2 && line[:2] == "  " {
			out = append(out, line[2:])
			continue
		}
		inExamples = false
	}
	return out
}
