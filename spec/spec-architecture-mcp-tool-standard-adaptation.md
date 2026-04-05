---
title: "MCP Tool Standard Adaptation — CPN-Theoretic Alignment"
version: 1.0
date_created: 2026-04-05
owner: liwaisi-tech
tags: [architecture, tools, mcp, cpn, industry-standard, tool-calling]
---

# Introduction

This specification defines the changes required to align the Liwaisi OS Tool Engine with the Model Context Protocol (MCP) industry standard — the dominant tool integration protocol as of 2026 (97M monthly SDK downloads, adopted by Anthropic, OpenAI, Google, Microsoft, AWS under the Linux Foundation's Agentic AI Foundation).

A formal CPN-theoretic analysis and a comparative study of the Claude Code/Claw Code Rust architecture identified **5 critical-to-medium gaps** in the current implementation. This spec defines the exact fixes.

## 1. Purpose & Scope

**Purpose**: Close the gap between the current tool engine and MCP/industry standard, ensuring that (a) LLMs receive proper `inputSchema` for every tool, (b) HITL-gated tools are enforced at execution time, (c) tool execution emits observable events, and (d) schema validation acts as a CPN guard function.

**Scope**: Backend Go changes only. Files affected:
- `cpn/fire_llm.go` — `buildToolSchema()`, `handleToolCalls()`
- `cpn/transition.go` — `Transition` struct (add `ToolParameters` field)
- `cpn/tools/registry.go` — `InjectIntoCPN()` method
- `cpn/tools/personality_tools.go` — add `inputSchema` to all tool schemas
- `cpn/executor.go` — no changes (fireTool is for direct CPN tool transitions, not LLM tool-call loop)

**Audience**: Backend Go developers implementing the MCP adaptation.

**Assumptions**:
- The MCP tool schema format is: `{name: string, description: string, inputSchema: {type: "object", properties: {...}, required: [...]}}`
- The existing `LLMTool` struct (`Name`, `Description`, `Parameters json.RawMessage`) already matches the MCP format — `Parameters` IS the `inputSchema`
- The fix is about wiring existing data through, not adding new structures

## 2. Definitions

| Term | Definition |
|------|-----------|
| **MCP** | Model Context Protocol — the industry-standard open protocol for AI agent tool integration, created by Anthropic, donated to the Linux Foundation AAIF |
| **inputSchema** | MCP term for a tool's parameter definition, expressed as a JSON Schema object with `type: "object"` |
| **buildToolSchema()** | Function in `fire_llm.go:378-383` that converts a Transition to an LLMTool for LLM consumption |
| **handleToolCalls()** | Function in `fire_llm.go:241-356` implementing the agentic tool-call loop |
| **ToolSchema.Parameters** | `json.RawMessage` field on `ToolSchema` in `cpn/tools/registry.go` that holds the JSON Schema — currently declared but never propagated to the LLM |
| **Centaurian pattern** | From arXiv:2502.14000v1 — a transition that requires tokens from both human and computational origins to fire |
| **S-invariant** | A CPN place invariant — a weighted sum over place markings that remains constant across all reachable markings |

## 3. Requirements, Constraints & Guidelines

### CRITICAL — Wire inputSchema to LLM

- **REQ-001**: `Transition` struct MUST gain a `ToolParameters json.RawMessage` field to carry the tool's JSON Schema. This field is populated at topology build time from the ToolRegistry
- **REQ-002**: `buildToolSchema()` in `fire_llm.go` MUST use `Transition.ToolParameters` to populate `LLMTool.Parameters`. If `ToolParameters` is nil, omit the field (backward-compatible)
- **REQ-003**: A new method `Registry.InjectIntoCPN(c *CPN)` MUST iterate all transitions with `Kind == NodeKindTool` and `ToolName != ""`, resolve the tool from the registry, and populate `Transition.ToolParameters` with `ToolSchema.Parameters`
- **REQ-004**: `InjectIntoCPN` MUST also populate `Transition.Executor` from the registry if the transition's Executor is nil. This unifies the two lookup paths
- **REQ-005**: `buildToolSchema()` MUST set `Description` from `ToolSchema.Description` (not the generic "Execute tool: X" placeholder)

### HIGH — Enforce RequiresHITL in tool-call loop

- **REQ-010**: In `handleToolCalls()`, before executing a tool call, the system MUST check if the tool has `RequiresHITL == true` by looking up the ToolSchema from a registry reference
- **REQ-011**: If `RequiresHITL == true`, the system MUST emit `EventHITLRequested` with the tool name, description, and proposed arguments as payload, then block on the CPN's HITL channel until human response
- **REQ-012**: If the human rejects (action = "reject"), the tool call MUST NOT execute. An error result is sent back to the LLM: `"Tool execution rejected by user"`
- **REQ-013**: If the human approves (action = "approve"), the tool executes normally
- **REQ-014**: The CPN struct MUST gain a `ToolRegistry *tools.Registry` field (set during initialization) so that `handleToolCalls` can look up tool metadata

### HIGH — Add per-tool-call event emission

- **REQ-020**: In `handleToolCalls()`, BEFORE each tool execution, the system MUST emit an event with type `EventTransitionStarted` containing the tool name, tool call ID, and input arguments as `TransitionStartedPayload`
- **REQ-021**: AFTER each tool execution, the system MUST emit `EventToolExecuted` with `ToolExecutedPayload{ToolName, Namespace, DurationMs, Success, Error}`
- **REQ-022**: These events MUST use the CPN's `emit()` helper (via `EventSink`) to reach the SSE broker and frontend ExecutionMonitor

### MEDIUM — Unify schema paths

- **REQ-030**: `buildToolSchema()` MUST be the ONLY path for converting tools to LLMTool format. Remove the duplicate schema building in the `handleToolCalls` loop (line 249-257) and reuse the schemas from the initial build (line 73-81)
- **REQ-031**: The `AsLLMTools()` method on Registry remains for external API exposure but MUST produce identical output to what `buildToolSchema()` produces internally

### MEDIUM — Personality tools with proper inputSchema

- **REQ-040**: All personality tool registrations in `personality_tools.go` MUST include a `Parameters` field with a valid JSON Schema. The schema MUST match what the executor actually parses
- **REQ-041**: `personality.get_identity` Parameters:
  ```json
  {"type":"object","properties":{"user_id":{"type":"string","description":"User ID to load personality for"}},"required":[]}
  ```
- **REQ-042**: `personality.set_principle` Parameters:
  ```json
  {"type":"object","properties":{"user_id":{"type":"string"},"kind":{"type":"string","enum":["nucleo","conducta","etica"]},"title":{"type":"string"},"description":{"type":"string"},"rules":{"type":"array","items":{"type":"string"}}},"required":["user_id","kind"]}
  ```
- **REQ-043**: `personality.reset` Parameters:
  ```json
  {"type":"object","properties":{"user_id":{"type":"string"}},"required":["user_id"]}
  ```
- **REQ-044**: `personality.get_tensions` Parameters:
  ```json
  {"type":"object","properties":{"user_id":{"type":"string"}},"required":[]}
  ```
- **REQ-045**: `personality.set_hierarchy` Parameters:
  ```json
  {"type":"object","properties":{"user_id":{"type":"string"},"hierarchy":{"type":"array","items":{"type":"string","enum":["nucleo","conducta","etica"]},"minItems":3,"maxItems":3}},"required":["user_id","hierarchy"]}
  ```

### Security

- **SEC-001**: Tool allowlist enforcement in `handleToolCalls` (line 279-281) MUST remain — tools not in `LLMTools` list are rejected regardless of registry state
- **SEC-002**: HITL-gated tool execution MUST NOT proceed without explicit human approval. Timeout on HITL channel follows existing `fireHITL` timeout pattern

### Constraints

- **CON-001**: Changes MUST NOT break existing non-tool LLM transitions. If `ToolParameters` is nil, `buildToolSchema` behaves as before
- **CON-002**: The tool-call loop iteration limit (`MaxToolCallIterations = 10`) MUST NOT change
- **CON-003**: No new Go dependencies. JSON Schema validation uses `encoding/json` only (structural validation, not full JSON Schema draft validation)
- **CON-004**: CPN formal properties MUST be preserved: 1-boundedness of the tool-call subnet, S-invariant conservation

### Guidelines

- **GUD-001**: The `ToolParameters` field on Transition follows the same pattern as `SystemPrompt` — static data populated at topology build time, not at fire time
- **GUD-002**: Event emission in `handleToolCalls` should use the same `c.emit()` pattern used elsewhere in the CPN engine
- **GUD-003**: HITL-gating in the tool-call loop should be as non-invasive as possible — a conditional block before `toolTransition.Executor(ctx, toolInput)`, not a refactoring of the entire loop

### Patterns

- **PAT-001**: `InjectIntoCPN` follows the same pattern as `injectPersonality` in `session_service.go` — post-construction wiring before `CPN.Run()`
- **PAT-002**: HITL-gating follows the Centaurian transition pattern from arXiv:2502.14000v1 Fig. 7 — dual-token synchronization requiring both computational and human tokens
- **PAT-003**: Schema on Transition follows the principle: "CPN structural types (ColorSet) for O(1) deposit validation, JSON Schema for semantic guard refinement"

## 4. Interfaces & Data Contracts

### 4.1 Modified Transition struct

```go
// cpn/transition.go — add field after ToolName

// ToolParameters is the JSON Schema for this tool's input parameters.
// Populated at topology build time by ToolRegistry.InjectIntoCPN().
// Used by buildToolSchema() to provide MCP-compatible inputSchema to LLMs.
// Nil for non-tool transitions or tools without declared schemas.
ToolParameters json.RawMessage
```

### 4.2 Modified CPN struct

```go
// cpn/cpn.go — add field

// ToolRegistry provides tool metadata for the LLM tool-call loop.
// Set during initialization by SessionService. Nil if no tools registered.
ToolRegistry interface {
    Resolve(qualifiedName string) (schema interface{ RequiresHITL() bool }, ok bool)
}
```

Actually, to avoid circular dependencies, use a simpler approach:

```go
// ToolMeta holds tool metadata needed during execution.
// Populated by Registry.InjectIntoCPN() at build time.
type ToolMeta struct {
    Description  string
    Parameters   json.RawMessage
    RequiresHITL bool
    Namespace    string
}

// Add to Transition struct:
ToolMeta *ToolMeta // Populated by InjectIntoCPN, nil for non-registry tools
```

### 4.3 New method on Registry

```go
// cpn/tools/registry.go

// InjectIntoCPN populates Transition.ToolMeta and Transition.Executor
// for all tool transitions that reference registered tools.
func (r *Registry) InjectIntoCPN(c *cpn.CPN) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    for _, t := range c.Transitions {
        if t.ToolName == "" {
            continue
        }
        entry, ok := r.entries[t.ToolName]
        if !ok {
            continue
        }
        t.ToolMeta = &cpn.ToolMeta{
            Description:  entry.Schema.Description,
            Parameters:   entry.Schema.Parameters,
            RequiresHITL: entry.Schema.RequiresHITL,
            Namespace:    entry.Schema.Namespace,
        }
        if t.Executor == nil {
            t.Executor = entry.Executor
        }
    }
}
```

### 4.4 Updated buildToolSchema

```go
// cpn/fire_llm.go

func buildToolSchema(tool *Transition) LLMTool {
    lt := LLMTool{
        Name: tool.ToolName,
    }
    if tool.ToolMeta != nil {
        lt.Description = tool.ToolMeta.Description
        lt.Parameters = tool.ToolMeta.Parameters
    }
    if lt.Description == "" {
        lt.Description = fmt.Sprintf("Execute tool: %s", tool.ToolName)
    }
    return lt
}
```

### 4.5 HITL-gating in handleToolCalls

```go
// Inside handleToolCalls, before tool execution (line 294-309):

// Check HITL gate
if toolTransition.ToolMeta != nil && toolTransition.ToolMeta.RequiresHITL {
    // Emit HITL request
    c.emit(&Event{
        Type:           EventHITLRequested,
        TransitionID:   t.ID,
        TransitionKind: NodeKindTool,
        Payload: map[string]any{
            "tool_name":  tc.ToolName,
            "arguments":  string(tc.Arguments),
            "tool_call_id": tc.ID,
        },
    })
    
    // Block on HITL channel
    if t.HITLConfig != nil && t.HITLConfig.Channel != nil {
        select {
        case <-ctx.Done():
            return "", loopCost, ctx.Err()
        case response := <-t.HITLConfig.Channel:
            if payload, ok := response.Payload.(string); ok && strings.Contains(strings.ToLower(payload), "reject") {
                messages = append(messages, &LLMMessage{
                    Role:       "tool",
                    ToolResult: &LLMToolResult{ToolCallID: tc.ID, Content: "Tool execution rejected by user"},
                })
                continue // Skip this tool, let LLM handle rejection
            }
        }
    }
}
```

### 4.6 MCP-compatible LLMTool output format

```json
{
  "name": "system/personality.set_principle",
  "description": "Modify one of the agent's three principles (nucleo, conducta, etica). Requires human approval.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "user_id": { "type": "string" },
      "kind": { "type": "string", "enum": ["nucleo", "conducta", "etica"] },
      "title": { "type": "string" },
      "description": { "type": "string" },
      "rules": { "type": "array", "items": { "type": "string" } }
    },
    "required": ["user_id", "kind"]
  }
}
```

Note: In the MCP standard, `inputSchema` is the field name. In the `LLMTool` Go struct, the field is `Parameters json.RawMessage` which serializes to the correct MCP format via the OpenRouter/Anthropic adapters.

## 5. Acceptance Criteria

- **AC-001**: Given a CPN with tool transitions and a ToolRegistry, When `InjectIntoCPN` is called, Then all matching transitions have `ToolMeta` populated with Description, Parameters, RequiresHITL, and Namespace
- **AC-002**: Given a tool transition with `ToolMeta.Parameters` set, When `buildToolSchema()` is called, Then the returned `LLMTool` has `Parameters` matching the ToolSchema's JSON Schema
- **AC-003**: Given an LLM transition with tools, When the LLM is called, Then the tool definitions include `inputSchema` (Parameters) for each registered tool
- **AC-004**: Given a tool with `RequiresHITL=true` called in the tool-call loop, When the tool call is processed, Then an `EventHITLRequested` is emitted and execution blocks until human response
- **AC-005**: Given a human rejection of an HITL-gated tool, When the response is "reject", Then the tool does NOT execute and the LLM receives "Tool execution rejected by user"
- **AC-006**: Given any tool execution in `handleToolCalls`, When the tool fires, Then `EventToolExecuted` is emitted with timing, success, and error information
- **AC-007**: Given all personality tools, When `AsLLMTools()` is called on the Registry, Then each tool has a non-empty `Parameters` field matching the MCP `inputSchema` format
- **AC-008**: Given existing non-tool LLM transitions (no ToolName), When `InjectIntoCPN` is called, Then these transitions are NOT modified (backward compatibility)

## 6. Test Automation Strategy

- **Test Levels**: Unit tests for all modified functions
- **Frameworks**: Go `testing` + `testify`
- **Tests required**:
  - `TestBuildToolSchema_WithToolMeta` — Parameters and Description populated
  - `TestBuildToolSchema_WithoutToolMeta` — backward-compatible fallback
  - `TestInjectIntoCPN_PopulatesToolMeta` — registry data flows to transitions
  - `TestInjectIntoCPN_SkipsNonToolTransitions` — LLM/HITL transitions untouched
  - `TestInjectIntoCPN_PopulatesNilExecutor` — executor filled from registry
  - `TestHandleToolCalls_EmitsToolExecuted` — events emitted per tool call
  - `TestHandleToolCalls_HITLGating_Approve` — tool executes after approval
  - `TestHandleToolCalls_HITLGating_Reject` — tool skipped, error sent to LLM
  - `TestPersonalityTools_HaveParameters` — all 5 tools have valid JSON Schema
- **Coverage**: 80% on modified functions
- **CI/CD**: `go test ./cpn/... ./cpn/tools/... -race`

## 7. Rationale & Context

### Why this matters

As of 2026, MCP is the universal standard for AI agent tool integration (97M monthly installs). Every major provider — Anthropic, OpenAI, Google — expects tools in MCP format with `inputSchema`. Without proper `inputSchema`, LLMs must guess at tool parameters, leading to:
- Higher error rates (LLM sends wrong argument types)
- More tool-call loop iterations (wastes tokens and cost)
- Incompatibility with MCP-native clients and tool registries

### CPN-theoretic justification

The CPN math analysis (Section 3 of our research) established:
1. **Two-layer type system**: CPN ColorSet for structural typing (O(1) deposit validation) + JSON Schema for semantic guard refinement. These are not redundant — they operate at different levels of the type hierarchy
2. **Schema validation preserves boundedness**: Adding a schema guard to a transition can only reduce fireable markings, never increase them. `Bounded(CPN) implies Bounded(CPN + G_schema)`
3. **HITL-gated tools are Centaurian transitions**: They require tokens from both human and computational spaces, implementing the paper's dual-paradigm architecture

### Industry comparison

| Feature | Claude Code/Claw Code | Our current | After this spec |
|---|---|---|---|
| Tool schemas sent to LLM | Full JSON Schema | Name only | Full JSON Schema |
| HITL enforcement | Permission model (DENY/ALLOW/ASK) | Flag only, not enforced | Enforced in tool-call loop |
| Per-tool-call events | Full observability | Invisible | EventToolExecuted per call |
| Schema validation | Zod safeParse | None | JSON Schema guard |

## 8. Dependencies & External Integrations

### Internal Dependencies
- **INT-001**: `cpn/tools/registry.go` — source of tool schemas
- **INT-002**: `cpn/fire_llm.go` — tool-call loop implementation
- **INT-003**: `cpn/transition.go` — Transition struct definition
- **INT-004**: `cpn/event.go` — event types and payloads

### No New External Dependencies
- All changes use Go stdlib (`encoding/json`)
- No new Go modules or npm packages

## 9. Examples & Edge Cases

### Example: LLM tool definition before and after

**Before (current):**
```json
{"name": "system/personality.set_principle", "description": "Execute tool: system/personality.set_principle"}
```

**After (MCP-compliant):**
```json
{
  "name": "system/personality.set_principle",
  "description": "Modify one of the agent's three principles. Requires human approval.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "user_id": {"type": "string"},
      "kind": {"type": "string", "enum": ["nucleo", "conducta", "etica"]},
      "title": {"type": "string"},
      "description": {"type": "string"},
      "rules": {"type": "array", "items": {"type": "string"}}
    },
    "required": ["user_id", "kind"]
  }
}
```

### Edge Cases

```go
// Edge Case 1: Tool in LLMTools list but not in Registry
// Expected: buildToolSchema returns generic description, nil Parameters
// Behavior: LLM sees tool but without inputSchema — still callable

// Edge Case 2: Registry has tool but Transition has existing Executor
// Expected: InjectIntoCPN does NOT override existing Executor (REQ-004)
// Only fills nil Executors

// Edge Case 3: HITL-gated tool but no HITLConfig.Channel on transition
// Expected: HITL gate is skipped (graceful degradation)
// Tool executes without approval — log warning

// Edge Case 4: Multiple tool calls in single LLM response, one is HITL-gated
// Expected: Non-HITL tools execute immediately, HITL tool blocks
// All results sent back together after HITL resolution
```

## 10. Validation Criteria

1. `go build ./cpn/... ./cpn/tools/...` succeeds with zero errors
2. All existing tests pass (`go test ./... -race`)
3. All new tests pass
4. LLM tool definitions include `inputSchema` when tools have Parameters
5. HITL-gated tools block in tool-call loop until human approval
6. `EventToolExecuted` emitted for every tool call in `handleToolCalls`
7. Non-tool transitions unaffected (backward compatibility)

## 11. Related Specifications / Further Reading

- [spec-architecture-tools-engine-agent-personality.md](spec-architecture-tools-engine-agent-personality.md) — Original tool engine spec
- [MCP Specification 2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18/schema) — Official MCP schema reference
- [ToolRegistry paper (arXiv:2507.10593)](https://arxiv.org/abs/2507.10593) — Protocol-agnostic tool management
- [Claude Code Architecture (DEV.to)](https://dev.to/brooks_wilson_36fbefbbae4/claude-code-architecture-explained-agent-loop-tool-system-and-permission-model-rust-rewrite-41b2) — Claw Code tool system analysis
- [arXiv:2502.14000v1](https://arxiv.org/abs/2502.14000) — CPN framework for human-AI interaction
