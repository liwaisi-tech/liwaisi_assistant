---
title: "Tools Engine & Agent Personality System"
version: 1.0
date_created: 2026-04-04
owner: liwaisi-tech
tags: [architecture, tools, personality, cpn, identity, system-tools, agent-soul]
---

# Introduction

This specification defines the Tools Engine — a reusable, registry-based tool system where tools are first-class CPN transitions — and the Agent Personality System, the first system tool set that allows the agent to identify itself at boot and users to customize its personality through three configurable principles based on a Freudian tripartite model (Nucleo/Conducta/Etica mapped to Id/Ego/Superego).

The personality system introduces an **auto-wake-up** mechanism: when a CPN topology initializes, the agent automatically fires `personality.get_identity` as its first transition, loading its principles before any other computation. This gives the agent self-awareness — it knows *who it is* before it knows *what to do*.

Grounded in the paper arXiv:2502.14000v1 (Borghoff, Bottoni, Pareschi — "Human-Artificial Interaction in the Age of Agentic AI"), this design models tools as CPN transitions within communication spaces and personality as a token color (IDENTITY) that flows through the net.

## 1. Purpose & Scope

**Purpose**: Enable a pluggable tool system within the CPN engine and provide the first system tool set for agent personality customization with auto-initialization at boot.

**Scope**: Full-stack. Backend Go changes in `cpn/`, `cpn/persist/`, `cpn/tools/`, `internal/app/`, `internal/driving/httpapi/`, `store/postgres/`, and `cmd/server/`. Frontend React+TypeScript changes in `front/react-assistant/src/features/personality/`, `src/hooks/`, `src/types/`, `src/services/`.

**Audience**: Backend Go developers, frontend React developers, and AI agents implementing this system.

**Assumptions**:
- The existing `NodeKindTool` transition kind and `Transition.Executor` function remain the primitive for tool execution
- The existing `FuncRegistry` in `cpn/persist/registry.go` handles function serialization
- System tools ship with the binary; user tools are defined per-user via the API
- The personality CORE.md fallback is embedded in the Go binary at compile time
- PostgreSQL stores per-user personality overrides; the CPN domain remains storage-agnostic

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Tool** | A reusable capability registered in the ToolRegistry and materialized as a `NodeKindTool` transition in CPN topologies |
| **ToolSchema** | Metadata describing a tool: name, description, input/output colors, parameter JSON schema, namespace |
| **ToolRegistry** | Singleton registry mapping tool names to ToolSchema + Executor pairs, partitioned by namespace |
| **System Tool** | Tool shipped with the binary under the `system/` namespace. Cannot be overridden by users |
| **User Tool** | Tool defined per-user under the `user/` namespace. Stored in PostgreSQL |
| **Personality** | The agent's configurable identity: three principles (Nucleo, Conducta, Etica) with hierarchy and tension rules |
| **CORE.md** | Default personality definition file, embedded in the binary as fallback |
| **Principle** | One of three personality axes: Nucleo (comprehension drive), Conducta (behavioral warmth), Etica (privacy ethics) |
| **Hierarchy** | Priority order for conflict resolution between principles. Default: Etica > Conducta > Nucleo |
| **Tension** | Productive friction between two principles that resolves via specific behavioral rules |
| **Wake-up** | Auto-initialization sequence where `personality.get_identity` fires before any other transition |
| **IDENTITY** | New token color carrying the agent's loaded personality state |
| **HITL-gated** | A tool whose execution requires human approval via a HITL transition guard |

## 3. Requirements, Constraints & Guidelines

### 3.1 Tools Engine — Requirements

- **REQ-001**: A `ToolSchema` struct MUST define: `Name string`, `Namespace string` (`"system"` or `"user"`), `Description string`, `InputColor ColorSet`, `OutputColor ColorSet`, `Parameters json.RawMessage` (JSON Schema), `RequiresHITL bool`, `Version string`
- **REQ-002**: A `ToolRegistry` struct MUST store `map[string]*ToolEntry` keyed by fully-qualified name (`namespace/toolname`, e.g. `system/personality.get_identity`)
- **REQ-003**: `ToolEntry` MUST contain both `Schema *ToolSchema` and `Executor func(ctx context.Context, in Token) (Token, error)`
- **REQ-004**: `ToolRegistry` MUST support `Register(schema *ToolSchema, executor func(...)) error`, `Resolve(name string) (*ToolEntry, bool)`, `List(namespace string) []*ToolSchema`, `ListAll() []*ToolSchema`
- **REQ-005**: `Register` MUST reject duplicate names within the same namespace and MUST reject `system/` namespace registrations after initialization (immutable once sealed)
- **REQ-006**: `ToolRegistry.Seal()` MUST lock system namespace. After sealing, only `user/` tools can be registered
- **REQ-007**: Tool executors MUST be registered in `FuncRegistry` for topology serialization. Registration name pattern: `tool-exec-{namespace}-{toolname}`
- **REQ-008**: When a topology includes tool transitions, the `ToolName` field MUST reference a fully-qualified registry name. `fireTool` MUST resolve the executor from `ToolRegistry` if `Transition.Executor` is nil
- **REQ-009**: Tool execution MUST emit `EventToolExecuted` (new event type) with payload `ToolExecutedPayload{ToolName, Namespace, DurationMs, Success, Error}`
- **REQ-010**: Tool resolution failures (unknown tool name) MUST route to `ErrorPlace` with `ColorError` token, NOT panic

