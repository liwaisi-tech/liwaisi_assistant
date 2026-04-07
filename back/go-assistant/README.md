# BRAE Backend -- Go Assistant

The backend is a Go HTTP server that hosts the CPN execution engine, exposes a REST + SSE API, and manages persistence across PostgreSQL and Redis.

## Architecture

```
cmd/
  server/main.go          Composition root -- wires ports to adapters
  server/topologies.go    Built-in CPN topologies (default, unified, HITL)
  cli/main.go             CLI binary entry point

cpn/                      Domain core (zero external imports)
  cpn.go                  CPN definition & lifecycle
  place.go                Token buffer with color/space constraints
  transition.go           Unit of computation (guards, retries, kinds)
  token.go                Fundamental data unit with origin provenance
  executor.go             Main execution loop (fire algorithm)
  fire_llm.go             LLM transition firing
  fire_validate.go        Validation transition firing
  event.go                Append-only event types
  ports.go                Port interfaces (LLMClient, ChannelAdapter, etc.)
  session.go              Session context management
  memory.go               Conversation memory with sliding window
  retry.go                Retry policies & circuit breaker
  observe.go              Event observation space
  personality.go          Three-layer personality model
  group_agent.go          Sub-CPN group management
  subnet.go               Sub-net topology composition
  mode.go                 MAS / Centaurian mode switching
  validate.go             Topology validation
  hitl.go                 Human-in-the-loop interaction
  persist/                Serialization, repository interfaces, function registry
  tools/                  MCP-compatible tool registry

infra/                    Infrastructure adapters
  openrouter/             LLM provider (streaming, structured output, extended thinking)
  googleauth/             Google OAuth token verification
  billing/                OpenRouter billing API
  activity/               Activity reconciliation
  guardrails/             Spending limit enforcement

internal/
  app/                    Application layer (SessionService, persistence config)
  driving/httpapi/        HTTP driving adapter (routes, handlers, SSE broker, middleware)

store/
  postgres/               PostgreSQL repositories (sessions, events, ledger, flows, etc.)
  redis/                  Redis repositories (session cache, HITL pending requests)
```

### Hexagonal boundaries

The `cpn/` package defines **port interfaces** with zero knowledge of infrastructure:

```go
type LLMClient interface {
    Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)
    CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(chunk string)) (LLMResponse, error)
    EstimateCost(req *LLMRequest) (float64, error)
}
```

Infrastructure adapters (`infra/`, `store/`) implement these ports. The composition root (`cmd/server/main.go`) wires everything together.

## CPN execution engine

The executor (`cpn/executor.go`) runs a deterministic loop:

1. **Validate** topology (node kinds, arc connectivity, initial marking)
2. **Collect** firable transitions (all input places satisfied + guard passes + circuit breaker open)
3. **Consume** tokens on the main goroutine (consume-before-launch pattern)
4. **Fire** each transition in its own goroutine with retry + timeout
5. **Wait** for all goroutines (WaitGroup)
6. **Route** errors to configured error places or fail the CPN
7. **Repeat** until completion, deadlock, or context cancellation

### Transition kinds

| Kind | Description |
|---|---|
| `LLM` | Calls the LLM provider with conversation history + system prompt |
| `Tool` | Executes a registered tool function |
| `Validate` | Runs validation logic on tokens |
| `SubNet` | Spawns a child CPN (isolated, observable via event bus) |
| `Observer` | Watches a sub-CPN's event bus, deposits event tokens into parent |
| `HITL` | Blocks execution until a human provides input via channel |

### Retry & circuit breaker

Each transition can have a `RetryPolicy` with exponential backoff, jitter, and max attempts. A circuit breaker trips after repeated failures, causing `CanFire()` to return false until the breaker resets.

## HTTP API

All endpoints are prefixed with `/api/v1/`.

### Sessions

| Method | Path | Description |
|---|---|---|
| `POST` | `/sessions` | Create a new session |
| `GET` | `/sessions` | List sessions |
| `GET` | `/sessions/{id}` | Get session details |
| `PATCH` | `/sessions/{id}` | Update session topology |
| `DELETE` | `/sessions/{id}` | Delete session |
| `POST` | `/sessions/{id}/messages` | Send a message (triggers CPN execution) |
| `GET` | `/sessions/{id}/events` | SSE event stream (long-lived) |
| `POST` | `/sessions/{id}/fork` | Fork session from a point in history |
| `POST` | `/sessions/{id}/hitl/{transitionID}` | Resolve HITL request |

