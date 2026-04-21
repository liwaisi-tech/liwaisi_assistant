package helpparse

import "sort"

// ReduceHelpResults filters out failed HelpResults (REQ-1104: parse failures
// must not abort awakening) and returns only the successfully-validated
// HelpSchemas, sorted by Binary name for deterministic downstream consumption.
func ReduceHelpResults(results []HelpResult) []HelpSchema {
	out := make([]HelpSchema, 0, len(results))
	for _, r := range results {
		if r.Err != "" {
			continue
		}
		if r.Schema.Binary == "" {
			continue
		}
		out = append(out, r.Schema)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Binary < out[j].Binary })
	return out
}

// ReduceHelpFailures returns only the failed results, for observability /
// SC-12 "emitted without schema" handling.
func ReduceHelpFailures(results []HelpResult) []HelpResult {
	out := make([]HelpResult, 0)
	for _, r := range results {
		if r.Err == "" {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Binary < out[j].Binary })
	return out
}
