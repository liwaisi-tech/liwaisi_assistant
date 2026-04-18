---
title: Authored-Artifact Ledger — Provenance and Reversibility for Everything brae Writes to Disk
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, provenance, ledger, rollback, brae, gap-10]
---

# Introduction

Every file `brae` writes — source code drafted by the forge, compiled binaries, man pages, spec.json dumps, policy overrides — lives in the user's `$HOME`. The user must be able to ask "what did you write and when?" and "undo that" with confidence. Today there is no answer beyond `ls` and `git log --diff` (which doesn't apply to binaries).

This spec adds a Postgres-backed ledger that records every write, its provenance (which CPN, which forge run, which prompt digest), its SHA-256, its classification (source, binary, config, log), and a rollback endpoint that atomically removes a set of artefacts.

## 1. Purpose & Scope

**Purpose.** Give `brae` (and its user) complete, audit-ready reversibility over every file it writes to the host.

**In scope.**
- Postgres table `authored_artifacts`.
- A `ProvenanceRecorder` interface called by every `HostAdapter.WriteFile` (pre-write: record intent; post-write: record result with SHA-256).
- A `RollbackService` that accepts an artefact-set ID and removes the files atomically (with a two-phase delete: move to quarantine → wait-grace-period → hard-delete).
- HTTP admin endpoints: list artefacts, view detail, rollback, purge quarantine.
- Grace period: default 7 days; during grace, files can be restored.
- Redaction: file *contents* are not persisted (too large, potentially sensitive), only `path, sha256, size, mime-type, classification`.
- Tests: every forge-authored file appears; rollback of a forge run deletes all its files; restore-from-quarantine within grace works; post-grace purge is irrevocable.

**Out of scope.**
- Version control of file contents — use `git` for source if you want history. `authored_artifacts` tracks *which file and when*, not *diff over time*.
- Cross-user rollback — single user only.
- Snapshots of user files `brae` did NOT author — out of scope.

**Audience.** `golang-pro`.

## 2. Definitions

- **Artefact**: A single write operation recorded in the ledger.
- **Artefact set**: All artefacts belonging to one forge run / one synth run / one admin action. Rolled back atomically.
- **Quarantine**: `$HOME/.local/brae/quarantine/<artefact_id>/` — the two-phase delete location.
- **Classification**: `source | binary | config | log | man | other`.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: Postgres schema (`0019_authored_artifacts.sql`):
  ```sql
  CREATE TABLE authored_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artefact_set_id UUID NOT NULL,
    host_id TEXT NOT NULL,
    path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    mime TEXT NOT NULL DEFAULT 'application/octet-stream',
    classification TEXT NOT NULL,
    authored_by_cpn_id TEXT,
    forge_run_id TEXT,
    synth_run_id TEXT,
    prompt_digest TEXT,
    written_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quarantined_at TIMESTAMPTZ,
    purged_at TIMESTAMPTZ
  );
  CREATE INDEX idx_aa_set ON authored_artifacts(artefact_set_id);
  CREATE INDEX idx_aa_host_path ON authored_artifacts(host_id, path);
  ```
- **REQ-002**: Port `AuthoredArtefactLedger` in `cpn/persist/artefacts.go` with methods `Record(ctx, a Artefact) error`, `SetForForgeRun(ctx, runID) ([]Artefact, error)`, `Rollback(ctx, setID) error`, `Restore(ctx, setID) error`, `PurgeExpired(ctx, cutoff time.Time) error`.
- **REQ-003**: `HostAdapter.WriteFile` MUST call `ProvenanceRecorder.PreWrite(ctx, intent)` before opening the file and `PostWrite(ctx, result)` after fsync. Failure at `PreWrite` aborts the write.
- **REQ-004**: Grace period default 7 days, configurable via `ARTEFACT_GRACE_DAYS` env var. Purge job runs hourly.
- **REQ-005**: Classification inferred from path extension + MIME sniff: `.c/.h/.go/.py/.sh/.rs/.js → source`, executable bit set → `binary`, under `share/man/` → `man`, under `logs/` → `log`, else `other`.
- **REQ-006**: Rollback is atomic per `artefact_set_id`: move all files to `quarantine/<set_id>/` → update rows' `quarantined_at` → return. If any move fails, all previously moved files are restored and the call returns error.
- **REQ-007**: HTTP:
  - `GET /api/admin/artefacts?forge_run_id=…&set_id=…&host_id=…`
  - `GET /api/admin/artefacts/:id`
  - `POST /api/admin/artefacts/sets/:set_id/rollback`
  - `POST /api/admin/artefacts/sets/:set_id/restore`
  - `POST /api/admin/artefacts/purge` body `{older_than}`