### Other

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/version` | Build metadata |
| `GET` | `/billing/balance` | Account balance |
| `GET` | `/flows` | List crystallized topologies |
| `GET` | `/flows/{hash}` | Get flow details |
| `GET` | `/flows/{hash}/executions` | Flow execution history |
| `GET` | `/personality` | Current personality |
| `PATCH` | `/personality/principles/{kind}` | Update a principle |
| `PUT` | `/personality/hierarchy` | Set principle hierarchy |
| `DELETE` | `/personality` | Reset to defaults |
| `POST` | `/personality/preview` | Preview personality changes |
| `GET` | `/tools` | List registered tools |
| `GET` | `/tools/{name}` | Tool details |
| `POST` | `/waitlist` | Public waitlist signup |

### SSE events

The `/sessions/{id}/events` endpoint streams CPN events in real-time:

- `transition_started` / `transition_completed` / `transition_fired`
- `stream_chunk` -- LLM streaming output
- `subnet_started` / `subnet_completed` / `subnet_failed`
- `hitl_requested` / `hitl_resolved`
- `mode_switch` -- MAS / Centaurian toggle
- `personality_loaded` / `personality_modified`

### Middleware

- **CORS** -- configurable allowed origins
- **Google OAuth** -- optional JWT verification (dev mode skips auth)
- **Rate limiting** -- token bucket per user
- **Logging** -- structured request/response logging

## Persistence

### PostgreSQL (durable storage)

Repositories: sessions, messages, events (monthly-partitioned), ledger, flows (soft-delete), intelligence metrics, personalities, users, waitlist.

Features: connection pooling (pgx), event batching (100 events or 500ms flush), SQL migrations via golang-migrate.

### Redis (hot cache + HITL)

- Session cache (L1 over PostgreSQL, LRU eviction)
- HITL pending requests (1-hour TTL, auto-cleanup)

### CPN serialization

Go functions cannot be marshaled to JSON. The `cpn/persist/` package solves this with a **function registry pattern**: tool executors and subnet factories are registered by name at startup. Serialization writes names; deserialization resolves them back to functions.

## Personality system

Three-layer model inspired by psychoanalytic structure:

| Layer | Psychic analog | Purpose |
|---|---|---|
| **Nucleo** | Id | Comprehension drive -- how the agent understands |
| **Conducta** | Ego | Behavioral warmth -- how the agent communicates |
| **Etica** | Superego | Privacy ethics -- what the agent refuses to do |

Each layer has a title, description, and enforceable rules. A configurable hierarchy determines which principle wins in conflict, with explicit tension resolution rules for each pair.

The `Identity` struct provides radical transparency per EU AI Act Article 50: name, nature, creator, LLM disclosure, gender-neutral pronouns.

## LLM integration (OpenRouter)

The `infra/openrouter/` adapter supports:

- OpenAI-compatible and Anthropic-native endpoints
- Streaming via `onChunk` callback
- Structured JSON output (json_object, JSONSchema)
- Extended thinking (reasoning tokens, budget)
- Prompt caching with TTL
- Fallback models
- Per-session token ledger for cost tracking

**Default model registry** (all overridable via environment):

| Role | Model |
|---|---|
| Classifier | `google/gemini-2.0-flash-001` |
| Structured | `anthropic/claude-haiku-4-5-20251001` |
| Reasoning | `anthropic/claude-sonnet-4-6` |
| Thinking | `anthropic/claude-opus-4-6` |
| Summarize | `meta-llama/llama-3.3-8b-instruct` |

## Setup

### Prerequisites

- Go 1.25+
- PostgreSQL 16 (optional -- falls back to in-memory)
- Redis 7 (optional -- falls back to no cache)
- An [OpenRouter](https://openrouter.ai/) API key

### Build & run

```bash
# Build the server
make build-server

# Set environment
export OPENROUTER_API_KEY=sk-or-...
export LIWAISI_DB_DSN=postgres://user:pass@localhost:5432/liwaisi  # optional
export LIWAISI_REDIS_URL=redis://localhost:6379                    # optional

# Run
./bin/liwaisi-server
```

### Build the CLI

```bash
make build
./bin/liwaisi version
```

### Makefile targets

| Target | Description |
|---|---|
| `make build` | Build CLI binary |
| `make build-server` | Build HTTP server binary |
| `make test` | Unit tests with race detection |
| `make test-coverage` | Coverage report (HTML) |
| `make test-integration` | Integration tests (requires `OPENROUTER_API_KEY`) |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code |
| `make clean` | Remove build artifacts |
| `make deps` | Download and tidy dependencies |

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `OPENROUTER_API_KEY` | Yes | OpenRouter API key |
| `LIWAISI_DB_DSN` | No | PostgreSQL connection string |
| `LIWAISI_REDIS_URL` | No | Redis connection URL |
| `CORS_ORIGINS` | No | Allowed CORS origins (default: `*`) |
| `DEFAULT_MODEL` | No | Default LLM model |
| `MODEL_*` | No | Per-role model overrides |
| `GOOGLE_CLIENT_ID` | No | Google OAuth client ID |
| `LOG_LEVEL` | No | Logging level |
| `LISTEN_ADDR` | No | Server listen address |
| `READ_TIMEOUT` | No | HTTP read timeout |
| `IDLE_TIMEOUT` | No | HTTP idle timeout |
| `SHUTDOWN_TIMEOUT` | No | Graceful shutdown timeout |
