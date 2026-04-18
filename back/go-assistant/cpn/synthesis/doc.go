// Package synthesis implements the GAP-4 CPN topology synthesiser.
//
// It holds the safe primitive catalogue (SafeRegistry), the topology
// linter, and the canonicalisation helper used to compute a stable
// SHA-256 over agent-authored topologies. The fire_synthesize /
// fire_instantiate handlers live in the parent cpn/ package because
// they are part of the executor dispatch table; this sub-package is
// strictly helper / pure-function territory so it can be imported by
// both the executor and the HTTP admin layer without cycles.
//
// Spec: spec/spec-architecture-cpn-synthesis-instantiate.md.
package synthesis
