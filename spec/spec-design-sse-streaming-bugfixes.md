---
title: "SSE Streaming Pipeline Bug Fixes: Truncation, Duplicate Sentinels, History Pollution"
version: 1.0
date_created: 2026-04-01
owner: liwaisi-tech
tags: [design, bugfix, streaming, cpn, sse, session-history]
---

# Introduction

This specification defines the fixes for five interconnected bugs discovered in the SSE streaming pipeline of the go-assistant CPN engine. The bugs were identified through systematic curl simulation of the full conversation lifecycle. The **primary user-facing symptom** is that long LLM responses appear "cut" mid-content (e.g., code blocks ending abruptly with `outputPlaces = append(outputPlaces, p`). Secondary symptoms include duplicate messages in conversation history, classifier metadata leaking into user-facing history, and duplicate Done sentinels creating race conditions between the SSE broker and the Session.Stream channel.

## 1. Purpose & Scope

**Purpose**: Fix five bugs that degrade the streaming experience — response truncation, duplicate signaling, and history pollution — in a coordinated manner since the bugs share overlapping code paths.

**Scope**: Backend Go changes across four packages (`cpn/`, `internal/app/`, `cmd/server/`, `infra/openrouter/`) and no frontend changes. The frontend already handles all SSE events correctly; the bugs are entirely server-side.

**Audience**: Backend Go developers implementing these fixes.

**Assumptions**:
- The `unifiedTopologyFactory` is the active topology (default `TOPOLOGY=unified`)
- The frontend `useChat.ts` reducer correctly handles `stream_chunk` events and Done sentinels
- OpenRouter API returns `stop_reason: "length"` when MaxTokens is exhausted
- The `Session.Stream` channel (buffered 64) and SSE broker `client.events` channel (buffered 128) are the only two delivery paths for stream data

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Done sentinel** | A `StreamChunk{Content: "", Done: true}` that signals the end of a streaming response |
| **Dual-channel delivery** | The current architecture where SSE events arrive via both the broker (`client.events`) and `Session.Stream` |
| **History sync** | The post-`CPN.Run()` loop in `session_service.go` (lines 199-216) that copies `c.History` entries back to `Session.history` |
| **Terminal place consumption** | The post-`CPN.Run()` loop in `session_service.go` (lines 242-276) that reads tokens from terminal places and creates session messages |
| **StopReason** | The `LLMResponse.StopReason` field parsed from LLM provider SSE streams — `"stop"` for natural completion, `"length"` / `"max_tokens"` for token limit exhaustion |
| **Classifier transition** | `t-classify` — a non-streaming LLM transition with `SkipHistory: true` and `RequireJSON: true` that produces routing JSON |
| **MaxTokens** | `LLMConfig.MaxTokens` — upper bound on LLM output tokens per call |

## 3. Requirements, Constraints & Guidelines

### Requirements

- **REQ-001**: `MaxTokens` for user-facing streaming transitions (`t-direct`, `t-plan`, `t-execute`) MUST be configurable via environment variables with sensible defaults that prevent truncation for typical responses
- **REQ-002**: Default `MaxTokens` MUST be `4096` for `t-direct` and `t-plan`, and `8192` for `t-execute`
- **REQ-003**: Each streaming LLM response MUST produce exactly **one** Done sentinel, emitted via the CPN EventSink (broker path). The `session_service.go` background goroutine MUST NOT emit a duplicate Done sentinel on the `Session.Stream` channel after successful completion when streaming was active
- **REQ-004**: The `session_service.go` background goroutine MUST continue to emit a Done sentinel on `Session.Stream` **only** on error paths (when `runErr != nil`) to ensure the frontend always receives completion signals even on failure
- **REQ-005**: When `fireLLM` appends to `c.History`, it MUST skip the append when the transition's `LLMConfig.SkipHistory` is `true`. This prevents classifier JSON from polluting the conversation history
- **REQ-006**: The terminal place consumption loop (lines 242-276) MUST NOT append messages to session history when the content was already synced via the history sync loop (lines 199-216). The loop MUST still consume tokens to clear terminal places, but MUST NOT call `session.AppendMessage()` when `streamingActive` is `true` or when the content already exists in the synced history
- **REQ-007**: When `LLMResponse.StopReason` indicates token limit exhaustion (`"length"` or `"max_tokens"`), `fireLLM` MUST emit a final content chunk with a truncation notice before emitting the Done sentinel
- **REQ-008**: The truncation notice content MUST be `"\n\n---\n*[Response truncated — token limit reached]*"` — a markdown-formatted notice that renders cleanly in the frontend

