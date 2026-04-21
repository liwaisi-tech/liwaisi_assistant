package toolapproval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

func fixturePending() toolsynth.PendingTool {
	schema := json.RawMessage(`{"type":"object","properties":{"color":{"type":"string"},"json":{"type":"boolean"},"subcommand":{"type":"string","enum":["list","show"]}}}`)
	return toolsynth.PendingTool{
		Manifest: cpn.ToolManifest{
			Namespace: toolsynth.DefaultNamespace,
			Name:      "rg",
			Version:   toolsynth.DefaultVersion,
			Schema:    schema,
			HelpText:  "rg\nFlags:\n  --color\tColor output\n  --json\tJSON output\nSubcommands:\n  list\tList items\n  show\tShow item\nExamples:\n  rg foo\n  rg --json bar\n",
			Kind:      toolsynth.KindSynthesized,
			Origin:    toolsynth.OriginHelpParser,
		},
		SourceSHA256:     "src-abc",
		ProvenanceSHA256: "prov-def",
		SessionID:        "sess-1",
		CreatedAt:        time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestBuildHITLPreview_IncludesAllFields(t *testing.T) {
	pt := fixturePending()
	msg := BuildHITLPreview(pt)

	if len(msg.Components) != 1 {
		t.Fatalf("want 1 component, got %d", len(msg.Components))
	}
	c := msg.Components[0]
	if c["type"] != "hitl" {
		t.Fatalf("want type=hitl, got %v", c["type"])
	}
	props := c["props"].(map[string]any)
	if props["cpn_role"] != A2UICPNRole {
		t.Fatalf("want cpn_role=%s, got %v", A2UICPNRole, props["cpn_role"])
	}
	preview := props["preview"].(map[string]any)

	if preview["tool_name"] != "rg" {
		t.Errorf("tool_name: %v", preview["tool_name"])
	}
	if preview["kind"] != toolsynth.KindSynthesized {
		t.Errorf("kind: %v", preview["kind"])
	}
	if preview["source_sha256"] != "src-abc" {
		t.Errorf("source_sha256: %v", preview["source_sha256"])
	}
	if preview["provenance_sha256"] != "prov-def" {
		t.Errorf("provenance_sha256: %v", preview["provenance_sha256"])
	}
	flags := preview["flags"].([]string)
	if len(flags) != 2 || flags[0] != "color" || flags[1] != "json" {
		t.Errorf("flags: %v", flags)
	}
	subs := preview["subcommands"].([]string)
	if len(subs) != 2 || subs[0] != "list" || subs[1] != "show" {
		t.Errorf("subcommands: %v", subs)
	}
	examples := preview["examples"].([]string)
	if len(examples) != 2 || examples[0] != "rg foo" || examples[1] != "rg --json bar" {
		t.Errorf("examples: %v", examples)
	}
	if !strings.Contains(preview["created_at"].(string), "2026-04-21") {
		t.Errorf("created_at: %v", preview["created_at"])
	}
}

func TestBuildHITLPreview_ByteStable(t *testing.T) {
	pt := fixturePending()
	a, err := BuildHITLPreview(pt).Marshal()
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	b, err := BuildHITLPreview(pt).Marshal()
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("preview not byte-stable:\n a=%s\n b=%s", a, b)
	}
	// Sanity: envelope includes the hitl component and preview key.
	if !strings.Contains(string(a), `"type":"hitl"`) {
		t.Fatalf("envelope missing hitl type: %s", a)
	}
	if !strings.Contains(string(a), `"provenance_sha256":"prov-def"`) {
		t.Fatalf("envelope missing provenance: %s", a)
	}
}

func TestBuildHITLPreview_EmptySchema(t *testing.T) {
	pt := toolsynth.PendingTool{
		Manifest: cpn.ToolManifest{
			Name: "bare",
			Kind: toolsynth.KindSynthesized,
		},
		ProvenanceSHA256: "prov",
	}
	msg := BuildHITLPreview(pt)
	preview := msg.Components[0]["props"].(map[string]any)["preview"].(map[string]any)
	if flags, _ := preview["flags"].([]string); flags == nil || len(flags) != 0 {
		t.Fatalf("want empty flags slice, got %#v", preview["flags"])
	}
}
