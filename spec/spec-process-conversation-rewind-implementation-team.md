---
title: Implementation Team & Delivery Plan — Conversation Rewind via Fork-and-Promote
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: liwaisi-tech
tags: [process, delivery, team, rewind, hexagonal, tdd, ddd, quality-gates, rollout]
---

# Introduction

This specification defines the **execution plan** for shipping the Conversation Rewind feature described in [spec-architecture-conversation-rewind-fork-promote.md](spec-architecture-conversation-rewind-fork-promote.md) (hereafter **"the architecture spec"**). It enumerates the nine expert roles on the delivery team, their mandates, concrete deliverables traced to architecture-spec requirement IDs, cross-role interfaces and blockers, quality gates, sequencing across six calendar weeks, rollout phases behind a feature flag, and the open policy questions that must be resolved before General Availability (GA). Every deliverable cites the architecture-spec ID it fulfills so traceability is unambiguous.

## 1. Purpose & Scope

**Purpose**: Provide a single, self-contained execution contract that every role on the delivery team can read once and know (a) what they own, (b) what blocks them, (c) what they block, (d) what "done" means for each piece of their work, and (e) how and when their work ships to production.

**Scope**:
- **In scope**: Role mandates and deliverables; cross-role interfaces and hand-offs; risks per role; sequencing across weeks 1–6+; quality gates and coverage thresholds; feature-flag rollout phases (alpha → beta → GA); open policy questions owed before GA; cadence and ceremonies; escalation path.
- **Out of scope**: Requirements of the feature itself (owned by the architecture spec); detailed code design inside each role's deliverables (owned by the individual engineers during implementation); hiring decisions, compensation, or individual performance management.

**Intended Audience**: Engineering managers, tech leads, the nine role owners, QA, DevOps, Product, and anyone reviewing release-readiness for the Rewind feature.

**Assumptions**:
- The architecture spec is the canonical source of truth for *what* the feature does. This spec is the canonical source of truth for *how* it ships.
- The user follows the documented SDLC workflow (issue analysis → dream team → plan → TDD/DDD → quality gates → PR review loop → human approval).
- Existing hexagonal architecture conventions in `back/go-assistant` and React-hook conventions in `front/react-assistant` are followed; see GUD-001 and GUD-003 in the architecture spec.
- A feature flag system is available (a no-op boolean check in Go + a runtime toggle exposed to the frontend is sufficient; a full flag platform is not required).
- All nine roles are staffed; when a single engineer covers multiple roles, they still execute each role's deliverables and gates in sequence.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Architecture spec** | [spec-architecture-conversation-rewind-fork-promote.md](spec-architecture-conversation-rewind-fork-promote.md), the feature requirements. |
| **Role** | One of the nine expert profiles defined in §3.1. A single person MAY hold more than one role; the deliverables remain distinct. |
| **Mandate** | The single sentence describing what a role owns end-to-end. Mandates MUST NOT overlap. |
| **Deliverable** | A concrete artifact (file, migration, test suite, design document, dashboard) produced by exactly one role. |
| **Interface** | The synchronous or asynchronous hand-off between two roles — who produces what, who consumes what, and what the contract is. |
| **Quality Gate** | A binary pass/fail check that MUST succeed before the next phase proceeds. See §3.6. |
| **Rollout Phase** | One of `alpha`, `beta`, `GA`. See §3.7. |
| **`rewind_enabled`** | The feature flag that gates every user-facing rewind affordance and endpoint. Off by default in all environments until Phase `beta`. |
| **Policy Question** | A product/business decision deliberately deferred by the architecture spec; MUST be resolved before the GA gate. See §3.8. |
| **ADR** | Architecture Decision Record — a short markdown file recording a binding architectural decision, its context, and its consequences. |
| **DoD** | Definition of Done — the checklist that a deliverable MUST satisfy before it is considered complete. See §3.4. |
| **AC** | Acceptance Criterion defined in the architecture spec (e.g. AC-009). |
| **REQ / CON / GUD / PAT / SEC** | Requirement / Constraint / Guideline / Pattern / Security Requirement IDs from the architecture spec. |
| **WCAG 2.2 AA** | Web Content Accessibility Guidelines v2.2, Level AA. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Role Roster

- **ROL-001**: The team SHALL consist of exactly the nine roles listed in §4.1. No additional roles are created; no role is removed.
- **ROL-002**: Each role SHALL have exactly one accountable owner. A backup is identified for every role; the backup does not share accountability during normal operation.
- **ROL-003**: A single person MAY hold more than one role; when they do, they SHALL deliver every role's artifacts and pass every role's gates as if the roles were held by distinct people.
- **ROL-004**: Role mandates SHALL NOT overlap. When scope ambiguity arises, the Solution Architect adjudicates (see PAT-001).

### 3.2 Deliverable Rules

- **REQ-001**: Every deliverable SHALL cite at least one architecture-spec requirement ID (REQ / CON / GUD / PAT / SEC / AC). Deliverables without a citation SHALL be rejected at pull-request review.
- **REQ-002**: Every deliverable SHALL have a single responsible role. Shared ownership is not permitted.
- **REQ-003**: Every pull request SHALL carry the feature tag `feature:rewind` in its title prefix and link back to the architecture-spec requirement IDs it addresses.
- **REQ-004**: A deliverable is complete only when it passes the Definition of Done checklist in §3.4 and the applicable Quality Gate in §3.6.

### 3.3 Interface Rules

- **REQ-005**: Every blocking dependency between roles SHALL be represented in §4.2 "Role Interfaces" with a stated contract (what is produced, what shape, when).
- **REQ-006**: A downstream role SHALL NOT begin a deliverable that depends on an unfinished upstream contract unless a mock/stub satisfying the contract is agreed in writing by both role owners.
- **REQ-007**: Contract changes after a downstream role has begun SHALL require written sign-off from every downstream consumer.

