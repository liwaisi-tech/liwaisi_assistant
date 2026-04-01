---
title: "Block 20: LLM Real-Time Streaming Through CPN Engine"
version: 1.0
date_created: 2026-03-31
owner: liwaisi-tech
tags: [architecture, streaming, cpn, llm, sse, block-20]
---

# Introduction

This specification defines the requirements for enabling real-time LLM response streaming through the CPN (Coloured Petri Net) engine to the frontend via Server-Sent Events. Currently, `fireLLM` calls `LLMClient.Complete()` which blocks until the full response is available. For long responses (30s-3min), the user sees nothing until the entire response arrives as a single blob. This spec enables token-by-token delivery.

## 1. Purpose & Scope

**Purpose**: Enable real-time streaming of LLM responses from the CPN engine through the SSE layer to the frontend, reducing time-to-first-token from 30-180 seconds to under 500ms.

**Scope**: Backend only. The frontend already handles `stream_chunk` SSE events correctly. Changes span four layers: domain (`cpn/`), infrastructure (`infra/openrouter/`), application (`internal/app/`), and composition root (`cmd/server/`).

**Audience**: Backend Go developers implementing Block 20.

**Assumptions**:
- OpenRouter API supports `"stream": true` for both `/chat/completions` and `/messages` endpoints
- `StreamHandler.ParseChatSSE()` and `ParseAnthropicSSE()` correctly parse SSE streams (tested)
- Frontend `useSSE.ts` and `useChat.ts` already handle `stream_chunk` events with `{SessionID, CPNID, CPNRole, Content, Done}` format
- The existing `Session.Stream` channel (buffered 64) remains for the done sentinel

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the execution engine for agent workflows |
| **fireLLM** | Function that executes LLM transitions within the CPN |
| **StreamChunk** | `struct {SessionID, CPNID, CPNRole, Content string; Done bool}` — delta payload |
| **EventSink** | `func(*Event)` callback on CPN for emitting events to the SSE broker |
| **SSE** | Server-Sent Events — HTTP streaming protocol used by the frontend |
| **Block 20** | Internal milestone for LLM streaming feature |
| **Delta** | A single content fragment from a streaming LLM response |
| **Time-to-first-token** | Latency between request and first visible character in the UI |

## 3. Requirements, Constraints & Guidelines

### Requirements

- **REQ-001**: `LLMClient` interface MUST expose a `CompleteStream` method that accepts a `func(chunk string)` callback for delivering content deltas
- **REQ-002**: `fireLLM` MUST use `CompleteStream` when `LLMConfig.StreamOutput == true` and `Complete` when `false`
- **REQ-003**: Each content delta MUST be emitted as an `EventStreamChunk` via `CPN.EventSink` with a `StreamChunk` payload matching the frontend's `StreamChunkData` format
- **REQ-004**: After the final content is determined (post tool-call loop), a done sentinel (`StreamChunk{Done: true}`) MUST be emitted
- **REQ-005**: `CompleteStream` MUST return the complete accumulated `LLMResponse` (including usage metrics, tool calls, stop reason) identical to what `Complete` returns
- **REQ-006**: Token accounting via `TokenLedger` MUST work identically for streaming and non-streaming calls
- **REQ-007**: Tool-call loops within `fireLLM` MUST also use `CompleteStream` when streaming is enabled, so re-calls after tool execution stream too
- **REQ-008**: The `Session.Stream` channel MUST continue to receive a done sentinel after `CPN.Run()` completes, for SSE handler cleanup
- **REQ-009**: When streaming was active, the post-run terminal token collection in `SendMessage` MUST NOT re-send content that was already streamed via the broker

### Security Requirements

- **SEC-001**: Streaming responses MUST respect the same CORS and session validation as non-streaming
- **SEC-002**: The streaming HTTP response body MUST be properly closed on context cancellation to prevent resource leaks

### Constraints

- **CON-001**: Domain layer (`cpn/`) MUST NOT import infrastructure layer (`infra/openrouter/`). Communication through `LLMClient` interface only (hexagonal architecture)
- **CON-002**: The `Complete` method signature MUST remain unchanged for backward compatibility
- **CON-003**: The `onChunk` callback MUST be invoked synchronously within the SSE parsing loop (no additional goroutines)
- **CON-004**: HTTP client timeout for streaming calls MUST be configurable and default to at least 600 seconds

### Guidelines

- **GUD-001**: Only user-facing LLM transitions (producing visible text) should set `StreamOutput: true`. Background transitions (classifiers, validators, structured JSON) should leave it `false`
- **GUD-002**: The existing `ParseChatSSE` and `ParseAnthropicSSE` methods SHOULD remain unchanged. Add `WithCallback` variants that accept the delta callback
- **GUD-003**: Mock `CompleteStream` in tests SHOULD delegate to `Complete` when no streaming behavior is needed, to minimize test changes

