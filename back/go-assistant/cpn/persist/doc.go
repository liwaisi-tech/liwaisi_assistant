// Package persist defines the persistence contracts for the Agentic CPN engine.
//
// It provides 6 repository interfaces, 16 DTOs, a generic Page[T] type for
// cursor-based pagination, and 9 sentinel errors. All types and interfaces
// live in this sub-package to enforce Axiom A13: the cpn/ package never
// imports persistence drivers.
//
// In-memory implementations are provided as reference implementations and
// test doubles. They are thread-safe and require no external dependencies.
//
// Dependencies: only Go stdlib (context, encoding/json, errors, fmt, sort,
// strconv, sync, time) plus parent cpn/ package types for DTO conversion.
package persist
