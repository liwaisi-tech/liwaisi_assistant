package jit

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestAdversarialLint_SelfLoopOnObserver(t *testing.T) {
	// Observer kind self-loop is forbidden (tool self-loops are exempt).
	d := cpn.TopologyDraft{
		Places: []cpn.PlaceSpec{
			{ID: "p-input"}, {ID: "p-a"}, {ID: "p-output"},
		},
		Transitions: []cpn.TransitionSpec{
			{ID: "t-bad", Kind: string(cpn.NodeKindObserver), Inputs: []string{"p-a"}, Outputs: []string{"p-a"}},
		},
		InitialMarking: []cpn.MarkingSpec{{Place: "p-input", Count: 1}},
	}
	got := adversarialLint(d)
	if got.Passed || got.Rule != RuleSelfLoop {
		t.Fatalf("result = %+v, want self-loop fail", got)
	}
}

func TestAdversarialLint_ToolSelfLoopAllowed(t *testing.T) {
	// A legitimate parallel-fanout tool self-loop must NOT fail this rule.
	d := cpn.TopologyDraft{
		Places: []cpn.PlaceSpec{
			{ID: "p-input"}, {ID: "p-result-0"}, {ID: "p-output"},
		},
		Transitions: []cpn.TransitionSpec{
			{ID: "t-fanout", Kind: string(cpn.NodeKindObserver), Inputs: []string{"p-input"}, Outputs: []string{"p-result-0"}},
			{ID: "t-tool-0", Kind: string(cpn.NodeKindTool), Inputs: []string{"p-result-0"}, Outputs: []string{"p-result-0"}},
			{ID: "t-agg", Kind: string(cpn.NodeKindObserver), Inputs: []string{"p-result-0"}, Outputs: []string{"p-output"}},
		},
		InitialMarking: []cpn.MarkingSpec{{Place: "p-input", Count: 1}},
	}
	if got := adversarialLint(d); !got.Passed {
		t.Fatalf("tool self-loop wrongly rejected: %+v", got)
	}
}

func TestAdversarialLint_DeadEnd(t *testing.T) {
	d := cpn.TopologyDraft{
		Places: []cpn.PlaceSpec{
			{ID: "p-input"}, {ID: "p-orphan"}, {ID: "p-output"},
		},
		Transitions: []cpn.TransitionSpec{
			{ID: "t-x", Kind: string(cpn.NodeKindObserver), Inputs: []string{"p-input"}, Outputs: []string{"p-output"}},
		},
		InitialMarking: []cpn.MarkingSpec{{Place: "p-input", Count: 1}},
	}
	got := adversarialLint(d)
	if got.Passed || got.Rule != RuleDeadEnd {
		t.Fatalf("result = %+v, want dead-end", got)
	}
}

func TestAdversarialLint_FanoutOverflow(t *testing.T) {
	outs := make([]string, 9)
	places := []cpn.PlaceSpec{{ID: "p-input"}, {ID: "p-output"}}
	for i := range outs {
		id := "p-fan-" + string(rune('a'+i))
		outs[i] = id
		places = append(places, cpn.PlaceSpec{ID: id})
	}
	d := cpn.TopologyDraft{
		Places: places,
		Transitions: []cpn.TransitionSpec{
			{ID: "t-big", Kind: string(cpn.NodeKindObserver), Inputs: []string{"p-input"}, Outputs: outs},
		},
		InitialMarking: []cpn.MarkingSpec{{Place: "p-input", Count: 1}},
	}
	got := adversarialLint(d)
	if got.Passed || got.Rule != RuleFanoutOverflow {
		t.Fatalf("result = %+v, want fanout-overflow", got)
	}
}
