// Package architect implements the CPN Agent Architect lane defined in
// spec-architecture-cpn-agent-architect.md.
//
// The package is intentionally dependency-light. It composes over:
//
//   - cpn.FlowLibrary (signature-aware lookup, spec §4.2, §11 slice 1)
//   - cpn/tools.Registry (hashtag-indexed tool taxonomy, spec §11 slice 2)
//   - cpn/synthesis/jit.Compose (deterministic template composition, spec §11 slice 3)
//
// Nothing in this package calls an LLM directly. The LLM-backed
// `t-architect-design` transition will be added in a later slice as a thin
// wrapper that delegates back into the orchestrator defined here.
package architect
