---
title: Conversation Rewind via Fork-and-Promote
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: liwaisi-tech
tags: [architecture, cpn, session, rewind, hitl, a2ui, ux, accessibility, backend, frontend]
---

# Introduction

This specification defines the architecture and design for **Conversation Rewind** — a feature that lets a user return to an earlier Human-In-The-Loop (HITL) block in a conversation, edit their previous answer (including the free-text "Other" option of a questionnaire), and re-run the agent from that point. The feature is implemented as a **branch-and-promote** model under a stable `session_id`, preserving Axiom A9 (events are append-only) and reusing 80% of the existing conversation-forking machinery. The UX is designed with explicit requirements for users with **aphantasia** (cannot mentally visualize state) and **anxiety** (require predictability and reversibility).

## 1. Purpose & Scope

**Purpose**: Give the user a safe, reversible way to revise a prior decision inside an ongoing conversation without losing their place, spawning a separate chat, or re-paying for deterministic upstream LLM work. The mental model is git `reset` combined with git `reflog`: the old timeline is archived (not destroyed), the new edited timeline becomes canonical, and an undo window lets the user revert cleanly.

**Scope**:
- **Backend (Go, `back/go-assistant`)**: New `session_branches` table; `branch_id` columns on `messages`, `events`, `cpn_markings`; CPN marking checkpointing at HITL boundaries; idempotent replay cache for deterministic transitions; new HTTP endpoints for rewind, promote, undo, and archived-branch garbage collection.
- **Frontend (React/TypeScript, `front/react-assistant`)**: A `RewindDialog` distinct from the existing `ForkDialog`; an "Edit this answer" affordance on locked questionnaires (`QuestionnaireLocked`); a persistent **Rewind Mode banner**; a diff-style preview of discarded turns; a before/after side-by-side view for free-text "Other" edits; a persistent **Undo toast** that lives on the page (not ephemeral).
- **UX**: Accessibility requirements specifically addressing aphantasia (no reliance on mental state, explicit textual anchors, stable deep-linkable IDs) and anxiety (preview-before-commit, undo grace window, no destructive ephemeral actions).
- **Out of scope**: Multi-user collaborative rewind; rewinding transitions that perform external side effects (tool calls with real-world mutation) — these are addressed conservatively (see REQ-023, EDGE-004) but a full treatment is deferred.

**Intended Audience**: Backend engineers (Go, CPN engine, persistence), frontend engineers (React/TypeScript, A2UI protocol), UX designers, accessibility reviewers, AI agent engineers working on CPN topologies.

**Assumptions**:
- The multi-chat forking feature described in [spec-design-multi-chat-conversation-forking.md](spec-design-multi-chat-conversation-forking.md) is already shipped (session list, `ForkDialog`, `forkSession` API, `forked_from_session_id` column).
- The HITL response persistence and lock-after-submit behavior described in [spec-process-bugfix-a2ui-hitl-response-persistence.md](spec-process-bugfix-a2ui-hitl-response-persistence.md) is in place (`parent_message_id`, `QuestionnaireLocked`, `resolvedPayload`, `resolvedAt`).
- Users are authenticated via Google OAuth; session ownership is enforced by the existing `AuthMiddleware`.
- The CPN engine runs in-memory per session and persists messages/events/markings best-effort via `PersistDeps`.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the orchestration engine executing conversations through Places, Transitions, and Tokens. Source: `back/go-assistant/cpn/cpn.go:12-86`. |
| **HITL** | Human-In-The-Loop — a CPN transition that blocks until the user submits a response. Source: `back/go-assistant/cpn/hitl.go:88-120`. |
| **A2UI** | Agent-to-UI protocol — the payload format (prefixed `$$a2ui:` inside `stream_chunk.Content`) that the backend emits to describe UI components the frontend must render. Source: `front/react-assistant/src/features/chat/a2ui/types.ts`. |
| **Marking** | A snapshot of the CPN's state: the set of tokens in each Place plus the current Mode (MAS or Centaurian), taken at a specific `seq`. |
| **Block** | The user-facing concept matching a CPN checkpoint: a boundary where a marking is captured (typically at every HITL prompt and every completed message turn). Analogous to a blockchain "block". |
| **Branch** | A linear sequence of messages/events/markings identified by `branch_id`. A session has exactly one **active** branch at any time; previous branches become **archived**. |
| **Rewind** | Operation that creates a new branch from an earlier block, promotes it to active, archives the previous active branch, and re-runs the CPN from the rewound marking with edited inputs. |
| **Promote** | The atomic step that points `sessions.current_branch_id` to a new branch id. All reads (list, stream, CPN load) filter by the active branch. |
| **Archive** | A branch that is no longer active but retained for the Undo Grace Window. After the window expires, it is hard-deleted by a background job. |
| **Undo Grace Window** | The period (default 60 seconds) after a rewind during which the user can restore the previously-active branch with one click. |
| **Fork vs Rewind** | **Fork** creates a *new session* with its own `session_id` and billing ledger (existing feature). **Rewind** creates a *new branch inside the same session_id*, archiving the previous branch; billing continues against the same ledger. |
| **Replay Cache** | A keyed cache of transition outputs (`transition_outputs` table) that lets deterministic transitions short-circuit to a previously-produced token when their input marking hash is unchanged, avoiding cost and non-determinism. |
| **Axiom A9** | Existing architectural invariant: events and event records are append-only; no deletion or mutation. Source: `back/go-assistant/cpn/persist/types.go:92`. |
| **Aphantasia** | The condition of not being able to form mental images. UX implication: state must be explicit in the DOM, not inferred from transient signals. |
| **Anxiety-Safe UX** | UI that avoids destructive ephemeral actions, previews consequences before commit, and provides clear undo paths. |

## 3. Requirements, Constraints & Guidelines

### CPN Checkpointing (Backend)

