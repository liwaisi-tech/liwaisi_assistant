---
title: brae Context Assembly & Tool-Result Hygiene
version: 1.0
date_created: 2026-04-23
last_updated: 2026-04-23
owner: liwaisi
tags: [architecture, cpn, memory, prompts, tools]
---

# Introduction

This specification fixes four observed failure modes in the brae assistant flow surfaced by analysis of session `d616720d6f7407fd44d5196153c3318e` (140 messages, 41 minutes, two Go tools authored). The failures all stem from how `cpn/memory.go` assembles per-transition LLM context and how tool results are shaped before they re-enter that context:

1. **Amnesia** — facts established outside the last 20 raw messages disappear (`DefaultContextWindowSize = 10` turns × 2 roles).
2. **File-write loops** — the LLM re-creates files it already wrote because nothing durable in context says otherwise.
3. **Bloated non-customer transitions** — classifier / router / validator transitions ship the full assistant persona prompt, wasting tokens and biasing structured outputs.
4. **Code echo in chat** — after `write_file` the model dumps the file content into the reply because the tool result echoes it back and no rule forbids the behavior.

The fix is a coordinated change across `cpn/memory.go`, the per-transition `SystemPrompt` authoring, the tool-result schema for state-changing operations, and the `assistant`-role prompt rules. No new tables; no schema migration.

## 1. Purpose & Scope

**Purpose.** Make brae's effective context durable, dense, and role-appropriate so that long, tool-heavy sessions stop forgetting prior actions, stop repeating themselves, and stop leaking artefact bytes into the chat stream.

**Scope.** All transitions of `NodeKindLLM` and `NodeKindTool` inside the go-assistant CPN engine (`back/go-assistant/cpn/`). The frontend artefact-panel work (Layer 3 of the code-echo fix) is **out of scope** for this iteration and tracked as a follow-up.

**Audience.** go-assistant maintainers; the `/golang-pro` agent invoked to implement this spec.

**Assumptions.**
- Persistence layer (`persist/topology.go`, `persist/memory.go`) already round-trips `Message.Role`, `CPNRole`, `CPNDepth`, `Timestamp`.
- `RoleObserver` is a recognised role and survives the T2 selector in `BuildContext` (`cpn/memory.go:40-47`).
- Per-transition `SystemPrompt` is already a field on the topology (`persist/topology.go:204`, `:470`) — authoring a different string per transition is structurally supported today.

## 2. Definitions

| Term | Meaning |
|---|---|
| **CPN** | Coloured Petri Net — the agent's execution graph. |
| **Transition** | A node in the CPN that fires when input tokens are present. Kinds: `LLM`, `Tool`, `HITL`, etc. |
| **T1 / T2 / T3** | The three context tiers in `cpn/memory.go`. T1 = system prompt; T2 = `RoleObserver` summaries (durable); T3 = sliding window of raw `RoleUser`/`RoleAssistant` messages. |
| **Ledger entry** | A compressed one-line `RoleObserver` message recording the outcome of a state-changing tool call. Lives in T2, never expires. |
| **State-changing tool** | A tool whose effect persists beyond the call: `write_file`, `edit_file`, `bash` invocations that mutate filesystem (build, mv, chmod, mkdir), DDL/DML SQL. |
| **Read-only tool** | A tool whose effect is the returned bytes only: `read_file`, `ls`, `find`, SELECT. |
| **Workspace state preamble** | A short, freshly-generated block injected into T1 each `assistant`-role LLM call listing files and binaries the agent has authored in the current session. |
| **Receipt** | A lean tool-result payload that records *what happened* without echoing *the bytes that were written*. |

## 3. Requirements, Constraints & Guidelines

### Memory tier changes (`cpn/memory.go`)

- **REQ-001**: `BuildContext` MUST accept a per-transition `Role` (or equivalent discriminator) and use it to select the system prompt and window size from a `ContextPolicy` resolved per role.
- **REQ-002**: A new `ContextPolicy` struct MUST exist with fields `{SystemPrompt string, RawWindowTurns int, IncludeWorkspacePreamble bool}` and MUST be resolvable from a registry keyed by `cpn.Role`.
- **REQ-003**: T2 selection MUST continue to include all `RoleObserver` messages and MUST additionally include any message whose `Metadata` carries the `ledger=true` marker, regardless of role.
- **REQ-004**: T3 raw-window assembly MUST drop the `$$a2ui:` payload (already handled by `sanitizeForLLM`) and MUST additionally collapse any message marked `ephemeral=true` so it does not consume window slots after the turn that produced it.
- **CON-001**: `DefaultContextWindowSize = 10` MUST remain the fallback default; per-role overrides happen via `ContextPolicy`.
- **CON-002**: No change to the `messages` table schema. Implementation note: the `messages` table has no `metadata` column, so the ledger discriminator is encoded in the existing `cpn_role` column via the sentinel value `__ledger__` (constant `CPNRoleLedger` in `cpn/memory.go`). Ephemeral marking, when needed, is encoded similarly via a future `__ephemeral__` sentinel; not implemented in this iteration.

