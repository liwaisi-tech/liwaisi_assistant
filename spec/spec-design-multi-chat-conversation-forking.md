---
title: Multi-Chat Management with Conversation Forking
version: 1.0
date_created: 2026-04-04
last_updated: 2026-04-04
owner: liwaisi-tech
tags: [design, chat, session, forking, billing, frontend, backend]
---

# Introduction

This specification defines the design for multi-chat management and conversation forking in the Liwaisi Assistant. Currently, each user has a single active conversation stored in localStorage. This feature introduces a persistent chat list, the ability to create/delete/resume chats, and a conversation forking mechanism that clones a conversation while resetting billing and preserving the original's CPN execution history.

## 1. Purpose & Scope

**Purpose**: Enable users to manage multiple concurrent conversations with full CPN execution visibility, and to fork (clone) conversations at any point for experimentation without affecting the original or its billing.

**Scope**:
- Backend: New API endpoints, database migration, session service extensions, fork logic
- Frontend: Chat sidebar, chat switching, fork dialog, updated session management
- Billing: Cost isolation between original and forked conversations
- CPN: Topology lineage tracking across forked sessions

**Intended Audience**: Backend engineers (Go), frontend engineers (React/TypeScript), AI agents engineers (CPN topology), product managers.

**Assumptions**:
- Users are authenticated via Google OAuth (immutable `sub` claim as user identity)
- Sessions are identified by 32-char hex IDs generated server-side
- The CPN engine runs in-memory; persistence is best-effort via `PersistDeps`
- Token costs are tracked per-session in `token_ledger` table
- The existing `SessionRepository.GetByUserID` returns all sessions for a user

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the orchestration engine that routes AI conversations through topologies of places and transitions |
| **Session** | A server-side conversation instance with a unique ID, bound to a user, containing messages, events, and cost tracking |
| **Chat** | The user-facing representation of a session; displayed in the sidebar with title, preview, cost |
| **Fork** | Creating a new session that copies messages from an existing session up to a specified point, with reset billing |
| **Fork Point** | The message index at which a conversation is cloned; messages after this point are NOT copied |
| **HITL** | Human-In-The-Loop — a CPN transition that pauses execution until the user approves/rejects |
| **Soft Delete** | Setting `deleted_at` timestamp on a session; the session is excluded from listings but preserved in the database |
| **Token Ledger** | Per-session tracking of LLM token usage (input/output tokens, calls, total cost in USD) |
| **Flow Hash** | SHA-256 hash of a CPN topology; used to deduplicate and link sessions to their execution flow |
| **SSE** | Server-Sent Events — the streaming protocol delivering real-time CPN events and LLM chunks to the frontend |
| **Google Sub** | The immutable `sub` claim from Google's OAuth JWT; used as primary user identifier |

## 3. Requirements, Constraints & Guidelines

### Chat List Management

- **REQ-001**: The system SHALL provide a `GET /api/v1/sessions` endpoint that returns paginated sessions for the authenticated user, ordered by `last_activity_at DESC`, excluding soft-deleted sessions.
- **REQ-002**: Each session listing item SHALL include: `id`, `title`, `state`, `last_message_preview` (first 120 chars of last message content), `last_activity_at`, `created_at`, `total_cost_usd`, `message_count`, `forked_from_session_id` (nullable).
- **REQ-003**: The system SHALL provide a `PATCH /api/v1/sessions/{id}` endpoint to update session `title` and/or perform soft-delete (set `deleted_at`).
- **REQ-004**: Session titles SHALL be auto-generated from the first user message (first 80 characters, truncated at word boundary) when not explicitly set.
- **REQ-005**: The frontend SHALL display a collapsible chat sidebar listing all active chats with title, time-ago label, cost badge, and last message preview.
- **REQ-006**: Users SHALL be able to create a new chat from the sidebar, which creates a new backend session and switches the active view.
- **REQ-007**: Users SHALL be able to soft-delete a chat via a contextual action (swipe, menu, or button) with a confirmation step.
- **REQ-008**: Users SHALL be able to switch between chats; switching loads the session's messages and reconnects the SSE stream.

### Conversation Forking