- **REQ-001**: The CPN executor SHALL capture a **marking snapshot** at the following boundaries: (a) immediately before a HITL transition blocks (pre-HITL marking), (b) immediately after a HITL response is consumed and downstream tokens are deposited (post-HITL marking), (c) on successful completion of a user-visible assistant turn (end-of-turn marking).
- **REQ-002**: A marking snapshot SHALL record: `branch_id`, monotonically-increasing `seq` within the branch, the full token contents of every Place (as JSONB), the CPN `Mode` (`MAS` | `Centaurian`), a `transition_id` if the snapshot is transition-aligned, and `created_at`.
- **REQ-003**: Marking snapshots SHALL be persisted via a new `cpn_markings` table and a new `MarkingRepository` port in `cpn/persist/interfaces.go`. Persistence failure SHALL NOT block execution but SHALL emit a warning log (`persist.marking.failed`) consistent with the existing best-effort `PersistDeps` pattern.
- **REQ-004**: Each CPN `Token` persisted inside a marking SHALL carry `OriginID`, `OriginDepth`, `Color`, `Payload`, and `Space` exactly as in-memory (reuse `cpn.Token` serialization). Tokens are immutable; markings are immutable once written.

### Branching & Promotion

- **REQ-005**: The `sessions` table SHALL gain a `current_branch_id` column (`TEXT`, not null after backfill). On existing sessions the column SHALL be backfilled to the initial branch's id created by the migration.
- **REQ-006**: A new `session_branches` table SHALL store: `id` (pk, hex), `session_id`, `parent_branch_id` (nullable — null for the root branch), `forked_at_marking_seq` (nullable — null for root), `forked_at_message_id` (nullable), `state` (`active` | `archived` | `purged`), `created_at`, `archived_at` (nullable), `purge_after` (nullable — the earliest time this branch may be hard-deleted).
- **REQ-007**: `messages`, `events`, and `cpn_markings` SHALL all carry a `branch_id` column. All read paths (session load, SSE stream replay, CPN rehydration, monitor views) SHALL filter by the session's current active branch unless an explicit archived-branch lookup is requested.
- **REQ-008**: A **rewind** operation SHALL: (1) open a DB transaction; (2) create a new branch row with `parent_branch_id` = current active branch, `forked_at_marking_seq` = the target block's marking seq; (3) copy messages, events, and markings from the parent branch up to the target `message_id` inclusive, re-stamped with the new `branch_id` and fresh `id`s (events keep their original `timestamp`); (4) mark the previous active branch as `archived` with `purge_after = now() + undo_grace_window`; (5) update `sessions.current_branch_id` to the new branch; (6) commit. The transaction SHALL be all-or-nothing.
- **REQ-009**: On the new active branch, the HITL response message being edited SHALL be rewritten in place (still append semantics inside the *new* branch — no mutation of the archived branch) with the user's revised `resolvedPayload`. All messages and events that existed *after* this message on the parent branch SHALL NOT be copied.
- **REQ-010**: Immediately after commit, the backend SHALL rehydrate the in-memory CPN from the new branch's last marking, re-fire the HITL transition output with the edited response token, and resume normal execution. Frontend clients already connected to SSE SHALL receive a new event `branch_switched` followed by the resumed stream (see §4.4).

### Idempotent Replay Cache

- **REQ-011**: A `transition_outputs` table SHALL cache outputs of deterministic transitions keyed by `(transition_id, input_marking_hash)` where `input_marking_hash` is a stable SHA-256 hash over the sorted multiset of input tokens (Color + Payload canonical JSON). Stored value is the produced output token(s).
- **REQ-012**: Transitions in the CPN topology SHALL declare a `ReplayPolicy` of `Deterministic`, `NonDeterministic`, or `SideEffectful`. LLM transitions default to `NonDeterministic` unless an explicit `replay_cache=true` kind-flag is set at topology construction. Tool transitions with real-world side effects default to `SideEffectful`.
- **REQ-013**: On a rewind replay, the executor SHALL consult the cache for every `Deterministic` transition. A cache hit SHALL produce the cached output token without invoking the transition's work function. `NonDeterministic` transitions SHALL execute fresh. `SideEffectful` transitions SHALL NOT auto-replay; the backend SHALL instead emit a `hitl_requested` surface asking the user to confirm or skip the side-effectful step (see EDGE-004).
- **REQ-014**: Replay cache entries SHALL be pruned when the branch whose execution produced them is purged (see REQ-017).

### Undo Grace Window & Garbage Collection

- **REQ-015**: The default Undo Grace Window SHALL be **60 seconds**, configurable per deployment via `REWIND_UNDO_WINDOW_SECONDS`.
- **REQ-016**: While a branch is `archived` and within its grace window, a `POST /api/v1/sessions/{id}/rewind/undo` endpoint SHALL restore it as active and archive the currently-active branch in its place (symmetric operation). A branch may be re-undone until the previous branch's grace window itself expires.
- **REQ-017**: A background job (interval: 5 minutes, configurable) SHALL find `archived` branches where `purge_after < now()`, transition them to `purged` state, and hard-delete their messages, events, markings, and replay-cache rows in a single transaction. `session_branches` rows remain (now with `state='purged'`) for lineage audit; their child-referencing columns are retained.
- **REQ-018**: Undo operations on a `purged` branch SHALL return HTTP 410 Gone with code `branch_purged`.

### Frontend Rewind UX

