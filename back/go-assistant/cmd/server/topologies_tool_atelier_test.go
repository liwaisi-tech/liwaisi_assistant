package main

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/toolbuilder"
)

func TestRegisterToolAtelier_AddsEntry(t *testing.T) {
	lib := cpn.NewFlowLibrary()
	registerToolAtelier(lib)

	got, ok := lib.Get(toolbuilder.FlowName)
	if !ok {
		t.Fatalf("expected FlowLibrary entry under %q", toolbuilder.FlowName)
	}
	if got == nil {
		t.Fatal("entry CPN is nil")
	}
	if got.Role != toolbuilder.FlowName {
		t.Fatalf("role/flow mismatch: %q want %q", got.Role, toolbuilder.FlowName)
	}
}

func TestRegisterToolAtelier_SignatureHasHashtags(t *testing.T) {
	lib := cpn.NewFlowLibrary()
	registerToolAtelier(lib)

	entry, ok := lib.GetEntry(toolbuilder.FlowName)
	if !ok {
		t.Fatal("no entry")
	}
	if entry.Signature.Template != "tool-atelier" {
		t.Fatalf("expected template 'tool-atelier', got %q", entry.Signature.Template)
	}
	found := false
	for _, h := range entry.Signature.Hashtags {
		if h == "tool-atelier" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hashtag 'tool-atelier' missing from signature: %v", entry.Signature.Hashtags)
	}
	if entry.Origin != "builtin" {
		t.Fatalf("expected origin 'builtin', got %q", entry.Origin)
	}
}

func TestRegisterToolAtelier_NilLibrary(t *testing.T) {
	// Should not panic.
	registerToolAtelier(nil)
}
