// Package toolbuilder hosts the "tool-atelier" CPN — brae's self-authoring
// pipeline for hexagonal Go micro-backend tools.
//
// Spec: spec/spec-architecture-tool-atelier-cpn.md
//
// Distinct from the single-binary "tool-forge" (GAP-5, cmd/server/topologies_tool_forge.go):
// the atelier produces a full Go project in workspace/tools/src/<name>/ with
// git history, Makefile, hexagonal layout, TDD-driven implementation, and a
// coverage gate ≥85% before registration.
//
// Subpackages:
//   - scaffold: embedded project templates + renderer
//
// Top-level entry point: BuildToolAtelierTopology.
package toolbuilder