### 3.4 Definition of Done (applies to every deliverable)

- **DOD-001**: Cites the architecture-spec IDs it fulfills.
- **DOD-002**: Has automated tests at the appropriate level (unit and/or integration and/or E2E) per §3.6.
- **DOD-003**: Passes `make test`, `make lint`, and project coverage thresholds.
- **DOD-004**: Has been reviewed via `/pr-review` and every actionable comment addressed.
- **DOD-005**: Has been manually signed off by the Solution Architect when the deliverable touches any of the three cross-cutting invariants (Axiom A9, rewind-transaction atomicity, `branch_id` propagation).
- **DOD-006**: Is feature-flagged behind `rewind_enabled` if it is user-observable and is not yet at GA.
- **DOD-007**: Has corresponding entries in runbooks, dashboards, or monitoring where applicable (DevOps deliverables only).
- **DOD-008**: Has an entry in the release notes for its landing phase.

### 3.5 Sequencing & Milestones

- **SEQ-001**: Work proceeds across six phases named **Foundation**, **Backend Core**, **Frontend Core**, **Integration & GC**, **Quality Gates**, **Rollout**. Phases 2 and 3 overlap by design; other phases are strictly sequential.
- **SEQ-002**: The indicative calendar is six weeks (Weeks 1–6+ as documented in §4.3). Slippage is tracked by phase, not by week; a phase that slips does not truncate the next phase.
- **SEQ-003**: A phase SHALL NOT start until its entry gate (§3.6) has passed.

### 3.6 Quality Gates

Quality Gates are binary. Each one has an **owner** and a **check**. A gate failure blocks phase transition.

- **GATE-FOUNDATION** (end of Phase 1, owned by Solution Architect):
  - ADR on fork-and-promote is merged (cites CON-001, PAT-001).
  - Code-review checklist for the three invariants is merged to `.github/`.
  - Migration `009_rewind_branches` draft is reviewed against §4.1 of the architecture spec.
  - AI Engineer's hashing spec + `ReplayPolicy` design note is merged.
- **GATE-BACKEND** (end of Phase 2, owned by Golang Backend Engineer):
  - Migration has been run on staging with backfill verification queries (DAT-001 of architecture spec §8).
  - `BranchRepository`, `MarkingRepository`, `ReplayCacheRepository` all have ≥85% line coverage.
  - HTTP endpoints `POST /sessions/{id}/rewind`, `POST /sessions/{id}/rewind/undo`, `GET /sessions/{id}/branches` return documented contracts (architecture spec §4.3) under integration tests.
  - SSE events carry `branch_id`; clients filtering by old branch discard correctly.
- **GATE-FRONTEND** (end of Phase 3, owned by React Engineer):
  - `RewindDialog`, `UndoToast`, `StateBadge`, `EditAnswerButton`, `TurnPicker` all merged behind `rewind_enabled = false`.
  - `useChat` reducer handles `REWIND_STARTED`, `REWIND_COMPLETED`, `BRANCH_SWITCHED`, `UNDO_TOAST_DISMISSED`, `UNDO_WINDOW_EXPIRED` with ≥85% branch coverage.
  - Visual QA against UX mocks is signed off by the UX/UI Designer.
- **GATE-INTEGRATION** (end of Phase 4, owned by Solution Architect):
  - End-to-end scenario in staging: answer questionnaire → rewind → edit "Other" → new plan → undo within window → original restored.
  - GC background job purges due-for-purge archived branches on staging without production-like lag.
  - Rate-limit (default 20/hr/user) returns HTTP 429 with `Retry-After`.
  - Replay cache hit/miss metrics are observable on the dashboard.
- **GATE-QUALITY** (end of Phase 5, owned by QA Engineer):
  - All acceptance criteria AC-001 through AC-018 of the architecture spec have automated coverage; exceptions are documented with a manual-verification artifact.
  - `axe-core` reports zero WCAG 2.2 AA violations on `RewindDialog` and `UndoToast`.
  - ≥85% coverage on new Go and TS code; **100% coverage on the atomic-rewind transaction, replay-cache hit path, undo-window expiration path, and rate-limit enforcement path**.
  - Two manual accessibility walkthroughs completed (one aphantasic reviewer, one anxiety-focused reviewer) with findings addressed or explicitly accepted by Product.
- **GATE-ALPHA**, **GATE-BETA**, **GATE-GA**: see §3.7.

### 3.7 Rollout Phases

- **ROLL-001**: The feature SHALL ship behind the feature flag `rewind_enabled`. The flag is off in all environments at Phase 4 start.
- **ROLL-002 (GATE-ALPHA)**: Enter alpha when GATE-INTEGRATION has passed. Flag is ON only for the internal team and named design partners. Minimum alpha duration: 5 business days. Exit criteria: zero production incidents tagged `rewind-bug-*`; no data-integrity issues found; every alpha participant successfully rewound and undid at least once.
- **ROLL-003 (GATE-BETA)**: Enter beta when GATE-QUALITY has passed AND alpha exit criteria are met. Flag becomes an **opt-in** user preference (users enable it themselves). Minimum beta duration: 10 business days. Exit criteria: <0.5% rewind-related error rate; zero P0/P1 incidents in the window; every policy question in §3.8 resolved and implemented.
- **ROLL-004 (GATE-GA)**: Enter GA when beta exit criteria are met AND the AI Product Manager has formally signed off. Flag defaults ON for all users; a kill switch (flag forced OFF globally) remains available for 30 days post-GA.
- **ROLL-005**: At no point during alpha or beta SHALL the feature flag be enabled for all users by default. This prevents accidental broad exposure to the feature before GA.

