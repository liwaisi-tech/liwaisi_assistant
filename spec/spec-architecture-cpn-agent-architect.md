# spec-architecture-cpn-agent-architect

Status: draft
Owner: liwaisi
Related: spec-architecture-brae-toolbox-taxonomy (in code), spec-architecture-http-sse-api, feat/toolbox-taxonomy-jit-builder

## 1. Purpose

Promote the existing "Biblioteca de flujos CPN" from a passive ranked log into an active **architect lane**: at JIT time, brae (a) determines whether an existing CPN in the library fits the user's request, (b) if yes, runs it, (c) if no, invokes a **CPN Agent Architect** transition that iterates with the user and composes a new CPN from the hashtag-indexed toolboxes. Newly synthesised, validated, and accepted CPNs are persisted back into the library with a structured signature so future lookups hit.

This spec is the bridge between the existing scaffolding (`cpn/tools/{toolbox,hashtag,registry}`, `cpn/synthesis/jit/composer.go`, `cpn/flow_library.go`, `cpn/awakens/toolforge.go`, `cpn/fire_register_tool.go`) and the goal stated by the user: "brae can create its own tools, saved in toolboxes with hashtags; the Architect reads tools and the request, iterates with the user based on their purpose and the tools, and proposes CPN solutions."

## 2. Non-goals

- **Self-learning / reinforcement.** Out of scope. The spec only ensures the traces, signatures and provenance needed *later* are captured now. No reward model, no policy update loop.
- **Replacing the tool-in-LLM shortcut.** Tracked separately (see §7 prerequisite).
- **Authoring new tool categories.** Toolbox taxonomy is already specified in-repo.

## 3. Actors (as CPN transitions)

| Name | Kind | Role |
|---|---|---|
| `t-architect-intake` | `NodeKindLLM` | Reads the user turn, emits a typed `ArchitectRequest` token: normalised intent, candidate hashtags, required capabilities, budget hint. |
| `t-lib-lookup` | `NodeKindTool` (deterministic) | Queries `flow_library` for signature-compatible CPNs; emits `LibraryHit` or `LibraryMiss`. |
| `t-retrieve-tools` | `NodeKindTool` (deterministic) | On miss, asks the tool retriever for a ranked `ToolMatchSet` using the request's hashtags. |
| `t-architect-design` | `NodeKindLLM` | The **Architect** proper: given request + ToolMatchSet + library near-misses, drafts a `TopologyDraft` (or proposes to extend an existing flow). Iterates with the user via HITL. |
| `t-toolforge` | `NodeKindLLM` | (Already exists — `cpn/awakens/toolforge.go`.) Invoked by the architect when no tool covers a capability: synthesises a new tool, registers it in a toolbox with hashtags. |
| `t-validate` | `NodeKindTool` | `fire_validate` — size caps, lint, adversarial lint (`synthesis/jit/lint_adversarial.go`). |
| `t-instantiate` | `NodeKindTool` | `fire_instantiate` — materialise subnet, register in engine, deposit activation token. |
| `t-lib-persist` | `NodeKindTool` | Store accepted CPN in `flow_library` with `FlowSignature` + provenance. |

