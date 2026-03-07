package web

import (
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

// Compile-time check that ToolSet implements tool.Set.
var _ tool.Set = (*ToolSet)(nil)

func TestToolSet_Register(t *testing.T) {
	reg := tool.NewRegistry()
	ts := NewToolSet()
	ts.Register(reg)

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}

	want := "web_fetch"
	got := defs[0].Function.Name
	if got != want {
		t.Errorf("tool name = %q, want %q", got, want)
	}
}

func TestToolSet_WithOptions(t *testing.T) {
	ts := NewToolSet(
		WithTimeout(10*time.Second),
		WithMaxBodySize(1024),
		WithMaxContentLength(500),
		WithUserAgent("test-agent/1.0"),
	)

	if ts.pipeline == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if ts.pipeline.cfg.MaxBodySize != 1024 {
		t.Errorf("MaxBodySize = %d, want 1024", ts.pipeline.cfg.MaxBodySize)
	}
	if ts.pipeline.cfg.MaxContentLength != 500 {
		t.Errorf("MaxContentLength = %d, want 500", ts.pipeline.cfg.MaxContentLength)
	}
	if ts.pipeline.cfg.UserAgent != "test-agent/1.0" {
		t.Errorf("UserAgent = %q, want %q", ts.pipeline.cfg.UserAgent, "test-agent/1.0")
	}
}