### 3.8 Open Policy Questions (must be resolved before GATE-GA)

- **POL-001**: Billing treatment of replay-cache hits — do cache-hit transitions bill the user zero (reflecting zero upstream cost), bill at a discounted rate, or bill identically to a live call? Owner: AI Product Manager. Input: metrics from alpha showing hit rate distribution. Deliverable: a short ADR and a configuration in `token_ledger` accounting.
- **POL-002**: Default behavior for `SideEffectful` transitions on replay — does the user always see the confirm/skip HITL surface, or can the topology author declare a per-transition default? Owner: AI Product Manager + Agentic Architect. Deliverable: decision recorded in the ADR and, if the answer is "per-transition default", a topology-declaration field added.
- **POL-003**: Per-user rewind quota — is the default of 20 rewinds/hour (SEC-003) sufficient, tightened, or productized (tiered by plan)? Owner: AI Product Manager. Deliverable: a written decision and any config changes to `REWIND_RATE_LIMIT_PER_HOUR`.
- **POL-004**: Retention window for archived branches — does the default 60s undo window meet user expectations discovered in alpha, or should a longer default (e.g. 5 min, 1 h) be adopted? Owner: UX/UI Designer + AI Product Manager. Deliverable: decision and updated default for `REWIND_UNDO_WINDOW_SECONDS` within the allowed range (30s–24h, per CON-006).
- **POL-005**: Whether the CPN Monitor should expose archived branches in a "history" filter or hide them entirely until GA. Owner: Agentic Architect + AI Product Manager.

### 3.9 Process Guidelines