- **REQ-009**: The system SHALL provide a `POST /api/v1/sessions/{id}/fork` endpoint that creates a new session by copying messages from the original up to a specified `message_index` (0-based, inclusive).
- **REQ-010**: The forked session SHALL have a NEW `token_ledger` entry starting at zero (0 input tokens, 0 output tokens, 0 calls, $0.00 cost).
- **REQ-011**: The forked session SHALL record `forked_from_session_id` and `fork_message_count` to maintain lineage.
- **REQ-012**: The forked session SHALL inherit the same `flow_hash` as the original session (same CPN topology).
- **REQ-013**: The original session SHALL NOT be modified in any way by the fork operation (messages, cost, state preserved).
- **REQ-014**: The forked session SHALL be independently continuable — users can send new messages that trigger new CPN executions with independent cost tracking.
- **REQ-015**: The forked session's execution history in the CPN Monitor SHALL show ONLY executions that occurred after the fork (not the original's executions).
- **REQ-016**: The frontend SHALL provide a fork action on each chat that opens a dialog showing the conversation with a slider/selector to choose the fork point.
- **REQ-017**: The frontend SHALL visually indicate forked chats with a fork icon and a link to the original conversation.

### Billing & Cost Isolation

- **REQ-018**: Each session's cost SHALL be tracked independently in `token_ledger` keyed by `session_id`.
- **REQ-019**: The forked session's in-memory `TokenLedger` SHALL start fresh (no carry-over from the original).
- **REQ-020**: The session list SHALL display `total_cost_usd` from the `token_ledger` for each session.

### Security & Authorization

- **SEC-001**: All new endpoints SHALL enforce Google OAuth authentication via the existing `AuthMiddleware`.
- **SEC-002**: Session listing SHALL only return sessions owned by the authenticated user (`user_id = google_sub`).
- **SEC-003**: Fork, update, and delete operations SHALL verify session ownership before execution.
- **SEC-004**: Soft-deleted sessions SHALL NOT be accessible via `GET /sessions/{id}` unless the user is the owner (for audit purposes via a future admin API).

### Constraints

- **CON-001**: The migration MUST be backward-compatible — existing sessions without `title`, `deleted_at`, `forked_from_session_id`, or `fork_message_count` SHALL continue to function.
- **CON-002**: The fork operation MUST be atomic — either all messages are copied and the new session is created, or the operation fails entirely (database transaction).
- **CON-003**: The in-memory session map in `SessionService` MUST remain the source of truth for active sessions; the fork only persists to Postgres and loads into memory on demand.
- **CON-004**: SSE connections MUST be cleanly disconnected when switching chats and re-established for the new active chat.
- **CON-005**: The chat sidebar MUST NOT cause additional API calls while a chat is active (only fetch list on mount and after mutations).

### Guidelines

- **GUD-001**: Follow the existing hexagonal architecture — new endpoints in `httpapi`, business logic in `app`, persistence contracts in `persist`, implementations in `store/postgres`.
- **GUD-002**: Use cursor-based pagination for session listing, consistent with existing `Page[T]` pattern.
- **GUD-003**: Frontend state management SHALL use React hooks and reducers, consistent with the existing `useChat` pattern — no external state libraries.
- **GUD-004**: Apply glass-morphism dark theme design patterns consistent with existing UI (CSS variables, JetBrains Mono, DM Sans fonts).
- **GUD-005**: Fork lineage visualization (showing parent → child relationships) is a future enhancement; this spec only requires storing the lineage data and displaying a simple fork indicator.

### Patterns

- **PAT-001**: Session listing follows the existing `GetByUserID` pattern but adds pagination, filtering, and enrichment with ledger data.
- **PAT-002**: Fork follows a "snapshot + create" pattern: read original messages → create new session → bulk-insert copied messages → return new session.
- **PAT-003**: Title auto-generation follows a "first-write-wins" pattern: generated on first user message, never auto-updated after explicit user edit.
- **PAT-004**: Chat switching follows the "disconnect + reconnect" pattern: close existing EventSource → clear local state → load new session → open new EventSource.

## 4. Interfaces & Data Contracts

### 4.1 Database Migration (008_chat_management)

```sql
-- 008_chat_management.up.sql

-- Add chat management columns to sessions
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS forked_from_session_id TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS fork_message_count INT;

-- Index for listing active chats (exclude soft-deleted)
CREATE INDEX IF NOT EXISTS idx_sessions_user_active
  ON sessions (user_id, last_activity_at DESC)
  WHERE deleted_at IS NULL AND state != 'expired';

-- Index for fork lineage queries
CREATE INDEX IF NOT EXISTS idx_sessions_forked_from
  ON sessions (forked_from_session_id)
  WHERE forked_from_session_id IS NOT NULL;
```

