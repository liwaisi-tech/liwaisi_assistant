// Package cpn — host.go defines the hexagonal port for OS-level execution.
//
// GAP-1: This file adds HostAdapter, HostGate, and BashSessionManager
// interfaces plus their supporting domain types. Implementations live in
// infra/host/ (os_adapter.go, session_manager.go, gate.go).
//
// Hexagonal constraint: this file MUST NOT import os/exec, os, syscall, or
// github.com/creack/pty. Only stdlib types (context, errors, io/fs, time)
// are permitted so the domain stays platform-neutral. A static check at
// scripts/check_hexagonal.sh enforces the invariant.
package cpn

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// ── Signals ─────────────────────────────────────────────────────────────────

// Signal identifies a POSIX signal delivered to a process.
// Kept as a string-typed enum in the domain so the infra adapter is free
// to map it to its native signal representation.
type Signal string

const (
	// SignalTerm is a graceful termination request (SIGTERM).
	SignalTerm Signal = "SIGTERM"

	// SignalKill is an unconditional terminate (SIGKILL).
	SignalKill Signal = "SIGKILL"

	// SignalInt is an interrupt (SIGINT).
	SignalInt Signal = "SIGINT"
)

// ── Sandbox profiles ────────────────────────────────────────────────────────

// SandboxProfile enumerates execution sandboxes. v1 accepts the value but
// does not enforce it beyond SEC-001/SEC-002; GAP-6 wires policy enforcement.
type SandboxProfile string

const (
	// SandboxNone disables sandboxing (default for v1 allow-all gate).
	SandboxNone SandboxProfile = "none"

	// SandboxReadonly marks the process as read-only (v2).
	SandboxReadonly SandboxProfile = "readonly"

	// SandboxFSJail confines filesystem access to the jail root (v2).
	SandboxFSJail SandboxProfile = "fsjail"

	// SandboxNetworkOff disables network access (v2).
	SandboxNetworkOff SandboxProfile = "network-off"
)

// ── Request / response value objects ────────────────────────────────────────

// ExecRequest describes a one-shot command invocation.
type ExecRequest struct {
	Command      string
	Args         []string
	Stdin        []byte
	Env          []string // KEY=VALUE pairs.
	Cwd          string
	Timeout      time.Duration
	Sandbox      SandboxProfile
	AllowNonZero bool
}

// ExecResult carries the captured output and exit status of a command.
//
// Truncated is true when stdout or stderr was capped at the adapter's
// configured maximum (1 MiB by default).
type ExecResult struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	DurationMs int64
	Truncated  bool
}

// PTYRequest describes a long-lived pseudo-terminal session.
type PTYRequest struct {
	Command string
	Args    []string
	Env     []string
	Cwd     string
	Rows    uint16
	Cols    uint16
	Sandbox SandboxProfile

	// EventSink, when non-nil, receives EventProcessStarted /
	// EventProcessStdout / EventProcessStderr / EventProcessExit events
	// for the lifetime of the session (GAP-9 REQ-002). The session
	// manager MUST invoke it non-blockingly — a stalled sink drops the
	// event via the rate limiter / metrics port.
	//
	// Callers typically pass cpn.CPN.PublishProcessEvent (see
	// CPN.SessionEventSink) so process events fan out to the same
	// observer-drain pipeline as in-process emits.
	EventSink func(Event)
}

// BashSessionOpener is the extended BashSessionManager contract that
// permits the caller to inject an event sink at Open time. Adapters
// implementing this interface emit process events directly without the
// caller having to poll the chunk channel.
//
// The base BashSessionManager remains intact so existing callers that
// only care about chunks/status keep working; new callers looking for
// observer-ready streams type-assert to BashSessionOpener.
type BashSessionOpener interface {
	BashSessionManager
	OpenWithEvents(ctx context.Context, req PTYRequest, sink func(Event)) (string, error)
}

// PTYHandle is the domain-facing view of a live PTY session.
//
// Stdout/Stderr deliver line-chunked output from the session; Status receives
// lifecycle notifications (ready/exited/error). All channels are closed when
// the session ends so consumers can range-over them safely.
type PTYHandle struct {
	SessionID string
	PID       int
	Stdout    <-chan []byte
	Stderr    <-chan []byte
	Status    <-chan PTYStatus
}

// PTYStatus is a single event on a PTYHandle.Status channel.
type PTYStatus struct {
	Kind     string // "ready", "exited", "error"
	ExitCode int
	Err      error
}

// FileInfo mirrors the subset of fs.FileInfo the domain exposes through
// HostAdapter.Stat without tying callers to the concrete os.FileInfo type.
type FileInfo struct {
	Path    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
}

// SessionInfo is a domain-level snapshot of a BashSessionManager entry.
type SessionInfo struct {
	ID        string
	Command   string
	StartedAt time.Time
	PID       int
	Alive     bool
}