- **GUD-001**: Every commit message SHALL use conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`).
- **GUD-002**: PRs SHALL be small and focused (≤500 lines of diff where possible); larger PRs require Solution Architect pre-approval.
- **GUD-003**: Test-Driven Development (write the failing test first) is the default for Go and TypeScript code. Exceptions (e.g. pure glue code) are called out in the PR description.
- **GUD-004**: Domain-Driven Design boundaries SHALL be preserved: `cpn/` owns engine concerns, `cpn/persist/` owns ports, `store/postgres/` owns adapters, `internal/driving/httpapi/` owns HTTP handlers.
- **GUD-005**: Accessibility is not a post-hoc check; every user-facing PR SHALL include an `axe-core` test where applicable, not only at GATE-QUALITY.
- **GUD-006**: Daily 15-minute stand-up during Phases 2–5; weekly 60-minute planning and retro during all phases.
- **GUD-007**: The `rewind` feature flag SHALL default OFF for all environments; a PR that flips the default globally requires explicit GA sign-off.

### 3.10 Patterns

- **PAT-001**: **Scope adjudication** — when two roles believe they own the same deliverable, the Solution Architect decides in writing (an issue comment suffices) and updates §4.1 of this spec if the boundary was genuinely unclear.
- **PAT-002**: **Mock-then-integrate** — when an upstream contract is defined but not yet implemented, the downstream role builds against a mock that satisfies the contract verbatim; integration replaces the mock in a separate PR.
- **PAT-003**: **Traceability in PR body** — every PR starts with a "Fulfills" list that cites the architecture-spec IDs it addresses; reviewers verify the citations before approving.
- **PAT-004**: **Feature-flag-first** — no user-observable code path lands without being gated by `rewind_enabled` until GA; removing the flag is a separate post-GA PR.

### 3.11 Security & Compliance

- **SEC-001**: Every HTTP deliverable SHALL be behind `AuthMiddleware` and verify session ownership before any branch mutation (aligns with SEC-001 of the architecture spec).
- **SEC-002**: Any PR that adds a new logged field SHALL be reviewed for PII and structured-logging rules.
- **SEC-003**: The DevOps Engineer SHALL verify that the GC job, archived-branch listings, and rewind endpoints do not emit user payload content into access logs.
- **COM-001**: WCAG 2.2 AA compliance on `RewindDialog` and `UndoToast` is a GATE-QUALITY requirement (aligns with COM-001 of the architecture spec).

### 3.12 Constraints

- **CON-001**: The team SHALL NOT expand scope beyond what the architecture spec defines. Scope creep (e.g. a "visual branch graph" UI, cross-session rewind, multi-user collaborative rewind) SHALL be rejected and filed as post-GA follow-ups.
- **CON-002**: A role's deliverables SHALL NOT regress any existing feature covered by an existing spec (in particular, the forking spec and the HITL persistence/rehydration specs).
- **CON-003**: No deliverable SHALL skip its Quality Gate. A gate that cannot pass triggers a phase slip, not an override.
- **CON-004**: The kill-switch mechanism MUST remain available for 30 days post-GA (per ROLL-004). Removing it earlier requires AI Product Manager sign-off.

## 4. Interfaces & Data Contracts

### 4.1 Role Roster, Mandates, and Deliverables

Each role entry below specifies: **Mandate**, **Deliverables** (with architecture-spec citations), **Blocks** (what the role must wait for) and **Blocked By whom** (who this role blocks downstream), and **Primary risk**.

#### Role 1 — Solution Architect

- **Mandate**: Guard the three cross-cutting invariants — Axiom A9, rewind-transaction atomicity, `branch_id` propagation on every read path.
- **Deliverables**:
  - ADR `adr-rewind-fork-and-promote.md` (CON-001 of architecture spec, PAT-001 of architecture spec).
  - Code-review checklist at `.github/pull_request_template_rewind.md`.
  - Sign-off on `009_rewind_branches` migration shape (architecture spec §4.1).
  - Sign-off on `ReplayPolicy` design note (architecture spec §3 "Idempotent Replay Cache").
  - Invariant-breach escalation owner throughout Phases 2–5.
- **Blocks**: none (entry role).
- **Blocked by**: Data Engineer, AI Engineer (they cannot merge foundational work before sign-off).
- **Primary risk**: Silent drift in downstream consumers that forget `branch_id` filtering. Mitigation: invariant checklist enforced on every PR with the `feature:rewind` tag.

#### Role 2 — AI Engineer / Agentic Architect

- **Mandate**: Extend the CPN executor to checkpoint markings, restore from them, and honor `ReplayPolicy`.
- **Deliverables** (in `back/go-assistant/cpn/`):
  - `ReplayPolicy` enum and transition declaration support (REQ-012 of architecture spec).
  - Marking capture at the three boundaries (REQ-001 of architecture spec).
  - `CPN.RehydrateFromMarking` (CON-004 of architecture spec, AC-002 of architecture spec).
  - Canonical input-marking hash function (REQ-011 of architecture spec).
  - Executor integration with `ReplayCacheRepository`, including `Replayed: true` event emission (REQ-013, CON-005 of architecture spec).
  - `SideEffectful` confirm/skip HITL surface on replay (EDGE-004 of architecture spec).
  - Unit tests covering AC-001, AC-002, AC-006, AC-007, AC-008 of the architecture spec.
- **Blocks**: Solution Architect sign-off on hashing spec.
- **Blocked by**: Golang Backend Engineer (cache adapter), QA Engineer (behavioral tests).
- **Primary risk**: Hidden non-determinism inside a "deterministic" transition (e.g. current-time payload). Mitigation: canonical-JSON spec documented with explicit exclusions.

#### Role 3 — Data Engineer

- **Mandate**: Ship the `009_rewind_branches` migration and backfill such that existing data survives and every new column is NOT NULL after backfill.
- **Deliverables** (in `back/go-assistant/store/postgres/migrations/`):
  - `009_rewind_branches.up.sql` + `.down.sql` per architecture spec §4.1.
  - Backfill verification query set.
  - Rollback rehearsal log from staging.
  - `pgcrypto` enablement verification.
- **Blocks**: Solution Architect sign-off.
- **Blocked by**: Golang Backend Engineer (ports consume these tables), DevOps Engineer (online-migration execution on production).
- **Primary risk**: Large-table `SET NOT NULL` locks `messages`/`events`. Mitigation: batched backfill, `NOT VALID` + later validation.

#### Role 4 — Golang Backend Engineer

- **Mandate**: Implement every server-side port, HTTP endpoint, SSE change, background job, and the rate limiter.
- **Deliverables**:
  - Ports `BranchRepository`, `MarkingRepository`, `ReplayCacheRepository` (architecture spec §4.2) with Postgres adapters.
  - Advisory-lock scoped by `session_id` around rewind (EDGE-001 of architecture spec).
  - HTTP handlers: `POST /sessions/{id}/rewind`, `POST /sessions/{id}/rewind/undo`, `GET /sessions/{id}/branches` (architecture spec §4.3), with `AuthMiddleware` + ownership checks (SEC-001, SEC-002 of architecture spec).
  - Rate limiter (SEC-003 of architecture spec) honoring `REWIND_RATE_LIMIT_PER_HOUR`.
  - SSE broker: per-event `branch_id` attachment; filter on delivery; new `branch_switched` event type (architecture spec §4.4).
  - GC background job with leader-election awareness (REQ-017 of architecture spec).
  - Env-var plumbing for `REWIND_UNDO_WINDOW_SECONDS`, `REWIND_RATE_LIMIT_PER_HOUR`, `REWIND_GC_INTERVAL_SECONDS` (honors CON-006 of architecture spec).
  - Integration tests covering AC-003, AC-004, AC-005, AC-009, AC-010, AC-011 of the architecture spec.
- **Blocks**: Data Engineer (migration), AI Engineer (CPN hooks).
- **Blocked by**: React Engineer (contracts consumption), QA Engineer (integration tests).
- **Primary risk**: Concurrent-rewind + SSE write race. Mitigation: Solution Architect reviews lock ordering; integration test for the race is mandatory.

#### Role 5 — React Engineer

- **Mandate**: Build every UI element and wire the rewind state machine into `useChat`.
- **Deliverables** (in `front/react-assistant/src/`):
  - `TurnPicker` extraction from `ForkDialog.tsx` (GUD-002 of architecture spec).
  - `RewindDialog.tsx` with turn picker, editable answers form, `OtherBeforeAfter` panel, `DiscardedTurnsPreview` (truncation per CON-007 of architecture spec), `SideEffectWarningPanel`, summary line, explicit Confirm/Cancel bar (REQ-020, REQ-023, REQ-028 of architecture spec; AC-013, AC-014, AC-016).
  - `UndoToast.tsx` anchored top-of-panel with live `MM:SS` `aria-live="polite"` countdown, Undo and Dismiss, expired state (REQ-022 of architecture spec; AC-015).
  - `EditAnswerButton` on `QuestionnaireLocked` (REQ-019 of architecture spec; AC-012).
  - `StateBadge` in `ChatHeader` (REQ-025 of architecture spec).
  - `useChat` reducer actions `REWIND_STARTED`, `REWIND_COMPLETED`, `BRANCH_SWITCHED`, `UNDO_TOAST_DISMISSED`, `UNDO_WINDOW_EXPIRED`; new state `currentBranchId`, `undoToast`, `rewindInProgress` (architecture spec §4.6).
  - SSE client filter discarding non-matching `branch_id` events (architecture spec §4.4).
  - Stable DOM anchor `id="block-{branch_id}-{message_id}"` + URL fragment scroll restoration (REQ-024 of architecture spec; AC-018).
  - No auto-scroll/auto-focus on rewind complete (REQ-030 of architecture spec).
  - Copy change for two entry points (REQ-026 of architecture spec).
  - Unit + `axe-core` tests covering REQ-027 through REQ-031 and AC-012 through AC-017.
- **Blocks**: Golang Backend Engineer (contract availability), UX/UI Designer (mocks + copy).
- **Blocked by**: QA Engineer (E2E), UX/UI Designer (visual sign-off).
- **Primary risk**: SSE `branch_switched` arriving before rewind HTTP response on slow networks. Mitigation: reducer handles either order idempotently; a dedicated test exercises both orderings.

#### Role 6 — UX/UI Designer

- **Mandate**: Design the Rewind UX such that every aphantasia and anxiety requirement in the architecture spec is visible in the final UI.
- **Deliverables**:
  - High-fidelity mocks for `RewindDialog`, `UndoToast`, `StateBadge`, `OtherBeforeAfter` panel, `DiscardedTurnsPreview` (REQ-019 through REQ-026, REQ-027 through REQ-030 of architecture spec).
  - Copy deck covering every button, aria-label, state text, expired-window text.
  - Two entry-point labels for Fork vs Rewind (REQ-026 of architecture spec).
  - Color tokens on the warning/amber palette distinct from primary (GUD-004 of architecture spec) with verified WCAG 2.2 AA contrast.
  - Motion spec explicitly forbidding auto-scroll, auto-focus, click-outside-to-close, and timer-only dismissal.
  - Recruitment and coordination of two manual accessibility reviewers (aphantasic + anxiety-focused) completed at least two weeks before GATE-QUALITY.
  - Walkthrough script and record of findings from both manual reviews (Validation Criteria 7 and 8 of architecture spec).
- **Blocks**: React Engineer.
- **Blocked by**: AI Product Manager (reviewer recruitment context).
- **Primary risk**: Instinct to add ephemeral toasts or motion-only feedback. Mitigation: review checklist explicitly rejects those patterns for this feature.

#### Role 7 — QA Engineer

- **Mandate**: Automate verification of every architecture-spec AC and validation criterion; coordinate manual accessibility walkthroughs.
- **Deliverables**:
  - Go unit tests: `branch_repo_test.go`, `marking_repo_test.go`, `replay_cache_test.go`, `session_service_rewind_test.go`.
  - Go integration tests: `rewind_integration_test.go` (atomicity, concurrency, GC, rate limit).
  - Go behavioral tests: `executor_replay_test.go` (cache hit path, mode restore, SideEffectful gating).
  - Vitest + RTL: `RewindDialog.test.tsx`, `UndoToast.test.tsx`, `useChat.rewind.test.ts`, `TurnPicker.test.tsx`.
  - `@axe-core/react` checks with zero AA violations on RewindDialog and UndoToast.
  - Playwright E2E: the scenario in Validation Criterion 9 of the architecture spec.
  - Coverage report achieving ≥85% on new code and 100% on the four critical paths listed in §3.6 `GATE-QUALITY`.
  - Per-AC traceability matrix mapping architecture-spec AC-001..AC-018 to test file and test name.
- **Blocks**: Golang Backend Engineer, React Engineer.
- **Blocked by**: Solution Architect (merge to main gate).
- **Primary risk**: Flaky timing on the 60-second grace-window test. Mitigation: `REWIND_UNDO_WINDOW_SECONDS` is env-configurable; E2E sets it to 5 seconds.

#### Role 8 — DevOps Engineer

- **Mandate**: Ship the migration, runtime config, monitoring, and GC leader-election without production regressions.
- **Deliverables**:
  - Online migration execution plan and staging rehearsal log (architecture spec §4.1, DAT-001 of architecture spec §8).
  - Runtime env vars registered in Helm/Docker/Compose manifests: `REWIND_UNDO_WINDOW_SECONDS` (default 60; clamp [30, 86400] per CON-006 of architecture spec), `REWIND_RATE_LIMIT_PER_HOUR` (default 20), `REWIND_GC_INTERVAL_SECONDS` (default 300).
  - `pgcrypto` extension verified in prod + staging.
  - GC job leader-election for multi-replica deployment (REQ-017 of architecture spec).
  - Monitoring: metrics for rewinds/hr, replay cache hit rate, archived-branch backlog, GC job duration. Alert: archived-branch backlog > 10,000.
  - Redis cache invalidation on branch promotion (INF-002 of architecture spec).
  - Structured-log audit confirming no payload leakage (SEC-003 of this spec).
  - Kill-switch runbook for 30-day post-GA availability (ROLL-004, CON-004).
- **Blocks**: Data Engineer (migration timing), Solution Architect (production release).
- **Blocked by**: AI Product Manager (GA sign-off for flag default flip).
- **Primary risk**: Migration locking `messages`/`events` on production. Mitigation: batched backfill strategy agreed with Data Engineer.

#### Role 9 — AI Product Manager

- **Mandate**: Own scope discipline, rollout phasing, policy-question resolution, and final GA sign-off.
- **Deliverables**:
  - Written rollout plan covering Phases `alpha`, `beta`, `GA` with exit criteria per ROLL-002..ROLL-004.
  - Scope guardrail decisions for every proposed extension (CON-001 of this spec).
  - Resolutions for POL-001 through POL-005 of this spec, each captured as a short ADR.
  - Recruitment brief for manual accessibility reviewers (paired with UX/UI Designer).
  - AC sign-off for every architecture-spec AC at GATE-QUALITY.
  - Final GA sign-off (ROLL-004).
  - Post-GA review (30 days in) deciding whether the kill switch can be removed (CON-004).
- **Blocks**: Alpha / beta / GA phase transitions.
- **Blocked by**: Solution Architect (technical-readiness signal), UX/UI Designer (accessibility signal), QA Engineer (quality signal).
- **Primary risk**: Scope creep toward a branch-graph visualizer. Mitigation: explicit rejection per CON-001 of this spec; file as post-GA follow-up.

### 4.2 Role Interfaces (who depends on whom, what the contract is)

| Upstream Role | Contract (deliverable) | Downstream Role | Contract shape |
|---|---|---|---|
| Solution Architect | Invariants checklist + fork-and-promote ADR | All roles | Markdown under `.github/` and `docs/adr/`; checklist items cite architecture-spec IDs |
| Data Engineer | `009_rewind_branches` migration | Golang Backend Engineer | Tables, columns, indexes exactly as architecture spec §4.1; backfill completes before gate |
| AI Engineer | `ReplayPolicy` enum + marking types | Golang Backend Engineer | Go types and interfaces in `cpn/` and `cpn/persist/` |
| Golang Backend Engineer | HTTP + SSE contracts | React Engineer | Request/response shapes and event types match architecture spec §4.3, §4.4; OpenAPI stub published at Phase 2 end |
| UX/UI Designer | Mocks + copy deck + motion spec | React Engineer | Figma (or equivalent) file + markdown copy deck + explicit motion "forbidden list" |
| Golang Backend Engineer + React Engineer | Feature-flag-gated wiring | QA Engineer | `rewind_enabled=true` path reachable on staging |
| QA Engineer | Per-AC traceability matrix | AI Product Manager | CSV/markdown table linking AC → test file → status |
| DevOps Engineer | Staging + prod readiness report | AI Product Manager | Runbook, dashboards, alert rules, kill-switch procedure |
| AI Product Manager | Rollout plan + policy ADRs | Solution Architect + DevOps | Markdown ADRs; config values ready for plumbing |

### 4.3 Sequencing — Phases, weeks, parallelism

```
Phase 1 — Foundation            Week 1
  [Solution Architect]  ADR + invariants checklist
  [Data Engineer]        Migration draft + staging rehearsal plan
  [AI Engineer]          ReplayPolicy + hashing design note
  [UX/UI Designer]       Mocks kickoff; reviewer recruitment starts
  [AI Product Manager]   Rollout plan v1; policy-question owners assigned
  GATE-FOUNDATION