- **REQ-019**: The frontend SHALL render an **"Edit this answer"** control on every `QuestionnaireLocked` surface (`A2UIMessageRenderer.tsx:581-636`). The control SHALL be visible, keyboard-focusable, and labeled (`aria-label="Edit this answer and rewind"`). It SHALL NOT appear while the CPN is actively streaming a later turn (to avoid mid-stream state surgery).
- **REQ-020**: Clicking "Edit this answer" SHALL open a **`RewindDialog`** (distinct component from `ForkDialog`) containing: (a) the target block's metadata (turn number, timestamp, transition id), (b) the current questionnaire answers rendered in an editable form pre-filled with the user's prior values, (c) a **before/after side-by-side panel** for every free-text "Other" field (left: current value; right: proposed value; both simultaneously visible), (d) a **Discarded Turns Preview** list showing every message and HITL surface that will be removed from the active branch (rendered with strikethrough and a count badge), (e) a persistent summary line `"Rewind mode — Turn N of M · K turns will be archived"`, (f) explicit **Confirm Rewind** and **Cancel** buttons with no default/implicit keyboard submit (Enter inside a textarea SHALL NOT submit the dialog).
- **REQ-021**: The `RewindDialog` SHALL NOT auto-close on backdrop click. Closing requires explicit Cancel or a successful rewind. Rationale: anxiety-safe commit boundaries.
- **REQ-022**: After a successful rewind, the frontend SHALL render a persistent **Undo Toast** anchored to the top of the conversation panel (not a corner toast, not auto-dismissing via timer alone). The toast SHALL display: `"Rewound to Turn N. Undo available for MM:SS."` with a live-counting countdown (`aria-live="polite"`), an **Undo** button, and a **Dismiss** button. The toast disappears only when (a) the user dismisses it explicitly, (b) the undo window expires (the text transitions to `"Undo window expired."` for 3 seconds before fading), or (c) the user issues a subsequent rewind.
- **REQ-023**: If the selected rewind point is upstream of a transition declared `SideEffectful`, the `RewindDialog` SHALL surface a warning panel listing each side-effectful step (tool name, timestamp) and require a separate checkbox acknowledgement (`"I understand these steps will need to re-run or be skipped"`) before the Confirm button enables.
- **REQ-024**: Every block SHALL have a stable DOM anchor (`id="block-{branch_id}-{message_id}"`) and be addressable via URL fragment. Aphantasia rationale: users can navigate by re-reading, leave, and return to the exact same visible anchor without reconstructing mental state.
- **REQ-025**: The `RewindDialog` open/close, the active Rewind Mode, and the Undo Toast presence SHALL each be reflected in a visible state badge on the chat header (`"Rewind mode active"` while the dialog is open; `"Recently rewound — undo available"` while the toast is live). Badges SHALL be text, not icon-only.
- **REQ-026**: The existing `ForkDialog` SHALL be retained unchanged. Its entry point SHALL be renamed in the UI copy from "Fork" to **"Fork to new chat (keep this one)"**, and the rewind entry point SHALL be labeled **"Rewind and edit (replaces turns after here)"**, to make the destructive-but-undoable nature explicit.

### Accessibility: Aphantasia & Anxiety

- **REQ-027**: No state required to understand the rewind flow SHALL exist only in animation, transient toast, or ephemeral visual cue. Every piece of state SHALL have a persistent textual representation somewhere in the current viewport or a clearly-labeled panel.
- **REQ-028**: All destructive actions (Confirm Rewind, Delete Chat, Fork into new chat, dismissal of the Undo Toast) SHALL require an explicit click on a button labeled with the action verb. Default keyboard submit (Enter) SHALL be disabled for these buttons when focus is on a content-editable element of the surrounding form.
- **REQ-029**: All time-based countdowns SHALL be displayed as concrete `MM:SS` values, not as progress bars alone. Progress bars MAY be present as secondary reinforcement but SHALL NOT be the sole indicator.
- **REQ-030**: The frontend SHALL NOT auto-scroll or auto-focus the viewport on rewind completion. The new stream SHALL render at the rewound position; the user retains their scroll frame of reference. A persistent **"Jump to live"** button (already appropriate for long conversations) SHALL be visible if the user is above the live edge.
- **REQ-031**: The rewind confirmation dialog SHALL pass `axe-core` automated accessibility checks at the WCAG 2.2 AA level for color contrast, focus order, and ARIA labeling.

### Security & Authorization

- **SEC-001**: All rewind endpoints SHALL enforce Google OAuth authentication and verify session ownership (`sessions.user_id = google_sub`) before any branch mutation.
- **SEC-002**: Archived and purged branches SHALL be accessible only to the owning user and only via explicitly-scoped endpoints (no accidental inclusion in session listing, monitor views, or SSE replay).
- **SEC-003**: The rewind endpoint SHALL be rate-limited per user (default: 20 rewinds / hour) to prevent runaway replay-cache cost in pathological edit loops. Limits are configurable.
- **SEC-004**: Replay cache entries SHALL be scoped by `session_id` (never shared across sessions) to prevent cross-session token leakage, even when token payload hashes collide.

### Constraints

- **CON-001**: Axiom A9 (events are append-only within a branch) MUST remain unviolated. Rewind is implemented by creating a new branch and switching the pointer — the parent branch's events are never modified or deleted in-place, only eventually purged *as a whole branch*.
- **CON-002**: The rewind DB transaction MUST be atomic. Partial failure (e.g., marking copy succeeds, pointer flip fails) MUST roll back to the pre-rewind state.
- **CON-003**: The in-memory CPN per session is single-owner. Rehydrating into a new branch MUST be done after the old branch's SSE broker has flushed pending events and the old CPN instance has been cleanly torn down. No two CPN instances per `session_id` MAY run concurrently.
- **CON-004**: A rewind that lands on a marking whose `Mode` differs from the current in-memory CPN MUST restore the recorded `Mode` before re-firing. Silent mode mismatch is a correctness bug.
- **CON-005**: Replay cache hits MUST NOT skip event emission: an `EventTransitionCompleted` with a `replayed=true` flag MUST still be emitted so the Monitor view shows the same execution trace.
- **CON-006**: The undo grace window's minimum is 30 seconds; the maximum is 24 hours. Values outside this range SHALL cause the service to refuse to start.
- **CON-007**: The `RewindDialog` UI MUST NOT attempt to render a discarded-turns preview longer than 200 items; above that threshold it SHALL render the first 50, the last 50, and a collapsed `"… N more turns …"` section with an expand affordance. Rationale: DOM performance and cognitive-load bounds.

### Guidelines