```sql
-- 008_chat_management.down.sql

DROP INDEX IF EXISTS idx_sessions_forked_from;
DROP INDEX IF EXISTS idx_sessions_user_active;
ALTER TABLE sessions DROP COLUMN IF EXISTS fork_message_count;
ALTER TABLE sessions DROP COLUMN IF EXISTS forked_from_session_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE sessions DROP COLUMN IF EXISTS title;
```

### 4.2 Updated Persistence Types

```go
// SessionRecord — extended fields (additions only)
type SessionRecord struct {
    // ... existing fields ...
    Title               string     // Auto-generated or user-set chat title
    DeletedAt           *time.Time // Soft-delete timestamp; nil = active
    ForkedFromSessionID string     // Source session ID if forked; empty = original
    ForkMessageCount    int        // Number of messages copied at fork time
}

// SessionListItem is a lightweight DTO for session listing (avoids loading all messages).
type SessionListItem struct {
    ID                  string
    Title               string
    State               SessionState
    LastMessagePreview  string     // First 120 chars of last message
    LastActivityAt      time.Time
    CreatedAt           time.Time
    TotalCostUSD        float64    // From token_ledger JOIN
    MessageCount        int        // COUNT(messages)
    ForkedFromSessionID string     // Empty if original
}

// SessionListOpts provides filtering and pagination for session listing.
type SessionListOpts struct {
    Limit  int    // Default: 50, max: 100
    Cursor string // Keyset cursor: "last_activity_at_unix|session_id"
}

// ForkRequest contains parameters for forking a session.
type ForkRequest struct {
    MessageIndex int // 0-based inclusive index; messages[0..index] are copied
}
```

### 4.3 Updated Repository Interface

```go
// SessionRepository — new methods (additions to existing interface)
type SessionRepository interface {
    // ... existing methods ...

    // ListByUserID returns paginated session list items for a user,
    // excluding soft-deleted and expired sessions, ordered by last_activity_at DESC.
    // Enriched with message count and cost from JOINs.
    ListByUserID(ctx context.Context, userID string, opts *SessionListOpts) (*Page[*SessionListItem], error)

    // UpdateTitle sets the session title. Returns ErrSessionNotFound if absent.
    UpdateTitle(ctx context.Context, sessionID string, title string) error

    // SoftDelete sets deleted_at on a session. Returns ErrSessionNotFound if absent.
    SoftDelete(ctx context.Context, sessionID string) error

    // ForkSession atomically creates a new session copying messages from the source.
    // Returns the new session record. The caller is responsible for in-memory session creation.
    ForkSession(ctx context.Context, newSessionID string, sourceSessionID string, messageIndex int, userID string, channel string) (*SessionRecord, error)
}
```

### 4.4 Backend HTTP API

#### `GET /api/v1/sessions` — List User Sessions

**Request**:
```
GET /api/v1/sessions?limit=50&cursor=<opaque>
Authorization: Bearer <google_id_token>
```

**Response** (200 OK):
```json
{
  "items": [
    {
      "id": "a1b2c3d4...",
      "title": "Help me design a REST API",
      "state": "idle",
      "last_message_preview": "Sure! Let me help you design a RESTful API. First, let's identify the resources...",
      "last_activity_at": "2026-04-04T10:30:00Z",
      "created_at": "2026-04-04T09:00:00Z",
      "total_cost_usd": 0.0234,
      "message_count": 12,
      "forked_from_session_id": ""
    }
  ],
  "next_cursor": "1712223000|a1b2c3d4",
  "has_more": true
}
```

#### `PATCH /api/v1/sessions/{id}` — Update Session

**Request** (update title):
```json
{
  "title": "My Custom Chat Title"
}
```

**Request** (soft-delete):
```json
{
  "deleted": true
}
```

**Response** (200 OK):
```json
{
  "status": "updated"
}
```

#### `POST /api/v1/sessions/{id}/fork` — Fork Conversation

**Request**:
```json
{
  "message_index": 5
}
```

**Response** (201 Created):
```json
{
  "id": "new-session-id-hex",
  "user_id": "google-sub-123",
  "channel": "web",
  "state": "idle",
  "created_at": "2026-04-04T11:00:00Z",
  "title": "Fork of: Help me design a REST API",
  "forked_from_session_id": "original-session-id",
  "fork_message_count": 6,
  "total_cost_usd": 0.0
}
```