Phase 2 — Backend Core           Weeks 2–3     ┐
  [Data Engineer]        Migration shipped to staging with backfill        │
  [AI Engineer]          CPN marking capture + rehydration + hash          │ PARALLEL
  [Golang Backend Eng.]  Ports + adapters + HTTP contracts + SSE + GC v0   │ WITH PHASE 3
  [Solution Architect]   Invariants PR reviews                             │
  GATE-BACKEND                                                             │
                                                                           │
Phase 3 — Frontend Core          Weeks 2–4     ┘
  [UX/UI Designer]       Mocks + copy deck finalized
  [React Engineer]       TurnPicker, RewindDialog, UndoToast, reducer, SSE filter
  GATE-FRONTEND

Phase 4 — Integration & GC       Week 4
  [Golang Backend Eng.]  GC background job + leader-election + rate limiter
  [DevOps Engineer]      Staging dashboards + alerts + kill-switch runbook
  [Solution Architect]   E2E walkthrough sign-off
  GATE-INTEGRATION  →  GATE-ALPHA enabled for internal + design partners

Phase 5 — Quality Gates          Week 5
  [QA Engineer]          Full automation suite green; coverage targets met
  [UX/UI Designer]       Manual accessibility walkthroughs (aphantasic + anxiety) completed
  [AI Product Manager]   AC sign-off; POL-001..POL-005 resolved
  GATE-QUALITY  →  GATE-BETA (opt-in)