- **GUD-001**: Follow the existing hexagonal architecture. New endpoints live in `internal/driving/httpapi`; business logic in `app`; ports in `cpn/persist/interfaces.go`; Postgres implementations in `store/postgres`.
- **GUD-002**: The `RewindDialog` SHALL reuse the message-picker primitive from `ForkDialog.tsx:63-108` (range slider + message preview) rather than re-implement it. Extract the shared primitive into `features/chat/TurnPicker.tsx` if cleaner.
- **GUD-003**: Use React reducers and hooks consistent with `useChat.ts` — no external state libraries. Add a new reducer action `REWIND_COMPLETED` that replaces the message list atomically with the new branch's messages received from the rewind API response.
- **GUD-004**: Apply the glass-morphism dark theme with JetBrains Mono / DM Sans fonts. The Rewind Mode badge and Undo Toast SHALL use a distinct accent color from primary action buttons to avoid confusion (suggested: amber/warning palette).
- **GUD-005**: Event naming on the SSE channel SHALL use `branch_switched` (not `rewound` or `reset`) to keep the vocabulary neutral and reusable for future features such as manual branch promotion from archive.

### Patterns

- **PAT-001**: "Branch-and-promote" — the canonical implementation pattern. Rewind is never a destructive edit; it is always *create new branch → promote → archive previous*. This gives free undo, preserves audit, and honors Axiom A9.
- **PAT-002**: "Hash-keyed replay" — deterministic transitions are idempotent by virtue of their input marking hash. The cache is the single source of truth for "have I computed this before?" across branches in the same session.
- **PAT-003**: "Persistent undo affordance" — anxiety-safe UX treats undo as a first-class, non-ephemeral UI element anchored in the main viewport until explicitly dismissed or expired.
- **PAT-004**: "Explicit state over animation" — aphantasia-safe UX. All state transitions (dialog open, rewind in progress, undo available, undo expired) have a textual counterpart in the DOM at all times.

## 4. Interfaces & Data Contracts

### 4.1 Database Migration (009_rewind_branches)

```sql
-- 009_rewind_branches.up.sql

-- Branch registry: one row per branch in a session's lineage
CREATE TABLE session_branches (
    id                      TEXT PRIMARY KEY,
    session_id              TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_branch_id        TEXT REFERENCES session_branches(id),
    forked_at_marking_seq   BIGINT,
    forked_at_message_id    TEXT,
    state                   TEXT NOT NULL CHECK (state IN ('active','archived','purged')),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at             TIMESTAMPTZ,
    purge_after             TIMESTAMPTZ
);

CREATE INDEX idx_session_branches_session ON session_branches(session_id, state);
CREATE INDEX idx_session_branches_purge   ON session_branches(purge_after)
    WHERE state = 'archived';

-- Session points at its active branch
ALTER TABLE sessions ADD COLUMN current_branch_id TEXT
    REFERENCES session_branches(id);

-- Branch_id on every branch-scoped row
ALTER TABLE messages ADD COLUMN branch_id TEXT;
ALTER TABLE events   ADD COLUMN branch_id TEXT;

CREATE INDEX idx_messages_branch ON messages(branch_id, timestamp);
CREATE INDEX idx_events_branch   ON events(branch_id, timestamp);

-- CPN marking snapshots
CREATE TABLE cpn_markings (
    id              TEXT PRIMARY KEY,
    branch_id       TEXT NOT NULL REFERENCES session_branches(id) ON DELETE CASCADE,
    seq             BIGINT NOT NULL,
    cpn_id          TEXT NOT NULL,
    mode            TEXT NOT NULL CHECK (mode IN ('MAS','Centaurian')),
    transition_id   TEXT,
    message_id      TEXT,
    places          JSONB NOT NULL,  -- map<place_id, [Token...]>
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, seq)
);

CREATE INDEX idx_cpn_markings_branch_seq ON cpn_markings(branch_id, seq DESC);

-- Replay cache for deterministic transitions
CREATE TABLE transition_outputs (
    session_id          TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    transition_id       TEXT NOT NULL,
    input_marking_hash  TEXT NOT NULL,  -- sha256 hex
    output_tokens       JSONB NOT NULL,
    produced_by_branch  TEXT NOT NULL REFERENCES session_branches(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, transition_id, input_marking_hash)
);

CREATE INDEX idx_transition_outputs_branch ON transition_outputs(produced_by_branch);

-- Backfill: every existing session gets a root branch, and its rows are stamped
INSERT INTO session_branches (id, session_id, parent_branch_id, state, created_at)
SELECT
    encode(gen_random_bytes(16), 'hex'),
    id,
    NULL,
    'active',
    created_at
FROM sessions;

UPDATE sessions s
SET current_branch_id = (SELECT id FROM session_branches b WHERE b.session_id = s.id LIMIT 1);

UPDATE messages m SET branch_id = (
    SELECT current_branch_id FROM sessions WHERE id = m.session_id
);
UPDATE events   e SET branch_id = (
    SELECT current_branch_id FROM sessions WHERE id = e.session_id
);

ALTER TABLE sessions ALTER COLUMN current_branch_id SET NOT NULL;
ALTER TABLE messages ALTER COLUMN branch_id        SET NOT NULL;
ALTER TABLE events   ALTER COLUMN branch_id        SET NOT NULL;
```

```sql
-- 009_rewind_branches.down.sql
ALTER TABLE events   DROP COLUMN branch_id;
ALTER TABLE messages DROP COLUMN branch_id;
ALTER TABLE sessions DROP COLUMN current_branch_id;
DROP TABLE transition_outputs;
DROP TABLE cpn_markings;
DROP TABLE session_branches;
```

### 4.2 Persistence Ports (Go)

