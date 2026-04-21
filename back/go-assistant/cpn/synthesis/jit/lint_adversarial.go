package jit

import (
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Adversarial rule names (REQ-LINT-ADV).
const (
	RuleSelfLoop       = "self-loop"
	RuleDeadEnd        = "dead-end"
	RuleFanoutOverflow = "fanout-overflow"
)

// adversarialMaxFanout is the per-transition output-arc cap.
const adversarialMaxFanout = 8

// AdversarialResult captures the outcome of the JIT adversarial lint pass.
type AdversarialResult struct {
	Passed bool
	Rule   string
	Reason string
}

// adversarialLint enforces the three REQ-LINT-ADV rules on a TopologyDraft.
// Pure, no side effects. Returns the first violation encountered. Rules run
// in order: self-loop → fanout-overflow → dead-end. Fanout-overflow runs
// before dead-end because an overflowing fan-out leaves orphaned places
// downstream; reporting the overflow is more actionable than its symptom.
func adversarialLint(d cpn.TopologyDraft) AdversarialResult {
	// Rule 1: self-loop — any non-tool transition with a place in both
	// Inputs and Outputs is rejected. Tool transitions (NodeKindTool) are
	// exempt because the JIT parallel-fanout template uses intentional
	// in-place colour transforms (intent→artifact on the same p-result-i).
	for _, t := range d.Transitions {
		if t.Kind == string(cpn.NodeKindTool) {
			continue
		}
		in := make(map[string]struct{}, len(t.Inputs))
		for _, p := range t.Inputs {
			in[p] = struct{}{}
		}
		for _, p := range t.Outputs {
			if _, ok := in[p]; ok {
				return AdversarialResult{
					Rule:   RuleSelfLoop,
					Reason: fmt.Sprintf("transition %q has self-loop on place %q", t.ID, p),
				}
			}
		}
	}

	// Rule 2: fanout overflow. Runs before dead-end because an overflowing
	// fanout usually leaves orphaned downstream places.
	for _, t := range d.Transitions {
		if len(t.Outputs) > adversarialMaxFanout {
			return AdversarialResult{
				Rule:   RuleFanoutOverflow,
				Reason: fmt.Sprintf("transition %q has fanout=%d > %d", t.ID, len(t.Outputs), adversarialMaxFanout),
			}
		}
	}

	// Rule 3: dead-end / unreachable. Input places are those appearing in
	// InitialMarking; the designated output is `p-output` by convention.
	// Every other place MUST have both upstream and downstream transitions.
	inputs := make(map[string]struct{}, len(d.InitialMarking))
	for _, m := range d.InitialMarking {
		inputs[m.Place] = struct{}{}
	}
	upstream := make(map[string]int, len(d.Places))
	downstream := make(map[string]int, len(d.Places))
	for _, t := range d.Transitions {
		for _, p := range t.Outputs {
			upstream[p]++
		}
		for _, p := range t.Inputs {
			downstream[p]++
		}
	}
	for _, p := range d.Places {
		_, isInput := inputs[p.ID]
		isOutput := p.ID == "p-output"
		if isInput || isOutput {
			continue
		}
		if upstream[p.ID] == 0 {
			return AdversarialResult{
				Rule:   RuleDeadEnd,
				Reason: fmt.Sprintf("place %q has no upstream transition", p.ID),
			}
		}
		if downstream[p.ID] == 0 {
			return AdversarialResult{
				Rule:   RuleDeadEnd,
				Reason: fmt.Sprintf("place %q has no downstream transition", p.ID),
			}
		}
	}

	return AdversarialResult{Passed: true}
}
