// Package awakens implements the `brae-awakens` CPN topology — the first
// agentic turn of every new session. It drives the LLM through read-only
// shell introspection, validates a structured AwakeningReport, persists it
// as a host_capability_snapshots row, registers discovered tools, and emits
// the first assistant message to the session.
//
// Spec: spec/spec-architecture-brae-awakening-self-discovery.md
//
// The package is deliberately self-contained: report/validator, cache,
// fallback glue, awakening prompt, topology factory, and classifier helper
// for the Host-gate `introspection` class. The legacy deterministic path in
// cmd/server/topologies_host_discovery.go stays intact and is invoked via
// Fallback when the LLM is unreachable (REQ-010, CON-006).
package awakens
