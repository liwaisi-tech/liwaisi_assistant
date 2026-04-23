package cpn

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HostHITLHandler is the minimal hexagonal seam that fire_bash uses to
// route an ErrRequiresHITL-style gate error through the policy gate's
// HandleRequiresHITL helper without introducing a cpn→infra/host/gate
// import cycle. Infrastructure wraps the gate and its session-backed
// router behind this interface; the cpn package only knows it can ask
// "this opaque error requires HITL, please handle it for me".
//
// The handler MUST:
//   - Return nil when the user approves (the caller then proceeds to
//     execute the command).
//   - Return a *HostError with HostErrCodeGateDenied when the user denies
//     so the transition's ErrorPlace pipeline routes the failure.
//   - Respect ctx cancellation so session teardown never leaks the
//     pending wait (PAT-002).
//
// hitlErr is the original error returned by HostGate.Check; implementations
// are expected to unwrap it via errors.As for the gate-private sentinel.
type HostHITLHandler interface {
	HandleHITL(ctx context.Context, t *Transition, c *CPN, op GateOp, hitlErr error) error
}

// fireBash executes a NodeKindBash transition.
//
// Dispatch:
//   - BashConfig.SessionID == "":  one-shot ExecRequest through HostAdapter.
//   - BashConfig.SessionID != "":  write Stdin to the BashSessionManager
//     session and read chunks until a terminal status is observed.
//
// Streaming (BashConfig.Streaming == true) deposits one ColorShellChunk
// token per stdout/stderr line and emits a non-blocking EventProcessStdout /
// EventProcessStderr event for each chunk.
//
// The final terminal token is always ColorShellResult and carries the full
// captured stdout/stderr plus the exit code and duration.
//
// Error routing:
//   - Gate denial, timeout, and command-not-found return a *HostError so the
//     executor can push them to the transition's ErrorPlace.
//   - Non-zero exits produce ErrNonZeroExit unless BashConfig.AllowNonZeroExit
//     is true, in which case the result is deposited normally.
func fireBash(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if t.BashConfig == nil {
		return nil, 0, fmt.Errorf("transition %s: nil BashConfig", t.ID)
	}
	if c.HostRuntime == nil || c.HostRuntime.Adapter == nil {
		return nil, 0, fmt.Errorf("transition %s: HostRuntime not injected — cannot fire NodeKindBash", t.ID)
	}

	cfg := t.BashConfig

	// Gate check (REQ-003). Even in one-shot mode we consult the gate before
	// touching the adapter so policy substitution (GAP-6) is a pure swap.
	gate := c.HostRuntime.Gate
	if gate != nil {
		gateKind := "exec"
		if cfg.SessionID != "" {
			gateKind = "spawn_pty"
		}
		op := GateOp{
			Kind:      gateKind,
			Command:   cfg.Command,
			Sandbox:   cfg.SandboxProfile,
			SessionID: c.SessionID,
		}
		// Thread the owning session ID so infra/host/gate's
		// SessionIDResolver (and the awakening silent-deny registry)
		// can correlate the ctx back to a session (SEC-004 / CON-003).
		gateCtx := WithSessionID(ctx, c.SessionID)
		if err := gate.Check(gateCtx, op); err != nil {
			// REQ-001..REQ-005: when the gate asks for a human decision we
			// route through the policy-gate's HandleRequiresHITL helper via
			// the HostRuntime.HITLHandler seam. The handler resolves to nil
			// on approve (fall through to Exec/session) or to a terminal
			// *HostError on deny (routed to ErrorPlace as before). Non-HITL
			// errors and nil-handler wirings keep the pre-existing
			// short-circuit behaviour (CON-003).
			if c.HostRuntime.HITLHandler == nil {
				return nil, 0, err
			}
			handled := c.HostRuntime.HITLHandler.HandleHITL(ctx, t, c, op, err)
			if handled != nil {
				return nil, 0, handled
			}
			// handled == nil ⇒ user approved; proceed to Exec below.
		}
	}

	// Emit a "started" event so observers can correlate the run even before
	// the first chunk arrives. Ignored when the CPN has no event bus.
	emitProcessEvent(c, t, EventProcessStarted, ProcessOutputPayload{
		SessionID: cfg.SessionID,
	})

	if cfg.SessionID != "" {
		return fireBashSession(ctx, t, c, consumed)
	}
	return fireBashOneShot(ctx, t, c, consumed)
}