```go
// back/go-assistant/cpn/persist/interfaces.go (additions)

type BranchRepository interface {
    // Create inserts a new branch row.
    Create(ctx context.Context, b *BranchRecord) error

    // GetActive returns the currently-active branch for a session.
    GetActive(ctx context.Context, sessionID string) (*BranchRecord, error)

    // ListForSession returns all branches (any state) for a session, newest first.
    ListForSession(ctx context.Context, sessionID string) ([]*BranchRecord, error)

    // Promote atomically sets the given branch as active and archives the previous
    // active branch with the provided purge_after time. Errors if branch is not in
    // state=archived or if session/branch not found.
    Promote(ctx context.Context, sessionID, newActiveBranchID string, purgeAfter time.Time) error

    // Purge transitions an archived branch to state=purged and hard-deletes its
    // messages, events, markings, and replay-cache rows in a single transaction.
    Purge(ctx context.Context, branchID string) error

    // ListArchivedDue returns archived branches whose purge_after < now, capped at n.
    ListArchivedDue(ctx context.Context, now time.Time, n int) ([]*BranchRecord, error)
}

type MarkingRepository interface {
    // Save persists a marking snapshot. Idempotent on (branch_id, seq).
    Save(ctx context.Context, m *MarkingRecord) error

    // LatestForBranch returns the most recent marking for a branch.
    LatestForBranch(ctx context.Context, branchID string) (*MarkingRecord, error)

    // GetByMessage returns the marking taken immediately after a given message.
    GetByMessage(ctx context.Context, branchID, messageID string) (*MarkingRecord, error)

    // CopyRange copies markings from srcBranchID up to and including targetSeq
    // onto dstBranchID, preserving seq numbers. Used by the rewind transaction.
    CopyRange(ctx context.Context, srcBranchID, dstBranchID string, targetSeq int64) error
}

type ReplayCacheRepository interface {
    Get(ctx context.Context, sessionID, transitionID, inputHash string) (tokens []Token, ok bool, err error)
    Put(ctx context.Context, sessionID, transitionID, inputHash, producedByBranch string, tokens []Token) error
    DeleteByBranch(ctx context.Context, branchID string) error
}

type BranchRecord struct {
    ID                  string
    SessionID           string
    ParentBranchID      string     // "" for root
    ForkedAtMarkingSeq  *int64
    ForkedAtMessageID   string
    State               BranchState // active | archived | purged
    CreatedAt           time.Time
    ArchivedAt          *time.Time
    PurgeAfter          *time.Time
}

type MarkingRecord struct {
    ID           string
    BranchID     string
    Seq          int64
    CPNID        string
    Mode         string
    TransitionID string
    MessageID    string
    Places       map[string][]Token // place_id → tokens
    CreatedAt    time.Time
}
```

### 4.3 HTTP API

#### `GET /api/v1/sessions/{id}/branches` — List branches (lineage)

Response (200):
```json
{
  "current_branch_id": "b2...",
  "branches": [
    {
      "id": "b2...",
      "parent_branch_id": "b1...",
      "forked_at_message_id": "m42...",
      "forked_at_marking_seq": 17,
      "state": "active",
      "created_at": "2026-04-14T10:02:00Z"
    },
    {
      "id": "b1...",
      "parent_branch_id": null,
      "state": "archived",
      "archived_at": "2026-04-14T10:02:00Z",
      "purge_after": "2026-04-14T10:03:00Z"
    }
  ]
}
```

#### `POST /api/v1/sessions/{id}/rewind` — Rewind & edit

Request:
```json
{
  "target_message_id": "m12ab...",
  "edited_resolved_payload": {
    "answers": {
      "q_region": "other",
      "q_region__other_text": "Colombia - Valle del Cauca",
      "q_tone": "formal"
    }
  }
}
```

Response (200):
```json
{
  "new_branch_id": "b3...",
  "archived_branch_id": "b2...",
  "purge_after": "2026-04-14T10:06:00Z",
  "new_current_message_id": "m12ab...",
  "discarded_message_count": 7,
  "replay_cache_hits": 3,
  "replay_cache_misses": 0
}
```

Errors:
- `400 invalid_target` — message not in active branch, or not a HITL response.
- `400 active_stream` — the CPN is currently mid-stream for this session; user must wait.
- `409 side_effect_ack_required` — an upstream tool transition between the target and head is `SideEffectful` and the client did not set `acknowledge_side_effects: true`.
- `429 rewind_rate_limit` — exceeded per-user rewind limit.

#### `POST /api/v1/sessions/{id}/rewind/undo` — Restore the previously-active branch

Request: *(empty body)*

Response (200):
```json
{
  "restored_branch_id": "b2...",
  "archived_branch_id": "b3...",
  "purge_after": "2026-04-14T10:07:00Z"
}
```

Errors:
- `410 branch_purged` — grace window expired.
- `409 no_undoable_branch` — no archived branch within window.

### 4.4 SSE Event Additions

New event type delivered on the existing session SSE channel:

```json
{ "type": "branch_switched",
  "session_id": "s1...",
  "old_branch_id": "b2...",
  "new_branch_id": "b3...",
  "new_current_message_id": "m12ab...",
  "reason": "rewind" | "undo",
  "timestamp": "2026-04-14T10:02:00.123Z" }
```

Existing event types (`stream_chunk`, `hitl_requested`, `hitl_resolved`, `transition_*`, `token_deposited`, `mode_switch`) SHALL gain a `branch_id` field. Clients MUST discard events whose `branch_id` does not match the currently-active branch they hold locally.

### 4.5 Frontend Component Architecture

```
MessageBubble (existing)
 └── A2UIMessageRenderer (existing)
      ├── QuestionnaireActive (existing)
      └── QuestionnaireLocked (existing)
           └── EditAnswerButton            (NEW — REQ-019)

DesktopLayout
 ├── ChatHeader
 │    └── StateBadge                       (NEW — REQ-025)
 ├── MessageList
 │    └── UndoToast (pinned top)           (NEW — REQ-022)
 ├── ForkDialog (existing, relabeled)
 └── RewindDialog                          (NEW — REQ-020)
      ├── TurnPicker                       (extracted primitive — GUD-002)
      ├── EditableAnswersForm
      │    └── OtherBeforeAfter (per free-text)  (NEW — REQ-020.c)
      ├── DiscardedTurnsPreview            (NEW — REQ-020.d)
      ├── SideEffectWarningPanel           (NEW — REQ-023, conditional)
      ├── SummaryLine                      (NEW — REQ-020.e)
      └── ConfirmCancelBar                 (NEW — REQ-020.f, REQ-028)
```

### 4.6 Frontend Reducer Actions

