package fanout

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Fakes ───────────────────────────────────────────────────────────────────

type fakeAdapter struct {
	result cpn.ExecResult
	err    error
	calls  int
}

func (f *fakeAdapter) Exec(_ context.Context, _ cpn.ExecRequest) (cpn.ExecResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeAdapter) SpawnPTY(context.Context, cpn.PTYRequest) (cpn.PTYHandle, error) {
	return cpn.PTYHandle{}, errors.New("not implemented")
}
func (f *fakeAdapter) KillPID(context.Context, int, cpn.Signal) error {
	return errors.New("not implemented")
}
func (f *fakeAdapter) ReadFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeAdapter) WriteFile(context.Context, string, []byte, fs.FileMode) error {
	return errors.New("not implemented")
}
func (f *fakeAdapter) Stat(context.Context, string) (cpn.FileInfo, error) {
	return cpn.FileInfo{}, errors.New("not implemented")
}

type denyGate struct{ err error }

func (d denyGate) Check(context.Context, cpn.GateOp) error { return d.err }

// ── Tests ───────────────────────────────────────────────────────────────────

func TestMakeProbeExecutor_Exit0Present(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		result: cpn.ExecResult{ExitCode: 0, Stdout: []byte("/bin/sh\n"), DurationMs: 3},
	}
	entry := AwakeningProbeEntry{
		ID: "cmd-sh", Kind: ProbeKindBinary, Target: "sh", Command: "command -v sh",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter, HostGate: nil}, DefaultPerProbeTimeout)
	tok, err := exec(context.Background(), cpn.Token{})
	if err != nil {
		t.Fatalf("executor returned unexpected error: %v", err)
	}
	res, ok := tok.Payload.(AwakeningProbeResult)
	if !ok {
		t.Fatalf("expected AwakeningProbeResult payload, got %T", tok.Payload)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}
	if res.Stdout != "/bin/sh\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if res.GateDenied {
		t.Fatalf("expected gate not denied")
	}
	if adapter.calls != 1 {
		t.Fatalf("expected 1 adapter call, got %d", adapter.calls)
	}
}

func TestMakeProbeExecutor_Exit1Absent(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{
		result: cpn.ExecResult{ExitCode: 1, Stderr: []byte("not found")},
	}
	entry := AwakeningProbeEntry{
		ID: "cmd-x", Kind: ProbeKindBinary, Target: "nope", Command: "command -v nope",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter}, DefaultPerProbeTimeout)
	tok, _ := exec(context.Background(), cpn.Token{})
	res := tok.Payload.(AwakeningProbeResult)
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d", res.ExitCode)
	}
	if res.Stderr != "not found" {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestMakeProbeExecutor_TimeoutContext(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{err: context.DeadlineExceeded}
	entry := AwakeningProbeEntry{
		ID: "slow", Kind: ProbeKindBinary, Target: "slow", Command: "sleep 10",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter}, 100*time.Millisecond)
	tok, _ := exec(context.Background(), cpn.Token{})
	res := tok.Payload.(AwakeningProbeResult)
	if res.ExitCode != TimeoutExitCode {
		t.Fatalf("expected TimeoutExitCode (%d), got %d", TimeoutExitCode, res.ExitCode)
	}
	if res.Stderr == "" {
		t.Fatalf("expected stderr populated on timeout")
	}
}

func TestMakeProbeExecutor_TimeoutHostError(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{err: cpn.ErrTimeoutHost}
	entry := AwakeningProbeEntry{
		ID: "slow", Kind: ProbeKindBinary, Target: "slow", Command: "sleep 10",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter}, 100*time.Millisecond)
	tok, _ := exec(context.Background(), cpn.Token{})
	res := tok.Payload.(AwakeningProbeResult)
	if res.ExitCode != TimeoutExitCode {
		t.Fatalf("expected TimeoutExitCode on HostErrTimeout, got %d", res.ExitCode)
	}
}

func TestMakeProbeExecutor_OtherExecError(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{err: errors.New("boom")}
	entry := AwakeningProbeEntry{
		ID: "x", Kind: ProbeKindBinary, Target: "x", Command: "command -v x",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter}, DefaultPerProbeTimeout)
	tok, err := exec(context.Background(), cpn.Token{})
	if err != nil {
		t.Fatalf("executor must not return error on adapter failure: %v", err)
	}
	res := tok.Payload.(AwakeningProbeResult)
	if res.ExitCode != TimeoutExitCode {
		t.Fatalf("expected error exit code, got %d", res.ExitCode)
	}
	if res.Stderr == "" {
		t.Fatalf("expected stderr populated on exec error")
	}
}

func TestMakeProbeExecutor_GateDenied(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{}
	gate := denyGate{err: cpn.ErrGateDenied}
	entry := AwakeningProbeEntry{
		ID: "bad", Kind: ProbeKindBinary, Target: "bad", Command: "rm -rf /",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter, HostGate: gate}, DefaultPerProbeTimeout)
	tok, err := exec(context.Background(), cpn.Token{})
	if err != nil {
		t.Fatalf("executor should not return gate errors: %v", err)
	}
	res := tok.Payload.(AwakeningProbeResult)
	if !res.GateDenied {
		t.Fatalf("expected GateDenied=true")
	}
	if res.ExitCode != GateDenyExitCode {
		t.Fatalf("expected GateDenyExitCode, got %d", res.ExitCode)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter must not be invoked on gate deny (calls=%d)", adapter.calls)
	}
}

func TestMakeProbeExecutor_NilAdapter(t *testing.T) {
	t.Parallel()
	entry := AwakeningProbeEntry{
		ID: "x", Kind: ProbeKindBinary, Target: "x", Command: "command -v x",
	}
	exec := makeProbeExecutor(entry, Deps{}, DefaultPerProbeTimeout)
	tok, err := exec(context.Background(), cpn.Token{})
	if err != nil {
		t.Fatalf("nil adapter must not raise, got %v", err)
	}
	res := tok.Payload.(AwakeningProbeResult)
	if !res.GateDenied {
		t.Fatalf("expected GateDenied synthesis when adapter missing")
	}
}

func TestMakeProbeExecutor_ClockInjection(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	steps := []time.Time{start, start.Add(42 * time.Millisecond)}
	i := 0
	clock := func() time.Time {
		t := steps[i]
		if i < len(steps)-1 {
			i++
		}
		return t
	}
	adapter := &fakeAdapter{result: cpn.ExecResult{ExitCode: 0}}
	entry := AwakeningProbeEntry{
		ID: "x", Kind: ProbeKindBinary, Target: "x", Command: "command -v x",
	}
	exec := makeProbeExecutor(entry, Deps{HostAdapter: adapter, Clock: clock}, DefaultPerProbeTimeout)
	tok, _ := exec(context.Background(), cpn.Token{})
	res := tok.Payload.(AwakeningProbeResult)
	// ExecResult.DurationMs is 0, so executor falls back to clock-derived duration.
	if res.DurationMs != 42 {
		t.Fatalf("expected DurationMs=42 from clock, got %d", res.DurationMs)
	}
}
