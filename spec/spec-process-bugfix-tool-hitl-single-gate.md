---
title: "Bug Fix — Unify Tool HITL Approval into a Single HOST·HITL Gate"
version: 1.0
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech
tags: [process, bugfix, hitl, cpn, tools, a2ui, frontend, backend, ux]
---

## Changelog

- **1.0 (2026-04-20)**: Initial spec. Closes a UX defect reported on
  branch `feat/model-registry-foundation`: when the agent invokes a
  host-side tool (`bash_exec`, file operations) the chat renders **two**
  sequential approval cards — a `HOST · HITL` card followed by a legacy
  `t-review` card. Approving the second card fails with
  `Failed to respond` because the tool has already executed and the
  review transition is orphaned. The `HOST · HITL` card additionally
  displays the raw tool-invocation JSON
  (`bash_exec {"args":["-c","..."],"command":"bash"}`) instead of the
  human-readable shell command.

# Introduction

Host-side tool executions in the CPN engine go through the
Human-In-The-Loop (HITL) approval subsystem. Commit `f99b2f1`
("fix(hitl): wire tool-HITL channels and render HostApprovalCard for
shell/file tools") introduced a dedicated `HOST · HITL` channel and
React card (`HostApprovalCard.tsx`) for tool approvals. However, the
legacy `t-review` transition that existed previously was not retired
from the CPN topology for tool-bearing flows, and its `A2UI` review
card continues to be emitted after the tool has already run.

This specification unifies tool approval into **one** gate (the
`HOST · HITL` surface), removes the orphan second card, renders a
clean shell command in the approval card, and adds a typed error for
resolution attempts against already-resolved or orphaned transitions.

## 1. Purpose & Scope

### Purpose
Deliver a simple, single-step approval UX for tool executions that:
1. Presents exactly **one** approval card per tool invocation.
2. Displays the **parsed shell command** (e.g.
   `uname -a && whoami && pwd && ls -F`) prominently, with the raw
   tool-invocation JSON available only via an optional collapsed
   "technical details" disclosure.
3. Never surfaces a second approval step that the user cannot resolve.
4. Returns a clear, user-friendly error if a stale approval request is
   submitted (e.g. from a reloaded tab racing a newer token).

### Scope
- **In scope**:
  - CPN topology for tool-bearing flows (shell, file, and any future
    `RequiresHITL=true` tool).
  - Backend Go code: `back/go-assistant/cpn/tools/registry.go`,
    `back/go-assistant/cpn/hitl.go`,
    `back/go-assistant/cmd/server/system_tools.go`,
    `back/go-assistant/internal/app/session_service.go`.
  - Frontend React code:
    `front/react-assistant/src/features/chat/hitl/HostApprovalCard.tsx`,
    `front/react-assistant/src/features/chat/MessageBubble.tsx`.
  - A2UI payload shape for `HostApprovalCard`.
  - Error channel from `ResolveHITL` to the UI.

- **Out of scope**:
  - `t-clarify` (questionnaire) surfaces — unchanged.
  - `t-review` for **non-tool** agent outputs (plans, drafts, generated
    artifacts) — unchanged; the review card remains the correct UX for
    those flows.
  - Persistence / rehydration semantics — covered by
    [`spec-process-bugfix-treview-surface-and-locked-parser.md`](spec-process-bugfix-treview-surface-and-locked-parser.md)
    and its predecessors; this spec MUST NOT regress them.

### Audience
Backend Go engineers, frontend React engineers, CPN architects, and QA
engineers working on the `liwaisi_assistant` repository.

## 2. Definitions

| Term | Definition |
|------|------------|
| **CPN** | Colored Petri Net — the formal model underlying the go-assistant orchestration engine (`back/go-assistant/cpn/`). |
| **HITL** | Human-In-The-Loop — transitions that suspend CPN firing until a human decision is recorded. |
| **HOST · HITL** | HITL gate owned by the host-tool channel; requests permission to execute a tool on the user's machine. Renders as `HostApprovalCard`. |
| **t-review** | Generic review transition for approving/revising/rejecting agent-produced artifacts (plans, drafts). Renders as an A2UI review card. |
| **A2UI** | Agent-to-UI protocol v0.8 (https://a2ui.org/specification/v0.8-a2ui/). |
| **A2UIPayloadBuilder** | Transition-owned function that produces the A2UI surface payload for a HITL decision (see `cpn/hitl.go`). |
| **Orphan transition** | A HITL transition whose resolution produces a token that has no live downstream consumer (e.g. because its parent flow already completed). |
| **Tool HITL** | HITL gate produced because a tool schema has `RequiresHITL: true`. |
| **Gate A / Gate B** | Informal names for the two observed approval cards — Gate A = `HOST · HITL`, Gate B = legacy `t-review`. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: A tool invocation with `RequiresHITL=true` MUST produce
  **exactly one** HITL surface in the A2UI stream and in
  `c.History` — the `HOST · HITL` card.
- **REQ-002**: The CPN topology builder MUST NOT wire a `t-review`
  transition in the path of a tool whose HITL is already handled by the
  host-tool channel.
- **REQ-003**: The `HostApprovalCard` payload MUST include a
  `command` field populated with the **parsed shell command string**,
  extracted from the tool invocation as defined in
  §4 (Interfaces & Data Contracts).
- **REQ-004**: The `HostApprovalCard` payload MUST include a separate
  `invocation` field containing the raw tool-invocation JSON, to
  preserve full diagnostic information.
- **REQ-005**: The React `HostApprovalCard` MUST render `command` in
  the primary "COMANDO" block, and MUST render `invocation` only inside
  a collapsed-by-default disclosure element labelled
  "Detalles técnicos" (Spanish) / "Technical details" (English).
- **REQ-006**: `SessionService.ResolveHITL` MUST return a typed error
  `ErrTransitionOrphaned` (wrapping or in addition to
  `ErrSessionInactive`) when invoked against a transition whose token
  has already been consumed or whose session is no longer waiting on
  it.
- **REQ-007**: The frontend MUST treat `ErrTransitionOrphaned`
  (surfaced via a stable error code in the HTTP/SSE error payload) as a
  non-fatal hint: dismiss the stale card with a neutral toast message
  ("Esta aprobación ya fue resuelta") instead of the red
  `Failed to respond` banner.
- **REQ-008**: Non-tool `t-review` flows (plans, drafts, generated
  artifacts) MUST continue to render the A2UI review card with the
  three-button layout (Approve / Request Changes / Discard) — this
  spec only removes `t-review` from the **tool execution** path.

### Security requirements

- **SEC-001**: Removing Gate B MUST NOT weaken the approval contract:
  the single remaining Gate A MUST still block tool execution until a
  positive approval decision is recorded.
- **SEC-002**: The `invocation` field (REQ-004) MUST serialize the
  exact arguments that will be passed to the host adapter. Any
  divergence between displayed and executed invocation is a critical
  security bug.
- **SEC-003**: "Aprobar y recordar" (remember-approval) semantics from
  the existing `HostApprovalCard` MUST be preserved unchanged.

### Constraints

- **CON-001**: Must not break persistence/rehydration established by
  `spec-process-bugfix-a2ui-hitl-rehydration.md` and
  `spec-process-bugfix-treview-surface-and-locked-parser.md`.
  All surface and response rows for the `HOST · HITL` card MUST be
  persisted via the same `appendHITLResponseToHistory` path.
- **CON-002**: Must be backwards compatible with already-persisted
  sessions: rehydrating a chat that was recorded **before** this fix
  (and therefore contains both Gate A and Gate B history rows) MUST
  still render correctly. Historical Gate B rows MAY display in a
  locked/resolved state and MUST NOT crash the renderer.
- **CON-003**: No database migration is permitted; the change is
  purely topology + payload + renderer.
- **CON-004**: The `RequiresHITL` schema flag on tool definitions is
  the single source of truth for "this tool needs approval". No tool
  should need to set additional flags.

### Guidelines

- **GUD-001**: Prefer extracting the shell command into a small pure
  helper (`parseShellInvocation`) so it can be unit-tested in
  isolation.
- **GUD-002**: Log (at `slog.Debug`) when the topology builder skips a
  `t-review` wiring because the tool already carries HITL — useful for
  post-hoc auditing.
- **GUD-003**: UI copy MUST match the app's existing Spanish-first
  tone ("Permiso para operar en tu máquina", "Aprobar una vez",
  etc.). English strings go through the i18n layer.

### Patterns

- **PAT-001**: Tool HITL is **channel-owned**; review HITL is
  **transition-owned**. Never mix the two for the same flow.
- **PAT-002**: Every A2UI surface card corresponds to a live token
  consumer. If you emit a card you MUST have a handler that can fire
  on its response.

## 4. Interfaces & Data Contracts

### 4.1 `HostApprovalPayload` (updated A2UI shape)

```ts
type RiskLevel = "safe" | "caution" | "danger";

interface HostApprovalPayload {
  /** Clean, human-readable shell command or operation summary. */
  command: string;
  /** Raw tool invocation JSON, for the collapsed "technical details" panel. */
  invocation: {
    tool: string;              // e.g. "bash_exec"
    args: unknown;             // original args object/array
    [k: string]: unknown;      // forward-compatible
  };
  /** Risk classification drives card border color and warning copy. */
  risk: RiskLevel;
  /** Title shown at the top of the card. */
  title: string;               // e.g. "Permiso para operar en tu máquina"
  /** Optional working-directory or target path hint. */
  context?: string;
  /** True when the host has previously approved an equivalent command
   *  with "Aprobar y recordar"; frontend may hide "recordar" button. */
  rememberAvailable?: boolean;
}
```

### 4.2 Shell-command extraction contract

Given a `bash_exec` tool invocation, the backend helper
`parseShellInvocation(tool string, args any) (command string, ok bool)`
MUST produce:

| Input                                                                                 | `command` output                  |
|---------------------------------------------------------------------------------------|-----------------------------------|
| `tool="bash_exec"`, `args={"command":"bash","args":["-c","uname -a && whoami"]}`      | `uname -a && whoami`              |
| `tool="bash_exec"`, `args={"command":"bash","args":["-lc","ls -F"]}`                  | `ls -F`                           |
| `tool="bash_exec"`, `args={"command":"ls","args":["-la","/tmp"]}`                     | `ls -la /tmp`                     |
| `tool="file_write"`, `args={"path":"/x.txt","content":"..."}`                         | `write → /x.txt`                  |
| `tool="file_read"`,  `args={"path":"/x.txt"}`                                         | `read ← /x.txt`                   |
| Unknown tool, unrecognized shape                                                       | raw JSON (fallback), `ok=false`   |

### 4.3 `ResolveHITL` error contract

```go
// New typed sentinel; wraps ErrSessionInactive for compatibility.
var ErrTransitionOrphaned = errors.New("hitl transition already resolved or orphaned")
```

The HTTP/SSE error envelope returned to the frontend when
`ResolveHITL` fails with `ErrTransitionOrphaned` MUST include a stable
machine-readable code:

```json
{
  "error": {
    "code": "HITL_TRANSITION_ORPHANED",
    "message": "Esta aprobación ya fue resuelta",
    "transitionId": "t-review:…"
  }
}
```

### 4.4 CPN topology rule

When the topology builder encounters a tool with `RequiresHITL=true`
in `back/go-assistant/cpn/tools/registry.go`, it MUST:

1. Wire the host-tool HITL channel (Gate A).
2. Skip wiring any `t-review` transition in the direct pre-execution
   path of that tool. Post-execution review (e.g. summarizing the
   result) remains unchanged.

## 5. Acceptance Criteria

- **AC-001**: Given a session where the agent invokes `bash_exec`,
  When the CPN reaches the HITL gate,
  Then exactly one A2UI surface card (`HOST · HITL`) appears in the
  chat; no second review card is emitted for the same tool call.
- **AC-002**: Given a `HOST · HITL` card for
  `bash_exec {"args":["-c","uname -a && whoami"]}`,
  When the card renders,
  Then the visible "COMANDO" block shows `uname -a && whoami` and the
  raw JSON is hidden behind a collapsed "Detalles técnicos" toggle.
- **AC-003**: Given a user who approves the `HOST · HITL` card,
  When the tool finishes executing,
  Then no further approval card is displayed for that tool invocation.
- **AC-004**: Given a stale HITL resolution request for a transition
  that is no longer waiting,
  When the frontend POSTs the resolution,
  Then the backend returns `HITL_TRANSITION_ORPHANED` with HTTP 409 or
  equivalent, and the frontend dismisses the stale card with a
  neutral notice (no red `Failed to respond` banner).
- **AC-005**: Given a chat recorded before this fix (containing a
  resolved Gate A row followed by an unresolved Gate B row),
  When the chat is rehydrated,
  Then the chat renders without runtime error; the Gate B row renders
  in a resolved/locked state, or is gracefully hidden, per CON-002.
- **AC-006**: Given a non-tool `t-review` flow (e.g. plan review),
  When the review step fires,
  Then the A2UI review card still renders with the three-button
  layout and the existing behavior is unchanged.
- **AC-007**: Given the `parseShellInvocation` helper,
  When it is called with each row of §4.2,
  Then it returns the expected `command` string and `ok` flag.
- **AC-008**: Given a user with a prior "Aprobar y recordar" decision
  for an equivalent command,
  When the `HOST · HITL` card is built,
  Then `rememberAvailable=false` is sent and the frontend hides the
  "Aprobar y recordar" button.

## 6. Test Automation Strategy

- **Test Levels**:
  - **Unit (Go)**: `parseShellInvocation` table-driven tests;
    `ResolveHITL` error-classification tests; topology-builder test
    asserting tool flows emit exactly one HITL transition.
  - **Unit (TS/React)**: `HostApprovalCard` rendering tests with the
    new payload shape; collapsed-disclosure interaction; stale-card
    dismissal on `HITL_TRANSITION_ORPHANED`.
  - **Integration (Go)**: end-to-end CPN run of a scripted
    `bash_exec` tool that asserts exactly one `SurfaceAppended` event
    of kind `host_approval` and zero `review_card` events.
  - **End-to-End (Playwright or existing harness)**: user approves a
    bash command; assert only one card is shown and that after approval
    no second card appears.
- **Frameworks**: Go `testing` + `testify`; Vitest + React Testing
  Library for frontend; existing E2E harness in
  `front/react-assistant`.
- **Test Data**: Fixtures for each row in §4.2. Reuse the SSE-capture
  fixture system already used by bugfix-a2ui specs.
- **CI/CD**: Runs in the existing GitHub Actions pipeline; all new
  unit tests MUST pass. Coverage gate: ≥85 % for touched files.
- **Regression guard**: Add a replay test using a persisted
  pre-fix transcript (CON-002) to ensure rehydration still succeeds.

## 7. Rationale & Context

### Why two gates exist today
Before commit `f99b2f1`, tool approval piggy-backed on the generic
`t-review` transition because the CPN had no dedicated host-tool
channel. When `f99b2f1` introduced the `HOST · HITL` channel and
`HostApprovalCard`, it added the correct gate **in parallel** with the
existing `t-review` wiring instead of replacing it. Both gates now
fire for the same tool invocation, producing the reported UX defect.

### Why the second approve returns "Failed to respond"
The `t-review` transition in the tool path has no live downstream
consumer once the host-tool channel has fired and the tool has
executed. The resolution endpoint
(`internal/app/session_service.go:733`) checks
`isExecutionLive(st.get())` and returns `ErrSessionInactive`, which
the frontend surfaces as a generic failure banner.

### Why rendering the raw JSON is wrong
Users making a security-sensitive approval decision must see the
**exact shell command** that will run. A JSON wrapper obscures the
command and makes dangerous invocations harder to spot — this is a
security-affecting readability issue, not cosmetic.

### Why a typed error matters
Frontends must distinguish between "your session died" (a real error
the user should see) and "this card is stale because a newer event
already resolved it" (a benign race, especially after rehydration or
multi-tab use). A dedicated code allows the UI to degrade gracefully.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: A2UI v0.8 protocol — card-payload semantics for
  `host_approval` and `review_card` surfaces.

### Third-Party Services
- None.

### Infrastructure Dependencies
- **INF-001**: go-assistant CPN engine — topology builder and HITL
  transition machinery.
- **INF-002**: SSE broker transporting A2UI surfaces to the React app.

### Data Dependencies
- **DAT-001**: `messages` table rows persisting HITL surfaces and
  responses (see bugfix-a2ui-hitl-response-persistence spec).

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: React 18 + Vite + TypeScript 5 (existing).

### Compliance Dependencies
- **COM-001**: Host-execution approval surface must remain auditable —
  every approval/rejection MUST continue to produce a persisted
  response row per CON-001.

## 9. Examples & Edge Cases

### 9.1 Clean command rendering

```jsonc
// A2UI payload (new shape)
{
  "kind": "host_approval",
  "payload": {
    "command": "uname -a && whoami && pwd && ls -F",
    "invocation": {
      "tool": "bash_exec",
      "args": { "command": "bash", "args": ["-c", "uname -a && whoami && pwd && ls -F"] }
    },
    "risk": "caution",
    "title": "Permiso para operar en tu máquina"
  }
}
```

Rendered result:
```
┌─ HOST · HITL  ● RIESGO: PRECAUCIÓN ─────────┐
│ Permiso para operar en tu máquina           │
│ COMANDO                                     │
│ $ uname -a && whoami && pwd && ls -F        │
│ ▸ Detalles técnicos                         │  ← collapsed
│ [Aprobar una vez] [Aprobar y recordar]      │
│ [Rechazar] [Bloquear siempre]               │
└─────────────────────────────────────────────┘
```

### 9.2 Edge case — unknown tool shape

If `parseShellInvocation` receives a tool invocation it cannot parse,
it MUST fall back to the JSON stringification of `args` and set
`ok=false`. The card still displays a command (the raw JSON) and the
risk badge escalates to `caution`. Approval is still possible; users
can always reject.

### 9.3 Edge case — rehydration of a pre-fix chat

A persisted chat may contain:
```
row N   : HOST · HITL card (resolved: approved)
row N+1 : t-review card    (unresolved)
row N+2 : tool result
```
On rehydration, the renderer MUST:
- Render row N in locked/resolved state.
- Render row N+1 in locked/resolved state (display-only) with a
  neutral notice "Aprobación obsoleta" — never show live action
  buttons for it.
- Render row N+2 normally.

### 9.4 Edge case — multi-tab race

Tab A approves the card; tab B still sees the unresolved card. When
tab B clicks approve, the backend returns
`HITL_TRANSITION_ORPHANED`. Tab B hides the card and shows the toast
"Esta aprobación ya fue resuelta". No red error banner.

## 10. Validation Criteria

Compliance with this specification requires ALL of the following:

1. All acceptance criteria (AC-001 through AC-008) are covered by
   automated tests and all tests pass.
2. Manual QA on a fresh chat: invoking `bash_exec` produces exactly
   one approval card; approving runs the tool; no second card appears.
3. Manual QA on a replayed pre-fix chat: the UI renders without
   runtime errors.
4. `grep` for `t-review` in the tool topology path returns no live
   wiring for tools with `RequiresHITL=true`.
5. Coverage on `parseShellInvocation`, `ResolveHITL` error paths, and
   `HostApprovalCard` new rendering is ≥85 %.
6. No regression in `t-clarify`, non-tool `t-review`, or HITL
   persistence/rehydration flows.

## 11. Related Specifications / Further Reading

- [`spec-process-bugfix-treview-surface-and-locked-parser.md`](spec-process-bugfix-treview-surface-and-locked-parser.md)
  — prior fix to `t-review` persistence; this spec builds on its
  invariants and MUST NOT regress them.
- [`spec-process-bugfix-a2ui-hitl-response-persistence.md`](spec-process-bugfix-a2ui-hitl-response-persistence.md)
  — HITL response persistence contract (INV-002 must continue to hold).
- [`spec-process-bugfix-a2ui-hitl-rehydration.md`](spec-process-bugfix-a2ui-hitl-rehydration.md)
  — rehydration behavior for HITL cards.
- [`spec-architecture-host-adapter-nodekind-bash.md`](spec-architecture-host-adapter-nodekind-bash.md)
  — host-side bash adapter contract.
- [`spec-architecture-host-gate-security-policy.md`](spec-architecture-host-gate-security-policy.md)
  — security-policy invariants for host-gate approvals.
- [`.docs/motor_agentico_cpn.md`](../.docs/motor_agentico_cpn.md) —
  CPN engine foundations (sections 7 and 9).
- A2UI v0.8 specification — https://a2ui.org/specification/v0.8-a2ui/
