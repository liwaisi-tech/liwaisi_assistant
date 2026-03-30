// Package cpn implements a Coloured Petri Net engine for agentic AI orchestration.
//
// The CPN is the single substrate for tools, agents, coordination, and
// human-in-the-loop interaction. Every tool, agent, director, and
// orchestrator is an instance of the same CPN type whose identity
// emerges from topology (depth) and role label.
//
// # Architecture
//
// This package is the domain core (Hexagonal Architecture). It defines:
//   - Core types: CPN, Place, Transition, Token, Event
//   - Execution engine: Run(), fire functions, retry, memory
//   - Port interfaces: LLMClient, ChannelAdapter, GroupNotifier, CostProvider
//
// Infrastructure adapters live in infra/ packages:
//   - infra/openrouter: OpenRouterClient (LLMClient), StreamHandler, TokenLedger
//   - infra/guardrails: GuardrailsClient (spending limits)
//   - infra/activity: ActivityClient (billing reconciliation)
//
// The domain layer NEVER imports infrastructure. Adapters implement
// port interfaces defined here via dependency inversion.
//
// See: .docs/specs/agentic-cpn-v1.2.md
package cpn