// fireBashOneShot runs an ExecRequest through HostAdapter.Exec and deposits
// a single ColorShellResult token on each output place. When Streaming is
// true the captured stdout is still split into per-line ColorShellChunk
// tokens before the terminal result (line-chunks plus final result, REQ-014
// / REQ-015).
func fireBashOneShot(ctx context.Context, t *Transition, c *CPN, _ []Token) ([]TokenSnapshot, float64, error) {
	cfg := t.BashConfig
	req := ExecRequest{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Stdin:        cfg.Stdin,
		Env:          cfg.Env,
		Cwd:          cfg.Cwd,
		Timeout:      cfg.Timeout,
		Sandbox:      cfg.SandboxProfile,
		AllowNonZero: cfg.AllowNonZeroExit,
	}

	start := time.Now()
	result, err := c.HostRuntime.Adapter.Exec(ctx, req)
	durationMs := time.Since(start).Milliseconds()

	// ErrCommandNotFound / ErrTimeout / gate-denied / other HostErrors are
	// routed through the executor's ErrorPlace pipeline. Nothing to deposit
	// in that case.
	if err != nil {
		if cfg.AllowNonZeroExit && errors.Is(err, ErrNonZeroExit) {
			// Adapter returned a non-zero-exit error even though the caller
			// asked to tolerate them. Convert it back into a normal deposit.
			err = nil
		} else {
			return nil, 0, err
		}
	}

	if !cfg.AllowNonZeroExit && result.ExitCode != 0 && err == nil {
		return nil, 0, NewHostError(
			HostErrCodeNonZeroExit,
			fmt.Sprintf("%s exited with code %d", cfg.Command, result.ExitCode),
			nil,
		)
	}

	// Derive the event PID (domain has no notion of PIDs for one-shot runs;
	// the adapter does not surface it on ExecResult by design). We set 0.
	pid := 0

	// Optional streaming: chunk tokens + non-blocking events.
	var snaps []TokenSnapshot
	if cfg.Streaming {
		stdoutFragments := splitForEmit(result.Stdout, cfg)
		for _, frag := range stdoutFragments {
			tok := newShellChunkToken(c, t, string(frag))
			snaps = append(snaps, tok.Snapshot())
			if err := depositAll(t.OutputPlaces, c, &tok); err != nil {
				return nil, 0, err
			}
			emitProcessEvent(c, t, EventProcessStdout, ProcessOutputPayload{
				PID:  pid,
				Line: string(frag),
			})
		}
		for _, frag := range splitForEmit(result.Stderr, cfg) {
			emitProcessEvent(c, t, EventProcessStderr, ProcessOutputPayload{
				PID:  pid,
				Line: string(frag),
			})
		}
	}

	// Terminal ColorShellResult token (REQ-015).
	resultToken := newShellResultToken(c, t, result, durationMs)
	snaps = append(snaps, resultToken.Snapshot())
	if err := depositAll(t.OutputPlaces, c, &resultToken); err != nil {
		return nil, 0, err
	}

	emitProcessEvent(c, t, EventProcessExit, ProcessOutputPayload{
		PID:      pid,
		ExitCode: result.ExitCode,
	})

	// Best-effort ledger emission for state-changing commands. No-op for
	// read-only or unrecognised commands. See ledger_bash.go.
	emitBashLedger(c, cfg.Command, cfg.Args, result.ExitCode, string(result.Stderr))

	return snaps, 0, nil
}

