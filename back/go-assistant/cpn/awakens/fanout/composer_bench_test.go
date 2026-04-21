package fanout

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkProbeFanoutCompose exercises Compose with a plan sized at the
// SC-17 probe-fanout budget (N=16 LLM-emitted probes; the composer then
// augments with its mandatory info probes, staying under MaxProbes=24).
//
// REQ-1702 budget: ≤ 5 ms per compose on a dev laptop. The in-bench guard
// below fails the benchmark if the average exceeds that ceiling so a
// regression lands as a test failure, not just a number drift.
func BenchmarkProbeFanoutCompose(b *testing.B) {
	const n = 16
	plan := AwakeningProbePlan{
		Rationale:         "bench plan",
		TimeoutPerProbeMs: 1000,
		Probes:            make([]AwakeningProbeEntry, 0, n),
	}
	for i := 0; i < n; i++ {
		plan.Probes = append(plan.Probes, AwakeningProbeEntry{
			ID:      fmt.Sprintf("bench-probe-%02d", i),
			Kind:    ProbeKindBinary,
			Target:  fmt.Sprintf("tool-%02d", i),
			Command: fmt.Sprintf("command -v tool-%02d", i),
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := Compose("bench-sess", plan, Deps{}); err != nil {
			b.Fatalf("Compose: %v", err)
		}
	}
	b.StopTimer()

	avg := time.Since(start) / time.Duration(b.N)
	b.ReportMetric(float64(avg.Nanoseconds())/1e6, "ms/op")

	const budget = 5 * time.Millisecond
	if avg > budget {
		b.Fatalf("REQ-1702 violated: avg compose %s > budget %s (N=%d probes)", avg, budget, n)
	}
}