### Constraints

- **CON-001**: The `Session.Stream` channel MUST remain open across multiple messages (not closed after each response) — existing behavior, do not change
- **CON-002**: The `fire_llm.go` Done sentinel via `c.emit()` MUST remain the authoritative streaming completion signal — it flows through the broker which guarantees ordering relative to content chunks
- **CON-003**: Environment variable names MUST follow the existing pattern: `MAX_TOKENS_DIRECT`, `MAX_TOKENS_PLAN`, `MAX_TOKENS_EXECUTE`
- **CON-004**: The `SkipHistory` flag semantics change (REQ-005) MUST NOT break downstream transitions that rely on `c.History` for context. Classifier output is routing metadata, not conversational content — downstream transitions (`t-direct`, `t-plan`) receive the classified token via their input places, not via history

### Guidelines

- **GUD-001**: Prefer removing code paths over adding de-duplication logic. The terminal place consumption for message creation (lines 252-261) is redundant when history sync exists — remove rather than filter
- **GUD-002**: The `streamingActive` flag (`session.Root.StreamedOutput`) already exists and correctly tracks whether streaming occurred. Use it to gate the Done sentinel and terminal place message creation
- **GUD-003**: Environment variable parsing for MaxTokens SHOULD use `strconv.Atoi` with fallback to the default value, consistent with existing `envOr` and `parseDuration` helpers

## 4. Interfaces & Data Contracts

### Bug 1 Fix: Topology MaxTokens Configuration

```go
// cmd/server/topologies.go — updated t-direct
tDirect.LLMConfig = &cpn.LLMConfig{
    MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),  // was: 1024
    Temperature:  0.7,
    StreamOutput: true,
}

// cmd/server/topologies.go — updated t-plan
tPlan.LLMConfig = &cpn.LLMConfig{
    MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),    // was: 1024
    Temperature:  0.7,
    StreamOutput: true,
}

// cmd/server/topologies.go — updated t-execute
tExecute.LLMConfig = &cpn.LLMConfig{
    MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),  // was: 2048
    Temperature:  0.7,
    StreamOutput: true,
}

// cmd/server/main.go — new helper
func envInt(key string, defaultVal int) int {
    v := os.Getenv(key)
    if v == "" {
        return defaultVal
    }
    n, err := strconv.Atoi(v)
    if err != nil {
        return defaultVal
    }
    return n
}
```

### Bug 2 Fix: Session Service Done Sentinel (Conditional)

```go
// internal/app/session_service.go — updated background goroutine (after terminal place loop)

// Send done sentinel ONLY when streaming was NOT active.
// When streaming was active, fireLLM already emitted Done via the broker —
// emitting a second Done here creates a race condition where this Done
// (on Session.Stream) can arrive at the SSE handler before the broker
// has finished delivering all content chunks from client.events.
if !streamingActive {
    select {
    case session.Stream <- cpn.StreamChunk{
        SessionID: sessionID,
        CPNID:     session.Root.ID,
        CPNRole:   session.Root.Role,
        Content:   "",
        Done:      true,
    }:
    default:
    }
}
```

### Bug 3 Fix: Terminal Place Message De-duplication

```go
// internal/app/session_service.go — updated terminal place loop

// Collect output from terminal places.
// When streaming was active, messages were already synced from c.History above.
// We still consume tokens to clear the places, but skip message creation.
for _, p := range session.Root.TerminalPlaces() {
    for {
        tok, err := p.Consume()
        if err != nil {
            break
        }
        content, ok := tok.Payload.(string)
        if !ok {
            continue
        }

        // Skip message creation when history sync already captured this content.
        if len(session.Root.History) > historyLen {
            continue
        }

        // Non-streaming path: persist to session and send via Stream.
        session.AppendMessage(&cpn.Message{
            ID:        sessionID + "-resp-" + fmt.Sprintf("%d", time.Now().UnixNano()),
            Role:      cpn.RoleAssistant,
            Content:   content,
            CPNID:     session.Root.ID,
            CPNRole:   session.Root.Role,
            CPNDepth:  session.Root.Depth,
            Timestamp: time.Now(),
        })

        if !streamingActive {
            select {
            case session.Stream <- cpn.StreamChunk{
                SessionID: sessionID,
                CPNID:     session.Root.ID,
                CPNRole:   session.Root.Role,
                Content:   content,
                Done:      false,
            }:
            default:
                s.logger.Warn("stream buffer full, chunk dropped", "session_id", sessionID)
            }
        }
    }
}
```