### 3.2 Tools Engine — LLM Tool Integration

- **REQ-011**: `ToolRegistry.AsLLMTools(namespace ...string) []*LLMTool` MUST convert registered tools into the `LLMTool` format for injection into LLM transitions via `Transition.LLMTools`
- **REQ-012**: When an LLM transition receives a `tool_calls` response, `fireLLM` MUST resolve the tool from `ToolRegistry`, execute it, and feed the result back as `LLMToolResult` in the tool-call loop
- **REQ-013**: Tools with `RequiresHITL == true` MUST trigger a HITL approval gate before execution within the tool-call loop. The HITL prompt MUST include the tool name, description, and proposed arguments

### 3.3 Agent Personality — Requirements

- **REQ-020**: A `Personality` struct MUST define:
  ```go
  type Personality struct {
      UserID     string
      Principles [3]Principle
      Hierarchy  [3]PrincipleKind
      Tensions   [3]TensionRule
      Version    int
      UpdatedAt  time.Time
  }
  
  type Principle struct {
      Kind        PrincipleKind
      Title       string
      Description string
      Rules       []string
  }
  
  type PrincipleKind string
  const (
      PrincipleNucleo   PrincipleKind = "nucleo"    // Id — comprehension drive
      PrincipleConducta PrincipleKind = "conducta"  // Ego — behavioral warmth
      PrincipleEtica    PrincipleKind = "etica"      // Superego — privacy ethics
  )
  
  type TensionRule struct {
      Between    [2]PrincipleKind
      Friction   string  // Description of the tension
      Resolution string  // How the agent resolves it
  }
  ```
- **REQ-021**: A default `Personality` MUST be embedded in the binary from `CORE.md` content, parsed at init time
- **REQ-022**: `PersonalityRepository` interface MUST define:
  ```go
  type PersonalityRepository interface {
      Get(ctx context.Context, userID string) (*PersonalityRecord, error)
      Save(ctx context.Context, rec *PersonalityRecord) error
      Delete(ctx context.Context, userID string) error
  }
  ```
- **REQ-023**: If no user personality exists in the repository, the default embedded personality MUST be used (fallback behavior)
- **REQ-024**: New token color `ColorIdentity ColorSet = "IDENTITY"` MUST be added to `colors.go`. Tokens of this color carry `*Personality` as payload

### 3.4 Agent Wake-up — Requirements

- **REQ-030**: Every topology factory MUST prepend a wake-up subnet to the topology:
  ```
  p-boot (IDENTITY, computation) → t-identity (tool: system/personality.get_identity) → p-identity (IDENTITY, computation)
  ```
- **REQ-031**: The wake-up transition `t-identity` MUST fire BEFORE any other transition. This is enforced by making all other transitions' input places depend on `p-identity` having a token
- **REQ-032**: `t-identity` executor MUST: (1) load personality from repository for the session's user, (2) fall back to default if not found, (3) return an IDENTITY token with the loaded `*Personality` as payload
- **REQ-033**: After `t-identity` fires, the personality MUST be injected into all LLM transitions' `SystemPrompt` field as a prefix. This is done by a post-wake-up wiring step that reads the IDENTITY token and patches transition prompts
- **REQ-034**: The wake-up MUST emit `EventPersonalityLoaded` with payload `PersonalityLoadedPayload{UserID, Source string, Principles []PrincipleSnapshot}`
- **REQ-035**: Wake-up MUST complete within 500ms. Personality loading is a local database read, not an LLM call

### 3.5 System Personality Tools — Requirements

- **REQ-040**: `system/personality.get_identity` — Read current personality. Input: STRING (user ID or empty for current session). Output: IDENTITY token. No HITL gate. This is the wake-up tool
- **REQ-041**: `system/personality.set_principle` — Modify one principle. Input: JSON `{kind: PrincipleKind, title?: string, description?: string, rules?: []string}`. Output: IDENTITY. HITL-gated. MUST validate that Etica principle cannot have its core privacy rules removed (only extended)
- **REQ-042**: `system/personality.reset` — Restore default personality. Input: STRING (confirmation). Output: IDENTITY. HITL-gated
- **REQ-043**: `system/personality.get_tensions` — Read tension rules. Input: STRING. Output: JSON with current tensions and resolutions. No HITL gate
- **REQ-044**: `system/personality.set_hierarchy` — Change priority order. Input: JSON `{hierarchy: [PrincipleKind, PrincipleKind, PrincipleKind]}`. Output: IDENTITY. HITL-gated. MUST validate that Etica is always in position 1 or 2 (never last)
- **REQ-045**: `system/personality.preview` — Generate sample response under proposed personality. Input: JSON `{personality: Personality, sample_prompt: string}`. Output: STRING (generated preview). No HITL gate. Uses LLM transition internally

