package cpn

import "testing"

func TestRankScore_LowerLLMCallsWins(t *testing.T) {
	w := DefaultRankingWeights()
	recA := ExecutionRecord{LLMCallCount: 2, TotalCostUSD: 0.05}
	recB := ExecutionRecord{LLMCallCount: 5, TotalCostUSD: 0.05}

	scoreA := RankScore(&recA, w)
	scoreB := RankScore(&recB, w)

	if scoreA >= scoreB {
		t.Errorf("scoreA (%f) should be < scoreB (%f): fewer LLM calls should win", scoreA, scoreB)
	}
}

func TestRankScore_LowerCostWins(t *testing.T) {
	w := DefaultRankingWeights()
	recA := ExecutionRecord{LLMCallCount: 3, TotalCostUSD: 0.01}
	recB := ExecutionRecord{LLMCallCount: 3, TotalCostUSD: 0.10}

	scoreA := RankScore(&recA, w)
	scoreB := RankScore(&recB, w)

	if scoreA >= scoreB {
		t.Errorf("scoreA (%f) should be < scoreB (%f): lower cost should win", scoreA, scoreB)
	}
}

func TestRankScore_CustomWeights(t *testing.T) {
	w := RankingWeights{LLMCallWeight: 10.0, CostWeight: 0.1}
	rec := ExecutionRecord{LLMCallCount: 2, TotalCostUSD: 5.0}

	got := RankScore(&rec, w)
	want := 2.0*10.0 + 5.0*0.1 // 20.5

	if got != want {
		t.Errorf("RankScore = %f, want %f", got, want)
	}
}

func TestRankScore_ZeroCost(t *testing.T) {
	w := DefaultRankingWeights()
	rec := ExecutionRecord{LLMCallCount: 3, TotalCostUSD: 0.0}

	got := RankScore(&rec, w)
	want := 3.0

	if got != want {
		t.Errorf("RankScore = %f, want %f (cost=0 degrades to LLM-count-only)", got, want)
	}
}

func TestRankExecutions_SortOrder(t *testing.T) {
	w := DefaultRankingWeights()
	records := []ExecutionRecord{
		{CPNID: "B", LLMCallCount: 5, TotalCostUSD: 0.02, Success: true},
		{CPNID: "A", LLMCallCount: 2, TotalCostUSD: 0.05, Success: true},
		{CPNID: "C", LLMCallCount: 1, TotalCostUSD: 0.10, Success: true},
	}

	ranked := RankExecutions(records, w)

	if len(ranked) != 3 {
		t.Fatalf("len = %d, want 3", len(ranked))
	}
	// C (1.10) < A (2.05) < B (5.02)
	if ranked[0].CPNID != "C" {
		t.Errorf("ranked[0] = %q, want C (best score)", ranked[0].CPNID)
	}
	if ranked[1].CPNID != "A" {
		t.Errorf("ranked[1] = %q, want A", ranked[1].CPNID)
	}
	if ranked[2].CPNID != "B" {
		t.Errorf("ranked[2] = %q, want B (worst score)", ranked[2].CPNID)
	}
}

func TestRankExecutions_ExcludesFailed(t *testing.T) {
	w := DefaultRankingWeights()
	records := []ExecutionRecord{
		{CPNID: "ok", LLMCallCount: 3, Success: true},
		{CPNID: "fail-1", LLMCallCount: 1, Success: false},
		{CPNID: "fail-2", LLMCallCount: 0, Success: false},
	}

	ranked := RankExecutions(records, w)

	if len(ranked) != 1 {
		t.Fatalf("len = %d, want 1 (failed excluded)", len(ranked))
	}
	if ranked[0].CPNID != "ok" {
		t.Errorf("ranked[0] = %q, want %q", ranked[0].CPNID, "ok")
	}
}

func TestRankExecutions_Empty(t *testing.T) {
	w := DefaultRankingWeights()

	ranked := RankExecutions(nil, w)
	if ranked != nil {
		t.Errorf("RankExecutions(nil) = %v, want nil", ranked)
	}

	ranked = RankExecutions([]ExecutionRecord{}, w)
	if ranked != nil {
		t.Errorf("RankExecutions([]) = %v, want nil", ranked)
	}
}

func TestRankExecutions_AllFailed(t *testing.T) {
	w := DefaultRankingWeights()
	records := []ExecutionRecord{
		{CPNID: "f1", Success: false},
		{CPNID: "f2", Success: false},
	}

	ranked := RankExecutions(records, w)
	if ranked != nil {
		t.Errorf("all failed: got %v, want nil", ranked)
	}
}

func TestDefaultRankingWeights(t *testing.T) {
	w := DefaultRankingWeights()
	if w.LLMCallWeight != 1.0 {
		t.Errorf("LLMCallWeight = %f, want 1.0", w.LLMCallWeight)
	}
	if w.CostWeight != 1.0 {
		t.Errorf("CostWeight = %f, want 1.0", w.CostWeight)
	}
}
