package awakens

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// AC-001: toolbox + hashtags round-trip verbatim (after normalisation).
func TestRegisterBatch_TaxonomyRoundTrip_AC001(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "shell-exec", Basis: "sh", Toolbox: "system", Hashtags: []string{"tools", "shell"}},
	}}
	if _, err := RegisterBatch(context.Background(), reg, r, nil); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	if len(reg.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(reg.calls))
	}
	got := reg.calls[0]
	if got.Toolbox != "system" {
		t.Errorf("Toolbox: got %q want %q", got.Toolbox, "system")
	}
	if !reflect.DeepEqual(got.Hashtags, []string{"tools", "shell"}) {
		t.Errorf("Hashtags: got %v", got.Hashtags)
	}
}

// AC-002: empty hashtags still registers AND emits lexicon.tag.drifted.
func TestRegisterBatch_EmptyHashtags_AC002(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "shell-exec", Basis: "sh"},
	}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	if _, err := RegisterBatch(context.Background(), reg, r, logger); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	if len(reg.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(reg.calls))
	}
	out := buf.String()
	if !strings.Contains(out, `"event":"lexicon.tag.drifted"`) {
		t.Errorf("expected lexicon.tag.drifted event in logs; got:\n%s", out)
	}
	if !strings.Contains(out, "awakening: tool registered without hashtags") {
		t.Errorf("expected drift reason in logs; got:\n%s", out)
	}
}

// AC-006: empty toolbox falls back to the awakening namespace default.
func TestRegisterBatch_DefaultToolbox_AC006(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "x", Basis: "sh"},
	}}
	if _, err := RegisterBatch(context.Background(), reg, r, nil); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	if got := reg.calls[0].Toolbox; got != DefaultAwakeningToolbox {
		t.Errorf("Toolbox: got %q want %q", got, DefaultAwakeningToolbox)
	}
}

// AC-007: invalid hashtag tokens are silently dropped; the rest register.
func TestRegisterBatch_InvalidHashtagsDropped_AC007(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "x", Basis: "sh", Hashtags: []string{"#PDF!", "tools", "read"}},
	}}
	if _, err := RegisterBatch(context.Background(), reg, r, nil); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	// NormalizeSet drops "#PDF!" (contains '!'); keeps "tools","read".
	if !reflect.DeepEqual(reg.calls[0].Hashtags, []string{"tools", "read"}) {
		t.Errorf("Hashtags: got %v want [tools read]", reg.calls[0].Hashtags)
	}
}

// AC-015: 9 hashtags → cap to 6 (ascending sort) + one truncation event.
func TestRegisterBatch_CapHashtagsAt6_AC015(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "big", Basis: "sh", Hashtags: []string{
			"tools", "read", "write", "network", "compute", "transform",
			"shell", "fs", "proc",
		}},
	}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	if _, err := RegisterBatch(context.Background(), reg, r, logger); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	tags := reg.calls[0].Hashtags
	if len(tags) != MaxHashtagsPerAwakeningTool {
		t.Fatalf("kept %d tags, want %d", len(tags), MaxHashtagsPerAwakeningTool)
	}
	// After ascending sort the first 6 of {tools,read,write,network,compute,
	// transform,shell,fs,proc} are: compute, fs, network, proc, read, shell.
	want := []string{"compute", "fs", "network", "proc", "read", "shell"}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("kept %v, want %v", tags, want)
	}
	out := buf.String()
	if !strings.Contains(out, `"event":"personality.tooltags.truncated"`) {
		t.Errorf("expected personality.tooltags.truncated event in logs; got:\n%s", out)
	}
}

// Unknown (but non-empty) toolbox → remapped to "general" (GUD-002).
func TestRegisterBatch_UnknownToolboxMapsToGeneral(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistry{}
	r := AwakeningReport{ToolsRegister: []AwakeningToolRegister{
		{Name: "x", Basis: "sh", Toolbox: "wacky-unknown"},
	}}
	if _, err := RegisterBatch(context.Background(), reg, r, nil); err != nil {
		t.Fatalf("RegisterBatch: %v", err)
	}
	if got := reg.calls[0].Toolbox; got != "general" {
		t.Errorf("Toolbox: got %q want general", got)
	}
}