### 3.6 Conflict Resolution — Requirements

- **REQ-050**: If `personality.set_principle` attempts to modify Etica in a way that removes privacy guarantees (e.g., removing "confirm before network calls"), the tool MUST return an error token with explanation and suggested amendment
- **REQ-051**: If `personality.set_hierarchy` places Etica last, the tool MUST reject with explanation: "Etica cannot be the lowest priority — privacy is the non-negotiable foundation"
- **REQ-052**: Conflict detection events (`EventConflictDetected`) MUST be emitted with payload `ConflictPayload{Attempted, Rejected, Reason, Suggestion}`

### 3.7 Persistence — Requirements

- **REQ-060**: PostgreSQL migration MUST create `personalities` table:
  ```sql
  CREATE TABLE IF NOT EXISTS personalities (
      user_id     TEXT PRIMARY KEY REFERENCES users(id),
      principles  JSONB NOT NULL,
      hierarchy   JSONB NOT NULL,
      tensions    JSONB NOT NULL,
      version     INTEGER NOT NULL DEFAULT 1,
      created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```
- **REQ-061**: PostgreSQL migration MUST create `tool_definitions` table for user tools:
  ```sql
  CREATE TABLE IF NOT EXISTS tool_definitions (
      id          TEXT PRIMARY KEY,
      user_id     TEXT NOT NULL REFERENCES users(id),
      name        TEXT NOT NULL,
      namespace   TEXT NOT NULL DEFAULT 'user',
      description TEXT NOT NULL,
      input_color TEXT NOT NULL,
      output_color TEXT NOT NULL,
      parameters  JSONB,
      version     TEXT NOT NULL DEFAULT '1.0',
      created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      UNIQUE(user_id, namespace, name)
  );
  ```

### 3.8 HTTP API — Requirements

- **REQ-070**: `GET /api/v1/personality` — Get current user's personality. Returns `PersonalityResponse`
- **REQ-071**: `PATCH /api/v1/personality/principles/{kind}` — Update one principle. Body: `{title?, description?, rules?}`. Returns updated `PersonalityResponse`
- **REQ-072**: `PUT /api/v1/personality/hierarchy` — Set hierarchy. Body: `{hierarchy: [string, string, string]}`. Returns updated `PersonalityResponse`
- **REQ-073**: `DELETE /api/v1/personality` — Reset to defaults. Returns default `PersonalityResponse`
- **REQ-074**: `POST /api/v1/personality/preview` — Preview personality. Body: `{personality, sample_prompt}`. Returns `{response: string}`
- **REQ-075**: `GET /api/v1/tools` — List all available tools (system + user). Query params: `namespace` filter. Returns `ToolListResponse`
- **REQ-076**: `GET /api/v1/tools/{name}` — Get tool detail. Returns `ToolDetailResponse`

### 3.9 Frontend — Requirements

- **REQ-080**: New feature directory `features/personality/` with components: `PersonalityPanel.tsx`, `PrincipleEditor.tsx`, `TensionVisualizer.tsx`, `HierarchyControl.tsx`, `PersonalityPreview.tsx`
- **REQ-081**: `PersonalityPanel` MUST be accessible from `NavigationRail` settings button (currently no-op) and from `CommandPalette` (Cmd+K → "Personality")
- **REQ-082**: `PrincipleEditor` MUST show each principle with title, description, and rules as editable fields. Changes require HITL confirmation dialog before saving
- **REQ-083**: `TensionVisualizer` MUST render the three tensions as a triangle diagram with labeled edges showing friction and resolution text
- **REQ-084**: `HierarchyControl` MUST allow drag-and-drop reordering of the three principles with visual constraint indicator when Etica is dragged below position 2
- **REQ-085**: `PersonalityPreview` MUST allow typing a sample prompt and seeing a generated response under the current (or proposed) personality
- **REQ-086**: New SSE events MUST be handled: `personality_loaded`, `personality_modified`, `conflict_detected`, `tool_executed`
- **REQ-087**: New `features/tools/` directory with `ToolBrowser.tsx` listing registered system and user tools with name, description, namespace badge, and input/output color indicators
- **REQ-088**: `ToolBrowser` MUST be accessible from `NavigationRail` as a new app tab and from `AdaptivePanel`
- **REQ-089**: `StatusBar` MUST show personality status indicator (loaded principle names) next to the session state badge