### 4.5 Frontend API Service Extensions

```typescript
// New types in types/api.ts

export interface SessionListItem {
  id: string;
  title: string;
  state: SessionState;
  last_message_preview: string;
  last_activity_at: string;
  created_at: string;
  total_cost_usd: number;
  message_count: number;
  forked_from_session_id: string;
}

export interface SessionListResponse {
  items: SessionListItem[];
  next_cursor: string;
  has_more: boolean;
}

export interface UpdateSessionRequest {
  title?: string;
  deleted?: boolean;
}

export interface ForkSessionRequest {
  message_index: number;
}

export interface ForkSessionResponse extends SessionResponse {
  title: string;
  forked_from_session_id: string;
  fork_message_count: number;
  total_cost_usd: number;
}

// New API functions in services/api.ts

export async function listSessions(cursor?: string, limit?: number): Promise<SessionListResponse>;
export async function updateSession(sessionId: string, req: UpdateSessionRequest): Promise<StatusResponse>;
export async function forkSession(sessionId: string, req: ForkSessionRequest): Promise<ForkSessionResponse>;
```

### 4.6 Frontend Component Architecture

```
DesktopLayout
├── StatusBar (existing)
├── ChatSidebar (NEW)
│   ├── NewChatButton
│   ├── ChatList
│   │   └── ChatListItem (for each session)
│   │       ├── Title + fork indicator
│   │       ├── Preview + time-ago
│   │       ├── Cost badge
│   │       └── Context menu (rename, fork, delete)
│   └── (collapsible via toggle button)
├── Main Content Area
│   ├── Chat View (existing MessageList + MessageInput)
│   ├── Flows View (existing)
│   └── Monitor View (existing)
├── ForkDialog (NEW - modal)
│   ├── Conversation preview (scrollable)
│   ├── Fork point selector (message index)
│   ├── Summary (messages to copy, cost reset notice)
│   └── Confirm/Cancel buttons
└── Dock (existing)
```

### 4.7 Frontend Hook: `useChatList`

```typescript
interface UseChatListReturn {
  chats: SessionListItem[];
  isLoading: boolean;
  activeSessionId: string | null;
  createChat: () => Promise<void>;
  switchChat: (sessionId: string) => Promise<void>;
  deleteChat: (sessionId: string) => Promise<void>;
  renameChat: (sessionId: string, title: string) => Promise<void>;
  forkChat: (sessionId: string, messageIndex: number) => Promise<void>;
  loadMore: () => Promise<void>;
  hasMore: boolean;
  refresh: () => Promise<void>;
}
```

### 4.8 Updated `useChat` Hook Changes

The existing `useChat` hook needs the following modifications:

1. **Accept `sessionId` as prop** instead of auto-creating on mount — the `useChatList` hook controls which session is active.
2. **Remove localStorage session storage** — `useChatList` manages the active session selection.
3. **Expose `loadSession(sessionId)`** method for chat switching.
4. **Add `clearAndDisconnect()`** for clean teardown when switching chats.

```typescript
// Updated signature
export function useChat(sessionId: string | null, options?: UseChatOptions): UseChatReturn;

// The hook reacts to sessionId changes:
// - null → idle state, no SSE connection
// - new value → load session messages, connect SSE
// - changed value → disconnect old SSE, load new session, connect new SSE
```

## 5. Acceptance Criteria

### Chat List Management

- **AC-001**: Given an authenticated user with 3 sessions, When they call `GET /api/v1/sessions`, Then the response contains all 3 sessions ordered by `last_activity_at DESC` with correct titles, previews, costs, and message counts.
- **AC-002**: Given a user views the chat sidebar, When a session has no explicit title, Then the title is auto-generated from the first user message (truncated to 80 chars at word boundary).
- **AC-003**: Given a user soft-deletes a chat, When they subsequently list sessions, Then the deleted chat does NOT appear in the list, AND the session data is preserved in the database.
- **AC-004**: Given a user clicks "New Chat" in the sidebar, When the action completes, Then a new session is created on the backend, the sidebar shows the new chat as active, and the message area is empty.
- **AC-005**: Given a user switches from Chat A (running) to Chat B, When the switch occurs, Then the SSE connection for Chat A is disconnected, Chat B's messages load, and a new SSE connection opens for Chat B. Chat A continues running in the background on the server.

