package jit

import (
	"context"
	"encoding/json"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// RegisterComposeHook installs the deterministic TaskSpec→topology shortcut
// onto the cpn package (plan i-need-you-make-playful-dongarra.md Phase 3).
// Call once at server bootstrap after Register(safe) so SafeRegistry-backed
// primitives referenced by the emitted topology (jit-fanout, jit-aggregate)
// lint cleanly when fire_synthesize re-lints the blob.
//
// The hook delegates to Compose for parallelism_hint ∈ {"fanout","sequence"}
// and returns (nil, nil) for anything else — fire_synthesize then falls
// through to the LLM authoring loop, preserving existing behaviour for
// free-form intents.
//
// Why this sits in cpn/synthesis/jit rather than cpn/synthesis/bootstrap:
// jit already imports cpn/synthesis (for Lint), so adding the reverse edge
// there would create an import cycle. jit imports cpn but is not imported
// back by cpn, so the hook lives here cleanly.
func RegisterComposeHook() {
	cpn.SetComposeFromTaskSpec(func(ctx context.Context, spec cpn.TaskSpec, sr cpn.SafeRegistryPort) (json.RawMessage, error) {
		if len(spec.ToolsNeeded) == 0 {
			return nil, nil
		}
		var template TemplateID
		switch spec.ParallelismHint {
		case "fanout":
			template = TemplateParallelFanout
		case "sequence":
			template = TemplateSequentialPipeline
		default:
			return nil, nil
		}
		matches := make([]cpn.ToolMatch, 0, len(spec.ToolsNeeded))
		for _, name := range spec.ToolsNeeded {
			matches = append(matches, cpn.ToolMatch{QualifiedName: name})
		}
		matchSet := cpn.ToolMatchSet{Matches: matches}
		intent := cpn.Intent{NL: spec.Intent}
		_, blob, err := Compose(ctx, matchSet, intent, ComposeOptions{
			Template:     template,
			SafeRegistry: sr,
			Cap: cpn.SizeCap{
				MaxPlaces:      spec.Budget.MaxPlaces,
				MaxTransitions: spec.Budget.MaxTransitions,
			},
		})
		if err != nil {
			return nil, err
		}
		return blob, nil
	})
}