### Security Requirements

- **SEC-001**: Personality modifications MUST be authenticated — user can only modify their own personality
- **SEC-002**: System tools (`system/` namespace) MUST NOT be modifiable via API
- **SEC-003**: The base Etica principle's core privacy rules MUST NOT be removable, only extendable
- **SEC-004**: Tool executors MUST NOT have access to other users' data. Tool execution context carries only the authenticated user's session
- **SEC-005**: User tool definitions MUST be validated (no code injection via parameters schema)

### Constraints

- **CON-001**: Wake-up must not add more than 500ms to session creation latency
- **CON-002**: Tool registry must be sealed before the first session is created
- **CON-003**: Personality is loaded once per session, not per message. Modifications apply to the next session
- **CON-004**: Maximum 3 principles enforced at the type level (fixed-size array `[3]Principle`)
- **CON-005**: System tools are compiled into the binary. No dynamic loading of Go code at runtime
- **CON-006**: The frontend personality panel MUST follow the existing deep-space design system (CSS variables in `index.css`)

### Guidelines

- **GUD-001**: Tool names follow dot-notation within their namespace: `system/personality.get_identity`, `user/my_tool.search`
- **GUD-002**: All personality modifications should feel like a conversation, not a settings form — the HITL confirmation should explain what's changing and why
- **GUD-003**: The wake-up sequence should be invisible to the user — no loading spinner, no delay. The personality is ready before the first message
- **GUD-004**: Tension visualization should use the existing accent color palette: Nucleo=sky-500, Conducta=amber-500, Etica=emerald-500
- **GUD-005**: Tool execution events should appear in the ExecutionMonitor like any other transition firing

### Patterns

- **PAT-001**: Tool registration follows the same pattern as `FuncRegistry.RegisterExecutor` — name → function mapping with reverse lookup
- **PAT-002**: Personality loading follows the same pattern as topology loading in `SessionService.CreateSession` — factory function produces CPN, personality is injected before Run()
- **PAT-003**: HITL-gated tools follow the same pattern as `fireHITLWithRevision` — emit request, block on channel, process response
- **PAT-004**: System prompt injection follows the same pattern as `BuildContext` in `fire_llm.go` — personality is prepended to the system prompt string

## 4. Interfaces & Data Contracts

### 4.1 Go Interfaces — Tools Engine

```go
// cpn/tools/registry.go

package tools

type ToolSchema struct {
    Name         string          `json:"name"`
    Namespace    string          `json:"namespace"`
    Description  string          `json:"description"`
    InputColor   cpn.ColorSet    `json:"input_color"`
    OutputColor  cpn.ColorSet    `json:"output_color"`
    Parameters   json.RawMessage `json:"parameters,omitempty"`
    RequiresHITL bool            `json:"requires_hitl"`
    Version      string          `json:"version"`
}

type ToolEntry struct {
    Schema   *ToolSchema
    Executor func(ctx context.Context, in cpn.Token) (cpn.Token, error)
}

type Registry struct {
    entries map[string]*ToolEntry  // keyed by "namespace/name"
    sealed  bool
    mu      sync.RWMutex
}

func NewRegistry() *Registry
func (r *Registry) Register(schema *ToolSchema, exec func(context.Context, cpn.Token) (cpn.Token, error)) error
func (r *Registry) Seal()
func (r *Registry) Resolve(qualifiedName string) (*ToolEntry, bool)
func (r *Registry) List(namespace string) []*ToolSchema
func (r *Registry) ListAll() []*ToolSchema
func (r *Registry) AsLLMTools(namespaces ...string) []*cpn.LLMTool
func (r *Registry) InjectIntoFuncRegistry(fr *persist.FuncRegistry)
```

### 4.2 Go Interfaces — Personality

```go
// cpn/personality.go

package cpn

type PrincipleKind string
const (
    PrincipleNucleo   PrincipleKind = "nucleo"
    PrincipleConducta PrincipleKind = "conducta"
    PrincipleEtica    PrincipleKind = "etica"
)

type Principle struct {
    Kind        PrincipleKind `json:"kind"`
    Title       string        `json:"title"`
    Description string        `json:"description"`
    Rules       []string      `json:"rules"`
}

type TensionRule struct {
    Between    [2]PrincipleKind `json:"between"`
    Friction   string           `json:"friction"`
    Resolution string           `json:"resolution"`
}

type Personality struct {
    UserID     string          `json:"user_id"`
    Principles [3]Principle    `json:"principles"`
    Hierarchy  [3]PrincipleKind `json:"hierarchy"`
    Tensions   [3]TensionRule  `json:"tensions"`
    Version    int             `json:"version"`
    UpdatedAt  time.Time       `json:"updated_at"`
}

// Render personality as system prompt prefix
func (p *Personality) AsSystemPrompt() string

// Validate hierarchy constraints (Etica never last)
func (p *Personality) ValidateHierarchy() error

// Validate principle modification (Etica core rules immutable)
func (p *Personality) ValidatePrincipleUpdate(kind PrincipleKind, updated Principle) error
```

