---
title: Hot Topology Reconfiguration — NodeKindTopologyMutate for In-Flight Growth
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, cpn, dynamic, reconfiguration, brae, gap-7]
---

# Introduction

GAP-4 (CPN synthesizer) lets an agent author and spawn *new* sub-CPNs. In most situations this is sufficient — the paper §8 notes that "high-level reconfigurable networks" are the open problem for dynamic MAS composition, but in practice sub-CPN spawning covers nearly every use case. This spec defines a narrow, opt-in capability for the remaining cases: letting a running CPN add places or transitions to itself without first tearing down and re-spawning.

This is the lowest-priority GAP. Implement only after GAP-4 has been in production and demonstrable cases emerge where sub-CPN spawning is too heavy.

## 1. Purpose & Scope

**Purpose.** Enable a specific, bounded class of in-flight topology mutations (add-place, add-transition, deprecate-transition) with re-validation on each mutation, for scenarios where sub-CPN spawning is impractical.

**In scope.**
- `NodeKindTopologyMutate`: a transition whose effect is to call `CPN.Mutate(mutation)` on its own parent.
- `Mutation` union type: `AddPlace`, `AddTransition`, `MarkTransitionDeprecated`. No removal in v1 — removal is too dangerous while tokens are in flight.
- Incremental validation: when a mutation is applied, `Validate()` runs on the *full* topology and rolls back on failure.
- An explicit `CPN.MutableAfterStart` flag (default `false`); a CPN must opt in to hot reconfiguration at construction time.
- HITL approval: every mutation MUST pass through a `topology.mutation` HITL prompt by default; an override flag `TrustMutations` can skip it.
- Audit: every accepted and rejected mutation is recorded in a new `cpn_mutations` Postgres table.
- Tests: add-place mid-run, add-transition with valid arc wiring, mutation rejection when it would violate space isolation.

**Out of scope.**
- Remove-place / remove-transition — deferred. Too easy to orphan in-flight tokens.
- Mutations that cross into another running CPN.
- Mutations of the root session CPN.

**Audience.** `golang-pro`. Low priority.

## 2. Definitions

- **Mutation**: A single structural change to a CPN after it has started running.
- **Opt-in**: `CPN.MutableAfterStart=true` at construction; default is immutable (today's behaviour).
- **Quiescence**: The executor pauses the firing loop while a mutation is applied to prevent racing with in-flight `Fire` goroutines.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: `NodeKindTopologyMutate = "topology_mutate"` MUST be added.
- **REQ-002**: `Mutation` MUST be a union: `{Kind: "add_place" | "add_transition" | "deprecate_transition", Place?: Place, Transition?: Transition, TransitionID?: string}`.
- **REQ-003**: A CPN's executor MUST expose `cpn.Mutate(ctx, m Mutation) error` that: (a) acquires the executor pause token; (b) quiesces — waits for all in-flight fires to complete; (c) applies the mutation; (d) runs full `Validate()`; (e) on failure rolls back and returns error; (f) emits `EventTopologyMutated` / `EventTopologyMutationRejected`; (g) releases pause token.
- **REQ-004**: `CPN.MutableAfterStart` MUST default to `false`. A `NodeKindTopologyMutate` transition firing on an immutable CPN → `ErrMutationNotPermitted`.
- **REQ-005**: Every mutation MUST be persisted to `cpn_mutations (id, cpn_id, session_id, kind, payload JSONB, applied_at, accepted BOOLEAN, reason TEXT, mutation_token_id TEXT)`.
- **REQ-006**: Every mutation MUST go through HITL unless `TrustMutations=true` (dev-mode only; warns on boot).
- **CON-001**: `add_place`: the place color/space must match one of the allowed color sets; must not collide with existing place IDs.
- **CON-002**: `add_transition`: all referenced places must exist; must not violate space isolation.
- **CON-003**: `deprecate_transition`: marks a transition as "no longer firable" but does not remove it; existing input tokens drain via fall-through.
- **GUD-001**: Keep mutations atomic — one mutation per `NodeKindTopologyMutate` fire.
- **PAT-001**: Use the same `CPNTopology` JSON shape (GAP-4) for mutation payloads, so the audit log is readable.

## 4. Interfaces & Data Contracts

```go
type MutationKind string
const (
    MutationAddPlace              MutationKind = "add_place"
    MutationAddTransition         MutationKind = "add_transition"
    MutationDeprecateTransition   MutationKind = "deprecate_transition"
)

type Mutation struct {
    Kind         MutationKind
    Place        *Place
    Transition   *Transition
    TransitionID string
}

// On CPN:
func (c *CPN) Mutate(ctx context.Context, m Mutation) error
```

Postgres:
```sql
CREATE TABLE cpn_mutations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cpn_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  payload JSONB NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  accepted BOOLEAN NOT NULL,
  reason TEXT,
  mutation_token_id TEXT
);
```

## 5. Acceptance Criteria

- **AC-001**: Given a CPN with `MutableAfterStart=true`, when a `NodeKindTopologyMutate` fires adding a new place, then the place is present and the CPN continues firing.
- **AC-002**: Given an `add_transition` that would violate space isolation, when applied, then the mutation is rolled back and an error-place token is emitted.
- **AC-003**: Given a CPN with `MutableAfterStart=false`, when a mutation fires, then `ErrMutationNotPermitted` is raised.
- **AC-004**: Given 100 concurrent fires in flight, when a mutation is requested, then the executor quiesces before applying the mutation (no torn state).
- **AC-005**: Every accepted mutation has a matching row in `cpn_mutations` within 100 ms.

## 6. Test Automation Strategy

- Unit: each mutation kind.
- Race: 1000-goroutine stress with random mutation + fire interleaving; `goleak` at end.
- Integration: full CPN with a mutation transition and a post-mutation path.
- Coverage ≥ 85%.

## 7. Rationale & Context

**Why so restricted?** Because arbitrary mutation of a live state machine is a rich source of bugs. By limiting to add-only + deprecate, and by quiescing, we get most of the value (growing the net) without most of the risk.

**Why HITL?** A running net mutating itself surprises users. An approval step is a cheap trust anchor.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-4 (shares topology JSON shape).
- **EXT-002**: GAP-6 (HITL).
- **INF-001**: Postgres.

## 9. Examples & Edge Cases

### Add a logging observer mid-run

A long-running sub-CPN realises it needs to observe its children's progress. It fires `NodeKindTopologyMutate(add_place: p-log, add_transition: t-log-observer)`.

### Edge — mutation requested from a child CPN

Forbidden in v1. Only the CPN itself can mutate itself; child CPNs must request their parent mutate.

### Edge — deprecated transition still consumes tokens

Tokens in its input places drain naturally via fall-through; they do not re-enable the transition.

## 10. Validation Criteria

- No mutation leaves the CPN in an invalid state (validator runs post-apply).
- The audit table contains every attempted mutation, accepted or rejected.

## 11. Related Specifications / Further Reading

- [spec-architecture-cpn-synthesis-instantiate.md](./spec-architecture-cpn-synthesis-instantiate.md) — GAP-4, preferred approach for most cases.
- Paper §8 (future work on high-level reconfigurable nets).
