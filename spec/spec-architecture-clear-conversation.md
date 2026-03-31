---
title: Clear Conversation Feature
version: 1.0
date_created: 2026-03-31
owner: Liwaisi Engineering
tags: [architecture, app, frontend, backend]
---

# Introduction

This specification defines the "Clear Conversation" feature that allows users to reset the chat and start a new conversation from the UI. The feature is implemented as a frontend-driven new session creation with a backend session cleanup endpoint.

## 1. Purpose & Scope

Enable users to explicitly clear the current conversation and start fresh without reloading the page. The scope covers:

- A backend `DELETE /api/v1/sessions/{id}` endpoint for session cleanup.
- A frontend "New Conversation" button in the chat header with confirmation dialog.
- State management changes to reset the chat UI and create a new session.

**Intended audience**: Frontend and backend engineers working on the Liwaisi Assistant.

**Assumptions**: Sessions are in-memory only (no persistence layer). Cleared conversations are not recoverable.

## 2. Definitions

- **CPN**: Coloured Petri Net — the domain execution engine that processes user messages.
- **HITL**: Human-In-The-Loop — a workflow gate requiring human approval before proceeding.
- **SSE**: Server-Sent Events — the real-time streaming protocol used for delivering CPN events and LLM output to the frontend.
- **Session**: A stateful conversation context containing message history, a CPN instance, and a streaming channel.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: The system shall provide a `DELETE /api/v1/sessions/{id}` HTTP endpoint that removes the session from in-memory storage and cancels any running CPN via context cancellation.
- **REQ-002**: The frontend shall display a "New Conversation" button in the chat header that, when clicked, creates a new session via `POST /api/v1/sessions` and resets the UI to the welcome screen.
- **REQ-003**: The frontend shall show a confirmation dialog before clearing when the conversation contains messages. Empty conversations shall skip confirmation.
- **REQ-004**: The "New Conversation" button shall be disabled when the session state is `running` or `waiting` (HITL pending).
- **REQ-005**: The frontend shall fire-and-forget the `DELETE` call for the old session and optimistically reset the UI before the new session is confirmed.
- **SEC-001**: (Future) The `DELETE` endpoint should validate that the requesting user owns the session. Currently not enforced as sessions are ephemeral and unauthenticated at the API level.
- **CON-001**: Sessions are stored in-memory only. Once deleted, all conversation data is permanently lost.
- **CON-002**: The `DELETE` endpoint must NOT close the session's stream channel directly to avoid data races with background goroutines. Cleanup relies on context cancellation.
- **GUD-001**: The confirmation dialog should match the existing deep-space terminal aesthetic (glassmorphism, cyan accents, JetBrains Mono headings).
- **PAT-001**: SSE reconnection on session change is handled by the existing `useSSE` hook dependency on `sessionId` — no additional SSE logic is needed.

## 4. Interfaces & Data Contracts

### DELETE /api/v1/sessions/{id}

**Request**: No body required.

**Response (200 OK)**:
```json
{
  "status": "deleted"
}
```

**Response (404 Not Found)**:
```json
{
  "error": "session not found"
}
```

### Frontend State

**New reducer action**:
```typescript
{ type: 'CLEAR_CONVERSATION' }
```

Resets state to `initialState`: `{ sessionId: null, messages: [], sessionState: 'idle', error: null }`.

**New hook return value**:
```typescript
clearConversation: () => Promise<void>
```

## 5. Acceptance Criteria