```typescript
// src/hooks/useChat.ts (additions)

type ChatAction =
  | /* ... existing actions ... */
  | { type: 'REWIND_STARTED' }
  | { type: 'REWIND_COMPLETED';
      newBranchId: string;
      archivedBranchId: string;
      purgeAfter: string;           // ISO8601
      messages: ChatMessage[];      // the full new active-branch message list
      newCurrentMessageId: string }
  | { type: 'BRANCH_SWITCHED';      // from SSE
      oldBranchId: string;
      newBranchId: string;
      newCurrentMessageId: string;
      reason: 'rewind' | 'undo' }
  | { type: 'UNDO_TOAST_DISMISSED' }
  | { type: 'UNDO_WINDOW_EXPIRED' };
```

State shape additions:

```typescript
interface ChatState {
  // ... existing fields ...
  currentBranchId: string | null;
  undoToast: {
    archivedBranchId: string;
    purgeAfter: string;    // ISO8601
    visible: boolean;
    expired: boolean;
  } | null;
  rewindInProgress: boolean;
}
```

## 5. Acceptance Criteria

### CPN Checkpointing
- **AC-001**: Given a running CPN topology with a HITL transition, When the HITL blocks for input, Then a marking snapshot is persisted with `places` reflecting all current tokens and the correct `Mode`.
- **AC-002**: Given a marking persisted with `mode='Centaurian'`, When the CPN is rehydrated from that marking, Then `CPN.Mode == Centaurian` before the first transition fires.

### Rewind Transaction
- **AC-003**: Given a session with branch `b1` containing messages `m1..m10`, When the user rewinds to `m5`, Then a new branch `b2` is created with messages `m1..m5` only (content unchanged except for the edited HITL answer at `m5`), `sessions.current_branch_id = b2`, and `b1.state = 'archived'`.
- **AC-004**: Given a rewind operation, When the database transaction fails at any step (e.g., marking copy errors), Then no new branch row, message row, event row, or pointer change persists (full rollback).
- **AC-005**: Given a rewind request while the CPN is actively streaming a response, When the request arrives, Then the API returns `400 active_stream` and no branch is created.

### Idempotent Replay
- **AC-006**: Given a `Deterministic` classifier transition with cached output for input hash `H`, When rewind replays the topology and the input marking hashes to `H`, Then the transition's work function is NOT invoked, the cached output token is deposited, and an `EventTransitionCompleted` with `replayed=true` is emitted.
- **AC-007**: Given a `NonDeterministic` LLM transition, When rewind replays it, Then the work function IS invoked and may produce different output than the archived branch.
- **AC-008**: Given a `SideEffectful` tool transition between the rewind point and the head, When the user submits a rewind without `acknowledge_side_effects=true`, Then the API returns `409 side_effect_ack_required`.

### Undo
- **AC-009**: Given a rewind completed 10 seconds ago, When the user clicks **Undo**, Then the archived branch is promoted back to active, the rewind-produced branch is archived with its own 60s window, and the client receives a `branch_switched` SSE with `reason='undo'`.
- **AC-010**: Given a rewind completed 61 seconds ago (default window), When the user clicks **Undo**, Then the API returns `410 branch_purged` and the UI toast already shows `"Undo window expired."`.
- **AC-011**: Given an archived branch past its `purge_after`, When the GC job runs, Then the branch's messages, events, markings, and replay-cache rows are hard-deleted in one transaction and the branch row transitions to `state='purged'`.

### Frontend UX
- **AC-012**: Given a `QuestionnaireLocked` surface, When the user focuses it via keyboard, Then the **"Edit this answer"** button appears in the focus order with an accessible label.
- **AC-013**: Given a questionnaire whose prior answer includes an "Other" free-text value, When the user opens `RewindDialog`, Then both the original and the proposed values are rendered side-by-side and simultaneously visible without user interaction (no tab, no hover).
- **AC-014**: Given a rewind that would discard 7 turns, When the user opens `RewindDialog`, Then the Discarded Turns Preview lists exactly 7 items with strikethrough styling and the summary line reads `"Rewind mode — Turn 3 of 10 · 7 turns will be archived"`.
- **AC-015**: Given a successful rewind, When the response is received, Then the Undo Toast is rendered anchored to the top of the conversation panel, displaying a live `MM:SS` countdown, and it does NOT auto-dismiss on scroll, click-outside, or timer (only on explicit dismiss, window expiration, or a subsequent rewind).
- **AC-016**: Given the `RewindDialog` is open with unsaved edits, When the user presses Enter inside a textarea, Then the form does NOT submit (REQ-028).
- **AC-017**: Given a running accessibility audit on the `RewindDialog`, When `axe-core` is executed, Then zero violations at WCAG 2.2 AA severity are reported.
- **AC-018**: Given a user with aphantasia testing the flow, When they leave the browser, return 5 minutes later, and reload to the URL fragment `#block-b2-m5`, Then the viewport renders with the exact same block at the exact same scroll position.

## 6. Test Automation Strategy

**Test Levels**:
- **Unit (Go)**: `branch_repo_test.go`, `marking_repo_test.go`, `replay_cache_test.go`, `session_service_rewind_test.go` (pure logic with mocks).
- **Integration (Go)**: `rewind_integration_test.go` against a real Postgres — verifies atomicity, concurrent rewinds, GC correctness.
- **CPN Behavior (Go)**: `executor_replay_test.go` — verifies cache hit/miss behavior for each `ReplayPolicy`.
- **Unit (TypeScript)**: `RewindDialog.test.tsx`, `UndoToast.test.tsx`, `useChat.rewind.test.ts`, `TurnPicker.test.tsx`.
- **Accessibility (TypeScript)**: `axe-core` run against `RewindDialog` and `UndoToast` in jest-dom environment (AC-017).
- **End-to-End (Playwright)**: scenario — send messages → answer questionnaire → rewind → edit "Other" → verify new plan → undo within window → verify restoration.

**Frameworks**:
- Go: `testing` stdlib + `testify/assert` + `pgx` test containers.
- TypeScript: Vitest + React Testing Library + `@axe-core/react`.
- E2E: Playwright.

**Test Data Management**: fixture-builder helpers produce an in-memory session with a deterministic HITL topology; Postgres integration tests use a disposable schema per test.