### Patterns

- **PAT-001**: Callback injection pattern for streaming — `fireLLM` creates a closure that calls `c.emit()`, passed to `CompleteStream`
- **PAT-002**: Event routing pattern — the application layer's event callback detects `EventStreamChunk` and routes to `PublishStreamChunk` (flat format) instead of `PublishEvent` (nested format)

## 4. Interfaces & Data Contracts

### Updated `LLMClient` Interface

```go
// cpn/ports.go
type LLMClient interface {
    Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)
    CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(chunk string)) (LLMResponse, error)
    EstimateCost(req *LLMRequest) (float64, error)
}
```

### Updated `LLMRequest`

```go
// cpn/ports.go — add field to existing struct
type LLMRequest struct {
    // ... existing fields ...
    Stream bool // Enables SSE streaming for this request
}
```

### StreamHandler Callback Variants

```go
// infra/openrouter/streaming.go
func (h *StreamHandler) ParseChatSSEWithCallback(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error)
func (h *StreamHandler) ParseAnthropicSSEWithCallback(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error)
```

### Event Payload for Stream Chunks

```go
// Emitted by fireLLM via c.emit()
Event{
    Type:           EventStreamChunk,
    TransitionID:   t.ID,
    TransitionKind: NodeKindLLM,
    Payload: StreamChunk{
        SessionID: c.SessionID,
        CPNID:     c.ID,
        CPNRole:   c.Role,
        Content:   delta,    // Single chunk of text
        Done:      false,    // true only for final sentinel
    },
}
```

### Frontend Expected Format (unchanged)