// GateOp describes a HostAdapter call to the HostGate.
// Command is populated for exec/spawn_pty; Path is populated for
// read_file/write_file.
type GateOp struct {
	Kind    string // "exec", "spawn_pty", "read_file", "write_file", "kill"
	Command string
	Path    string
	Sandbox SandboxProfile
	// SessionID is the owning CPN session — populated by fire_bash and other
	// transition callers so PolicyHostGate can scope budget counters +
	// remembered approvals per session instead of bucketing everything into
	// a global "default" (REQ-FIX-009).
	SessionID string
}

// ── Ports ───────────────────────────────────────────────────────────────────

// HostAdapter is the hexagonal port abstracting all OS interactions.
// The v1 OSHostAdapter implementation lives in infra/host/os_adapter.go.
type HostAdapter interface {
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
	SpawnPTY(ctx context.Context, req PTYRequest) (PTYHandle, error)
	KillPID(ctx context.Context, pid int, sig Signal) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, mode fs.FileMode) error
	Stat(ctx context.Context, path string) (FileInfo, error)
}

// HostGate authorises HostAdapter operations. v1 is allow-all; GAP-6
// substitutes a policy-backed implementation without any adapter changes.
type HostGate interface {
	Check(ctx context.Context, op GateOp) error
}

// BashSessionManager holds long-lived PTY sessions addressed by logical ID.
type BashSessionManager interface {
	Open(ctx context.Context, req PTYRequest) (string, error)
	Write(ctx context.Context, id string, data []byte) error
	List(ctx context.Context) []SessionInfo
	Kill(ctx context.Context, id string) error
	Close(ctx context.Context, id string) error

	// Chunks returns the read-only channel of line chunks produced by the
	// session identified by id. The channel is closed when the session
	// terminates. Returns ErrSessionNotFound if the session is unknown.
	Chunks(id string) (<-chan []byte, error)

	// Status returns the read-only channel of lifecycle status events for the
	// session identified by id. The channel is closed when the session
	// terminates. Returns ErrSessionNotFound if the session is unknown.
	Status(id string) (<-chan PTYStatus, error)
}

// ProcessEventMetrics is the hexagonal port for counting process event
// throughput. It is consulted by fire_bash.go and the session manager on
// every emit attempt so operators can watch for noisy processes and tune
// rate-limit thresholds. All methods MUST be non-blocking.
//
// The default NoOpProcessEventMetrics is the safe fallback when no metrics
// sink is wired.
type ProcessEventMetrics interface {
	// OnEmit is called when an event is successfully sent to the CPN
	// event bus. sessionID is empty for one-shot (non-session) bash
	// transitions.
	OnEmit(sessionID string, kind EventType)

	// OnDrop is called when an event is dropped — either because the
	// rate limiter denied it or because the receiving channel was full.
	OnDrop(sessionID string, kind EventType, reason string)
}

// NoOpProcessEventMetrics is a do-nothing ProcessEventMetrics. Use it as
// the default so adapters never need a nil-check.
type NoOpProcessEventMetrics struct{}

// OnEmit is a no-op.
func (NoOpProcessEventMetrics) OnEmit(string, EventType) {}

// OnDrop is a no-op.
func (NoOpProcessEventMetrics) OnDrop(string, EventType, string) {}

// ProcessEventRateLimiter gates process events per session. Allow returns
// true when the event should be forwarded to the CPN event bus, or false
// when it must be dropped due to per-session saturation (REQ-004).
//
// Implementations MUST be safe for concurrent use by multiple PTY read
// loops and the fire_bash dispatcher.
type ProcessEventRateLimiter interface {
	Allow(sessionID string) bool
}

// AllowAllRateLimiter is the no-op rate limiter used when no explicit
// limiter is configured — every event is forwarded.
type AllowAllRateLimiter struct{}

// Allow always returns true.
func (AllowAllRateLimiter) Allow(string) bool { return true }

// HostRuntime aggregates the three host-side collaborators so a single
// pointer can be threaded through the CPN constructor. All fields are
// optional — a CPN that never fires a NodeKindBash transition may leave the
// runtime nil entirely (REQ-042).
type HostRuntime struct {
	Adapter  HostAdapter
	Gate     HostGate
	Sessions BashSessionManager

	// RateLimiter gates process event emission per session (GAP-9
	// REQ-004). Nil means "allow all" — typical for tests. Production
	// wires an infra/host.TokenBucketRateLimiter.
	RateLimiter ProcessEventRateLimiter

	// Metrics counts emits and drops for observability. Nil is treated
	// as NoOpProcessEventMetrics — no counters bumped, no logs written.
	Metrics ProcessEventMetrics

	// HITLHandler bridges a gate-originated "requires HITL" error onto
	// the session's HITL channel and resolves it to approve/deny. Nil
	// disables routing — any HITL requirement then surfaces as a raw
	// gate error on the transition's ErrorPlace. Implementations live in
	// infra/host/gate (see SessionHITLRouter + HandleRequiresHITL).
	HITLHandler HostHITLHandler
}

