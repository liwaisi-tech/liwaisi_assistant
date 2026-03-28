package cpn

import "sort"

// RankingWeights configures the ranking formula.
// Lower score = better flow. Operationalizes Axiom A11 (LLM is last resort).
type RankingWeights struct {
	LLMCallWeight float64 // weight for LLM call count (default: 1.0)
	CostWeight    float64 // weight for total cost in USD (default: 1.0)
}

// DefaultRankingWeights returns the default weights {1.0, 1.0} (REQ-010).
func DefaultRankingWeights() RankingWeights {
	return RankingWeights{
		LLMCallWeight: 1.0,
		CostWeight:    1.0,
	}
}

// RankScore computes a score for an execution. Lower is better (REQ-008).
// Formula: float64(rec.LLMCallCount) * w.LLMCallWeight + rec.TotalCostUSD * w.CostWeight
func RankScore(rec *ExecutionRecord, w RankingWeights) float64 {
	return float64(rec.LLMCallCount)*w.LLMCallWeight + rec.TotalCostUSD*w.CostWeight
}

// RankExecutions returns successful records sorted by score ascending (best first) (REQ-009).
// Failed executions are excluded.
func RankExecutions(records []ExecutionRecord, w RankingWeights) []ExecutionRecord {
	var successful []ExecutionRecord
	for i := range records {
		if records[i].Success {
			successful = append(successful, records[i])
		}
	}

	sort.SliceStable(successful, func(i, j int) bool {
		return RankScore(&successful[i], w) < RankScore(&successful[j], w)
	})

	return successful
}