These compose as an AND-/OR-joined sub-CPN (the paper's Fig. 7 pattern, extended with async completion tokens — see §7).

## 4. New / extended types

### 4.1 `ArchitectRequest`

```go
// cpn/architect/types.go (new package)
type ArchitectRequest struct {
    SessionID        string
    TurnID           string
    NL               string         // raw user text
    Intent           cpn.Intent     // normalised
    Hashtags         []string       // normalised via tools.NormalizeSet
    RequiredCaps     []string       // e.g. ["host.bash", "fs.read"]
    Budget           cpn.SizeCap
    ParallelismHint  string         // "fanout" | "sequence" | ""
    HITLPreference   HITLMode       // auto | always | never (per-user)
}
```

### 4.2 `FlowSignature`

Added to `cpn/flow_library.go`. Persisted alongside the flow blob; indexed for lookup.

```go
type FlowSignature struct {
    InputColors       []string  // sorted set of colours the root CPN consumes
    OutputColors      []string  // sorted set it emits
    RequiredTools     []string  // qualified tool names referenced by transitions
    RequiredCaps      []string  // host capabilities the flow transitively needs
    Hashtags          []string  // union of referenced tools' hashtags
    Template          string    // TemplateID if composed by jit, else "custom"
    Digest            string    // sha256(canonical(signature)) — stable key
}
```

**Lookup is deterministic** (no ML): a library entry *fits* the request iff
`request.Hashtags ⊆ sig.Hashtags` **and** `request.RequiredCaps ⊆ sig.RequiredCaps` **and** compatible input colours. Ranked ties break on `flow_ranking.Score(flowID)`.

### 4.3 `ArchitectDraft`

The Architect's output token, distinct from `TopologyDraft` because it carries *reasoning and gaps*:

```go
type ArchitectDraft struct {
    Strategy     string              // "reuse" | "extend" | "compose" | "toolforge-then-compose"
    BaseFlowID   string              // set when Strategy == "reuse" | "extend"
    MatchSet     cpn.ToolMatchSet    // tools the Architect plans to use
    MissingCaps  []string            // capabilities with no tool → triggers t-toolforge
    Topology     cpn.TopologyDraft   // empty when Strategy == "reuse"
    UserPrompt   string              // question for HITL iteration (may be empty)
    Confidence   float64             // 0..1
}
```

## 5. Firing flow (root CPN fragment)

```
user-turn ─► [t-architect-intake] ─► req ─┬─► [t-lib-lookup] ─► hit ─► [t-instantiate-existing] ─► run
                                          │                   └─► miss ─► [t-retrieve-tools] ─► matches ─┐
                                          │                                                              │
                                          │                              ┌───────────────────────────────┘
                                          │                              ▼
                                          │                    [t-architect-design] ─► draft
                                          │                              │
                                          │    ┌──────── missing tool ◄──┤
                                          │    ▼                         │
                                          │  [t-toolforge] ─► new tool ──┘ (loop back with updated matches)
                                          │                              │
                                          │                              ▼
                                          │                   [t-hitl-approve] (Fig. 7 AND-join: draft + user ack)
                                          │                              │
                                          │                              ▼
                                          │                     [t-validate] ─► [t-instantiate] ─► run
                                          │                              │
                                          │                              ▼
                                          │                      [t-lib-persist]
                                          └─────────────────────────────────► (provenance token to execution_records)
```

## 6. Iteration loop with user (HITL semantics)

The Architect iterates with the user through bounded HITL cycles:

- **Ask** — if `UserPrompt != ""`, emit `EventHITLRequested` with a custom A2UI surface listing the proposed flow (tool names + hashtags + expected token flow). User replies: approve, reject, or refine (free text).
- **Refine** — refinement text lands as a new input token to `t-architect-design`, which re-drafts with the amended intent. Same Match Set unless hashtags changed.
- **Budget** — max `N=3` refinement rounds by default (configurable per session). On exhaustion, `t-architect-design` emits a failure token explaining which constraint it could not satisfy; the library is not written.

HITL is AND-join on (Draft, UserApproval). This is the paper's Fig. 7 pattern applied verbatim.

## 7. Prerequisite: async transition lifecycle (depends-on)

Today tools execute inline in `fire_llm.go`'s tool-call loop. The Architect would synthesise topologies that reference tool *transitions* the runtime does not actually fire as transitions — the library entries would be lies.

Minimum fix before this spec can ship end-to-end:

1. (done) Emit `EventTransitionCompleted` for every tool invocation so the UI/event log closes the lifecycle.
2. (tracked separately) Route tool execution through the executor so each tool *is* a transition with its own start/await/complete lifecycle and its result token feeds back into the LLM via an AND-join place.

Until (2) lands, the Architect can still operate in "reuse-only + toolforge" mode (it can propose library hits and create new tools, but cannot compose new multi-step topologies whose fidelity depends on real tool transitions).

## 8. Persistence additions

- `flow_library` (migration): add `signature jsonb`, `signature_digest text`, `hashtags text[]` with GIN index on `hashtags` and btree on `signature_digest`.
- `execution_records` (migration): add `flow_id text`, `flow_strategy text` (reuse | extend | compose | toolforge-then-compose). Needed for provenance.
- `cpn_mutations` already records synthesis events — extend payload with `flow_id` + `signature_digest` when the architect persists.

## 9. Events (additions to `cpn/event.go`)

| Type | When |
|---|---|
| `architect_request` | After `t-architect-intake` completes. |
| `library_hit` / `library_miss` | After `t-lib-lookup`. Payload: `FlowSignature` + optional `BaseFlowID`. |
| `architect_draft` | After each pass of `t-architect-design`. Payload: `ArchitectDraft`. |
| `architect_iteration` | HITL refinement round. |
| `flow_persisted` | After `t-lib-persist`. |

Frontend renders these on the Flows page (the panel in the screenshot) so the library ceases to be a flat list: each flow shows its signature, hashtags, and last strategy/provenance.

## 10. Ports (hexagonal) — new interfaces

```go
// cpn/architect/ports.go
type ToolRetriever interface {
    Match(ctx context.Context, req ArchitectRequest) (cpn.ToolMatchSet, error)
}

type FlowLookup interface {
    FindBySignature(ctx context.Context, sig FlowSignature) ([]FlowCandidate, error)
}

type FlowPersister interface {
    Persist(ctx context.Context, entry FlowLibraryEntry) error
}
```

`ToolRetriever` is the missing producer flagged in `cpn/tool_match_set.go` — implement first (deterministic hashtag-∩-score is enough for v1; swap for BM25/hybrid later).

## 11. Minimum viable slice

In order, each slice shippable behind a feature flag:

1. **FlowSignature + library lookup** — deterministic fit, no Architect yet. Replace today's flat `flow_library` view with "hits for this request."
2. **ToolRetriever (hashtag-∩ + score)** — closes the TODO in `tool_match_set.go`. Reuses existing `tools/registry.go` taxonomy.
3. **`t-architect-design` transition (LLM)** — one-shot (no HITL iteration) emitting `ArchitectDraft`. Uses the existing `synthesis/jit/composer` for the actual topology emission.
4. **Persist back to library with signature + provenance.**
5. **HITL iteration loop** — Fig. 7 AND-join on draft + approval; up to N refinement rounds.
6. **`t-toolforge` integration** — on `MissingCaps != nil`, spawn toolforge, loop back.

Slices 1-2 require no LLM changes and unblock the UX improvement on the Flows page immediately.

## 12. Open questions

- Signature equivalence: should `InputColors` compatibility be exact or subtype? (Defer to tool-result-token typing.)
- Versioning: when a referenced tool's version changes, library entries should probably be marked stale rather than deleted. Needs a stale-detection job.
- Multi-user libraries: flows are currently per-session. Should accepted architect outputs be scoped per-user (not per-session) by default? Likely yes; makes reuse real.

## 13. Acceptance (v1 = slices 1-4)

- Given a request whose hashtags and required caps are covered by an existing library flow, brae reuses it (no LLM architect call) and the execution record carries `flow_strategy="reuse"` + `flow_id`.
- Given a request that misses the library, the Architect composes a topology using retriever matches, persists it, and the next equivalent request hits.
- `EventTransitionCompleted` is emitted for every tool invocation (done — Layer 1 fix landed).
- No regressions on existing `cpn/synthesis/jit` tests.
