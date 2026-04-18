---
title: PTY Observability Bridge — Streaming Process Output as CPN Events
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, observability, pty, observer, events, brae, gap-9]
---

# Introduction

GAP-1 emits stdout lines from bash transitions as tokens on output places. That covers the case where a single transition wants the output. What it does *not* cover is the paper's observer pattern (§11): a *different* transition (`NodeKindObserver`) that watches multiple running processes and reacts to their stdout in real time — "the URL reader has printed its first byte, show the user a progress indicator."

This spec wires `BashSessionManager` and `NodeKindBash` stdout into the existing `CPN.EventEmitter` channel so that `NodeKindObserver` transitions can filter and deposit observation tokens without any new subscription machinery.

## 1. Purpose & Scope

**Purpose.** Make long-running OS processes first-class observable citizens in the CPN event model, so observer transitions work uniformly across sub-CPN events and process events.

**In scope.**
- Event kinds: `EventProcessStarted`, `EventProcessStdout`, `EventProcessStderr`, `EventProcessExit`.
- Non-blocking emission: PTY read loop sends events; on full buffer, drop with a metric bump (never block).
- Per-line batching configurable: `EMIT_PER_LINE` (default) vs `EMIT_PER_CHUNK` (1024-byte chunks) as a transition-level flag.
- Observer filter extension: `EventFilter` receives the new kinds; existing filters keep working.
- Rate limiter: max 1000 events/s per session to protect downstream observers.
- Metrics: `host.process.events_emitted`, `host.process.events_dropped`.
- Tests: observer reacts to a noisy command; backpressure drops instead of blocks.

**Out of scope.**
- Persistence of process output (logs go to stdout / files if the forge writes them).
- UI-side streaming to user — that is SSE, handled elsewhere.
- Structured log parsing — observers apply their own regex.

**Audience.** `golang-pro`.

## 2. Definitions

- **Emitter**: `chan Event` owned by a running CPN, drained each tick by the observer drain pass.
- **Observation token**: A `ColorEvent` token deposited by a `NodeKindObserver` into a computation-space place for downstream processing.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: Event types added to `cpn/event.go`:
  ```go
  EventProcessStarted = "process.started"
  EventProcessStdout  = "process.stdout"
  EventProcessStderr  = "process.stderr"
  EventProcessExit    = "process.exit"
  ```
- **REQ-002**: `BashSessionManager` implementation MUST accept an `EventEmitter chan<- Event` on `Open`; it MUST send `EventProcessStarted` on spawn, a `EventProcessStdout` per line/chunk, `EventProcessStderr` per line/chunk, and `EventProcessExit` on process end.
- **REQ-003**: The emitter send MUST be non-blocking: `select { case emitter <- e: default: metric.Inc("events_dropped") }`.
- **REQ-004**: Rate limiter: a token bucket with rate 1000 events/s per session; over-rate events are dropped and counted in `events_dropped`.
- **REQ-005**: `NodeKindObserver` `EventFilter` signatures MUST continue to accept `Event` by value; adding the new kinds is strictly additive.
- **REQ-006**: Batching flag `BashConfig.EmitMode ∈ {per_line, per_chunk}`. Default `per_line`. `per_chunk` emits a `EventProcessStdout` per N-byte chunk — useful for binary streams.

### Guidelines

- **GUD-001**: Don't observe every process. Observer transitions are cheap but `EventProcessStdout` × 1000 req/s can saturate the drain.
- **GUD-002**: Prefer regex or prefix filters in the observer's `EventFilter` rather than catch-all + downstream filtering.

## 4. Interfaces & Data Contracts

```go
type ProcessEvent struct {
    Kind      EventKind
    SessionID string
    PID       int
    Line      []byte
    ExitCode  int
    Timestamp time.Time
}
```

Extension on the existing `Event`:
```go
type Event struct {
    Kind      EventKind
    CPNID     string
    Payload   any   // ProcessEvent for host events
    Timestamp time.Time
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a `NodeKindObserver` subscribed to `EventProcessStdout`, when a bash transition runs `for i in 1 2 3; do echo $i; done`, then exactly three observation tokens are deposited (one per echo).
- **AC-002**: Given a process emitting 10k lines/s, when the observer buffer is full, then events are dropped and the drop counter increases; no executor goroutine blocks.
- **AC-003**: Given a `NodeKindObserver` without a filter, when a process exits, then one observation token with `Kind=process.exit` is deposited.
- **AC-004**: Given `EmitMode=per_chunk`, when a process outputs 4096 bytes, then 4 events of 1024 bytes each are emitted.
- **AC-005**: `metric.events_dropped` is incremented on every drop; `metric.events_emitted` on every successful send.

## 6. Test Automation Strategy

- Unit: filter matching for the new kinds.
- Stress: 10k lines/s flood; verify no goroutine leak (`goleak`) and counters add up.
- Integration: end-to-end observer+bash+reactor topology.
- Coverage ≥ 85%.

## 7. Rationale & Context

**Why piggyback on `Event`?** The observer pattern already exists and is battle-tested for sub-CPN events. Unifying process events under the same channel keeps the mental model small.

**Why drop on full buffer?** Blocking the executor on a slow observer defeats the whole point of non-blocking execution. Drops are observable via metrics; operators can tune.

**Why rate limit?** A verbose command (`strace`, `find /`) can emit millions of lines. Without a limit, one bash can starve the entire CPN.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-1 — provides the emitter.
- **INF-001**: Prometheus metrics (existing).

## 9. Examples & Edge Cases

### Observer reacting to "error" lines

```go
obs := &cpn.Transition{
    ID: "t-watch-errors", Kind: cpn.NodeKindObserver,
    OutputPlaces: []string{"p-error-events"},
    ObservedCPNID: "",  // any
    EventFilter: func(e cpn.Event) bool {
        if e.Kind != cpn.EventProcessStderr { return false }
        pe := e.Payload.(cpn.ProcessEvent)
        return strings.Contains(string(pe.Line), "error")
    },
}
```

### Edge — process prints no newlines

`per_line` mode buffers until EOF or timeout; `per_chunk` emits by byte count.

## 10. Validation Criteria

- No executor goroutine ever blocks on an emitter send.
- Every `events_dropped` is paired with a log entry (sampled).

## 11. Related Specifications / Further Reading

- [spec-architecture-host-adapter-nodekind-bash.md](./spec-architecture-host-adapter-nodekind-bash.md) — GAP-1.
- [motor_agentico_cpn.md](../.docs/motor_agentico_cpn.md) §11 observer pattern.