### Per-transition system prompt scoping

- **REQ-010**: The `assistant` role prompt MUST contain persona, tools, and workflow rules (full preamble, ~current size).
- **REQ-011**: Non-customer-facing roles (`classify`, `route`, `validate`, `synthesize`, `architect`, `plan`) MUST ship a tight, task-only prompt with no persona, no regional flair, no tool catalogue.
- **REQ-012**: Each role's prompt MUST live in its own file under `cpn/prompts/roles/<role>.md` and be embedded via `go:embed` into a `prompts.For(role cpn.Role) string` accessor.
- **GUD-001**: Classifier and router prompts SHOULD specify input schema, output schema, and decision criteria, in that order, with one positive and one negative example.
- **GUD-002**: The `assistant` prompt MUST include the file-echo rule (REQ-040) verbatim in a `## Tool result handling` section.

### Tool-result ledger (T2 durable memory)

- **REQ-020**: Every successful state-changing tool call MUST emit, in addition to its normal tool result, a `RoleObserver` message with `metadata.ledger = true` and `Content` shaped as one line: `[ok] <verb> <path-or-target> (<size or rowcount>)`.
- **REQ-021**: Every failed state-changing tool call MUST emit a ledger line shaped as: `[fail] <verb> <path-or-target> → "<root cause, ≤120 chars>"`.
- **REQ-022**: Read-only tool calls MUST NOT emit ledger lines by default. They MAY mark their normal result message `metadata.ephemeral = true` so it scrolls out of T3 immediately after the next turn.
- **REQ-023**: Ledger emission MUST be a single hook at the tool-dispatch layer (`fire_bash`, `fire_*` write paths) so adding new tools does not require touching ledger code.
- **CON-010**: A ledger line MUST NOT exceed 200 bytes after sanitisation. Long paths are truncated middle-elided (`/very/long/.../file.go`).

### Workspace state preamble

- **REQ-030**: When `ContextPolicy.IncludeWorkspacePreamble` is true (defaults: true for `assistant`, false for everyone else), `BuildContext` MUST prepend a `[workspace state @ <RFC3339-ts>]` block to the system prompt.
- **REQ-031**: The preamble MUST be generated by inspecting the ledger (T2 entries with verb in {`write`, `build`, `chmod`, `mv`, `mkdir`, `cp`}) and emitting one line per durable artefact, deduplicated by path with the most recent action winning.
- **REQ-032**: The preamble generator MUST NOT shell out to the host — it consumes the ledger only. (Reason: deterministic, sandbox-safe, cheap.)
- **CON-020**: Preamble MUST be capped at 50 lines; oldest-by-action lines drop first.

### Tool-result hygiene (no content echo)

- **REQ-040**: The `assistant` role system prompt MUST contain the rule: *"After writing or editing a file, do not echo its contents in your reply. State only: file path, one-line change description, and next action. The user can ask 'show me the code' to view it."* with one GOOD and one BAD example.
- **REQ-041**: The `write_file` tool result MUST NOT include a `content` field. Required fields: `path`, `bytes`, `lines`, `sha256`, `created` (bool, true if new file).
- **REQ-042**: The `edit_file` tool result MUST return a unified diff (`diff` field, capped at 200 lines middle-elided) and MUST NOT return the post-edit full file body.
- **REQ-043**: A new read-only tool `read_file` MUST exist (or be confirmed extant) so the LLM can fetch the body explicitly when the user asks. If the tool already exists under a different name, REQ-040's example MUST cite that name.

### Cross-cutting

- **SEC-001**: Ledger lines MUST be sanitised through `sanitizeForLLM` before storage (no `$$a2ui:` leakage, no embedded prompt-injection markers).
- **CON-030**: All changes MUST be additive at the persistence layer — no migration of existing `messages` rows. Old sessions read back with empty ledger and absent preamble; new behaviour kicks in on first new turn.
- **PAT-001**: Follow the existing `cpn/prompts/regional.go` pattern for prompt assembly — small composable strings, embedded at compile time, assembled in `BuildContext`.

## 4. Interfaces & Data Contracts

### 4.1 `ContextPolicy` (new, `cpn/memory.go`)

```go
type ContextPolicy struct {
    SystemPrompt              string
    RawWindowTurns            int  // T3 size; 0 disables T3
    IncludeWorkspacePreamble  bool
    IncludeLedger             bool // true: surface ledger lines in T2
}

type ContextPolicyResolver interface {
    For(role Role) ContextPolicy
}
```

