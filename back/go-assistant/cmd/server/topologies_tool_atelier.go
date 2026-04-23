package main

// topologies_tool_atelier.go — registers the tool-atelier CPN in the
// FlowLibrary so the architect planner can select it for incoming
// "build a new Go tool" requests.
//
// Spec: spec/spec-architecture-tool-atelier-cpn.md
// Package: cpn/toolbuilder

import (
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/toolbuilder"
)

// toolAtelierHashtags is the hashtag set the architect planner uses to
// retrieve the atelier when a user (or upstream CPN) asks to "build a tool".
var toolAtelierHashtags = []string{
	"tool-atelier", "build-tool", "scaffold", "go", "tdd", "hexagonal",
}

// registerToolAtelier builds a session-less template topology and stores it
// in the FlowLibrary with a stable hash. The architect planner clones the
// entry per-session when it matches an incoming request.
func registerToolAtelier(lib *cpn.FlowLibrary) {
	if lib == nil {
		return
	}
	template := toolbuilder.BuildToolAtelierTopology("template", toolbuilder.AtelierDeps{})

	sig := cpn.ComputeFlowSignature(template, nil)
	sig.Template = "tool-atelier"
	sig.Hashtags = toolAtelierHashtags

	lib.RegisterWithSignature(template, toolbuilder.FlowName, sig, "builtin")
}