Phase 6 — Rollout                Weeks 6+
  Alpha (≥5 business days)   →   Beta (≥10 business days, opt-in)   →   GA
  Kill switch remains 30 days post-GA
  GATE-GA
```

### 4.4 Cadence & Ceremonies

| Ceremony | Frequency | Owner | Purpose |
|---|---|---|---|
| Stand-up | Daily, 15 min, Phases 2–5 | Tech lead | Blocker surfacing; hand-off confirmation |
| Planning | Weekly, 60 min, all phases | AI Product Manager | Scope check; phase-gate progress |
| Retro | Weekly, 30 min, all phases | QA Engineer | Gate-failure diagnosis; process adjustments |
| PR review | Async on every PR | Reviewer queue | DoD + invariants checklist |
| `/pr-review` deep review | On every non-trivial PR | Author triggers | Multi-disciplinary analysis |
| Accessibility walkthrough | Twice during Phase 5 | UX/UI Designer | Manual AC verification |
| GA go/no-go | Once, end of beta | AI Product Manager | ROLL-004 sign-off |

### 4.5 Escalation Path

- Technical ambiguity → Solution Architect → (if unresolved within 24 h) AI Product Manager.
- Scope creep → AI Product Manager → rejected and filed as follow-up (CON-001 of this spec).
- Quality-gate failure → QA Engineer raises to AI Product Manager within the same business day; phase slip is announced in the next stand-up.
- Production incident during alpha/beta → DevOps Engineer triggers kill switch; post-mortem within 5 business days.

## 5. Acceptance Criteria

- **AC-001**: Given the team is assembled, When a deliverable lands without citing at least one architecture-spec ID, Then the PR SHALL be rejected at review (REQ-001 of this spec).
- **AC-002**: Given Phase 1 begins, When the ADR `adr-rewind-fork-and-promote.md` is not merged by end of Phase 1, Then GATE-FOUNDATION SHALL NOT pass and Phase 2 SHALL NOT start.
- **AC-003**: Given the `009_rewind_branches` migration has run on staging, When the verification query set is executed, Then every pre-existing session has exactly one root branch and every pre-existing message/event has `branch_id` set to that branch's id.
- **AC-004**: Given Phase 3 is complete, When the UX/UI Designer reviews the built UI against mocks, Then sign-off is either granted or a written list of deltas is filed; GATE-FRONTEND SHALL NOT pass without a signed artifact.
- **AC-005**: Given Phase 4 has ended, When an end-to-end rewind + undo is executed on staging with `rewind_enabled=true` for a test user, Then the full scenario passes without manual intervention and SSE stream shows a single `branch_switched` event per branch change.
- **AC-006**: Given Phase 5 coverage gates are measured, When the report is produced, Then new Go code is ≥85%, new TypeScript code is ≥85%, and the four critical paths (atomic rewind, replay-cache hit, undo-window expiration, rate-limit enforcement) are each at 100%.
- **AC-007**: Given `axe-core` is executed against `RewindDialog` and `UndoToast`, When the report is produced, Then zero WCAG 2.2 AA violations are reported.
- **AC-008**: Given alpha has been live for at least 5 business days, When exit criteria are evaluated, Then zero production incidents tagged `rewind-bug-*` exist AND every alpha participant has completed at least one successful rewind + undo.
- **AC-009**: Given beta has been live for at least 10 business days, When exit criteria are evaluated, Then rewind-related error rate is <0.5%, zero P0/P1 incidents exist in the window, AND POL-001 through POL-005 are each resolved with an ADR.
- **AC-010**: Given GA sign-off is requested, When the AI Product Manager reviews, Then approval SHALL be granted only if every Phase's gate is green and the kill-switch runbook is in place.
- **AC-011**: Given GA has shipped, When 30 days pass without incident, Then the kill-switch mechanism MAY be removed via a separate PR with AI Product Manager sign-off.
- **AC-012**: Given any phase gate fails, When the team convenes, Then a phase slip SHALL be recorded rather than the gate being overridden (CON-003 of this spec).
- **AC-013**: Given a single person holds two roles, When their PR lands, Then it SHALL pass the Definition of Done and Quality Gate for each role's deliverables independently (ROL-003 of this spec).

## 6. Test Automation Strategy

**Test Levels**:
- **Unit**: Every port, reducer, and isolated component. Go `testing` stdlib; TypeScript Vitest.
- **Integration**: Postgres integration tests for the rewind transaction, GC, replay cache; SSE client-server integration in a test harness.
- **End-to-End**: Playwright scenario covering the full user flow (architecture-spec Validation Criterion 9).
- **Accessibility**: `@axe-core/react` at the component level and at the flow level in Playwright.

**Frameworks**:
- Go: `testing` + `testify/assert` + `pgx` test containers.
- TypeScript: Vitest + React Testing Library + `@axe-core/react`.
- E2E: Playwright.

**Test Data Management**: disposable schema per Postgres integration test; fixture-builder helpers for CPN topologies with known `ReplayPolicy` values; frontend uses MSW (or equivalent) to mock SSE during component tests.

**CI/CD Integration**: all levels run in existing GitHub Actions pipelines on every PR tagged `feature:rewind`; E2E runs on PR-to-main.

**Coverage Requirements**:
- ≥85% line coverage on new Go code.
- ≥85% branch coverage on new TypeScript code.
- 100% coverage on: atomic rewind transaction; replay-cache hit path (including event emission); undo-window expiration; rate-limit enforcement.

**Performance Testing**:
- Rewind on a 200-message branch at index 100 completes in <2s (architecture spec §10 item 2).
- GC job processes 1,000 due-for-purge branches inside a 5-minute tick without blocking (architecture spec §10 item 2).

## 7. Rationale & Context

**Why nine roles, not fewer.** Collapsing the roles risks a single owner silently deprioritising a dimension — typically accessibility or rollout discipline. Nine named mandates make every dimension visible even when one person holds multiple roles.

**Why the Solution Architect is a distinct role.** The three cross-cutting invariants (Axiom A9, atomicity, `branch_id` propagation) fail silently if no one is explicitly watching for them across workstreams. Assigning a dedicated owner is cheaper than discovering a drift in production.

**Why a strict phase gate model.** Architectural features with database migrations, CPN-engine changes, and novel UX have historically slipped when teams flowed work across parallel tracks without hard checkpoints. Phase gates detect misalignment early; phase slips are preferable to post-release incidents.

**Why feature-flag-first, even inside the team.** The rewind surface is destructive and state-altering; accidental exposure during Phase 3 or 4 could confuse users. `rewind_enabled` defaults off in every environment until alpha; flipping the default is a dedicated PR with AI Product Manager sign-off.

**Why open policy questions are deferred, not pre-decided.** POL-001 (billing of replay-cache hits) is best answered with real alpha data on hit rates; POL-004 (default undo window) is best answered with real user behavior. Pre-deciding either without data is lower-quality than deciding with alpha telemetry in hand.

**Why 100% coverage is demanded on four specific paths but 85% on the rest.** The four paths — atomic rewind, replay-cache hit, undo-window expiration, rate-limit enforcement — are the ones where a silent bug corrupts data or leaks cost. Elsewhere, 85% is sufficient and economic.

**Why alpha, beta, GA.** Internal + design-partner alpha catches obvious data-integrity bugs; opt-in beta catches scale and engagement issues with users who self-select into the feature; GA becomes available to everyone only after both.

**Why the kill switch persists 30 days post-GA.** Experience with novel state-altering features shows that most production incidents surface within four weeks of broad exposure. A kill switch lets the team suspend the feature without a redeploy during that window.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: GitHub — PR review, issue tracking, Actions CI/CD for all quality-gate automation.

### Third-Party Services
- **SVC-001**: `axe-core` — automated WCAG checks; required for GATE-QUALITY.
- **SVC-002**: Playwright — E2E execution; required for GATE-QUALITY.

### Infrastructure Dependencies
- **INF-001**: Feature-flag mechanism — boolean config read by backend and frontend; no specific platform required.
- **INF-002**: Staging environment representative of production schema, topology, and SSE stack; required for GATE-INTEGRATION.
- **INF-003**: Dashboards and alerting (metrics and logs) for rewind-specific KPIs; owned by the DevOps Engineer.

### Data Dependencies
- **DAT-001**: Architecture-spec requirement IDs — traceability source-of-truth; every deliverable cites at least one.
- **DAT-002**: Alpha telemetry — feeds policy decisions POL-001 and POL-004.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ — backend language (existing).
- **PLT-002**: React 19 + TypeScript 5.7 — frontend (existing).
- **PLT-003**: PostgreSQL 16 — migration target (existing).
- **PLT-004**: Redis 7 — cache invalidation target (existing).

### Compliance Dependencies
- **COM-001**: WCAG 2.2 Level AA — required for GATE-QUALITY.
- **COM-002**: Existing data-retention policies — archived-branch purge aligns with the platform's retention rules; no new compliance regime introduced.

## 9. Examples & Edge Cases

### Example 1 — Deliverable PR header template

```markdown
## Fulfills
- architecture-spec REQ-022 (anchored Undo Toast with live countdown)
- architecture-spec AC-015
- implementation-team-spec DOD-001 through DOD-006