```typescript
// Already implemented in types/sse.ts
interface StreamChunkData {
    SessionID: string;
    CPNID: string;
    CPNRole: string;
    Content: string;
    Done: boolean;
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a topology with `StreamOutput: true` on an LLM transition, When the user sends a message, Then the first `stream_chunk` SSE event arrives within 2 seconds of the LLM starting generation
- **AC-002**: Given a streaming LLM response with 100+ tokens, When the response completes, Then the frontend receives individual `stream_chunk` events (not one blob) AND a final `{Done: true}` sentinel
- **AC-003**: Given `StreamOutput: false` (default), When an LLM transition fires, Then behavior is identical to pre-Block-20 (no `stream_chunk` events during execution)
- **AC-004**: Given a streaming LLM call that triggers tool use, When tools execute and the LLM re-calls, Then the second response also streams
- **AC-005**: Given a streaming LLM call, When the response completes, Then `TokenLedger` records the same input/output token counts and cost as a non-streaming call would
- **AC-006**: Given a streaming response in progress, When the user's SSE connection drops, Then the CPN continues execution to completion (fire-and-forget streaming)
- **AC-007**: Given the HITL topology (`hitlTopologyFactory`), When the user approves and `t-execute` fires, Then the execution response streams in real-time

## 6. Test Automation Strategy

### Test Levels

**Unit tests (cpn/)**:
- Mock `CompleteStream` to invoke callback with known deltas
- Verify `EventSink` receives `EventStreamChunk` events with correct payloads
- Verify done sentinel emission
- Verify tool-call loop also streams
- Verify `StreamOutput: false` produces no stream events

**Unit tests (infra/openrouter/)**:
- Feed known SSE text to `ParseChatSSEWithCallback` / `ParseAnthropicSSEWithCallback`
- Verify callback receives each delta in order
- Verify final `LLMResponse` identical to non-callback variant
- Verify nil callback works (backward compatibility)
- Verify `"stream": true` added to request bodies

**Integration tests**:
- Mock HTTP server serves SSE responses
- Verify end-to-end: `CompleteStream` -> parser -> callback invocations -> final response

**SSE handler tests**:
- Verify `EventStreamChunk` routed through `PublishStreamChunk` (flat format)

### Coverage Requirements
- All new code must have 80%+ test coverage
- Race detector must pass: `go test -race`

## 7. Rationale & Context

### Why Streaming Matters
The CPN engine processes multi-step workflows (plan → review → execute). The execute step can generate very long responses (3+ minutes for code generation). Without streaming, users see a blank screen for minutes, then the entire response at once. This creates a perception of system failure and poor UX.

### Why Callback Over Channel
Channels would require a producer goroutine for the parser and consumer goroutine in `fireLLM`. The callback is simpler: invoked synchronously in the SSE parsing loop, no goroutine lifecycle management, no channel buffer sizing decisions.

### Why Via EventSink, Not Session.Stream
The `EventSink` is already wired in `session_service.go` and flows to the SSE broker. Using it keeps the domain layer clean (no session access needed). The broker has per-client buffering and non-blocking publish. The `Session.Stream` channel remains for the done sentinel only.

### Design Decisions Log
| Decision | Chosen | Alternative | Rationale |
|----------|--------|-------------|-----------|
| Interface approach | Add `CompleteStream` method | Separate `StreamingLLMClient` interface | Simpler, no type assertions needed |
| Delta delivery | Callback `func(string)` | `chan string` | No goroutine lifecycle, synchronous invocation |
| Chunk routing | `EventSink` → broker → SSE | Direct to `Session.Stream` | Domain layer stays clean, broker handles buffering |
| Parser refactoring | `WithCallback` variants | Modify existing methods | Backward compatible, no test changes for existing callers |

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter API — Must support `"stream": true` parameter on both `/chat/completions` and `/messages` endpoints

### Infrastructure Dependencies
- **INF-001**: SSE broker — Must handle increased event throughput (many small chunks vs one large event)
- **INF-002**: HTTP client — Must support long-lived streaming connections without premature timeout

### Technology Platform Dependencies
- **PLT-001**: Go 1.21+ — Required for existing codebase compatibility
- **PLT-002**: `bufio.Scanner` — Used by existing SSE parsers for line-by-line reading

## 9. Examples & Edge Cases

### Example: Streaming fireLLM Flow

```go
// In fireLLM, when StreamOutput is true:
if t.LLMConfig.StreamOutput {
    req.Stream = true
    onChunk := func(chunk string) {
        c.emit(&Event{
            Type:         EventStreamChunk,
            TransitionID: t.ID,
            Payload: StreamChunk{
                SessionID: c.SessionID,
                CPNID:     c.ID,
                CPNRole:   c.Role,
                Content:   chunk,
                Done:      false,
            },
        })
    }
    resp, err = c.LLMClient.CompleteStream(ctx, req, onChunk)
} else {
    resp, err = c.LLMClient.Complete(ctx, req)
}
```

### Edge Case: Tool Call During Streaming

```
LLM streams: "Let me search for..." [stream_chunk events emitted]
LLM stops with finish_reason: "tool_calls"
CompleteStream returns LLMResponse{ToolCalls: [...]}
fireLLM enters handleToolCalls loop
Tool executes synchronously
fireLLM re-calls CompleteStream with tool results
LLM streams: "Based on the search results..." [more stream_chunk events]
CompleteStream returns final LLMResponse
fireLLM emits done sentinel
```

### Edge Case: Context Cancellation During Streaming

```
LLM streaming in progress
User navigates away → SSE connection closes
Context not cancelled (fire-and-forget)
CPN continues to completion
Chunks emitted to EventSink → broker → dropped (no clients)
Terminal token deposited normally
Session history updated
```

### Edge Case: Empty Streaming Response

```
LLM returns empty content (rare)
No onChunk callbacks invoked
CompleteStream returns LLMResponse{Content: ""}
fireLLM deposits empty token
Done sentinel still emitted
```

## 10. Validation Criteria

1. All existing tests pass without modification (backward compatibility)
2. `go test -race ./cpn/... ./infra/... ./internal/... ./cmd/...` passes
3. `go vet ./...` passes
4. New streaming tests verify delta delivery, done sentinel, and tool-call loop
5. Manual test: HITL topology → approve → response streams character-by-character
6. Token accounting matches between streaming and non-streaming calls for same prompt

## 11. Related Specifications / Further Reading

- Paper: Borghoff et al. "Human-Artificial Interaction in the Age of Agentic AI" (2502.14000v1) — Section 4, Communication Spaces
- Existing code: `cpn/doc.go` — Architecture overview mentioning StreamHandler
- OpenRouter API docs: Streaming support for `/chat/completions` and `/messages`
- SSE specification: https://html.spec.whatwg.org/multipage/server-sent-events.html

## Files to Modify

| File | Layer | Change |
|------|-------|--------|
| `cpn/ports.go` | Domain | Add `CompleteStream` to `LLMClient`; add `Stream bool` to `LLMRequest` |
| `cpn/fire_llm.go` | Domain | Branch on `StreamOutput`; emit `EventStreamChunk` via callback; modify `handleToolCalls` |
| `infra/openrouter/streaming.go` | Infra | Add `WithCallback` parser variants |
| `infra/openrouter/openrouter.go` | Infra | Implement `CompleteStream`; add `"stream": true` to body builders |
| `cmd/server/main.go` | Composition | Route `EventStreamChunk` to `PublishStreamChunk` in event callback |
| `internal/app/session_service.go` | Application | Skip re-sending streamed content in post-run collection |
| `cmd/server/main.go` | Composition | Set `StreamOutput: true` on user-facing transitions in `hitlTopologyFactory` |
| `cpn/fire_llm_test.go` | Test | Add `CompleteStream` to mock; streaming test cases |
| `infra/openrouter/streaming_test.go` | Test | Callback variant tests |