- **AC-001**: Given a session with messages, when the user clicks "New Conversation" and confirms, then the messages are cleared, the welcome screen is displayed, and a new session ID is stored in localStorage.
- **AC-002**: Given a session in `running` state, when the user views the chat header, then the "New Conversation" button is visually disabled and not clickable.
- **AC-003**: Given a session in `waiting` (HITL) state, when the user views the chat header, then the "New Conversation" button is visually disabled and not clickable.
- **AC-004**: Given an empty conversation (no messages), when the user clicks "New Conversation", then the confirmation dialog is skipped and a new session is created directly.
- **AC-005**: Given a successful clear, when the SSE hook detects the sessionId change, then the old EventSource is closed and a new one is opened for the new session.
- **AC-006**: Given a network error when creating a new session, when the user clicks "New Conversation" and confirms, then an error message is displayed via the existing error banner.
- **AC-007**: Given a `DELETE` request for a non-existent session, when the backend processes it, then a 404 response with `{"error": "session not found"}` is returned.

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go + TypeScript), Integration (Go handler tests)
- **Frameworks**: Go standard `testing` package; table-driven tests with `-race` flag
- **Backend Unit Tests**:
  - `TestDeleteSession_Success`: Create and delete, verify `GetSession` returns `ErrSessionNotFound`
  - `TestDeleteSession_NotFound`: Delete non-existent, verify `ErrSessionNotFound`
  - `TestDeleteSession_CancelsRunning`: Verify context cancel func is called on delete
- **Backend Handler Tests**:
  - `TestHandleDeleteSession`: Full HTTP round-trip, verify 200 + subsequent 404
  - `TestHandleDeleteSession_NotFound`: Verify 404 for non-existent session
- **Coverage Requirements**: All new code must pass `golangci-lint` and race detector

## 7. Rationale & Context

Users currently have no way to clear their conversation short of reloading the page. The only implicit reset occurs when a session reaches `completed` state and the next message auto-creates a new session.

**Why new session instead of reset?** Resetting an existing session requires draining the stream channel, re-wiring HITL channels, and handling CPN concurrency — all introducing race hazards. Creating a fresh session reuses the existing, well-tested `CreateSession` code path.

**Why not close the stream channel in `DeleteSession`?** The background goroutine from `SendMessage` may still be sending on the channel. Closing it causes a panic ("send on closed channel"). Context cancellation is the safe shutdown mechanism.

## 8. Dependencies & External Integrations

### Internal Dependencies
- **INT-001**: `SessionService.CreateSession` — reused to create the replacement session
- **INT-002**: `useSSE` hook's `sessionId` dependency — drives automatic SSE reconnection
- **INT-003**: `chatReducer` initial state — `CLEAR_CONVERSATION` action returns `initialState`

### Infrastructure Dependencies
- **INF-001**: In-memory session storage — no database or cache required

## 9. Examples & Edge Cases

```typescript
// Frontend: clearing conversation
const clearConversation = async () => {
  const oldSessionId = state.sessionId;
  dispatch({ type: 'CLEAR_CONVERSATION' });  // Optimistic UI reset
  if (oldSessionId) {
    apiDeleteSession(oldSessionId).catch(() => {}); // Fire-and-forget
  }
  const session = await createSession(userId, 'web');
  localStorage.setItem(storageKey, session.id);
  dispatch({ type: 'SESSION_CREATED', sessionId: session.id });
};
```

**Edge case — rapid double-click**: The button is disabled while `clearDisabled` is true (during `running`/`waiting`). For idle state, the optimistic dispatch sets `sessionId` to null, preventing duplicate creates.

**Edge case — SSE to old session**: The `useSSE` effect cleanup closes the old EventSource when `sessionId` changes. The `sessionTerminalRef` is reset to `false` for the new session.

## 10. Validation Criteria

1. `go test -race ./internal/app/ ./internal/driving/httpapi/` — all tests pass, no data races
2. `golangci-lint run` — no new warnings
3. `npm run build` — frontend builds without TypeScript errors
4. Manual E2E: send messages, click "New Conversation", confirm, verify welcome screen, send new message

## 11. Related Specifications / Further Reading

- [HTTP SSE API Spec](../back/go-assistant/spec/spec-architecture-http-sse-api.md)
- [Agentic CPN v1.3](../.docs/specs/agentic-cpn-v1.3.md)
- [LLM Streaming Spec](spec-architecture-block20-llm-streaming.md)