**CI/CD Integration**: all levels wired into existing GitHub Actions pipelines; rewind tests run in the existing backend + frontend test jobs; E2E runs on PR against `main`.

**Coverage Requirements**:
- New Go code: ≥85% line coverage.
- New TypeScript code: ≥85% branch coverage.
- Critical paths (rewind transaction atomicity, cost isolation across branches, undo window expiration, replay cache correctness): 100% coverage.

**Performance Testing**:
- Rewind on a 200-message branch at message index 100 completes in <2s (aligns with AC-013 of forking spec).
- GC job processes 1,000 due-for-purge branches within a 5-minute tick without blocking.

## 7. Rationale & Context

### Why fork-and-promote rather than destructive edit

Three forces point to the same answer:
1. **Axiom A9** forbids deleting/mutating events. An in-place rewind would require either violating A9 or carving out an exception that complicates every downstream event consumer.
2. **Undo safety is a primary UX requirement** for anxiety-safe and aphantasia-safe design. A destructive model would force the undo to be a re-forward-simulation, which is fragile for non-deterministic transitions.
3. **The existing fork feature** already has mature machinery (session_branches analog in `forked_from_session_id`/`fork_message_count`, `ForkDialog` UI, `forkSession` API). Reusing its pattern at the *branch-within-session* level is the smallest-diff architecture that satisfies the other forces.

Branch-and-promote implements rewind as: *create a sibling branch from the target marking → promote it → archive the previous head → GC after the grace window*. The previous state is not gone during the window — it is quiet. This gives the user a blockchain-like mental model (which they asked for) with real semantics behind it.

### Why marking checkpoints at HITL boundaries (not every transition fire)

Checkpointing every transition would bloat storage and provide rewind targets that are meaningless to a human ("rewind to inside a classifier's middle step"). HITL boundaries and turn boundaries map to user-intelligible "blocks". REQ-001 anchors checkpoints where the user could plausibly want to go back to.

### Why a replay cache keyed by input marking hash

Without it, rewinding a conversation that contains expensive upstream LLM calls would re-pay for them on every rewind. With it, only the transitions downstream of the edited HITL response — the ones whose inputs actually change — are re-executed. This keeps cost under control and gives deterministic transitions idempotent replay, which is a desirable property in its own right.

### Why 60 seconds by default for the undo window

60 seconds is long enough for a user to realize they made a mistake and short enough to keep storage overhead bounded. The range (30s–24h) in CON-006 accommodates higher-stakes deployments where operators want longer undo windows. Memory of the specific edit typically decays after about 5–15 seconds; 60 provides generous margin without retaining large archived branch sets.

### Why the Undo Toast is anchored, not corner-floating

Floating corner toasts are invisible to users who have scrolled away, invisible to screen-reader users who have not navigated there, and invisible to aphantasic users who cannot mentally hold "there was a notification somewhere up-right five seconds ago". An anchored, text-heavy, persistent element in the conversation panel meets the REQ-027 principle: *state is where the user is looking*.

### Why a separate button for Fork vs Rewind

User testing of similar systems (git GUIs, document version history UIs) consistently shows that combining branch and reset into a single action creates unrecoverable misclicks for anxious users. Two explicitly-labeled buttons ("Fork to new chat (keep this one)" and "Rewind and edit (replaces turns after here)") remove the ambiguity at the cost of a small amount of screen real estate — a trade we accept.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Google OAuth 2.0 — session ownership (existing).
- **EXT-002**: OpenRouter API — LLM execution; non-deterministic transitions re-invoke this on rewind replay (existing).

### Infrastructure Dependencies
- **INF-001**: PostgreSQL 16 — required for the multi-row rewind transaction; needs `gen_random_bytes()` (`pgcrypto` extension) for branch id backfill.
- **INF-002**: Redis 7 — session cache. Cache entries for a session MUST be invalidated on branch promotion.
- **INF-003**: Background scheduler inside the Go service (existing interval-runner pattern) — hosts the archived-branch GC job.

### Data Dependencies
- **DAT-001**: `token_ledger` table — unchanged; billing continues against the stable `session_id` across branches.
- **DAT-002**: `messages`, `events` — gain a `branch_id` column; downstream consumers (monitor, analytics) MUST be updated to filter by the session's current branch.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: React 19 + TypeScript 5.7 (existing).
- **PLT-003**: Tailwind CSS v4 (existing).
- **PLT-004**: `@axe-core/react` in the frontend dev dependencies — required for AC-017.

### Compliance Dependencies
- **COM-001**: WCAG 2.2 Level AA — baseline accessibility target for all new UI (REQ-031, AC-017).
- **COM-002**: Data retention — archived branches are short-lived (grace window) then hard-deleted; the platform's primary audit trail remains the active branch plus any compliance-configured event export.

## 9. Examples & Edge Cases

### Example 1: Edit the "Other" free-text on a region questionnaire

```
Initial state (active branch b1):
  m1: assistant A2UI questionnaire "What region?"   → resolvedPayload: { q_region: "other",
                                                          q_region__other_text: "South America" }
  m2: assistant plan derived from m1
  m3: assistant A2UI questionnaire "What tone?"     → resolvedPayload: { q_tone: "formal" }
  m4: assistant revised plan

User clicks "Edit this answer" on m1, opens RewindDialog, changes
q_region__other_text from "South America" → "Colombia - Valle del Cauca"
and clicks Confirm Rewind.

Resulting state:
  branch b2 becomes active with messages m1' (edited resolvedPayload), nothing else.
  branch b1 is archived; purge_after = now + 60s.
  CPN re-fires the HITL output token with the new payload; a new plan streams.
  Undo Toast appears: "Rewound to Turn 1. Undo available for 00:59."
  Replay cache: 0 hits (all downstream was non-deterministic LLM work).
```

### Example 2: Rewind with a deterministic upstream and cached replay