### Conversation Forking

- **AC-006**: Given a session with 10 messages, When the user forks at message_index=5, Then a new session is created with messages[0..5] (6 messages) copied, AND the new session has `total_cost_usd = 0.00`, AND `forked_from_session_id` points to the original.
- **AC-007**: Given a forked session, When the user sends a new message, Then the CPN executes independently, costs accumulate only in the forked session's ledger, and the original session's cost is unchanged.
- **AC-008**: Given a forked session, When the user opens the CPN Monitor, Then only executions from the forked session are displayed (not the original's executions).
- **AC-009**: Given a fork operation fails mid-transaction (e.g., database error), When the error is caught, Then NO partial session or messages are created (transaction rollback), AND the user sees an error message.
- **AC-010**: Given a session that was forked, When the user views the chat list, Then the forked chat shows a fork icon and the original chat is unmodified.

### Billing

- **AC-011**: Given two sessions (original and fork), When the fork sends messages that cost $0.05, Then the fork's `total_cost_usd` is $0.05, AND the original's `total_cost_usd` remains at its pre-fork value.

### Performance

- **AC-012**: Given a user with 100+ sessions, When they load the chat sidebar, Then the initial list loads in under 500ms (first page, 50 items).
- **AC-013**: Given a fork operation on a session with 200 messages at message_index=100, When the fork executes, Then it completes in under 2 seconds including all message copies.

## 6. Test Automation Strategy

### Test Levels

**Unit Tests (Go)**:
- `session_service_test.go`: Test fork logic (message copying, ledger reset, lineage tracking)
- `handler_session_test.go`: Test new endpoints (list, update, fork) with mock repositories
- `session_repo_test.go`: Test `ListByUserID`, `SoftDelete`, `ForkSession`, `UpdateTitle` SQL logic

**Integration Tests (Go)**:
- `store_integration_test.go`: Test fork transaction atomicity against real PostgreSQL
- Test session listing with ledger JOINs against real data

**Unit Tests (TypeScript)**:
- `useChatList.test.ts`: Test chat list state management, CRUD operations, optimistic updates
- `ChatSidebar.test.tsx`: Test rendering, interactions, accessibility
- `ForkDialog.test.tsx`: Test fork point selection, validation, submission

**End-to-End**:
- Create session → send messages → fork → verify independent cost tracking
- Create session → soft-delete → verify exclusion from list → verify data preserved

### Frameworks
- **Go**: `testing` stdlib, `testify/assert`, `pgx` for integration tests
- **TypeScript**: Vitest (or existing test framework), React Testing Library

### Coverage Requirements
- New Go code: ≥80% line coverage
- New TypeScript hooks: ≥85% branch coverage
- Critical paths (fork atomicity, cost isolation): 100% coverage

## 7. Rationale & Context

### Why Multi-Chat?

The current single-session-per-user model limits the assistant's utility. Users cannot:
- Maintain parallel conversation threads for different topics
- Return to previous conversations after starting new ones
- Experiment with different approaches to the same problem

### Why Forking Instead of Branching?

**Forking** (full copy with independent lifecycle) was chosen over **branching** (shared history with divergence point) because:
1. **Simpler implementation**: No need for shared message tree or conflict resolution
2. **Cost isolation**: Clean billing boundary — the forked session has its own ledger from zero
3. **CPN independence**: Each session has its own in-memory CPN; sharing CPN state across sessions would violate the executor's single-owner model (PAT-001 in executor.go)
4. **Audit clarity**: Each session is a complete, self-contained conversation record

### Why Soft Delete?

Soft delete preserves conversation data for:
- Audit trails (compliance)
- Potential "undo delete" in future
- Analytics (understanding user behavior across all conversations)
- Fork lineage integrity (a forked session may reference a deleted original)

### Why Auto-Generate Titles?

Users rarely name conversations proactively. Auto-generating from the first message provides:
- Immediate visual identification in the sidebar
- Zero-effort organization
- Consistent UX (every chat has a title from the start)

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Google OAuth 2.0 — User identity for session ownership (existing)
- **EXT-002**: OpenRouter API — LLM execution and token cost tracking (existing)

### Infrastructure Dependencies
- **INF-001**: PostgreSQL 16 — Session, message, ledger persistence. The fork operation requires transaction support (existing).
- **INF-002**: Redis 7 — Session cache-aside. Cache must be invalidated on fork creation (existing).

### Data Dependencies
- **DAT-001**: `token_ledger` table — Provides `total_cost_usd` for session list enrichment via LEFT JOIN.
- **DAT-002**: `messages` table — Provides `message_count` (COUNT) and `last_message_preview` (last message content) via subquery/JOIN.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ — Required for enhanced `http.ServeMux` routing patterns (existing).
- **PLT-002**: React 19 + TypeScript 5.7 — Frontend framework (existing).
- **PLT-003**: Tailwind CSS v4 — Styling framework (existing).

## 9. Examples & Edge Cases

### Edge Case 1: Fork at Message Index 0

```json
// Request: Fork with only the first message
POST /api/v1/sessions/abc123/fork
{ "message_index": 0 }

// Result: New session with 1 message (the first user message)
// The user can then take the conversation in a completely different direction
```

### Edge Case 2: Fork of a Fork

```json
// Session A (original) → Fork at msg 5 → Session B
// Session B has 8 messages (5 copied + 3 new)
// Session B → Fork at msg 3 → Session C

// Session C:
// - forked_from_session_id: Session B's ID
// - fork_message_count: 4 (messages[0..3])
// - Messages are copied from Session B (which includes some originally from A)
// - Cost starts at $0.00
// - No reference to Session A (lineage is one level deep)
```

### Edge Case 3: Fork a Session While It's Running

```json
// Session is in "running" state (CPN executing)
POST /api/v1/sessions/abc123/fork
{ "message_index": 5 }

// Result: Fork succeeds using the PERSISTED messages (not in-memory)
// Only messages already persisted to DB are copied
// The running execution's eventual output is NOT included in the fork
// The fork is a snapshot of the conversation at the DB level
```

### Edge Case 4: Switching to a Chat That Was Deleted Concurrently

```typescript
// User A has the sidebar open with Chat X listed
// Concurrently, something soft-deletes Chat X (e.g., browser tab race)
// User A clicks Chat X

// Frontend: GET /sessions/{chatX_id} returns 404 or session with deleted_at set
// Frontend: Show "This conversation has been deleted" toast
// Frontend: Refresh the chat list to remove the stale entry
```

### Edge Case 5: Session Title Generation with Empty or Very Long Messages

```go
// First user message: "" (empty after trim)
// Title: "New Chat" (fallback)

// First user message: "I need help with the implementation of the distributed consensus algorithm based on Raft protocol with leader election, log replication, and safety guarantees..."
// Title: "I need help with the implementation of the distributed consensus algorithm based" (80 chars, word boundary)
```

### Edge Case 6: Cost Display for Session Without Ledger Entry

```sql
-- New session with no messages sent yet → no token_ledger row
-- LEFT JOIN returns NULL for total_cost_usd
-- Display: $0.00 (COALESCE to 0)
```

## 10. Validation Criteria

1. **Database migration** runs without error on existing data; existing sessions retain all functionality.
2. **Session listing** returns correct data for users with 0, 1, 50, and 100+ sessions.
3. **Fork operation** is atomic — partial failures produce zero side effects.
4. **Cost isolation** is verified: sending a message in a forked session does NOT increment the original's `token_ledger`.
5. **SSE reconnection** works correctly when switching between chats — no duplicate events, no missed events.
6. **Soft-deleted sessions** do not appear in any user-facing listing or count.
7. **Fork lineage** is correctly stored and displayed (fork icon, link to original).
8. **Auto-generated titles** are correct for edge cases (empty message, very long message, non-ASCII content).
9. **Pagination** works correctly under concurrent session creation/deletion.
10. **Authorization** — users cannot list, fork, update, or delete sessions they don't own.

## 11. Related Specifications / Further Reading

- [spec-architecture-google-oauth-login.md](spec-architecture-google-oauth-login.md) — User authentication and session ownership model
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md) — HTTP API with SSE streaming architecture
- [spec-architecture-clear-conversation.md](spec-architecture-clear-conversation.md) — Existing "New Conversation" delete-and-recreate flow (superseded by chat list for session management)
- [spec-design-cpn-execution-visualizer.md](spec-design-cpn-execution-visualizer.md) — CPN topology visualization and execution replay
- [spec-design-cpn-execution-monitor.md](spec-design-cpn-execution-monitor.md) — Real-time execution monitoring