### Bug 4 Fix: SkipHistory Write Behavior

```go
// cpn/fire_llm.go — updated history append block (lines 165-183)

// Append consumed input and LLM output to CPN history for downstream transitions.
// Skip when SkipHistory is true — classifier output is routing metadata,
// not conversational content that downstream transitions need in history.
if !t.LLMConfig.SkipHistory {
    c.mu.Lock()
    if len(userTokens) > 0 {
        c.History = append(c.History, &Message{
            Role:      RoleUser,
            Content:   formatTokenPayload(userTokens),
            Timestamp: time.Now(),
        })
    }
    c.History = append(c.History, &Message{
        Role:      RoleAssistant,
        Content:   content,
        CPNID:     c.ID,
        CPNRole:   c.Role,
        CPNDepth:  c.Depth,
        Timestamp: time.Now(),
    })
    c.mu.Unlock()
}
```

### Bug 5 Fix: Truncation Detection in fireLLM

```go
// cpn/fire_llm.go — after content is determined, before Done sentinel

// Detect token limit truncation and notify user.
truncated := resp.StopReason == "length" || resp.StopReason == "max_tokens"
if truncated && streamOutput {
    truncationNotice := "\n\n---\n*[Response truncated — token limit reached]*"
    content += truncationNotice
    c.emit(&Event{
        Type:           EventStreamChunk,
        TransitionID:   t.ID,
        TransitionKind: NodeKindLLM,
        Payload: StreamChunk{
            SessionID: c.SessionID,
            CPNID:     c.ID,
            CPNRole:   c.Role,
            Content:   truncationNotice,
            Done:      false,
        },
    })
}

// Emit done sentinel if streaming was active. (existing code)
if streamOutput {
    c.emit(&Event{
        Type:           EventStreamChunk,
        TransitionID:   t.ID,
        TransitionKind: NodeKindLLM,
        Payload: StreamChunk{
            SessionID: c.SessionID,
            CPNID:     c.ID,
            CPNRole:   c.Role,
            Content:   "",
            Done:      true,
        },
    })
}
```

### SSE Event Flow (After Fixes)

**Conversation path** (t-classify → t-direct):
```
1. t-classify fires (no streaming, no history append) → JSON in p-classified
2. t-direct fires (streaming) → stream_chunk events via broker
3. fireLLM emits Done sentinel via broker → 1 Done total
4. CPN.Run() returns → session_service skips Done (streamingActive=true)
5. History sync captures t-direct's response → 1 message in session
6. Terminal place consumed but NO message created (history already synced)
```

**Task path** (t-classify → t-plan → t-review → t-execute):
```
1. t-classify fires → JSON in p-classified (no history, no streaming)
2. t-plan fires (streaming) → stream_chunk events via broker
3. fireLLM emits Done sentinel via broker → frontend transitions to idle briefly
4. t-review fires (HITL) → hitl_requested event → frontend transitions to waiting
5. User approves → t-execute fires (streaming) → stream_chunk events via broker
6. fireLLM emits Done sentinel via broker → 1 Done for t-execute
7. CPN.Run() returns → session_service skips Done (streamingActive=true)
8. History sync captures t-plan + t-execute responses → 2 messages in session
```

## 5. Acceptance Criteria

- **AC-001**: Given a user message classified as "conversation" that produces a 2000+ token response, When the LLM generates the response, Then the **complete** response is delivered without truncation and a single Done sentinel is received
- **AC-002**: Given a session with 3 conversation exchanges, When the session is retrieved via `GET /api/v1/sessions/{id}`, Then the messages array contains exactly 6 entries (3 user + 3 assistant) with no duplicates and no classifier JSON
- **AC-003**: Given a streaming response, When `CPN.Run()` completes, Then exactly **one** Done sentinel `{Done: true, Content: ""}` is received by the SSE client — not two or three
- **AC-004**: Given a response that exhausts `MaxTokens`, When the LLM returns `stop_reason: "length"`, Then the frontend receives a final content chunk containing `"[Response truncated — token limit reached]"` before the Done sentinel
- **AC-005**: Given the unified topology with `t-classify` using `SkipHistory: true`, When the classifier produces `{"intent":"conversation"}`, Then this JSON does NOT appear in `c.History`, session messages, or any SSE event visible to the user
- **AC-006**: Given the plan-review-execute path, When the user approves and `t-execute` streams its response, Then the total Done sentinel count for the entire session run is 2 (one from `t-plan`, one from `t-execute`) — not 3
- **AC-007**: Given environment variables `MAX_TOKENS_DIRECT=2048` and `MAX_TOKENS_EXECUTE=4096`, When the server starts, Then the topology uses those values instead of the compiled defaults
- **AC-008**: Given a non-streaming LLM transition (e.g., `StreamOutput: false`), When it completes, Then a Done sentinel IS sent on `Session.Stream` (the fallback path for non-streaming responses remains intact)
- **AC-009**: Given a CPN run that fails (e.g., LLM error), When the error path executes, Then a Done sentinel IS sent on `Session.Stream` so the frontend transitions out of "running" state