## Summary
Adds `UndoToast.tsx` pinned to the top of the conversation panel, with aria-live polite
countdown, Undo and Dismiss buttons, and expired state.

## Test plan
- [x] Unit tests for timer, dismiss, expired transition
- [x] axe-core check: zero AA violations
- [x] Manual keyboard walkthrough captured in loom-xyz
```

### Example 2 — Escalation

```
Day 3 of Phase 2.
AI Engineer and Golang Backend Engineer both believe they own input-marking hash
implementation. Each has written a half of it.

Resolution (PAT-001 of this spec):
  Solution Architect posts a comment on the issue: "Hash function lives in cpn/hash.go
  and is owned by AI Engineer. Backend Engineer's adapter consumes it via the published
  Go signature. Any deviation is a blocker; raise a PR against spec-process if the
  boundary needs to move."
  AI Engineer continues; Backend Engineer refactors against the signature.
```

### EDGE-001 — A role owner is unavailable for ≥2 days

Backup owner assumes the role temporarily. All outstanding PRs under the role's mandate proceed with the backup's sign-off. Phase gates are not waived; if a gate slips due to the absence, the phase is re-scheduled rather than bypassed (CON-003).

### EDGE-002 — Alpha reveals a policy answer at odds with the spec's default

Example: POL-004 alpha data shows 60 seconds is too short; users want 5 minutes. AI Product Manager records the decision in a short ADR and requests a config change (not a code change) — the env-var mechanism exists exactly for this (architecture spec CON-006 plus this spec's DevOps deliverables).

### EDGE-003 — A P0 incident in beta

DevOps Engineer triggers the kill switch (flag forced OFF globally). Team halts beta. Post-mortem within 5 business days. Beta clock resets: the 10-business-day minimum restarts only after the fix and a re-enable.

### EDGE-004 — QA coverage below 85% at GATE-QUALITY

Gate fails. QA Engineer files a list of uncovered paths; the owning role writes the missing tests; the gate is re-evaluated. No coverage override is permitted (CON-003).

### EDGE-005 — A reviewer approves a PR that violates an invariant

The Solution Architect invokes a revert (or a follow-up fix PR) and updates the invariant checklist to add a line that would have caught the violation. This closes the loop; the invariant checklist is an evolving artifact.

## 10. Validation Criteria

1. Every deliverable in §4.1 has a cited architecture-spec ID in the PR that introduces it.
2. Every Quality Gate in §3.6 has passed before its successor phase begins.
3. The per-AC traceability matrix maps architecture-spec AC-001..AC-018 to existing test files; no entry is empty at GATE-QUALITY.
4. `axe-core` reports zero WCAG 2.2 AA violations on `RewindDialog` and `UndoToast`.
5. New Go code coverage ≥85%; new TypeScript code coverage ≥85%; 100% on the four critical paths.
6. Alpha and beta exit criteria in ROLL-002 and ROLL-003 are documented as met before the next phase starts.
7. POL-001 through POL-005 each resolve to an ADR merged before GATE-GA.
8. The kill-switch runbook exists and has been rehearsed on staging before GATE-ALPHA.
9. Every role has a named backup; the absence handling of EDGE-001 has been exercised at least once during the six weeks (even as a drill).
10. At no point during alpha or beta does the feature flag default to ON globally (ROLL-005).

## 11. Related Specifications / Further Reading

- [spec-architecture-conversation-rewind-fork-promote.md](spec-architecture-conversation-rewind-fork-promote.md) — the architecture spec this plan implements.
- [spec-design-multi-chat-conversation-forking.md](spec-design-multi-chat-conversation-forking.md) — sibling feature; reused primitives (`TurnPicker` extracted from `ForkDialog`).
- [spec-process-bugfix-a2ui-hitl-response-persistence.md](spec-process-bugfix-a2ui-hitl-response-persistence.md) — prerequisite for rewinding HITL answers.
- [spec-process-bugfix-a2ui-hitl-rehydration.md](spec-process-bugfix-a2ui-hitl-rehydration.md) — rehydration logic reused on branch switch.
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md) — transport for the new `branch_switched` event.
- [spec-design-cpn-execution-monitor.md](spec-design-cpn-execution-monitor.md) — consumer that must gain a branch filter.
- WCAG 2.2 Level AA — https://www.w3.org/TR/WCAG22/
- Conventional Commits — https://www.conventionalcommits.org/
