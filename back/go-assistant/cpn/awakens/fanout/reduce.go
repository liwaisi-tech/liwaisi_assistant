package fanout

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// makeReducer returns the ToolHandler installed on TransitionReduceID. It
// consumes one token from each probe-result place (AND-join per REQ-005),
// assembles a single AwakeningReport, and deposits it on both the report
// and egress terminal places (PAT-002).
//
// Ordering is fixed: results are sorted by probe ID so the emitted slices
// are deterministic regardless of token-arrival order. NFR-002 relies on
// this: re-running the reducer on the same inputs yields identical output.
func makeReducer() func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("%s: no probe results consumed", TransitionReduceID)
		}
		results := make([]AwakeningProbeResult, 0, len(consumed))
		for i, tok := range consumed {
			r, ok := tok.Payload.(AwakeningProbeResult)
			if !ok {
				return nil, fmt.Errorf("%s: token[%d] payload is %T, want AwakeningProbeResult",
					TransitionReduceID, i, tok.Payload)
			}
			results = append(results, r)
		}
		report := ReduceResults(results)
		reportToken := cpn.Token{
			Color:   cpn.ColorArtifact,
			Space:   cpn.SpaceComputation,
			Payload: report,
		}
		egressToken := reportToken
		return map[string]cpn.Token{
			PlaceReportID: reportToken,
			PlaceEgressID: egressToken,
		}, nil
	}
}

// ReduceResults assembles a deterministic AwakeningReport from the per-probe
// results. Exported so the topology-wiring teammate can unit-test the
// round-trip without spinning up a CPN.
//
// Classification rules:
//
//   - Kind="binary": append to Binaries[] with Present = (ExitCode == 0).
//     Non-zero exit (including absence) and timeouts yield Present = false.
//   - Kind="capability": append to Capabilities[] with Satisfied =
//     (ExitCode == 0); Evidence carries a short trace pointer.
//   - Timeouts (ExitCode == TimeoutExitCode) and gate-denials add a note
//     referencing the probe ID so operators can diagnose the absence.
//
// Notes are emitted in probe-ID order for determinism.
func ReduceResults(results []AwakeningProbeResult) AwakeningReport {
	sorted := make([]AwakeningProbeResult, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].ProbeID < sorted[j].ProbeID
	})

	report := AwakeningReport{
		Binaries:      []AwakeningReportBinary{},
		Capabilities:  []AwakeningReportCapability{},
		Notes:         []string{},
		ToolsRegister: []AwakeningReportToolRegister{},
	}

	for _, r := range sorted {
		switch r.Kind {
		case ProbeKindBinary:
			report.Binaries = append(report.Binaries, AwakeningReportBinary{
				Name:    r.Target,
				Present: r.ExitCode == 0,
				Version: firstLine(r.Stdout),
			})
		case ProbeKindCapability:
			evidence := []string{}
			if trimmed := strings.TrimSpace(r.Stdout); trimmed != "" {
				evidence = append(evidence, trimmed)
			}
			report.Capabilities = append(report.Capabilities, AwakeningReportCapability{
				Name:      r.Target,
				Satisfied: r.ExitCode == 0,
				Evidence:  evidence,
			})
		}
		// Notes for diagnostic outcomes — ordered by the outer loop so
		// they inherit the sorted-ID ordering.
		switch {
		case r.GateDenied:
			report.Notes = append(report.Notes,
				fmt.Sprintf("probe %s gate-denied", r.ProbeID))
		case r.ExitCode == TimeoutExitCode:
			report.Notes = append(report.Notes,
				fmt.Sprintf("probe %s timeout", r.ProbeID))
		}
	}
	return report
}

// firstLine returns the first non-empty line of s, trimmed of trailing
// whitespace. Used as a coarse "version" proxy for binary probes whose
// stdout typically carries a path or a version header.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}