// fireBashSession writes Stdin to an existing BashSessionManager session and
// reads chunks until the session returns a terminal status on its status
// channel. Streaming is implicit for sessions — every chunk becomes a
// ColorShellChunk token and a non-blocking EventProcessStdout event.
func fireBashSession(ctx context.Context, t *Transition, c *CPN, _ []Token) ([]TokenSnapshot, float64, error) {
	cfg := t.BashConfig
	if c.HostRuntime.Sessions == nil {
		return nil, 0, fmt.Errorf("transition %s: HostRuntime.Sessions is nil, cannot address session %s", t.ID, cfg.SessionID)
	}

	chunks, err := c.HostRuntime.Sessions.Chunks(cfg.SessionID)
	if err != nil {
		return nil, 0, err
	}
	statusCh, err := c.HostRuntime.Sessions.Status(cfg.SessionID)
	if err != nil {
		return nil, 0, err
	}

	start := time.Now()
	if len(cfg.Stdin) > 0 {
		if err := c.HostRuntime.Sessions.Write(ctx, cfg.SessionID, cfg.Stdin); err != nil {
			return nil, 0, err
		}
	}

	// Apply the per-call timeout as a context deadline when the transition
	// does not already carry one. The session itself outlives the call.
	callCtx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	var (
		stdoutBuf bytes.Buffer
		snaps     []TokenSnapshot
		exitCode  int
	)
	for {
		select {
		case <-callCtx.Done():
			if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				return nil, 0, NewHostError(HostErrCodeTimeout, "session write exceeded timeout", callCtx.Err())
			}
			return nil, 0, callCtx.Err()
		case line, ok := <-chunks:
			if !ok {
				// Channel closed — synthesise a terminal result with whatever
				// we captured so the transition still produces a token.
				res := ExecResult{
					Stdout:   stdoutBuf.Bytes(),
					ExitCode: exitCode,
				}
				token := newShellResultToken(c, t, res, time.Since(start).Milliseconds())
				snaps = append(snaps, token.Snapshot())
				if err := depositAll(t.OutputPlaces, c, &token); err != nil {
					return nil, 0, err
				}
				return snaps, 0, nil
			}
			stdoutBuf.Write(line)
			if !bytes.HasSuffix(line, []byte("\n")) {
				stdoutBuf.WriteByte('\n')
			}
			asText := strings.TrimRight(string(line), "\r\n")
			tok := newShellChunkToken(c, t, asText)
			snaps = append(snaps, tok.Snapshot())
			if err := depositAll(t.OutputPlaces, c, &tok); err != nil {
				return nil, 0, err
			}
			emitProcessEvent(c, t, EventProcessStdout, ProcessOutputPayload{
				SessionID: cfg.SessionID,
				Line:      asText,
			})
		case st, ok := <-statusCh:
			if !ok {
				continue
			}
			if st.Kind == "exited" || st.Kind == "error" {
				exitCode = st.ExitCode
				res := ExecResult{
					Stdout:   stdoutBuf.Bytes(),
					ExitCode: exitCode,
				}
				token := newShellResultToken(c, t, res, time.Since(start).Milliseconds())
				snaps = append(snaps, token.Snapshot())
				if err := depositAll(t.OutputPlaces, c, &token); err != nil {
					return nil, 0, err
				}
				emitProcessEvent(c, t, EventProcessExit, ProcessOutputPayload{
					SessionID: cfg.SessionID,
					ExitCode:  exitCode,
				})
				if st.Kind == "error" && st.Err != nil {
					return snaps, 0, st.Err
				}
				if exitCode != 0 && !cfg.AllowNonZeroExit {
					return snaps, 0, NewHostError(
						HostErrCodeNonZeroExit,
						fmt.Sprintf("session %s exited with code %d", cfg.SessionID, exitCode),
						nil,
					)
				}
				return snaps, 0, nil
			}
			// "ready" or other intermediate kinds — keep waiting.
		}
	}
}

// newShellChunkToken builds a fresh ColorShellChunk token stamped with CPN
// origin metadata. Space is resolved from the first output place so the
// token survives Place.Deposit's space-validation check.
func newShellChunkToken(c *CPN, t *Transition, line string) Token {
	space := SpaceComputation
	for _, pid := range t.OutputPlaces {
		if p, ok := c.Places[pid]; ok {
			space = p.Space
			break
		}
	}
	return Token{
		Color:       ColorShellChunk,
		Payload:     line,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindBash,
		Space:       space,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}
}