### 4.2 `BuildContext` signature change

```go
// before
func BuildContext(systemPrompt string, history []*Message, contextWindowSize int) ContextWindow

// after
func BuildContext(role Role, history []*Message, policy ContextPolicy) ContextWindow
```

Callers in `fire_llm.go` MUST be updated. Tests in `memory_test.go` MUST be updated to pass a `ContextPolicy`.

### 4.3 Ledger marker on `Message`

Implemented via sentinel `CPNRole` (no schema change required — the
`cpn_role` column already round-trips through `persist/topology.go`):

```go
const CPNRoleLedger = "__ledger__"

// Ledger entry shape (built by LedgerSuccess / LedgerFailure):
Message{
  Role:    RoleObserver,
  CPNRole: CPNRoleLedger,
  Content: "[ok] write ~/workspace/pg_explorer/main.go (1247 bytes)",
  // Verb + target are recoverable by parseLedgerLine(Content).
}
```

T2 selector predicate (in `BuildContextWithPolicy`):

```go
m.Role == RoleObserver && m.CPNRole != CPNRoleLedger        // ordinary observer
policy.IncludeLedger && m.CPNRole == CPNRoleLedger          // ledger entry
```

### 4.4 Tool-result schemas

`write_file` result:

```json
{ "path": "string", "bytes": 1247, "lines": 58, "sha256": "hex64", "created": true }
```

`edit_file` result:

```json
{ "path": "string", "bytes_after": 1305, "lines_after": 61, "diff": "unified diff, ≤200 lines", "diff_truncated": false }
```

Ledger emission contract (called by tool-dispatch):

```go
func EmitLedger(ctx context.Context, sess SessionStore, verb, target string, ok bool, summary string, size int64) error
```

### 4.5 Workspace preamble format (assembled at runtime, prepended to T1)

```
[workspace state @ 2026-04-23T01:42:21Z]
~/workspace/pg_explorer/main.go  1.2KB  wrote 01:21
~/bin/pg_explorer                exec   built 01:23
~/bin/sql_client                 exec   built 01:34
```

## 5. Acceptance Criteria

- **AC-001**: Given a session with 140 messages where `~/workspace/pg_explorer/main.go` was written 60+ messages ago, When the next `assistant` LLM call fires, Then the assembled `ContextWindow.SystemPrompt` MUST contain a workspace state line referencing that path.
- **AC-002**: Given a `classify` transition, When `BuildContext` runs, Then `SystemPrompt` MUST NOT contain the substring "brae" or "Órale" or any regional persona token.
- **AC-003**: Given a `write_file` tool call that succeeds, Then a `RoleObserver` message MUST be appended to history with `metadata.ledger == true` and Content matching the regex `^\[ok\] write \S+ \(\d+ bytes\)$`.
- **AC-004**: Given a `write_file` tool result, Then the JSON payload MUST NOT contain a key named `content`, `body`, or `bytes_written` carrying the file's textual contents.
- **AC-005**: Given a failed `bash` build, Then a ledger line of shape `[fail] build <target> → "<root cause>"` MUST be emitted, and the root cause MUST be ≤120 chars.
- **AC-006**: Given the assistant prompt loaded by `prompts.For(RoleAssistant)`, Then it MUST contain the verbatim string `do not echo its contents` and the tokens `GOOD:` and `BAD:` for the example block.
- **AC-007**: Given a session where a read-only `ls` is called, Then no ledger entry is created, and the result message carries `metadata.ephemeral == true`.
- **AC-008**: Given the workspace preamble exceeds 50 lines, Then the oldest-by-action lines are dropped and a trailing line `[+N older entries]` is appended.

## 6. Test Automation Strategy

- **Test Levels**: Unit (memory assembly, ledger emission, prompt selection), Integration (end-to-end CPN run with tool-call ledger surviving across 30+ turns), Golden (per-role prompt snapshot tests).
- **Frameworks**: Go stdlib `testing` + table-driven cases per existing repo convention. Use `cmp.Diff` for `ContextWindow` assertions.
- **Test Data Management**: Synthetic histories built by the existing `testutil_topology_test.go` helpers. Add a `histfix` helper that constructs N-turn histories with mixed roles, ledger entries, and ephemeral markers.
- **CI/CD Integration**: Existing `go test ./...` in CI. Add a benchmark `BenchmarkBuildContext_LongHistory` to ensure preamble+ledger does not regress build time.
- **Coverage Requirements**: ≥85% line coverage for `cpn/memory.go`, ledger hook, and `prompts.For`. Adversarial test for prompt-injection in ledger lines (REQ-001/SEC-001).
- **Performance Testing**: `BuildContext` MUST stay under 500µs for a 1000-message history with 50 ledger entries on the CI machine class.

## 7. Rationale & Context

