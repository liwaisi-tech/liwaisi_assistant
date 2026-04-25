package helpparse

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Compose builds the per-binary help-parser sub-CPN. For each entry in
// binaries the composer emits the 5-transition pipeline:
//
//	t-help-invoke-long-<bin>  → p-help-long-<bin>
//	t-help-invoke-short-<bin> → p-help-short-<bin>
//	t-help-merge-<bin>        (AND-join long + short) → p-help-merged-<bin>
//	t-help-llm-parse-<bin>    → p-help-parsed-<bin>
//	t-help-validate-<bin>     → p-help-result-<bin>
//
// Determinism: binaries are sorted by Binary name before transition/place
// construction, so map iteration order never leaks into the topology.
//
// Compose caps at MaxBinaries. SC-11 is bounded per-session so the sub-CPN
// stays small even for hosts with many unknown binaries.
func Compose(sessionID string, binaries []HelpInput, deps Deps) (*cpn.CPN, error) {
	if len(binaries) == 0 {
		return nil, ErrNoBinaries
	}
	if len(binaries) > MaxBinaries {
		return nil, fmt.Errorf("%w: got %d max %d", ErrTooManyBinaries, len(binaries), MaxBinaries)
	}

	// Deduplicate and normalise.
	seen := make(map[string]struct{}, len(binaries))
	norm := make([]HelpInput, 0, len(binaries))
	for _, b := range binaries {
		name := strings.TrimSpace(b.Binary)
		path := strings.TrimSpace(b.Path)
		if name == "" || path == "" {
			return nil, fmt.Errorf("helpparse: invalid binary entry (binary=%q path=%q)", b.Binary, b.Path)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		norm = append(norm, HelpInput{Binary: name, Path: path})
	}
	sort.SliceStable(norm, func(i, j int) bool { return norm[i].Binary < norm[j].Binary })

	places := map[string]*cpn.Place{
		PlaceTriggerID: cpn.NewPlace(PlaceTriggerID, cpn.ColorString, cpn.SpaceComputation),
	}
	transitions := make(map[string]*cpn.Transition, len(norm)*5)

	for _, b := range norm {
		longPlace := PlaceHelpLongPrefix + b.Binary
		shortPlace := PlaceHelpShortPrefix + b.Binary
		mergedPlace := PlaceHelpMergedPrefix + b.Binary
		parsedPlace := PlaceHelpParsedPrefix + b.Binary
		resultPlace := PlaceHelpResultPrefix + b.Binary

		places[longPlace] = cpn.NewPlace(longPlace, cpn.ColorArtifact, cpn.SpaceComputation)
		places[shortPlace] = cpn.NewPlace(shortPlace, cpn.ColorArtifact, cpn.SpaceComputation)
		places[mergedPlace] = cpn.NewPlace(mergedPlace, cpn.ColorArtifact, cpn.SpaceComputation)
		places[parsedPlace] = cpn.NewPlace(parsedPlace, cpn.ColorArtifact, cpn.SpaceComputation)
		places[resultPlace] = cpn.NewPlace(resultPlace, cpn.ColorArtifact, cpn.SpaceComputation)

		longID := TransitionInvokeLongPrefix + b.Binary
		tLong := cpn.NewTransition(longID, cpn.NodeKindTool,
			[]string{PlaceTriggerID}, []string{longPlace})
		tLong.ToolName = longID
		tLong.Executor = makeInvokeExecutor(b.Binary, b.Path, HelpVariantLong, deps)
		transitions[longID] = tLong

		shortID := TransitionInvokeShortPrefix + b.Binary
		tShort := cpn.NewTransition(shortID, cpn.NodeKindTool,
			[]string{PlaceTriggerID}, []string{shortPlace})
		tShort.ToolName = shortID
		tShort.Executor = makeInvokeExecutor(b.Binary, b.Path, HelpVariantShort, deps)
		transitions[shortID] = tShort

		mergeID := TransitionMergePrefix + b.Binary
		tMerge := cpn.NewTransition(mergeID, cpn.NodeKindTool,
			[]string{longPlace, shortPlace}, []string{mergedPlace})
		tMerge.ToolName = mergeID
		tMerge.ToolHandler = makeMergeHandler(b.Binary, mergedPlace)
		transitions[mergeID] = tMerge

		llmID := TransitionLLMParsePrefix + b.Binary
		tLLM := cpn.NewTransition(llmID, cpn.NodeKindTool,
			[]string{mergedPlace}, []string{parsedPlace})
		tLLM.ToolName = llmID
		tLLM.ToolHandler = makeLLMParseHandler(b.Binary, parsedPlace, deps)
		transitions[llmID] = tLLM

		valID := TransitionValidatePrefix + b.Binary
		tVal := cpn.NewTransition(valID, cpn.NodeKindTool,
			[]string{parsedPlace}, []string{resultPlace})
		tVal.ToolName = valID
		tVal.ToolHandler = makeValidateHandler(b.Binary, resultPlace)
		transitions[valID] = tVal
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-helpparse", sessionID),
		"brae-awakens-helpparse",
		1,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.SeedFunc = func(cc *cpn.CPN) { seedTrigger(cc, len(norm)) }
	seedTrigger(c, len(norm))

	if err := assertFlatTopology(c); err != nil {
		return nil, err
	}
	return c, nil
}

func seedTrigger(c *cpn.CPN, binaries int) {
	trigger, ok := c.Places[PlaceTriggerID]
	if !ok {
		return
	}
	// Each binary has TWO invoke transitions both reading from the trigger;
	// deposit 2 tokens per binary so the invoke fan-out is enabled.
	for i := 0; i < 2*binaries; i++ {
		_ = trigger.Deposit(&cpn.Token{
			Color:   cpn.ColorString,
			Space:   cpn.SpaceComputation,
			Payload: "go",
		})
	}
}

func assertFlatTopology(c *cpn.CPN) error {
	for id, t := range c.Transitions {
		if t.Kind == cpn.NodeKindInstantiate || t.Kind == cpn.NodeKindSubNet {
			return fmt.Errorf("helpparse: sub-CPN must be flat: transition %q kind=%q", id, t.Kind)
		}
	}
	return nil
}

// ResultPlaceIDs returns, in Binary-sorted order, the IDs of every per-binary
// result place produced by Compose. SC-12 uses this to wire its aggregator
// to the helpparse outputs without knowing the internal naming scheme.
func ResultPlaceIDs(binaries []HelpInput) []string {
	seen := make(map[string]struct{}, len(binaries))
	names := make([]string, 0, len(binaries))
	for _, b := range binaries {
		n := strings.TrimSpace(b.Binary)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = PlaceHelpResultPrefix + n
	}
	return out
}

// Ensure context import stays used under `unused` linters even when the
// executor/composer references are only indirect.
var _ = context.Background
