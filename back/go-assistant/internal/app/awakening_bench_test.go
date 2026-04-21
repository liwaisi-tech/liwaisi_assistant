package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// BenchmarkAwakeningE2E drives runAwakening end-to-end through the legacy
// single-turn topology (BRAE_AWAKENING_MODE=legacy). This keeps the
// benchmark reproducible on any dev laptop — no sandbox (bwrap/firejail)
// and no probe subprocess dependencies — while still exercising the full
// factory wire-up, LLM client round-trip (mocked), snapshot projection,
// and A2UI envelope emission.
//
// REQ-1703 budget: ≤ 12 s total with a mocked LLM.
func BenchmarkAwakeningE2E(b *testing.B) {
	b.Setenv("BRAE_AWAKENING_MODE", "legacy")

	origRead := osReadFileHostID
	origHost := osHostname
	osReadFileHostID = func(_ string) ([]byte, error) { return []byte("bench-host\n"), nil }
	osHostname = func() (string, error) { return "bench-host", nil }
	b.Cleanup(func() {
		osReadFileHostID = origRead
		osHostname = origHost
	})

	reportJSON := `{"os":{"name":"linux","version":"22.04","kernel":"6.1","arch":"amd64"},` +
		`"shell":{"path":"/bin/bash","implementation":"bash"},` +
		`"identity":{"user":"brae","uid":1000,"gid":1000,"home":"/home/brae"},` +
		`"present_tools":[{"name":"git","version":"2.42.0"}],` +
		`"absent_tools":["docker"],` +
		`"capabilities":[],"tools_to_register":[],` +
		`"narrative_md":"bench","probe_trace":[]}`

	llm := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *cpn.LLMRequest) (cpn.LLMResponse, error) {
			return cpn.LLMResponse{Content: reportJSON}, nil
		},
	}

	repo := persist.NewMemoryHostCapabilityRepository()
	quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &SessionService{
		logger:             quietLogger,
		hostCapabilityRepo: repo,
		llm:                llm,
		awakensFactory: func(_ string, _ awakens.Deps) *cpn.CPN {
			b.Fatal("legacy mode must NOT invoke fanout factory")
			return nil
		},
	}

	// Silence the package-level default slog so bench stdout stays parseable
	// by scripts/benchstat_gate.sh. cpn/fire_llm.go emits "llm call" via
	// slog.InfoContext on the default logger regardless of svc.logger.
	origDefault := slog.Default()
	slog.SetDefault(quietLogger)
	b.Cleanup(func() { slog.SetDefault(origDefault) })

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		snap, envelope, _, err := svc.runAwakening(context.Background(), "bench-sess", "bench-user")
		if err != nil {
			b.Fatalf("runAwakening: %v", err)
		}
		if snap.HostID == "" {
			b.Fatal("empty snapshot")
		}
		if len(envelope.Components) == 0 {
			b.Fatal("empty envelope")
		}
	}
	b.StopTimer()

	avg := time.Since(start) / time.Duration(b.N)
	b.ReportMetric(float64(avg.Milliseconds()), "ms/op")

	const budget = 12 * time.Second
	if avg > budget {
		b.Fatalf("REQ-1703 violated: avg awakening %s > budget %s", avg, budget)
	}
}
