package fanout

import (
	"context"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestReduceResults_AllSuccess(t *testing.T) {
	t.Parallel()
	results := []AwakeningProbeResult{
		{ProbeID: "cmd-sh", Kind: ProbeKindBinary, Target: "sh",
			ExitCode: 0, Stdout: "/bin/sh"},
		{ProbeID: "cmd-git", Kind: ProbeKindBinary, Target: "git",
			ExitCode: 0, Stdout: "/usr/bin/git"},
		{ProbeID: "cap-python", Kind: ProbeKindCapability, Target: "python-runtime",
			ExitCode: 0, Stdout: "Python 3.11"},
	}
	report := ReduceResults(results)
	if len(report.Binaries) != 2 {
		t.Fatalf("expected 2 binaries, got %d", len(report.Binaries))
	}
	for _, b := range report.Binaries {
		if !b.Present {
			t.Fatalf("binary %s should be present", b.Name)
		}
	}
	if len(report.Capabilities) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(report.Capabilities))
	}
	if !report.Capabilities[0].Satisfied {
		t.Fatalf("capability should be satisfied")
	}
	if len(report.Notes) != 0 {
		t.Fatalf("expected no notes on all-success, got %v", report.Notes)
	}
}

func TestReduceResults_PartialTimeout(t *testing.T) {
	t.Parallel()
	results := []AwakeningProbeResult{
		{ProbeID: "cmd-a", Kind: ProbeKindBinary, Target: "a", ExitCode: 0},
		{ProbeID: "cmd-slow-b", Kind: ProbeKindBinary, Target: "b",
			ExitCode: TimeoutExitCode, Stderr: "timeout"},
		{ProbeID: "cmd-slow-c", Kind: ProbeKindBinary, Target: "c",
			ExitCode: TimeoutExitCode, Stderr: "timeout"},
	}
	report := ReduceResults(results)
	if len(report.Binaries) != 3 {
		t.Fatalf("expected 3 binaries, got %d", len(report.Binaries))
	}
	presentCount := 0
	for _, b := range report.Binaries {
		if b.Present {
			presentCount++
		}
	}
	if presentCount != 1 {
		t.Fatalf("expected 1 present, got %d", presentCount)
	}
	if len(report.Notes) != 2 {
		t.Fatalf("expected 2 notes for the 2 timeouts, got %v", report.Notes)
	}
	joined := strings.Join(report.Notes, "\n")
	for _, id := range []string{"cmd-slow-b", "cmd-slow-c"} {
		if !strings.Contains(joined, id) {
			t.Fatalf("expected probe %s in notes, got %v", id, report.Notes)
		}
	}
}

func TestReduceResults_AllFailedAndGateDenied(t *testing.T) {
	t.Parallel()
	results := []AwakeningProbeResult{
		{ProbeID: "cmd-a", Kind: ProbeKindBinary, Target: "a", ExitCode: 1},
		{ProbeID: "cmd-b", Kind: ProbeKindBinary, Target: "b",
			ExitCode: GateDenyExitCode, GateDenied: true},
		{ProbeID: "cmd-c", Kind: ProbeKindBinary, Target: "c",
			ExitCode: TimeoutExitCode},
	}
	report := ReduceResults(results)
	for _, b := range report.Binaries {
		if b.Present {
			t.Fatalf("binary %s should be absent", b.Name)
		}
	}
	if len(report.Notes) != 2 {
		t.Fatalf("expected 2 notes (gate + timeout), got %v", report.Notes)
	}
	// Notes emitted in sorted-probe-id order.
	if !strings.Contains(report.Notes[0], "cmd-b") || !strings.Contains(report.Notes[0], "gate-denied") {
		t.Fatalf("first note must reference gate-denied cmd-b, got %q", report.Notes[0])
	}
	if !strings.Contains(report.Notes[1], "cmd-c") || !strings.Contains(report.Notes[1], "timeout") {
		t.Fatalf("second note must reference cmd-c timeout, got %q", report.Notes[1])
	}
}

func TestReduceResults_DeterministicOrdering(t *testing.T) {
	t.Parallel()
	a := []AwakeningProbeResult{
		{ProbeID: "z", Kind: ProbeKindBinary, Target: "z", ExitCode: 0},
		{ProbeID: "a", Kind: ProbeKindBinary, Target: "a", ExitCode: 0},
		{ProbeID: "m", Kind: ProbeKindBinary, Target: "m", ExitCode: 0},
	}
	b := []AwakeningProbeResult{
		{ProbeID: "a", Kind: ProbeKindBinary, Target: "a", ExitCode: 0},
		{ProbeID: "m", Kind: ProbeKindBinary, Target: "m", ExitCode: 0},
		{ProbeID: "z", Kind: ProbeKindBinary, Target: "z", ExitCode: 0},
	}
	ra := ReduceResults(a)
	rb := ReduceResults(b)
	if len(ra.Binaries) != len(rb.Binaries) {
		t.Fatalf("binary counts diverge: %d vs %d", len(ra.Binaries), len(rb.Binaries))
	}
	for i := range ra.Binaries {
		if ra.Binaries[i].Name != rb.Binaries[i].Name {
			t.Fatalf("position %d differs: %q vs %q", i, ra.Binaries[i].Name, rb.Binaries[i].Name)
		}
	}
	if ra.Binaries[0].Name != "a" || ra.Binaries[2].Name != "z" {
		t.Fatalf("expected sorted order [a, m, z], got %+v", ra.Binaries)
	}
}

func TestReduce_ToolHandler(t *testing.T) {
	t.Parallel()
	handler := makeReducer()

	consumed := []cpn.Token{
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation,
			Payload: AwakeningProbeResult{ProbeID: "a", Kind: ProbeKindBinary, Target: "a", ExitCode: 0}},
		{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation,
			Payload: AwakeningProbeResult{ProbeID: "b", Kind: ProbeKindBinary, Target: "b", ExitCode: 1}},
	}
	out, err := handler(context.Background(), consumed)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if _, ok := out[PlaceReportID]; !ok {
		t.Fatalf("expected %s token", PlaceReportID)
	}
	if _, ok := out[PlaceEgressID]; !ok {
		t.Fatalf("expected %s token", PlaceEgressID)
	}
	report, ok := out[PlaceReportID].Payload.(AwakeningReport)
	if !ok {
		t.Fatalf("expected AwakeningReport payload")
	}
	if len(report.Binaries) != 2 {
		t.Fatalf("expected 2 binaries, got %d", len(report.Binaries))
	}
}

func TestReduce_ToolHandler_ErrorsOnEmpty(t *testing.T) {
	t.Parallel()
	handler := makeReducer()
	if _, err := handler(context.Background(), nil); err == nil {
		t.Fatalf("expected error on empty consumed")
	}
}

func TestReduce_ToolHandler_ErrorsOnWrongPayload(t *testing.T) {
	t.Parallel()
	handler := makeReducer()
	_, err := handler(context.Background(), []cpn.Token{{Payload: "not a result"}})
	if err == nil {
		t.Fatalf("expected type-mismatch error")
	}
}
