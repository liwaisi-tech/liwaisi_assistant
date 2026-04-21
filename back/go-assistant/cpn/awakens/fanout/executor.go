package fanout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// makeProbeExecutor returns the closure installed on each per-probe
// NodeKindTool transition. The executor:
//
//  1. Applies the host-gate (REQ-003). A denied probe never touches the
//     adapter; instead the result carries GateDenied=true and ExitCode =
//     GateDenyExitCode so the reducer can emit a matching note.
//  2. Invokes HostAdapter.Exec with `sh -c <command>` and AllowNonZero=true
//     (GUD-003) — a non-zero exit from `command -v foo` is valid data, not
//     a failure.
//  3. On context.DeadlineExceeded (or any timeout-coded HostError), returns
//     a result with ExitCode = TimeoutExitCode so partial-failure AC-004
//     can distinguish timeouts from gate denials and real exit codes.
//
// The executor never returns an error — the fanout is designed to tolerate
// per-branch failure, so every outcome is modelled as an
// AwakeningProbeResult token (ColorArtifact). This keeps the reducer's
// AND-join wait logic simple: it always receives exactly one token per
// probe place.
func makeProbeExecutor(entry AwakeningProbeEntry, deps Deps, timeout time.Duration) func(context.Context, cpn.Token) (cpn.Token, error) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	return func(ctx context.Context, _ cpn.Token) (cpn.Token, error) {
		result := AwakeningProbeResult{
			ProbeID: entry.ID,
			Kind:    entry.Kind,
			Target:  entry.Target,
			Command: entry.Command,
		}
		// Defensive: a nil adapter means misconfiguration upstream. We
		// return a synthesised "gate denied" result so the reducer still
		// assembles a report and the total-failure path surfaces cleanly.
		if deps.HostAdapter == nil {
			result.ExitCode = GateDenyExitCode
			result.GateDenied = true
			result.Stderr = "host adapter not wired"
			if deps.OnProbeFired != nil {
				deps.OnProbeFired(ctx, result.ProbeID, 0, result.ExitCode)
			}
			return probeResultToken(result), nil
		}
		if deps.HostGate != nil {
			if err := deps.HostGate.Check(ctx, cpn.GateOp{Kind: "exec", Command: entry.Command}); err != nil {
				result.ExitCode = GateDenyExitCode
				result.GateDenied = true
				result.Stderr = err.Error()
				if deps.OnProbeFired != nil {
					deps.OnProbeFired(ctx, result.ProbeID, 0, result.ExitCode)
				}
				return probeResultToken(result), nil
			}
		}

		cmd, args := "sh", []string{"-c", entry.Command}
		if !deps.Sandbox.IsZero() {
			cmd, args = deps.Sandbox.Wrap(cmd, args)
		}
		start := clock()
		execResult, err := deps.HostAdapter.Exec(ctx, cpn.ExecRequest{
			Command:      cmd,
			Args:         args,
			Timeout:      timeout,
			AllowNonZero: true,
		})
		elapsed := clock().Sub(start)
		result.DurationMs = elapsed.Milliseconds()

		if err != nil {
			// Timeout is a first-class outcome: REQ-004 pins ExitCode to
			// TimeoutExitCode so the reducer can distinguish it from a
			// real non-zero exit.
			if isTimeoutErr(ctx, err) {
				result.ExitCode = TimeoutExitCode
				result.Stderr = "timeout"
				if deps.OnProbeFired != nil {
					deps.OnProbeFired(ctx, result.ProbeID, elapsed, result.ExitCode)
				}
				return probeResultToken(result), nil
			}
			// Any other adapter error is surfaced as a negative exit code
			// with the underlying message. We still return the token so
			// the reducer's AND-join does not stall.
			result.ExitCode = TimeoutExitCode
			result.Stderr = fmt.Sprintf("exec error: %v", err)
			if deps.OnProbeFired != nil {
				deps.OnProbeFired(ctx, result.ProbeID, elapsed, result.ExitCode)
			}
			return probeResultToken(result), nil
		}
		result.ExitCode = execResult.ExitCode
		result.Stdout = string(execResult.Stdout)
		result.Stderr = string(execResult.Stderr)
		result.Truncated = execResult.Truncated
		if execResult.DurationMs > 0 {
			result.DurationMs = execResult.DurationMs
		}
		if deps.OnProbeFired != nil {
			deps.OnProbeFired(ctx, result.ProbeID, time.Duration(result.DurationMs)*time.Millisecond, result.ExitCode)
		}
		return probeResultToken(result), nil
	}
}

// probeResultToken builds the ColorArtifact token the reducer consumes.
func probeResultToken(r AwakeningProbeResult) cpn.Token {
	return cpn.Token{
		Color:   cpn.ColorArtifact,
		Space:   cpn.SpaceComputation,
		Payload: r,
	}
}

// isTimeoutErr reports whether err indicates a deadline-exceeded outcome
// from either the Go context layer or the HostAdapter's structured error
// model.
func isTimeoutErr(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, cpn.ErrTimeoutHost) {
		return true
	}
	return false
}