## 6. Test Automation Strategy

### Test Levels

- **Unit Tests** (per-bug):
  - `fire_llm_test.go`: Verify SkipHistory write suppression (Bug 4), truncation notice emission (Bug 5), single Done sentinel (Bug 2)
  - `session_service_test.go`: Verify no duplicate messages (Bug 3), conditional Done sentinel (Bug 2), error path Done sentinel (Bug 2)
  - `topology_test.go`: Verify MaxTokens defaults and env var override (Bug 1)

- **Integration Tests**:
  - `streaming_integration_test.go`: Full HTTP flow — create session, connect SSE, send messages, count Done sentinels, verify message history, check for truncation notice

### Frameworks

- Go standard `testing` package
- `httptest` for integration HTTP tests
- `testify/assert` if already used in the project, otherwise standard `t.Fatal` / `t.Errorf`

### Test Data

- Mock LLM client returning configurable content length and `StopReason`
- Mock LLM client for streaming with configurable chunk count

### Coverage Requirements

- All five bugs MUST have at least one dedicated test
- Regression test: "3 conversation exchanges produce exactly 6 session messages"
- Regression test: "streaming response produces exactly 1 Done sentinel"

### Key Test Scenarios

```go
// Test: SkipHistory suppresses history write
func TestFireLLM_SkipHistory_NoHistoryAppend(t *testing.T) {
    // Given: transition with SkipHistory=true
    // When: fireLLM executes
    // Then: c.History length unchanged
}

// Test: Streaming response produces exactly 1 Done
func TestSendMessage_Streaming_SingleDoneSentinel(t *testing.T) {
    // Given: session with streaming topology
    // When: message sent and response completes
    // Then: exactly 1 Done sentinel received (count from Stream + events)
}

// Test: No duplicate messages in session history
func TestSendMessage_NoDuplicateMessages(t *testing.T) {
    // Given: session with streaming topology
    // When: 3 messages sent and responses complete
    // Then: session.Messages() has exactly 6 entries
}

// Test: Truncation notice on MaxTokens exhaustion
func TestFireLLM_StopReasonLength_EmitsTruncationNotice(t *testing.T) {
    // Given: mock LLM returning StopReason="length"
    // When: fireLLM executes with StreamOutput=true
    // Then: truncation notice chunk emitted before Done sentinel
}

// Test: MaxTokens configurable via env
func TestTopology_MaxTokensFromEnv(t *testing.T) {
    // Given: MAX_TOKENS_DIRECT=2048 in environment
    // When: unifiedTopologyFactory creates CPN
    // Then: t-direct.LLMConfig.MaxTokens == 2048
}
```

## 7. Rationale & Context

### Why MaxTokens Was Too Low (Bug 1)
The original values (1024/2048) were set during early development when responses were short. As the assistant matured and users began asking complex questions (code generation, detailed explanations), 1024 tokens (~750 words) became insufficient. The LLM hits the token ceiling and stops mid-sentence, which the user perceives as a "stream cut." Increasing to 4096/8192 provides headroom for code-heavy responses while remaining within OpenRouter rate limits.

### Why Dual Done Sentinels Exist (Bug 2)
Block 20 (streaming spec) introduced the `c.emit()` Done sentinel in `fireLLM` for real-time signaling via the broker. The pre-existing Done sentinel in `session_service.go` (on `Session.Stream`) was designed for the non-streaming path where content arrives as a single blob via terminal place consumption. After Block 20, both paths fire on every streaming response, creating duplicates. The fix gates the `Session.Stream` Done behind `!streamingActive`.

### Why Messages Duplicate (Bug 3)
Two independent loops create session messages from the same content:
1. **History sync** (added in Block 20) — captures messages appended by `fireLLM` to `c.History` during streaming
2. **Terminal place consumption** (pre-Block 20) — reads tokens from terminal places after `CPN.Run()`