```go
// cpn/persist/interfaces.go — add to existing file

type PersonalityRecord struct {
    UserID     string          `json:"user_id"`
    Principles json.RawMessage `json:"principles"`
    Hierarchy  json.RawMessage `json:"hierarchy"`
    Tensions   json.RawMessage `json:"tensions"`
    Version    int             `json:"version"`
    CreatedAt  time.Time       `json:"created_at"`
    UpdatedAt  time.Time       `json:"updated_at"`
}

type PersonalityRepository interface {
    Get(ctx context.Context, userID string) (*PersonalityRecord, error)
    Save(ctx context.Context, rec *PersonalityRecord) error
    Delete(ctx context.Context, userID string) error
}
```

### 4.3 New Event Types

```go
// cpn/event.go — add to existing EventType constants

const (
    EventToolExecuted       EventType = "tool_executed"
    EventPersonalityLoaded  EventType = "personality_loaded"
    EventPersonalityModified EventType = "personality_modified"
    EventConflictDetected   EventType = "conflict_detected"
    EventToolRegistered     EventType = "tool_registered"
)

// Payloads

type ToolExecutedPayload struct {
    ToolName   string `json:"tool_name"`
    Namespace  string `json:"namespace"`
    DurationMs int64  `json:"duration_ms"`
    Success    bool   `json:"success"`
    Error      string `json:"error,omitempty"`
}

type PersonalityLoadedPayload struct {
    UserID     string              `json:"user_id"`
    Source     string              `json:"source"` // "database" or "default"
    Principles []PrincipleSnapshot `json:"principles"`
}

type PrincipleSnapshot struct {
    Kind  string `json:"kind"`
    Title string `json:"title"`
}

type PersonalityModifiedPayload struct {
    UserID       string `json:"user_id"`
    ModifiedKind string `json:"modified_kind"`
    ChangeType   string `json:"change_type"` // "principle_updated", "hierarchy_changed", "reset"
}

type ConflictPayload struct {
    Attempted  string `json:"attempted"`
    Rejected   string `json:"rejected"`
    Reason     string `json:"reason"`
    Suggestion string `json:"suggestion"`
}
```

### 4.4 New Token Color

```go
// cpn/colors.go — add to existing constants

const ColorIdentity ColorSet = "IDENTITY" // Personality state token
```

### 4.5 HTTP API Contracts

```typescript
// GET /api/v1/personality
interface PersonalityResponse {
  user_id: string;
  principles: PrincipleResponse[];
  hierarchy: string[];  // ["etica", "conducta", "nucleo"]
  tensions: TensionResponse[];
  version: number;
  updated_at: string;
}

interface PrincipleResponse {
  kind: 'nucleo' | 'conducta' | 'etica';
  title: string;
  description: string;
  rules: string[];
}

interface TensionResponse {
  between: [string, string];
  friction: string;
  resolution: string;
}

// PATCH /api/v1/personality/principles/{kind}
interface UpdatePrincipleRequest {
  title?: string;
  description?: string;
  rules?: string[];
}

// PUT /api/v1/personality/hierarchy
interface SetHierarchyRequest {
  hierarchy: [string, string, string];
}

// POST /api/v1/personality/preview
interface PreviewRequest {
  personality: PersonalityResponse;
  sample_prompt: string;
}
interface PreviewResponse {
  response: string;
}

// GET /api/v1/tools
interface ToolListResponse {
  tools: ToolSummary[];
}
interface ToolSummary {
  name: string;
  namespace: string;
  description: string;
  input_color: string;
  output_color: string;
  requires_hitl: boolean;
  version: string;
}
```

### 4.6 Frontend SSE Events

```typescript
// types/sse.ts — add to existing SSEEventType union

type SSEEventType =
  | /* ...existing... */
  | 'tool_executed'
  | 'personality_loaded'
  | 'personality_modified'
  | 'conflict_detected';

interface ToolExecutedData {
  tool_name: string;
  namespace: string;
  duration_ms: number;
  success: boolean;
  error?: string;
}

interface PersonalityLoadedData {
  user_id: string;
  source: 'database' | 'default';
  principles: { kind: string; title: string }[];
}

interface PersonalityModifiedData {
  user_id: string;
  modified_kind: string;
  change_type: 'principle_updated' | 'hierarchy_changed' | 'reset';
}

interface ConflictDetectedData {
  attempted: string;
  rejected: string;
  reason: string;
  suggestion: string;
}
```

## 5. Acceptance Criteria

### Tools Engine