// newShellResultToken builds a terminal ColorShellResult token.
func newShellResultToken(c *CPN, t *Transition, r ExecResult, durationMs int64) Token {
	space := SpaceComputation
	for _, pid := range t.OutputPlaces {
		if p, ok := c.Places[pid]; ok {
			space = p.Space
			break
		}
	}
	payload := ShellResultPayload{
		ExitCode:   r.ExitCode,
		Stdout:     string(r.Stdout),
		Stderr:     string(r.Stderr),
		DurationMs: durationMs,
		Truncated:  r.Truncated,
	}
	return Token{
		Color:       ColorShellResult,
		Payload:     payload,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindBash,
		Space:       space,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}
}

// depositAll deposits tok into every place id in places.
// Each deposit receives its own copy of the token to avoid shared-state
// surprises downstream.
func depositAll(placeIDs []string, c *CPN, tok *Token) error {
	for _, pid := range placeIDs {
		p, ok := c.Places[pid]
		if !ok {
			return fmt.Errorf("output place %s not found", pid)
		}
		clone := *tok
		clone.Space = p.Space
		if err := p.Deposit(&clone); err != nil {
			return fmt.Errorf("deposit to %s: %w", pid, err)
		}
	}
	return nil
}

// splitLines splits b on newline boundaries, dropping the terminating CR/LF.
// An empty slice is returned for empty input.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []string
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	return out
}

// splitForEmit chops a captured byte buffer into fragments according to
// BashConfig.EmitMode. It returns one fragment per event to emit:
//   - EmitPerLine (default): one fragment per newline-terminated line,
//     dropping the trailing CR/LF. Matches GAP-1 behaviour.
//   - EmitPerChunk: one fragment per BashConfig.EmitChunkBytes bytes.
//     A final shorter fragment is emitted when the payload does not
//     divide evenly (AC-004 asserts the 4096 / 1024 = 4 case).
//
// Each returned slice is a fresh copy so callers may safely store it in
// a Token payload without aliasing the underlying buffer.
func splitForEmit(b []byte, cfg *BashConfig) [][]byte {
	if len(b) == 0 {
		return nil
	}
	mode := EmitPerLine
	if cfg != nil && cfg.EmitMode != "" {
		mode = cfg.EmitMode
	}
	if mode == EmitPerChunk {
		size := DefaultEmitChunkBytes
		if cfg != nil && cfg.EmitChunkBytes > 0 {
			size = cfg.EmitChunkBytes
		}
		out := make([][]byte, 0, (len(b)+size-1)/size)
		for i := 0; i < len(b); i += size {
			end := i + size
			if end > len(b) {
				end = len(b)
			}
			chunk := make([]byte, end-i)
			copy(chunk, b[i:end])
			out = append(out, chunk)
		}
		return out
	}

	// Default: per-line.
	lines := splitLines(b)
	out := make([][]byte, len(lines))
	for i, line := range lines {
		out[i] = []byte(line)
	}
	return out
}

// emitProcessEvent publishes a process-lifecycle event after consulting
// the rate limiter (GAP-9 REQ-004). Successful emissions bump the Emit
// counter on the metrics port; rate-limited or full-channel drops bump
// the Drop counter. All bookkeeping is non-blocking — the executor
// goroutine never stalls on a noisy process.
func emitProcessEvent(c *CPN, t *Transition, kind EventType, payload ProcessOutputPayload) {
	if c == nil {
		return
	}

	var (
		limiter ProcessEventRateLimiter = AllowAllRateLimiter{}
		metrics ProcessEventMetrics     = NoOpProcessEventMetrics{}
	)
	if c.HostRuntime != nil {
		if c.HostRuntime.RateLimiter != nil {
			limiter = c.HostRuntime.RateLimiter
		}
		if c.HostRuntime.Metrics != nil {
			metrics = c.HostRuntime.Metrics
		}
	}

	if !limiter.Allow(payload.SessionID) {
		metrics.OnDrop(payload.SessionID, kind, "rate_limited")
		return
	}

	c.emit(&Event{
		Type:           kind,
		TransitionID:   transitionID(t),
		TransitionKind: NodeKindBash,
		Payload:        payload,
	})
	metrics.OnEmit(payload.SessionID, kind)
}

// transitionID returns the transition ID or "" when t is nil. Used by
// emitProcessEvent for session-manager-originated events that do not
// correspond to a specific transition.
func transitionID(t *Transition) string {
	if t == nil {
		return ""
	}
	return t.ID
}
