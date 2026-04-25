package main

// topologies_tool_creator.go — registers the tool-creator CPN in the
// FlowLibrary so the architect planner can select it for incoming
// "build a new Go tool" requests.
//
// Spec: spec/spec-architecture-tool-creator-cpn.md
// Package: cpn/toolbuilder

import (
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/toolbuilder"
)

// toolCreatorHashtags is the hashtag set the architect planner uses to
// retrieve tool-creator when a user (or upstream CPN) asks to "build a
// tool".
var toolCreatorHashtags = []string{
	"tool-creator", "build-tool", "scaffold", "go", "tdd", "hexagonal",
}

// registerToolCreator builds a session-less template topology and
// stores it in the FlowLibrary with a stable hash. The architect
// planner clones the entry per-session when it matches an incoming
// request.
func registerToolCreator(lib *cpn.FlowLibrary) {
	if lib == nil {
		return
	}
	template := toolbuilder.BuildToolCreatorTopology("template", toolbuilder.ToolCreatorDeps{})

	sig := cpn.ComputeFlowSignature(template, nil)
	sig.Template = "tool-creator"
	sig.Hashtags = toolCreatorHashtags

	lib.RegisterWithSignature(template, toolbuilder.FlowName, sig, "builtin")
}
