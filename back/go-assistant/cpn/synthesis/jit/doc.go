// Package jit implements the JIT CPN Builder (spec-architecture-brae-jit-
// cpn-builder.md). It composes a persist.CPNTopology JSON blob from a
// ToolMatchSet + Intent pair, using one of two deterministic templates:
// parallel-fanout or sequential-pipeline. The composer:
//
//   - Re-resolves every matched tool through a caller-supplied ToolResolver
//     (SEC-002) before emission.
//   - Emits canonical JSON matching persist.CPNTopology shape without
//     importing persist (Axiom A13).
//   - Gates the emission through the existing synthesis.Lint plus a
//     JIT-specific adversarial lint (self-loop / dead-end / fanout>8).
//   - Caches emitted blobs per-session under an LRU capped at 64 entries
//     keyed on (intentDigest, matchSetDigest, personalityDigest, template).
//
// Two SafeRegistry primitives — jit-fanout and jit-aggregate — are
// contributed via Register so emitted topologies lint cleanly.
//
// # Deferred (tracked in the spec, not implemented here)
//
//   - REQ-008 / REQ-009 / REQ-010 — SubNet spawn + inheritance of session,
//     trace, personality. Requires NodeKindInstantiate runtime wiring that
//     lands with the tool-request node chunk.
//   - REQ-013 — per-tool timeout (min(30s/N, 5s)). Runtime concern; arc
//     inscriptions stay static today.
//   - REQ-014 — SubNet egress / error-place contract. Lives on the parent
//     CPN; composer cannot populate it in isolation.
//   - BEH-002 — whole-SubNet 30s timeout + jit.subnet.timeout event.
//     Same runtime dependency as REQ-013.
//   - BEH-003 — jit.topology.preview event. Depends on the frontend preview
//     surface contract, out of scope for the composer slice.
//   - SEC-003 — tool_compose policy gate for agent-authored tools.
//     Requires the policy gate component not yet implemented.
//   - CON-005 — ErrOrphanedSubNet. Raised at instantiate time, not compose.
//
// These gaps are filled by the retriever and tool-request-node chunks.
package jit
