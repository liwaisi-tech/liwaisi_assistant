package cpn

import (
	"testing"
	"time"
)

func TestExecutionTracker_Finalize_IncludesProvenance(t *testing.T) {
	tracker := newExecutionTracker("cpn", "root", 0, "sess")
	tracker.SetProvenance("flow-abc", "reuse")
	rec := tracker.Finalize(true, 0.01)
	if rec.FlowID != "flow-abc" {
		t.Errorf("FlowID = %q", rec.FlowID)
	}
	if rec.Strategy != "reuse" {
		t.Errorf("Strategy = %q", rec.Strategy)
	}
}

func TestExecutionTracker_Finalize_ProvenanceDefaultsEmpty(t *testing.T) {
	tracker := newExecutionTracker("cpn", "root", 0, "sess")
	rec := tracker.Finalize(true, 0)
	if rec.FlowID != "" || rec.Strategy != "" {
		t.Errorf("expected zero provenance for unstamped tracker, got FlowID=%q Strategy=%q",
			rec.FlowID, rec.Strategy)
	}
}

func TestCPN_SetRunProvenance(t *testing.T) {
	c := &CPN{}
	c.SetRunProvenance("f1", "compose")
	if c.RunProvenance.FlowID != "f1" || c.RunProvenance.Strategy != "compose" {
		t.Errorf("RunProvenance = %+v", c.RunProvenance)
	}
}

// Sanity check that the existing RankScore formula still works with the
// extended ExecutionRecord.
func TestRankScore_IgnoresProvenance(t *testing.T) {
	rec := ExecutionRecord{
		LLMCallCount: 2,
		TotalCostUSD: 0.5,
		FlowID:       "f",
		Strategy:     "reuse",
		CompletedAt:  time.Now(),
	}
	got := RankScore(&rec, DefaultRankingWeights())
	if got != 2.5 {
		t.Errorf("RankScore = %v, want 2.5 (formula unchanged)", got)
	}
}