- **AC-001**: Given a tool registered as `system/personality.get_identity`, When `ToolRegistry.Resolve("system/personality.get_identity")` is called, Then the ToolEntry with correct schema and executor is returned
- **AC-002**: Given the registry is sealed, When `Register` is called with namespace `system/`, Then an error is returned
- **AC-003**: Given a topology with a tool transition referencing `system/personality.get_identity`, When the transition fires, Then the executor is resolved from ToolRegistry and the IDENTITY token is deposited in the output place
- **AC-004**: Given a tool with `RequiresHITL == true`, When the tool is called within an LLM tool-call loop, Then a HITL request is emitted and execution blocks until human approval
- **AC-005**: Given a tool name that does not exist in the registry, When fireTool attempts to resolve it, Then an error token is deposited in ErrorPlace and EventToolExecuted is emitted with `Success: false`

### Personality Wake-up

- **AC-010**: Given a new session is created, When the CPN topology initializes, Then `t-identity` fires as the FIRST transition before any other
- **AC-011**: Given a user with a saved personality in PostgreSQL, When `t-identity` fires, Then the saved personality is loaded and an IDENTITY token is deposited
- **AC-012**: Given a user with NO saved personality, When `t-identity` fires, Then the default CORE.md personality is loaded
- **AC-013**: Given `t-identity` has fired, When any LLM transition fires, Then its SystemPrompt is prefixed with the personality rendered via `Personality.AsSystemPrompt()`
- **AC-014**: Given a session creation request, When the full wake-up completes, Then total added latency is under 500ms

### Personality Tools

- **AC-020**: Given the user calls `personality.set_principle` with kind="conducta" and valid data, When the HITL approval is received, Then the principle is updated in PostgreSQL and EventPersonalityModified is emitted
- **AC-021**: Given the user calls `personality.set_principle` and tries to remove a core Etica rule about network confirmation, Then the tool returns an error with suggestion and EventConflictDetected is emitted
- **AC-022**: Given the user calls `personality.set_hierarchy` with `["nucleo", "conducta", "etica"]` (Etica last), Then the tool rejects with explanation
- **AC-023**: Given the user calls `personality.reset`, When HITL approval is received, Then the personality record is deleted from PostgreSQL and default is restored

### Frontend

- **AC-030**: Given the user clicks Settings in NavigationRail, When the PersonalityPanel opens, Then all three principles are displayed with their current values
- **AC-031**: Given the user edits a principle and clicks Save, When the HITL confirmation dialog appears and the user approves, Then the API is called and the principle is updated
- **AC-032**: Given a `personality_loaded` SSE event, When the StatusBar renders, Then it shows the loaded principle titles (e.g., "Comprension | Calidez | Privacidad")
- **AC-033**: Given the user opens ToolBrowser, When the panel renders, Then all registered system and user tools are listed with namespace badges

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go + React), Integration (Go + PostgreSQL), End-to-End (Playwright)
- **Frameworks**: Go `testing` + `testify`, React `vitest` + `@testing-library/react`
- **Go Unit Tests**:
  - `cpn/tools/registry_test.go`: Register, Resolve, Seal, List, duplicate rejection, namespace isolation
  - `cpn/personality_test.go`: AsSystemPrompt rendering, ValidateHierarchy, ValidatePrincipleUpdate
  - `cpn/tools/personality_tools_test.go`: Each tool executor with mock repositories
- **Go Integration Tests**:
  - `store/postgres/personality_test.go`: CRUD against real PostgreSQL
  - `internal/app/session_service_test.go`: Wake-up sequence fires t-identity first
- **Frontend Unit Tests**:
  - `features/personality/PrincipleEditor.test.tsx`: Edit + save + HITL confirmation flow
  - `features/personality/TensionVisualizer.test.tsx`: Renders triangle with correct labels
  - `features/tools/ToolBrowser.test.tsx`: Lists tools with namespace filtering
  - `hooks/usePersonality.test.ts`: API calls, state management, SSE integration
- **Coverage Requirements**: 80% line coverage for new packages
- **CI/CD**: `go test ./cpn/tools/... ./cpn/... ./store/postgres/... ./internal/...` in GitHub Actions

## 7. Rationale & Context

### Why Tools as CPN Transitions?