- **REQ-008**: Every rollback and restore MUST emit an event and be recorded in an immutable log table `artefact_events (event_id, set_id, kind, actor, at)`.
- **SEC-001**: No file *contents* persisted. Only metadata. If content preservation is desired, users use git.
- **GUD-001**: Be conservative — default to quarantine, never straight delete, unless `--hard` flag is passed by admin.

## 4. Interfaces & Data Contracts

```go
type Artefact struct {
    ID             string
    ArtefactSetID  string
    HostID         string
    Path           string
    SHA256         string
    SizeBytes      int64
    Mime           string
    Classification string
    AuthoringCPNID string
    ForgeRunID     string
    SynthRunID     string
    PromptDigest   string
    WrittenAt      time.Time
    QuarantinedAt  *time.Time
    PurgedAt       *time.Time
}

type WriteIntent struct {
    Path           string
    Classification string
    ArtefactSetID  string
    AuthoringCPNID string
    ForgeRunID     string
    SynthRunID     string
    PromptDigest   string
}

type ProvenanceRecorder interface {
    PreWrite(ctx context.Context, i WriteIntent) (string /* artefact_id */, error)
    PostWrite(ctx context.Context, id string, sha256 string, size int64, mime string) error
    Rollback(ctx context.Context, setID string) error
    Restore(ctx context.Context, setID string) error
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a forge run authoring three files (src, bin, man), when complete, then `authored_artifacts` contains three rows with the same `forge_run_id` and `artefact_set_id`.
- **AC-002**: Given `POST /api/admin/artefacts/sets/:id/rollback`, when called, then all three files move to quarantine within 100 ms and their rows have `quarantined_at` set.
- **AC-003**: Given a restore within grace, when called, then files return to their original paths with original mtime preserved (best-effort).
- **AC-004**: Given an expired set (quarantined_at > grace), when the hourly purge runs, then files are hard-deleted and rows have `purged_at` set.
- **AC-005**: Given a rollback of a set whose binary is currently registered in the tool registry, then the tool is auto-deprecated with reason `artefact_rolled_back`.
- **AC-006**: `HostAdapter.WriteFile` outside the path jail (GAP-1 SEC-002) is rejected before `PreWrite` is called, so no stub row exists.
- **AC-007**: Rollback is transactional: partial failure leaves the system consistent (rows reflect the actual filesystem state).

## 6. Test Automation Strategy

- Unit: classification inference, grace-period arithmetic.
- Integration: forge-run-simulated write → rollback → restore → re-write (new set_id).
- Race: concurrent rollbacks of different sets do not interfere.
- Coverage ≥ 90%.

## 7. Rationale & Context

**Why not git?** Git stores content; the ledger stores references. Git is optional for the user; the ledger is mandatory for `brae`'s safety story.

**Why two-phase delete?** Rollback decisions are often made mid-forge; reversing a reversal must be fast. Quarantine gives us the window.

**Why coupled to tool registry?** Deleting a binary without deprecating its registry entry would leave a dangling tool pointer (`ErrBinaryDrift` on next invocation). Auto-deprecating closes the loop.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-1 — write call site.
- **EXT-002**: GAP-3 — auto-deprecate on rollback.
- **EXT-003**: GAP-5 — primary source of sets.
- **INF-001**: Postgres.

## 9. Examples & Edge Cases

### Rollback of a failed forge run

Forge errors mid-run; `t-register` never fires; no tool was added. Rollback still works: the half-finished files disappear, registry unaffected.

### Edge — concurrent forge of same tool name

Two forge runs target `brae/http-get` simultaneously. Each has its own `artefact_set_id`; their outputs go to separate scratch dirs. The losing forge is rolled back (admin action or automatic via lint failure).

### Edge — user manually edited a file brae authored

SHA-256 in ledger no longer matches filesystem. Rollback refuses by default; admin `--force` overrides with explicit warning.

## 10. Validation Criteria

- Every `HostAdapter.WriteFile` succeeds only if `PreWrite` succeeds.
- The purge job never runs outside its window.
- Rollback is atomic; the filesystem never reflects a partial state.

## 11. Related Specifications / Further Reading

- [spec-architecture-host-adapter-nodekind-bash.md](./spec-architecture-host-adapter-nodekind-bash.md) — GAP-1 (write call site).
- [spec-architecture-dynamic-tool-registry.md](./spec-architecture-dynamic-tool-registry.md) — GAP-3 (auto-deprecate).
- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5 (primary producer).
