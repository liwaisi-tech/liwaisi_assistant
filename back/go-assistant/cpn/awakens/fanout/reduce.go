package fanout

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
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
		slog.Debug("awakening reducer probe",
			"probe_id", r.ProbeID, "kind", r.Kind, "target", r.Target,
			"exit", r.ExitCode, "stdout_preview", previewString(r.Stdout, 120), "gate_denied", r.GateDenied)
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
		case ProbeKindInfo:
			applyInfoProbe(&report, r)
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

// idLineRE parses the first fields of `id` output: uid=NNN(name) gid=NNN(grp).
var idLineRE = regexp.MustCompile(`uid=(\d+)(?:\(([^)]+)\))?\s+gid=(\d+)`)

// applyInfoProbe folds a single info-kind result into the structured
// OS/Shell/Identity fields of report. A failed probe (exit != 0) leaves the
// corresponding field untouched so stale zero values never replace real data
// that a sibling probe already wrote.
func applyInfoProbe(report *AwakeningReport, r AwakeningProbeResult) {
	stdout := strings.TrimSpace(r.Stdout)
	if r.ExitCode != 0 || stdout == "" {
		return
	}
	switch r.Target {
	case InfoTargetOSName:
		// os-release key=value lines. Prefer NAME / VERSION_ID.
		for _, line := range strings.Split(stdout, "\n") {
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			val = strings.Trim(strings.TrimSpace(val), `"`)
			switch strings.TrimSpace(key) {
			case "NAME":
				if report.OS.Name == "" {
					report.OS.Name = val
				}
			case "VERSION_ID":
				if report.OS.Version == "" {
					report.OS.Version = val
				}
			}
		}
	case InfoTargetOSKernel:
		report.OS.Kernel = firstLine(stdout)
	case InfoTargetOSArch:
		report.OS.Arch = firstLine(stdout)
	case InfoTargetShellPath:
		report.Shell.Path = firstLine(stdout)
	case InfoTargetBusyboxApp:
		report.Shell.Implementation = firstLine(stdout)
	case InfoTargetUser:
		report.Identity.User = firstLine(stdout)
	case InfoTargetIdentity:
		m := idLineRE.FindStringSubmatch(firstLine(stdout))
		if len(m) >= 4 {
			if uid, err := strconv.Atoi(m[1]); err == nil {
				report.Identity.UID = uid
			}
			if report.Identity.User == "" && m[2] != "" {
				report.Identity.User = m[2]
			}
			if gid, err := strconv.Atoi(m[3]); err == nil {
				report.Identity.GID = gid
			}
		}
	case InfoTargetHome:
		report.Identity.Home = firstLine(stdout)
	case InfoTargetHostname:
		report.Identity.Hostname = firstLine(stdout)
	}
}

// previewString clips s to at most n chars for log previews.
func previewString(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
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