```
Topology: user_input → classifier (Deterministic) → router → llm_plan (NonDeterministic) → HITL
The classifier output for input hash H was cached on b1.
User rewinds to edit the HITL answer.

Re-execution on b2:
  - classifier: cache HIT (replay_cache_hits += 1), emits EventTransitionCompleted{replayed:true}
  - router:     deterministic passthrough, cache HIT
  - llm_plan:   cache MISS (NonDeterministic policy), re-invokes OpenRouter
                → new plan token deposited
  - HITL:       pre-seeded with edited response token, fires immediately, continues

Billing impact: one LLM call billed on b2. Classifier/router billed zero (cache hit).
```

### EDGE-001: Rewind while another rewind is in flight

The rewind endpoint takes a per-session advisory lock. A second rewind request for the same session while the first is in-flight returns `409 rewind_in_progress`. The frontend disables the Confirm button between click and response to prevent client-side double-submit.

### EDGE-002: Undo chain (rewind → undo → rewind → undo)

Every promotion starts a fresh grace window on the newly-archived branch. An old archived branch past its window is purged on schedule regardless of subsequent activity. This means an undo chain is bounded in depth by the grace window, not unbounded. Documented behavior; do not attempt to make arbitrary-depth undo work via this mechanism.

### EDGE-003: Rewinding onto a marking with Mode=Centaurian while the live CPN is Mode=MAS

The rehydrator reads `Mode` from the marking and sets the CPN instance's mode before firing. The in-memory instance from the previous branch is torn down first (CON-003). The `branch_switched` SSE event precedes any `mode_switch` event to let the client reconcile state cleanly.

### EDGE-004: Rewind upstream of a side-effectful tool call (e.g., send-email)

The API blocks with `409 side_effect_ack_required`. The `RewindDialog` renders a `SideEffectWarningPanel` listing the tool name and timestamp; the user must check an acknowledgement box confirming they understand the side-effectful step will need to re-run or be skipped. On Confirm with acknowledgement, the backend re-submits with `acknowledge_side_effects: true`; the rewind proceeds but the side-effectful transition becomes a HITL surface on replay asking the user to confirm or skip the external action. Auto-replay of side-effectful work is never done silently.

### EDGE-005: Rewind beyond the root (message_index = -1)

Rejected at the API layer with `400 invalid_target`. The minimum rewind target is the first user HITL response in the conversation.

### EDGE-006: Very long conversations in the Discarded Turns Preview

Per CON-007, the preview shows the first 50 items, the last 50, and `"… N more turns …"` collapsed between them. The summary count is always accurate.

### EDGE-007: User closes the browser during a rewind

The HTTP transaction either commits atomically on the server or rolls back (CON-002). On reconnect, the client fetches the session; whichever branch is recorded as `active` is displayed. The Undo Toast is derived from server state (current archived branch with `purge_after > now`), so if the rewind committed before the disconnect, the undo affordance is still available when the user returns (within the grace window).

### EDGE-008: Replay cache collision from identical inputs across two sessions

Cannot occur: the replay cache primary key includes `session_id` (SEC-004). Hash collisions across distinct sessions are irrelevant because lookups are always scoped to the owning session.

## 10. Validation Criteria

1. Migration `009_rewind_branches.up.sql` runs successfully on a database populated with existing sessions/messages/events; every existing row is correctly backfilled with a root `branch_id`; every existing session has `current_branch_id` set non-null.
2. Rewinding a 100-message branch at message index 50 completes in under 2 seconds end-to-end (client click → new stream begins).
3. An archived branch is invisible to every listing, monitor, SSE replay, and CPN rehydration path after its `purge_after` has elapsed and the GC job has run.
4. Undo within the grace window returns the session to byte-for-byte the same active state it had before the rewind (same active branch id, same messages, same events, same replay cache entries).
5. An attempted rewind over a `SideEffectful` transition without acknowledgement is rejected with the correct HTTP 409 code; with acknowledgement, the subsequent replay surfaces a HITL prompt to the user rather than silently re-executing the side effect.
6. `axe-core` reports zero WCAG 2.2 AA violations on both `RewindDialog` and `UndoToast`.
7. Manual accessibility walkthrough by an aphantasic reviewer confirms: every state the feature reaches has a textual counterpart visible without hover, focus, or animation.
8. Manual walkthrough by an anxiety-focused reviewer confirms: no destructive action can be committed without an explicit button click; undo is discoverable without memory of a prior notification.
9. Frontend end-to-end Playwright scenario passes: answer questionnaire → rewind → edit "Other" → confirm → verify new plan streams → undo → verify original plan restored → verify URL fragment anchor `#block-...` still resolves correctly after undo.
10. Rate limit (default 20 rewinds/hour per user) returns HTTP 429 with a `Retry-After` header and the frontend surfaces a human-readable message.

## 11. Related Specifications / Further Reading

- [spec-design-multi-chat-conversation-forking.md](spec-design-multi-chat-conversation-forking.md) — Sibling feature: fork creates a new session; rewind creates a new branch within a session. Reuses `TurnPicker`/`ForkDialog` primitives.
- [spec-process-bugfix-a2ui-hitl-response-persistence.md](spec-process-bugfix-a2ui-hitl-response-persistence.md) — HITL response persistence and `parent_message_id` linkage; prerequisite for rewinding HITL answers.
- [spec-process-bugfix-a2ui-hitl-rehydration.md](spec-process-bugfix-a2ui-hitl-rehydration.md) — Rehydration of HITL surfaces on session reload; the rewind flow reuses the same pairing logic when switching branches.
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md) — HTTP + SSE adapter on which the new `branch_switched` event is delivered.
- [spec-design-cpn-execution-monitor.md](spec-design-cpn-execution-monitor.md) — Monitor must gain a branch filter to respect the active-branch read contract.
- [spec-design-cpn-execution-visualizer.md](spec-design-cpn-execution-visualizer.md) — Visualizer should optionally display branch lineage as a graph (future).
- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md) — Defines the A2UI payloads whose lock/unlock behavior is affected by rewind.
- WCAG 2.2 Level AA — https://www.w3.org/TR/WCAG22/
- Aphantasia Network research notes on explicit-state UIs — https://aphantasia.com/ (general reference)
