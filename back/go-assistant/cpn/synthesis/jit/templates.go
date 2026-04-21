package jit

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TemplateID identifies one of the v0.1 composer templates.
type TemplateID string

const (
	// TemplateAuto lets the composer choose via selectTemplate.
	TemplateAuto TemplateID = "auto"
	// TemplateParallelFanout fans the intent out to N tool branches then
	// joins the results via jit-aggregate.
	TemplateParallelFanout TemplateID = "parallel-fanout"
	// TemplateSequentialPipeline chains tools N→N+1 via intermediate places.
	TemplateSequentialPipeline TemplateID = "sequential-pipeline"
)

// pipelineCueRegex is English-only per BEH-004. Spanish and other languages
// fall through to parallel-fanout in v0.1.
var pipelineCueRegex = regexp.MustCompile(`(?i)\b(then|after|pipe|next)\b|→`)

// selectTemplate is deterministic: explicit override wins, otherwise we
// require Lang ∈ {"", "en", "en-*"} AND the pipeline cue regex to match
// before picking sequential-pipeline.
func selectTemplate(override TemplateID, intent cpn.Intent) TemplateID {
	if override == TemplateParallelFanout || override == TemplateSequentialPipeline {
		return override
	}
	if isEnglishLang(intent.Lang) && pipelineCueRegex.MatchString(intent.NL) {
		return TemplateSequentialPipeline
	}
	return TemplateParallelFanout
}

func isEnglishLang(lang string) bool {
	if lang == "" || lang == "en" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(lang), "en-")
}

// buildParallelFanout emits the minimal parallel topology:
//
//	p-input -> t-fanout -> p-result-i ↻ t-tool-i -> p-result-i -> t-aggregate -> p-output
//
// Each p-result-i serves as both the tool's input and output place (the
// tool transition is an in-place colour transform intent→artifact). This
// is a legitimate CPN self-loop on a NodeKindTool — the adversarial lint
// exempts tool transitions (REQ-LINT-ADV, §4.4 inspired layout).
//
// For N matches: 2 + N places and 2 + N transitions. For N=2: 4 + 4
// (AC-001).
func buildParallelFanout(id string, matches []cpn.ToolMatch) cpn.TopologyDraft {
	d := cpn.TopologyDraft{
		ID:       id,
		Role:     "jit-tool-flow",
		Metadata: map[string]string{},
		Places: []cpn.PlaceSpec{
			{ID: "p-input", Color: string(cpn.ColorString), Space: string(cpn.SpaceObservation)},
			{ID: "p-output", Color: string(cpn.ColorString), Space: string(cpn.SpaceSurface)},
		},
	}

	fanoutOutputs := make([]string, 0, len(matches))
	aggregateInputs := make([]string, 0, len(matches))

	for i, m := range matches {
		resultID := fmt.Sprintf("p-result-%d", i)
		toolTrID := fmt.Sprintf("t-tool-%d", i)

		d.Places = append(d.Places,
			cpn.PlaceSpec{ID: resultID, Color: string(cpn.ColorArtifact), Space: string(cpn.SpaceComputation)},
		)
		d.Transitions = append(d.Transitions, cpn.TransitionSpec{
			ID:       toolTrID,
			Kind:     string(cpn.NodeKindTool),
			Inputs:   []string{resultID},
			Outputs:  []string{resultID},
			ToolName: m.QualifiedName,
		})

		fanoutOutputs = append(fanoutOutputs, resultID)
		aggregateInputs = append(aggregateInputs, resultID)
	}

	d.Transitions = append([]cpn.TransitionSpec{{
		ID:           "t-fanout",
		Kind:         string(cpn.NodeKindObserver),
		Inputs:       []string{"p-input"},
		Outputs:      fanoutOutputs,
		ExecutorFunc: PrimitiveFanout,
	}}, d.Transitions...)
	d.Transitions = append(d.Transitions, cpn.TransitionSpec{
		ID:           "t-aggregate",
		Kind:         string(cpn.NodeKindObserver),
		Inputs:       aggregateInputs,
		Outputs:      []string{"p-output"},
		ExecutorFunc: PrimitiveAggregate,
	})
	d.InitialMarking = []cpn.MarkingSpec{{Place: "p-input", Count: 1}}
	return d
}

// buildSequentialPipeline emits a linear chain:
//
//	p-input -> t-step-0 -> p-mid-0 -> t-step-1 -> p-mid-1 -> ... -> p-output
//
// For N matches: N+1 places (p-input, p-mid-0 ... p-mid-(N-2), p-output)
// and N transitions. Single match degenerates to p-input -> t-step-0 ->
// p-output.
func buildSequentialPipeline(id string, matches []cpn.ToolMatch) cpn.TopologyDraft {
	d := cpn.TopologyDraft{
		ID:       id,
		Role:     "jit-tool-flow",
		Metadata: map[string]string{},
		Places: []cpn.PlaceSpec{
			{ID: "p-input", Color: string(cpn.ColorString), Space: string(cpn.SpaceObservation)},
			{ID: "p-output", Color: string(cpn.ColorArtifact), Space: string(cpn.SpaceSurface)},
		},
	}

	for i, m := range matches {
		inPlace := "p-input"
		if i > 0 {
			inPlace = fmt.Sprintf("p-mid-%d", i-1)
		}
		outPlace := "p-output"
		if i < len(matches)-1 {
			outPlace = fmt.Sprintf("p-mid-%d", i)
			d.Places = append(d.Places, cpn.PlaceSpec{
				ID:    outPlace,
				Color: string(cpn.ColorArtifact),
				Space: string(cpn.SpaceComputation),
			})
		}
		d.Transitions = append(d.Transitions, cpn.TransitionSpec{
			ID:       fmt.Sprintf("t-step-%d", i),
			Kind:     string(cpn.NodeKindTool),
			Inputs:   []string{inPlace},
			Outputs:  []string{outPlace},
			ToolName: m.QualifiedName,
		})
	}
	d.InitialMarking = []cpn.MarkingSpec{{Place: "p-input", Count: 1}}
	return d
}