The paper arXiv:2502.14000v1 establishes that in a Communication Space Petri Net, all computation happens through transitions. Tools are not external to the net — they ARE transitions. This means:
- Tools benefit from the same concurrency model (guard conditions, retry, circuit breakers)
- Tools emit events like any other transition (observability for free)
- Tools compose in topologies (a tool output can feed another tool's input)
- Tools participate in mode switching (a tool can trigger Centaurian mode)

### Why Personality as Token Color?

In Coloured Petri Nets, colors carry semantic meaning. An IDENTITY token is data that flows through the net, just like STRING or JSON tokens. This means:
- Personality can be consumed and produced by transitions (it's not a global variable)
- Personality changes are traceable through the event log
- Multiple personalities could coexist in a MAS topology (each sub-CPN has its own IDENTITY)

### Why Wake-up Before First Transition?

From the paper's Section 4.3 on Communication Spaces: "raw data never bypasses the necessary transformation steps." The personality IS that transformation — it defines HOW the agent processes everything else. Without identity, the agent is a generic LLM wrapper. With identity, it's a specific being with drives, behavior, and ethics.

### Why the Freudian Model?

The tripartite model provides productive tension (Section 2.2 of our analysis). Unlike flat attribute systems (Big-Five, CloChat), the Id/Ego/Superego mapping creates three FUNCTIONAL layers:
- The Id (Nucleo) generates motivational pressure to understand
- The Ego (Conducta) mediates between the drive and the world
- The Superego (Etica) constrains what the Ego can do

This is not a metaphor — it's a functional architecture where each layer has a different computational role in prompt construction.

### Why Etica Can Never Be Last?

From Asimov's Three Laws: the failure mode of hierarchical ethics is when the highest-priority rule can be overridden. In Asimov, the Zeroth Law was added to prevent this. In our system, the constraint that Etica is always position 1 or 2 IS the Zeroth Law — privacy cannot be sacrificed for warmth or comprehension.

## 8. Dependencies & External Integrations

### Infrastructure Dependencies
- **INF-001**: PostgreSQL 15+ — for `personalities` and `tool_definitions` tables
- **INF-002**: Existing `store/postgres/pool.go` connection pool
- **INF-003**: Existing `cpn/persist/registry.go` FuncRegistry for serialization

### Internal Dependencies
- **INT-001**: `cpn/` package — Token, Place, Transition, ColorSet, NodeKindTool, Event, EventType
- **INT-002**: `cpn/persist/` — FuncRegistry, repository interfaces
- **INT-003**: `internal/app/session_service.go` — TopologyFactory, session creation flow
- **INT-004**: `internal/driving/httpapi/` — HTTP handlers, SSE broker
- **INT-005**: `cmd/server/topologies.go` — topology factory functions
- **INT-006**: `front/react-assistant/src/` — existing hooks, types, services patterns

### No New External Dependencies
- No new Go modules required. All functionality uses stdlib + existing dependencies
- No new npm packages required. Frontend uses existing React, Tailwind, React Flow

## 9. Examples & Edge Cases

### Wake-up Topology Wiring

```go
// In topology factory (cmd/server/topologies.go)
func withWakeUp(factory TopologyFactory, toolReg *tools.Registry, persRepo persist.PersonalityRepository) TopologyFactory {
    return func(sessionID string) *cpn.CPN {
        // Build the base topology
        base := factory(sessionID)
        
        // Create wake-up places
        pBoot := cpn.NewPlace("p-boot", cpn.ColorIdentity, cpn.SpaceComputation)
        pIdentity := cpn.NewPlace("p-identity", cpn.ColorIdentity, cpn.SpaceComputation)
        
        // Create wake-up transition
        entry, _ := toolReg.Resolve("system/personality.get_identity")
        tIdentity := cpn.NewTransition("t-identity", cpn.NodeKindTool, []string{"p-boot"}, []string{"p-identity"})
        tIdentity.ToolName = "system/personality.get_identity"
        tIdentity.Executor = entry.Executor
        
        // Seed boot token
        bootToken := &cpn.Token{
            Color:   cpn.ColorIdentity,
            Payload: sessionID, // identity tool reads session's user
            Space:   cpn.SpaceComputation,
        }
        pBoot.Deposit(bootToken)
        
        // Wire: make existing input place depend on p-identity
        // Original: p-input → t-llm
        // New: p-boot → t-identity → p-identity AND p-input → t-llm (t-llm now also requires p-identity)
        for _, t := range base.Transitions {
            if t.Kind == cpn.NodeKindLLM || t.Kind == cpn.NodeKindSubNet {
                t.InputPlaces = append(t.InputPlaces, "p-identity")
            }
        }
        
        // Add wake-up components to topology
        base.Places["p-boot"] = pBoot
        base.Places["p-identity"] = pIdentity
        base.Transitions["t-identity"] = tIdentity
        
        return base
    }
}
```

### CORE.md Default Personality

```go
//go:embed core.md
var defaultCoreContent string

var DefaultPersonality = Personality{
    Principles: [3]Principle{
        {
            Kind:        PrincipleNucleo,
            Title:       "Comprension",
            Description: "Tu impulso mas profundo es entender antes de actuar. No ejecutes lo que no comprendes. La ambiguedad es una senal de pausa, no de aceleracion.",
            Rules: []string{
                "Antes de cualquier tarea no trivial, reformula la intencion del usuario",
                "Haz una sola pregunta precisa si algo no esta claro",
                "Prefiere preguntar antes que asumir y equivocarte",
            },
        },
        {
            Kind:        PrincipleConducta,
            Title:       "Calidez",
            Description: "Tu tono es humano. No corporativo, no robotico. Reconoces el contexto emocional sin performarlo ni exagerarlo.",
            Rules: []string{
                "Habla como un colega competente, no como un manual de instrucciones",
                "Cuando detectas urgencia o frustracion, nombrala brevemente antes de responder",
                "Explica el por que de tus acciones, no solo el que",
                "Nada de frases vacias: nunca uses gran pregunta ni claro que si",
            },
        },
        {
            Kind:        PrincipleEtica,
            Title:       "Privacidad radical",
            Description: "Nada sale de este sistema sin confirmacion explicita del usuario. Este limite no es negociable y no tiene excepciones silenciosas.",
            Rules: []string{
                "Toda llamada de red se anuncia ANTES de ejecutarse",
                "Los datos del usuario nunca viajan a APIs externas sin permiso explicito",
                "Si no puedes resolver algo localmente, dilo: que necesitarias y por que",
                "Enmarca la privacidad como cuidado: quiero asegurarme antes de enviar esto",
            },
        },
    },
    Hierarchy: [3]PrincipleKind{PrincipleEtica, PrincipleConducta, PrincipleNucleo},
    Tensions: [3]TensionRule{
        {
            Between:    [2]PrincipleKind{PrincipleNucleo, PrincipleConducta},
            Friction:   "Entender puede sentirse frio si se hace con demasiadas preguntas",
            Resolution: "El agente pregunta una sola cosa a la vez, con empatia",
        },
        {
            Between:    [2]PrincipleKind{PrincipleConducta, PrincipleEtica},
            Friction:   "La calidez empuja a ser util; la privacidad frena acciones rapidas",
            Resolution: "La privacidad se comunica como cuidado, no como obstaculo",
        },
        {
            Between:    [2]PrincipleKind{PrincipleNucleo, PrincipleEtica},
            Friction:   "Comprender puede requerir contexto externo",
            Resolution: "El agente resuelve con lo local primero, siempre",
        },
    },
}
```

### Edge Cases

```go
// Edge Case 1: User tries to remove core Etica rule
// Input: personality.set_principle({kind: "etica", rules: ["Be nice"]})
// Expected: Error — core rules cannot be removed, only extended
// The tool checks that all default Etica rules are present in the update

// Edge Case 2: Personality loading fails (DB down)
// Expected: Fall back to default personality, emit warning event
// Never block session creation due to personality loading failure

// Edge Case 3: Two concurrent sessions for same user, one modifies personality
// Expected: CON-003 — modifications apply to NEXT session, not current
// Each session loads personality once at wake-up, immutable after that

// Edge Case 4: User has no user record yet (first login)
// Expected: REQ-023 — default personality is used, no error

// Edge Case 5: Tool executor panics
// Expected: fireTool recovers panic, routes to ErrorPlace, emits EventToolExecuted with Success=false
```

## 10. Validation Criteria

1. **ToolRegistry** unit tests pass: Register, Resolve, Seal, List, AsLLMTools, duplicate rejection
2. **Personality** unit tests pass: AsSystemPrompt, ValidateHierarchy, ValidatePrincipleUpdate, conflict detection
3. **Wake-up** integration test: session creation → t-identity fires first → personality loaded → LLM transitions have prefixed system prompt
4. **Persistence** integration test: CRUD operations on `personalities` table
5. **HTTP API** integration test: All personality endpoints return correct responses
6. **Frontend** component tests: PersonalityPanel renders, PrincipleEditor edits, TensionVisualizer renders triangle
7. **SSE events** integration test: personality_loaded event received by frontend after session creation
8. **Latency** benchmark: wake-up adds < 500ms to session creation (measured with `testing.B`)
9. **Security** test: unauthenticated personality modification returns 401, cross-user modification returns 403
10. **Edge cases** tested: DB failure fallback, concurrent sessions, Etica protection, panic recovery

## 11. Related Specifications / Further Reading

- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md) — LLM streaming through CPN engine
- [spec-design-cpn-execution-monitor.md](spec-design-cpn-execution-monitor.md) — Execution monitor (tools should appear here)
- [spec-design-ux-refresh-navigation.md](spec-design-ux-refresh-navigation.md) — Navigation patterns for the personality panel
- [arXiv:2502.14000v1](https://arxiv.org/abs/2502.14000) — Borghoff, Bottoni, Pareschi — "Human-Artificial Interaction in the Age of Agentic AI"
- [NeoPsyke](https://dev.to/atomitl/open-sourcing-neopsyke-an-autonomous-ai-agent-built-around-motivation-planning-and-governance-1d0o) — Reference implementation of Id/Ego/Superego agent architecture
- [Personas Evolved](https://arxiv.org/html/2502.20513v1) — ACM workshop on ethical LLM persona design
