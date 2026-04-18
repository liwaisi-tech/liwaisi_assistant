package cpn

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// BenchmarkBashHighVolumeObserver measures throughput for a noisy
// streaming bash transition whose stdout is observed by a
// NodeKindObserver in the same CPN (GAP-9).
//
// The mock host adapter synthesises N lines of stdout so the benchmark
// is independent of any real shell. Each iteration drives one fire of
// the bash transition followed by a drainObservers pass, which is the
// hot path the spec cares about.
func BenchmarkBashHighVolumeObserver(b *testing.B) {
	const linesPerRun = 500
	// Pre-build the stdout blob once.
	var buf bytes.Buffer
	for i := 0; i < linesPerRun; i++ {
		buf.WriteString("noisy-line-")
		buf.WriteString(strings.Repeat("x", 40))
		buf.WriteByte('\n')
	}
	stdoutBlob := buf.Bytes()

	ctx := context.Background()
	b.ResetTimer()

	var totalObserved int64
	for iter := 0; iter < b.N; iter++ {
		pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
		pOut := NewPlace("p-out", ColorShellChunk, SpaceComputation)
		pEvents := NewPlace("p-events", ColorEvent, SpaceObservation)
		_ = pIn.Deposit(&Token{Color: ColorShellCmd, Space: SpaceComputation, Payload: "go"})

		bash := &Transition{
			ID:           "t-bash",
			Kind:         NodeKindBash,
			InputPlaces:  []string{"p-in"},
			OutputPlaces: []string{"p-out"},
			BashConfig: &BashConfig{
				Command:   "echo",
				Streaming: true,
			},
		}
		obs := &Transition{
			ID:           "obs",
			Kind:         NodeKindObserver,
			OutputPlaces: []string{"p-events"},
			EventFilter: func(e Event) bool {
				return e.Type == EventProcessStdout
			},
		}

		places := map[string]*Place{"p-in": pIn, "p-out": pOut, "p-events": pEvents}
		transitions := map[string]*Transition{bash.ID: bash, obs.ID: obs}
		c := NewCPN("bench", "test", 0, ModeMAS, "sess-b", places, transitions)
		c.HostRuntime = &HostRuntime{
			Adapter: &mockHostAdapter{execResult: ExecResult{
				ExitCode: 0,
				Stdout:   append([]byte(nil), stdoutBlob...),
			}},
			Gate: denyGate{allow: true},
		}

		// Direct fire avoids the run-loop color-mismatch on the
		// terminal ShellResult deposit (not what we're measuring).
		_, _, _ = fireBash(ctx, bash, c, nil)
		drainObservers(ctx, c)
		totalObserved += int64(pEvents.Len())
	}

	b.StopTimer()
	if b.N > 0 && b.Elapsed() > 0 {
		b.ReportMetric(float64(totalObserved)/b.Elapsed().Seconds(), "events/s")
		b.ReportMetric(float64(totalObserved)/float64(b.N), "events/run")
	}
}