The four failures observed share a single root: **the LLM has no persistent ground truth between turns other than the chat transcript**, and the transcript is aggressively truncated. Adding a durable ledger (T2 marker), a freshly-computed workspace preamble (T1 prepend), and lean tool receipts (no echo) closes the loop without growing the message table or rewriting the persistence layer.

Per-transition prompt scoping is independent but bundled here because (a) it touches `cpn/memory.go` callers anyway and (b) classifier/router transitions account for a non-trivial share of token spend that the persona preamble inflates with no upside.

The failure mode of "code dump in chat" has three remediation layers (prompt rule, lean receipt, artefact channel). Layers 1 and 2 are in scope; Layer 3 (artefact panel UI) is deferred because the underlying `authored_artefacts` table already exists and the frontend work is significant — handling 70%+ of user pain with the prompt+receipt fix is the right cost/value cut for this iteration.

Choosing **`metadata.ledger` on existing `messages` rows** rather than a new table avoids a migration, keeps ledger lines sortable by the existing timestamp index, and lets the existing T2 selector evolve with one extra predicate. The cost is a slight conceptual overload of `messages`, accepted as the simpler path.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: PostgreSQL — existing `messages` and `events` tables; no schema change.

### Infrastructure Dependencies
- **INF-001**: Go 1.22+ embed FS for prompt files under `cpn/prompts/roles/`.

### Data Dependencies
- **DAT-001**: Session history rows in `messages` table are the sole source for ledger reconstruction on session resume; no cross-session state.

### Technology Platform Dependencies
- **PLT-001**: go-assistant CPN engine (`back/go-assistant/cpn`) — internal.

### Compliance Dependencies
- None.

## 9. Examples & Edge Cases

### 9.1 Ledger entry on success

```
Message{
  Role: RoleObserver,
  Content: "[ok] write ~/workspace/pg_explorer/main.go (1247 bytes)",
  Metadata: {"ledger": true, "verb": "write", "target": "~/workspace/pg_explorer/main.go", "size_bytes": 1247},
  Timestamp: 2026-04-23T01:21:30Z,
}
```

### 9.2 Ledger entry on failure

```
Message{
  Role: RoleObserver,
  Content: `[fail] build ~/workspace/sql_client → "strings imported and not used"`,
  Metadata: {"ledger": true, "verb": "build", "target": "~/workspace/sql_client", "ok": false},
}
```

### 9.3 Per-role prompt selection

```go
policy := resolver.For(RoleClassify)
// policy.SystemPrompt does NOT mention "brae"
// policy.RawWindowTurns == 4
// policy.IncludeWorkspacePreamble == false
ctx := BuildContext(RoleClassify, history, policy)
```

### 9.4 write_file receipt (the BAD/GOOD distinction)

BAD (current):
```json
{ "path": "main.go", "content": "package main\nimport ...\n... (full file) ..." }
```

GOOD (target):
```json
{ "path": "main.go", "bytes": 1247, "lines": 58, "sha256": "a3f1...", "created": true }
```

### 9.5 Edge case — file rewritten with identical content

`write_file` MUST detect identical-by-sha256 writes and return `{ "created": false, "noop": true, ... }` plus emit ledger line `[ok] write <path> (no-op, identical)`. This prevents the "wrote the same file 4 times" loop from generating 4 ledger entries.

### 9.6 Edge case — ledger overflow

Session with 200+ writes: workspace preamble caps at 50 lines, ordered by most recent action, with `[+N older entries]` trailer. Ledger entries themselves are not pruned (they're cheap one-liners and may carry forensic value).

### 9.7 Edge case — session resume

When a session is loaded from PostgreSQL, ledger lines come back as ordinary `messages` rows with the metadata flag set. T2 selector picks them up automatically — no special restoration code path.

## 10. Validation Criteria

- All AC-001..AC-008 pass in CI.
- Replay of session `d616720d6f7407fd44d5196153c3318e` against the new code (history-as-fixture) MUST produce a context window at turn 100 that contains the workspace preamble lines for `pg_explorer` and `sql_client`.
- Token-count benchmark: classifier-style transition prompts MUST drop ≥30% in input tokens vs. baseline.
- Manual smoke: a fresh session that asks brae to write a file MUST result in an assistant reply that does NOT contain the file body verbatim.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening.md` — origin of the awakening transition that seeds the first ledger snapshot.
- `spec/spec-architecture-cpn-agent-architect.md` — consumer of the per-role prompt scoping (architect/intake/design roles benefit directly).
- `back/go-assistant/cpn/memory.go` — primary edit site.
- `back/go-assistant/cpn/prompts/regional.go` — pattern to follow for the new `prompts/roles/` layout.
- `back/go-assistant/cpn/persist/topology.go` — confirms per-transition `SystemPrompt` is already round-tripped.