Both produce identical assistant messages. The history sync is the more reliable path (captures all messages including from failed runs), so the terminal place loop should be reduced to just consuming tokens without creating messages when history sync already handled it.

### Why Classifier JSON Leaks (Bug 4)
`fireLLM` unconditionally appends its output to `c.History` for downstream transitions. The `SkipHistory` flag was implemented to prevent the classifier from *reading* prior history (ensuring independent classification), but it did not suppress *writing*. Since the classifier's JSON output is routing metadata (not conversational content), it should not be persisted in history at all.

### Why Truncation Is Silent (Bug 5)
The `StopReason` field is correctly parsed from both OpenAI and Anthropic SSE formats, but `fireLLM` never inspects it. When `StopReason == "length"`, the response is incomplete but treated as final. Adding a visible truncation notice ensures users know the response was cut and can request continuation.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter API — returns `stop_reason` field in streaming responses (`"stop"`, `"length"`, `"max_tokens"`)

### Infrastructure Dependencies
- **INF-001**: Environment variables (`MAX_TOKENS_DIRECT`, `MAX_TOKENS_PLAN`, `MAX_TOKENS_EXECUTE`) — optional, with compiled defaults

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ — required for `http.NewResponseController` used in SSE handler

## 9. Examples & Edge Cases

### Edge Case 1: Non-streaming transition (classifier)
```
Input: User message "Hola"
t-classify fires (SkipHistory=true, StreamOutput=false)
→ Output: {"intent":"conversation"}
→ History: NOT appended (Bug 4 fix)
→ Stream: No chunks emitted
→ Session messages: No classifier entry added
```

### Edge Case 2: Streaming response hits MaxTokens
```
Input: Complex code generation request
t-direct fires (StreamOutput=true, MaxTokens=4096)
→ LLM generates 4096 tokens, stops with StopReason="length"
→ Truncation notice emitted as final content chunk
→ Done sentinel emitted via broker
→ Session.Stream: NO Done sentinel (streamingActive=true)
→ Frontend shows: complete streamed content + "[Response truncated — token limit reached]"
```

### Edge Case 3: CPN error during streaming
```
Input: Message that causes LLM API error mid-stream
→ fireLLM returns error
→ Some stream_chunk events already delivered via broker
→ CPN.Run() returns error
→ session_service.go error path sends Done on Session.Stream (REQ-004)
→ Frontend receives Done, transitions to idle
```

### Edge Case 4: Non-streaming completion (future topologies)
```
Topology with StreamOutput=false on all transitions
→ No EventStreamChunk events emitted during CPN.Run()
→ streamingActive = false
→ Terminal place consumption creates session messages AND sends via Session.Stream
→ Done sentinel sent on Session.Stream (only path)
→ SSE handler delivers content + Done
```

### Edge Case 5: HITL rejection followed by retry
```
1. t-plan streams plan content → Done sentinel via broker
2. t-review HITL fires → hitl_requested event
3. User rejects → CPN.Run() returns error (HITL rejection)
4. session_service error path → Done on Session.Stream
5. Session state: idle (not terminal)
6. User sends new message → new CPN run
7. No stale Done sentinels from previous run
```

## 10. Validation Criteria

1. **Conversation test**: Send 3 messages ("Hola", "Dime en que me puedes ayudar?", "Como podría crear un sistema agéntico en Golang usando CPN...") via curl. Verify:
   - All 3 responses complete without truncation
   - Exactly 2 Done sentinels per message (Bug 2: reduced to 1 after fix, but 2 for non-streaming classifier + 1 for streaming = 1 per visible response)
   - `GET /api/v1/sessions/{id}` returns exactly 6 messages (3 user + 3 assistant)
   - No `{"intent":"..."}` entries in messages

2. **HITL test**: Send a "task" message, approve HITL, verify:
   - Plan streams completely → 1 Done → HITL requested
   - Execution streams completely → 1 Done
   - Session messages: user + plan response + execute response (no duplicates)

3. **Truncation test**: Set `MAX_TOKENS_DIRECT=64` via env var, send a complex question, verify:
   - Response includes truncation notice
   - Frontend receives the notice as a `stream_chunk` event before Done

4. **Error path test**: Cancel a session mid-stream, verify:
   - Done sentinel received on `Session.Stream`
   - Frontend transitions out of "running" state

## 11. Related Specifications / Further Reading

- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md) — Original streaming architecture specification (Block 20)
- [spec-architecture-clear-conversation.md](spec-architecture-clear-conversation.md) — Session lifecycle and clear conversation flow