// ── Error model ─────────────────────────────────────────────────────────────

// HostError is the structured error returned by HostAdapter implementations.
// Code is a stable machine-readable tag; Message and Cause give the human
// context. Error codes are also exposed as sentinel HostError values for
// errors.Is comparisons.
type HostError struct {
	Code    string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *HostError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause so errors.Is/As traverse the chain.
func (e *HostError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is implements errors.Is: two HostError values match when their Code is
// equal. This lets callers write
//
//	errors.Is(err, cpn.ErrTimeoutHost)
//
// without caring about the message or the wrapped cause.
func (e *HostError) Is(target error) bool {
	if e == nil {
		return false
	}
	other, ok := target.(*HostError)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

// NewHostError builds a HostError with the given code, message and cause.
func NewHostError(code, msg string, cause error) *HostError {
	return &HostError{Code: code, Message: msg, Cause: cause}
}

// Host error codes (stable strings — consumed by the executor and tests).
const (
	HostErrCodeGateDenied      = "gate_denied"
	HostErrCodeCommandNotFound = "command_not_found"
	HostErrCodeTimeout         = "timeout"
	HostErrCodeNonZeroExit     = "non_zero_exit"
	HostErrCodePathDenied      = "path_denied"
	HostErrCodeSessionNotFound = "session_not_found"
	HostErrCodeSessionExists   = "session_exists"
	HostErrCodeDenyList        = "deny_listed"
)

// Sentinel HostError values. Use errors.Is for comparison.
var (
	ErrGateDenied      = &HostError{Code: HostErrCodeGateDenied, Message: "operation denied by host gate"}
	ErrCommandNotFound = &HostError{Code: HostErrCodeCommandNotFound, Message: "command not found"}
	ErrTimeoutHost     = &HostError{Code: HostErrCodeTimeout, Message: "command exceeded timeout"}
	ErrNonZeroExit     = &HostError{Code: HostErrCodeNonZeroExit, Message: "command exited with non-zero status"}
	ErrPathDenied      = &HostError{Code: HostErrCodePathDenied, Message: "path outside allowed root"}
	ErrSessionNotFound = &HostError{Code: HostErrCodeSessionNotFound, Message: "bash session not found"}
	ErrSessionExists   = &HostError{Code: HostErrCodeSessionExists, Message: "bash session id already in use"}
	ErrDenyListed      = &HostError{Code: HostErrCodeDenyList, Message: "command matches built-in deny list"}
)

// HostErrorCode returns the Code embedded in a HostError, or "" when err is
// not a HostError.
func HostErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var he *HostError
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

// ── BashConfig ──────────────────────────────────────────────────────────────

// BashEmitMode controls how NodeKindBash stdout/stderr is converted into
// EventProcessStdout / EventProcessStderr events.
type BashEmitMode string

const (
	// EmitPerLine emits one event per newline-terminated line. Partial
	// tail buffers are flushed on exit. This is the default and matches
	// GAP-1 behaviour.
	EmitPerLine BashEmitMode = "per_line"

	// EmitPerChunk emits one event per N-byte chunk, where N is
	// BashConfig.EmitChunkBytes (default 1024). Useful for binary
	// streams and log fan-out where newlines are irregular.
	EmitPerChunk BashEmitMode = "per_chunk"
)

// DefaultEmitChunkBytes is the default chunk size applied when
// BashConfig.EmitMode == EmitPerChunk and EmitChunkBytes is zero.
const DefaultEmitChunkBytes = 1024

// BashConfig is the transition-specific configuration for NodeKindBash.
// When SessionID is non-empty the transition routes through the
// BashSessionManager; otherwise it falls through to HostAdapter.Exec.
type BashConfig struct {
	Command          string
	Args             []string
	Stdin            []byte
	Env              []string
	Cwd              string
	Timeout          time.Duration
	SandboxProfile   SandboxProfile
	Streaming        bool
	AllowNonZeroExit bool
	SessionID        string

	// EmitMode controls how stdout/stderr chunks become process events.
	// Defaults to EmitPerLine when empty (GAP-9 REQ-006).
	EmitMode BashEmitMode

	// EmitChunkBytes is the chunk size used when EmitMode == EmitPerChunk.
	// Zero means DefaultEmitChunkBytes (1024).
	EmitChunkBytes int
}

// ── Process Event payload ───────────────────────────────────────────────────

// ProcessOutputPayload is the payload carried by EventProcessStdout,
// EventProcessStderr and EventProcessStarted/Exit events.
type ProcessOutputPayload struct {
	PID       int    `json:"pid"`
	SessionID string `json:"session_id,omitempty"`
	Line      string `json:"line,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Err       string `json:"error,omitempty"`
}

// ShellResultPayload is deposited on each output place of a NodeKindBash
// transition when the process terminates successfully.
type ShellResultPayload struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}
