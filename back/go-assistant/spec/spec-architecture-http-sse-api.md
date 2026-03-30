---
title: HTTP API with Server-Sent Events (SSE) Driving Adapter
version: "1.0"
date_created: 2026-03-30
owner: liwaisi-tech
tags: [architecture, http, sse, driving-adapter, hexagonal, ddd]
---

# HTTP API with Server-Sent Events (SSE) Driving Adapter

## 1. Introduction & Purpose

This specification defines the first **driving adapter** for the Liwaisi CPN engine. The adapter exposes an HTTP API with Server-Sent Events (SSE) that enables web clients to:

- Create and query user sessions bound to CPN instances.
- Send messages that trigger asynchronous CPN execution.
- Receive real-time streaming of LLM `StreamChunk` data and CPN `Event` notifications over SSE.
- Submit Human-In-The-Loop (HITL) responses to unblock gated transitions.

The implementation uses Go's **stdlib `net/http`** with Go 1.22+ enhanced routing (method patterns, path variables). No external dependencies are introduced, consistent with the project's zero-dependency philosophy.

The adapter follows the Hexagonal Architecture established in the codebase: HTTP handlers delegate to an application-layer `SessionService`, which orchestrates domain operations on `cpn.Session` and `cpn.CPN`. The domain layer (`cpn/`) remains untouched.

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the universal agent type. A directed bipartite graph of places and transitions that models concurrent computation. Defined in `cpn/cpn.go`. |
| **SSE** | Server-Sent Events — an HTTP-native protocol for server-to-client streaming over a long-lived `text/event-stream` connection. |
| **HITL** | Human-In-The-Loop — a transition kind that blocks CPN execution until a human provides input (approve, reject, or revise). See `cpn.HITLAction`, `cpn.HITLResponse`. |
| **DDD** | Domain-Driven Design — the domain layer (`cpn/`) owns all business logic and types; outer layers depend inward. |
| **Hexagonal Architecture** | Ports & Adapters pattern. The domain defines port interfaces (`cpn.LLMClient`, `cpn.ChannelAdapter`); infrastructure provides implementations. |
| **Driving Adapter** | An adapter that initiates interaction with the domain (e.g., HTTP API). Lives in `internal/driving/`. |
| **Driven Adapter** | An adapter that the domain calls out to (e.g., OpenRouter LLM client). Lives in `infra/`. |
| **Port** | An interface defined in the domain layer. `cpn.LLMClient` and `cpn.ChannelAdapter` are the primary ports. |
| **Session** | Binds a user to a root CPN instance and a streaming channel. Defined in `cpn/session.go`. The only interface the user sees (Axiom A10). |
| **StreamChunk** | A single chunk of an LLM streaming response, delivered via `Session.Stream`. Fields: `SessionID`, `CPNID`, `CPNRole`, `Content`, `Done`. |
| **Event** | An append-only record emitted by CPN transitions. Carries `Type` (EventType), `SessionID`, `CPNID`, `CPNDepth`, `Payload`, `Timestamp`. Defined in `cpn/event.go`. |
| **EventType** | Enum classifying CPN events: `transition_fired`, `subnet_started`, `subnet_completed`, `subnet_failed`, `hitl_requested`, `hitl_resolved`, `token_deposited`, `mode_switch`, `stream_chunk`. |
| **Mode** | CPN execution mode: `mas` (autonomous) or `centaurian` (human co-trigger). Defined in `cpn/mode.go`. |
| **State** | CPN lifecycle state: `idle`, `running`, `waiting`, `completed`, `failed`. Defined in `cpn/mode.go`. |
| **ChannelType** | Communication channel origin: `web`, `whatsapp`, `telegram`. Defined in `cpn/session.go`. |
| **Application Layer** | `internal/app/` — orchestrates use cases via `SessionService`. Imports domain, does not import adapters. |
| **Composition Root** | `cmd/server/main.go` — the only place that wires all layers together. |
| **SSE Broker** | Hub/broker pattern component managing per-session SSE client subscriptions and event fan-out. |

---

## 3. Requirements, Constraints & Guidelines

### 3.1 Requirements

| ID | Requirement |
|----|-------------|
| **REQ-001** | HTTP server MUST use stdlib `net/http` with Go 1.22+ enhanced routing (method patterns, path variables). No third-party router or framework. |
| **REQ-002** | SSE endpoint MUST stream CPN events and LLM `StreamChunk` data to connected clients in real-time. Events are delivered as they are emitted by the CPN engine. |
| **REQ-003** | Application layer MUST provide `SessionService` as the single entry point for all use cases (create session, send message, resolve HITL, get session, stream events). |
| **REQ-004** | Server MUST implement graceful shutdown on `SIGINT`/`SIGTERM` with configurable timeout (default 30s). Active SSE connections and in-flight requests drain within the timeout window. |
| **REQ-005** | SSE connections MUST send heartbeat comments (`: heartbeat\n\n`) every 15 seconds to prevent proxy/load-balancer timeouts. |
| **REQ-006** | Each SSE event MUST include a monotonically increasing `id` field for `Last-Event-ID` reconnection tracking. IDs are per-connection, starting at 1. |
| **REQ-007** | `POST /api/v1/sessions/{id}/messages` MUST return `202 Accepted` immediately. CPN execution happens asynchronously in a background goroutine. |
| **REQ-008** | SSE Broker MUST use per-session client maps for O(clients-in-session) fan-out. Each session maintains its own client set, avoiding global iteration. |
| **REQ-009** | All request bodies MUST be limited via `http.MaxBytesReader` (1 MB default) to prevent memory exhaustion from oversized payloads. |
| **REQ-010** | Server MUST use `http.ResponseController` for SSE flush and write deadline management. No type assertions to `http.Flusher`. |
| **REQ-011** | `ChannelAdapter` implementation MUST bridge HTTP `POST /messages` to `CPN.Receive` and SSE to `CPN.Send`. The `HTTPChannelAdapter` implements `cpn.ChannelAdapter`. |

### 3.2 Security

| ID | Requirement |
|----|-------------|
| **SEC-001** | API key (`OPENROUTER_API_KEY`) MUST be read from environment variables. It MUST NOT be logged, returned in API responses, or included in error messages. |
| **SEC-002** | CORS middleware MUST validate `Origin` against a configurable list of allowed origins (env `CORS_ORIGINS`, default `*`). Preflight `OPTIONS` requests MUST be handled. `Last-Event-ID` MUST be included in `Access-Control-Allow-Headers`. |
| **SEC-003** | Request ID MUST be generated using `crypto/rand` (8 bytes hex-encoded, 16 characters). No `math/rand` or predictable sequences. |
| **SEC-004** | Error responses MUST NOT leak internal error details. `500 Internal Server Error` responses use a generic message (`"internal error"`). Detailed errors are logged server-side with the request ID for correlation. |

### 3.3 Constraints

| ID | Constraint |
|----|-----------|
| **CON-001** | Zero external dependencies — all new packages use stdlib only. No routers, no middleware libraries, no SSE libraries. |
| **CON-002** | Domain layer (`cpn/`) MUST NOT be modified to accommodate HTTP concerns. All adaptation happens in the driving adapter and application layer. |
| **CON-003** | Driving adapter (`internal/driving/httpapi/`) MUST NOT import driven adapter packages (`infra/`). It may import `cpn/` (domain types) and `internal/app/` (application layer). |
| **CON-004** | Application layer (`internal/app/`) MUST NOT import driving adapter (`internal/driving/`) or driven adapter (`infra/`) packages. It depends only on `cpn/` domain types and port interfaces. |

### 3.4 Guidelines

| ID | Guideline |
|----|-----------|
| **GUD-001** | Use `slog.Logger` with `slog.NewJSONHandler` for structured logging. Log fields: `method`, `path`, `status`, `duration`, `request_id`, `session_id`. |
| **GUD-002** | Use environment variables for server configuration. Supported variables: `LISTEN_ADDR`, `OPENROUTER_API_KEY`, `CORS_ORIGINS`, `READ_TIMEOUT`, `IDLE_TIMEOUT`, `SHUTDOWN_TIMEOUT`. |
| **GUD-003** | Middleware MUST use `func(http.Handler) http.Handler` signature for composability. Middleware is chained in the composition root or route registration. |
| **GUD-004** | SSE event types MUST mirror `cpn.EventType` string values for consistency. E.g., `cpn.EventTransitionFired` ("transition_fired") maps to SSE event type `transition_fired`. Two additional SSE-only types: `session_completed`, `session_failed`. |

### 3.5 Patterns

| ID | Pattern | Description |
|----|---------|-------------|
| **PAT-001** | Hub/Broker | SSE client connection management. `SSEBroker` maintains a map of session ID to client set. Clients subscribe on SSE connect, unsubscribe on disconnect. |
| **PAT-002** | Composition Root | All dependency wiring in `cmd/server/main.go`. Driven adapters, application service, and driving adapter are instantiated and connected here. |
| **PAT-003** | 202 Accepted + SSE Stream | `POST /messages` returns immediately with 202. The CPN processes the message asynchronously. Results stream to the client via the SSE connection. |
| **PAT-004** | Non-blocking Channel Send | SSE fan-out uses non-blocking sends to buffered client channels (128 capacity). If a client's buffer is full, the event is dropped and a warning is logged. Prevents slow clients from blocking CPN execution. |

---

## 4. Interfaces & Data Contracts

### 4.1 API Endpoints

Base path: `/api/v1`

#### 4.1.1 POST /api/v1/sessions

Create a new session bound to a root CPN instance.

**Request:**
```json
{
  "user_id": "string (required, non-empty)",
  "channel": "web | whatsapp | telegram (required)"
}
```

**Response 201 Created:**
```json
{
  "id": "string (hex-encoded session ID)",
  "user_id": "string",
  "channel": "web | whatsapp | telegram",
  "state": "idle | running | waiting | completed | failed",
  "created_at": "string (RFC 3339 timestamp)"
}
```

**Errors:**
- `400 Bad Request` — missing or invalid fields: `{"error": "user_id is required"}`, `{"error": "invalid channel: foo"}`

#### 4.1.2 GET /api/v1/sessions/{id}

Retrieve session state and conversation history.

**Response 200 OK:**
```json
{
  "id": "string",
  "user_id": "string",
  "channel": "web | whatsapp | telegram",
  "state": "idle | running | waiting | completed | failed",
  "created_at": "string (RFC 3339)",
  "messages": [
    {
      "id": "string",
      "role": "user | assistant | observer",
      "content": "string",
      "cpn_id": "string",
      "timestamp": "string (RFC 3339)"
    }
  ]
}
```

**Errors:**
- `404 Not Found` — `{"error": "session not found"}`

#### 4.1.3 GET /api/v1/sessions/{id}/events (SSE)

Long-lived SSE connection streaming CPN events and LLM chunks.

**Response Headers:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

**SSE Event Format:**
```
id: {monotonic_integer}
event: {event_type}
data: {json_payload}

```

**Event Types and Payloads:**

| Event Type | Data Schema | Source |
|-----------|-------------|--------|
| `stream_chunk` | `{"session_id": string, "cpn_id": string, "cpn_role": string, "content": string, "done": bool}` | `cpn.StreamChunk` |
| `transition_fired` | `{"session_id": string, "cpn_id": string, "cpn_depth": int, "transition_id": string, "transition_kind": string}` | `cpn.EventTransitionFired` |
| `subnet_started` | `{"session_id": string, "cpn_id": string, "cpn_depth": int, "cpn_role": string}` | `cpn.EventSubNetStarted` |
| `subnet_completed` | `{"session_id": string, "cpn_id": string, "cpn_depth": int, "cpn_role": string}` | `cpn.EventSubNetCompleted` |
| `subnet_failed` | `{"session_id": string, "cpn_id": string, "cpn_depth": int, "cpn_role": string, "error": string}` | `cpn.EventSubNetFailed` |
| `hitl_requested` | `{"session_id": string, "cpn_id": string, "transition_id": string, "cpn_role": string}` | `cpn.EventHITLRequested` |
| `hitl_resolved` | `{"session_id": string, "cpn_id": string, "transition_id": string, "action": string}` | `cpn.EventHITLResolved` |
| `mode_switch` | `{"session_id": string, "cpn_id": string, "from": string, "to": string}` | `cpn.EventModeSwitch` |
| `session_completed` | `{"session_id": string}` | Application layer (SSE-only) |
| `session_failed` | `{"session_id": string, "error": string}` | Application layer (SSE-only) |

**Heartbeat (every 15 seconds):**
```
: heartbeat

```

**Errors:**
- `404 Not Found` — session does not exist (returned before SSE headers are set)

#### 4.1.4 POST /api/v1/sessions/{id}/messages

Send a user message to the session. Returns immediately; CPN processes asynchronously.

**Request:**
```json
{
  "content": "string (required, non-empty)"
}
```

**Response 202 Accepted:**
```json
{
  "status": "accepted"
}
```

**Errors:**
- `400 Bad Request` — `{"error": "content is required"}`
- `404 Not Found` — `{"error": "session not found"}`
- `410 Gone` — `{"error": "session closed"}` (session in completed/failed state)

#### 4.1.5 POST /api/v1/sessions/{id}/hitl/{transitionID}

Resolve a pending HITL request by approving, rejecting, or revising.

**Request:**
```json
{
  "action": "approve | reject | revise (required)",
  "content": "string (optional, required when action is revise)"
}
```

**Response 200 OK:**
```json
{
  "status": "resolved"
}
```

**Errors:**
- `400 Bad Request` — `{"error": "invalid action: foo"}`, `{"error": "content required for revise action"}`
- `404 Not Found` — `{"error": "session not found"}`
- `409 Conflict` — `{"error": "no HITL pending for transition"}` (maps from `cpn.ErrNoHITLWaiting`)

#### 4.1.6 GET /api/v1/health

Health check endpoint.

**Response 200 OK:**
```json
{
  "status": "ok"
}
```

#### 4.1.7 GET /api/v1/version

Build and version information.

**Response 200 OK:**
```json
{
  "version": "string (semantic version or dev)",
  "commit": "string (git short SHA)",
  "built": "string (RFC 3339 build timestamp)"
}
```

Version values are injected via `-ldflags` at build time.

### 4.2 Common Error Response

All error responses use a consistent envelope:

```json
{
  "error": "string (human-readable, no internal details for 5xx)"
}
```

### 4.3 Common Response Headers

All responses include:
- `Content-Type: application/json` (except SSE endpoint)
- `X-Request-ID: {16-char-hex}` (from SEC-003)

### 4.4 Internal Interfaces

#### SessionService (Application Layer)

```go
// internal/app/session_service.go

type SessionService struct {
    sessions map[string]*cpn.Session
    mu       sync.RWMutex
    llm      cpn.LLMClient
    cost     cpn.CostProvider
    logger   *slog.Logger
    onEvent  func(sessionID string, evt cpn.Event)
}

func NewSessionService(llm cpn.LLMClient, cost cpn.CostProvider, logger *slog.Logger) *SessionService
func (s *SessionService) CreateSession(ctx context.Context, userID string, ch cpn.ChannelType) (*SessionInfo, error)
func (s *SessionService) GetSession(sessionID string) (*SessionInfo, error)
func (s *SessionService) SendMessage(ctx context.Context, sessionID, content string) error
func (s *SessionService) ResolveHITL(ctx context.Context, sessionID, transitionID string, resp cpn.HITLResponse) error
func (s *SessionService) StreamChannel(sessionID string) (<-chan cpn.StreamChunk, error)
func (s *SessionService) SetEventCallback(fn func(string, cpn.Event))
```

#### SSEBroker (Driving Adapter)

```go
// internal/driving/httpapi/sse_broker.go

type SSEBroker struct { /* per-session client maps, sync.RWMutex */ }

func NewSSEBroker(logger *slog.Logger) *SSEBroker
func (b *SSEBroker) Subscribe(sessionID string) (*sseClient, func())
func (b *SSEBroker) Publish(sessionID string, eventType string, data []byte)
```

#### HTTPChannelAdapter (Driving Adapter)

```go
// internal/driving/httpapi/channel_adapter.go
// Implements cpn.ChannelAdapter

type HTTPChannelAdapter struct { /* broker, incoming channel */ }

func (a *HTTPChannelAdapter) Send(ctx context.Context, chunk cpn.StreamChunk) error
func (a *HTTPChannelAdapter) Receive(ctx context.Context) (cpn.Message, error)
func (a *HTTPChannelAdapter) Channel() cpn.ChannelType
func (a *HTTPChannelAdapter) Inject(msg cpn.Message)  // called by POST /messages handler
```

---

## 5. Acceptance Criteria

### AC-01: Session Creation

```
GIVEN a running HTTP server
WHEN a client sends POST /api/v1/sessions with {"user_id": "u1", "channel": "web"}
THEN the server returns 201 with a JSON body containing id, user_id, channel, state="idle", and created_at
AND the session is retrievable via GET /api/v1/sessions/{id}
```

### AC-02: Session Retrieval

```
GIVEN a session "s1" exists with two messages in its history
WHEN a client sends GET /api/v1/sessions/s1
THEN the server returns 200 with session state and a messages array containing both messages
```

### AC-03: Session Not Found

```
GIVEN no session with ID "nonexistent" exists
WHEN a client sends GET /api/v1/sessions/nonexistent
THEN the server returns 404 with {"error": "session not found"}
```

### AC-04: Send Message (202 Accepted)

```
GIVEN a session "s1" exists in state "idle"
WHEN a client sends POST /api/v1/sessions/s1/messages with {"content": "hello"}
THEN the server returns 202 with {"status": "accepted"} without blocking
AND the CPN begins execution asynchronously
```

### AC-05: SSE Stream Receives StreamChunks

```
GIVEN a client is connected to GET /api/v1/sessions/s1/events
AND a message is sent to session "s1"
WHEN the CPN produces StreamChunk data from the LLM
THEN the SSE connection receives events with type "stream_chunk" containing the chunk content
AND each event has a monotonically increasing id field
```

### AC-06: SSE Stream Receives CPN Events

```
GIVEN a client is connected to GET /api/v1/sessions/s1/events
WHEN the CPN fires a transition
THEN the SSE connection receives an event with type "transition_fired"
```

### AC-07: SSE Heartbeat

```
GIVEN a client is connected to GET /api/v1/sessions/s1/events
WHEN 15 seconds pass with no events
THEN the SSE connection receives a heartbeat comment ": heartbeat\n\n"
```

### AC-08: SSE Client Disconnect Cleanup

```
GIVEN a client is connected to GET /api/v1/sessions/s1/events
WHEN the client closes the connection
THEN the server removes the client from the SSE broker
AND no goroutine leak occurs
```

### AC-09: Slow SSE Client Handling

```
GIVEN a slow SSE client whose buffer (128 events) is full
WHEN a new event is published to the session
THEN the event is dropped for the slow client
AND a warning is logged with the session ID and client identifier
AND other clients in the same session receive the event normally
```

### AC-10: HITL Resolution

```
GIVEN a session "s1" has a pending HITL request on transition "t1"
WHEN a client sends POST /api/v1/sessions/s1/hitl/t1 with {"action": "approve"}
THEN the server returns 200 with {"status": "resolved"}
AND the CPN resumes execution
AND an "hitl_resolved" event is emitted on the SSE stream
```

### AC-11: HITL No Pending Request

```
GIVEN a session "s1" has no pending HITL request on transition "t1"
WHEN a client sends POST /api/v1/sessions/s1/hitl/t1 with {"action": "approve"}
THEN the server returns 409 with {"error": "no HITL pending for transition"}
```

### AC-12: Graceful Shutdown

```
GIVEN the server is running with active SSE connections and in-flight requests
WHEN the process receives SIGTERM
THEN the server stops accepting new connections
AND waits for active connections to complete (up to the configured timeout, default 30s)
AND exits cleanly with status 0
```

### AC-13: CORS Preflight

```
GIVEN CORS_ORIGINS is set to "https://app.example.com"
WHEN a client sends OPTIONS /api/v1/sessions with Origin: https://app.example.com
THEN the server returns 204 with Access-Control-Allow-Origin: https://app.example.com
AND Access-Control-Allow-Headers includes Content-Type and Last-Event-ID
```

### AC-14: CORS Rejection

```
GIVEN CORS_ORIGINS is set to "https://app.example.com"
WHEN a client sends POST /api/v1/sessions with Origin: https://evil.com
THEN the response does NOT include Access-Control-Allow-Origin header
```

### AC-15: Request Body Size Limit

```
GIVEN a request body exceeding 1 MB
WHEN the client sends POST /api/v1/sessions
THEN the server returns 413 Request Entity Too Large
```

### AC-16: Error Response Privacy

```
GIVEN an unexpected internal error occurs during request processing
WHEN the error is returned to the client
THEN the response body is {"error": "internal error"} (no stack traces, no internal details)
AND the full error details are logged server-side with the request ID
```

### AC-17: Health Check

```
GIVEN a running HTTP server
WHEN a client sends GET /api/v1/health
THEN the server returns 200 with {"status": "ok"}
```

### AC-18: Version Endpoint

```
GIVEN the server was built with -ldflags setting version, commit, and built
WHEN a client sends GET /api/v1/version
THEN the server returns 200 with {"version": "...", "commit": "...", "built": "..."}
```

### AC-19: Request ID Propagation

```
GIVEN any API request
WHEN the server processes it
THEN the response includes X-Request-ID header with a 16-character hex string
AND all log entries for this request include the same request_id field
```

### AC-20: Invalid Channel Rejection

```
GIVEN a client sends POST /api/v1/sessions with {"user_id": "u1", "channel": "sms"}
THEN the server returns 400 with {"error": "invalid channel: sms"}
```

---

## 6. Test Automation Strategy

### 6.1 Unit Tests

- **Framework:** stdlib `testing` package with table-driven tests.
- **Race detection:** All tests run with `-race` flag.
- **Mocks:** `MockLLMClient` implementing `cpn.LLMClient` for deterministic LLM responses.
- **HTTP testing:** `httptest.ResponseRecorder` for handler-level tests.
- **Coverage target:** 80%+ line coverage.

**Unit test scope:**

| Package | Test Focus |
|---------|-----------|
| `internal/app/` | `SessionService` methods: create, get, send message, resolve HITL. Mock LLMClient and CostProvider. |
| `internal/driving/httpapi/` | Individual handlers: request parsing, response format, status codes, error cases. SSE writer formatting. Middleware behavior (request ID generation, CORS headers, recovery from panic). |

### 6.2 Integration Tests

- **Framework:** `httptest.NewServer` with real `SessionService` wired to mock `LLMClient`.
- **Scope:** Full request lifecycle — create session, send message, verify SSE events, resolve HITL.
- **SSE testing:** Connect to SSE endpoint, verify event delivery, heartbeat timing, client disconnect cleanup.

### 6.3 SSE-Specific Tests

| Scenario | Verification |
|----------|-------------|
| Event delivery | Published events reach subscribed clients with correct format. |
| Heartbeat | Heartbeat comment arrives within 15-second window. |
| Client disconnect | Client removal from broker, no goroutine leak (verify with `runtime.NumGoroutine`). |
| Slow client drop | Full buffer results in dropped event + log warning, no blocking. |
| Multiple clients | Events fan out to all clients subscribed to the same session. |
| Monotonic IDs | Event `id` fields increase strictly for each SSE connection. |

### 6.4 CI Integration

```makefile
make test    # go test -race -count=1 ./...
make lint    # golangci-lint run
make build-server  # CGO_ENABLED=0 go build ./cmd/server
```

---

## 7. Rationale & Context

### Why stdlib `net/http`?

Go 1.22 introduced enhanced routing with method patterns (`"POST /api/v1/sessions"`) and path variables (`{id}`). This eliminates the primary reason teams adopted third-party routers (chi, gorilla/mux). Using stdlib:

- Maintains the project's zero-dependency constraint (CON-001).
- Reduces supply-chain risk and dependency maintenance burden.
- Provides sufficient routing power for this API's 7 endpoints.
- Aligns with Go community consensus for moderate-sized APIs.

### Why SSE over WebSocket?

- **Simplicity:** SSE is a thin layer over HTTP; no upgrade handshake, no framing protocol.
- **Auto-reconnect:** Browsers natively reconnect SSE with `Last-Event-ID`.
- **HTTP-native:** Works through standard HTTP proxies, load balancers, and CDNs without special configuration.
- **Sufficient for this use case:** The server-to-client streaming pattern (LLM chunks, CPN events) is unidirectional. Client-to-server communication uses standard POST requests.
- **Debugging:** SSE is plain text; curl can consume it directly (`curl -N`).

### Why 202 Accepted + SSE Stream?

CPN execution is inherently asynchronous and potentially long-running (LLM inference, sub-CPN orchestration, HITL blocking). The 202 pattern:

- Decouples request acceptance from processing, preventing HTTP timeout issues.
- Allows the client to receive incremental results via the SSE stream.
- Matches the CPN engine's event-driven architecture naturally.

### Why Hub/Broker Pattern?

- **Per-session isolation:** Each session has its own client set, so publishing is O(clients-in-session) not O(total-clients).
- **Non-blocking fan-out:** Slow clients are dropped rather than blocking the CPN engine (PAT-004).
- **Clean lifecycle:** Subscribe on connect, unsubscribe on disconnect, no leaked resources.

---

## 8. Dependencies & External Integrations

### Runtime Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| Go stdlib | Go 1.22+ | Enhanced `net/http` routing, `http.ResponseController`, `slog`, `crypto/rand` |

### External Services

| Service | Integration Point | Notes |
|---------|------------------|-------|
| OpenRouter API | Driven adapter (`infra/openrouter/`) | Already implemented. `LLMClient` interface. API key via `OPENROUTER_API_KEY` env var. |

### Environment Variables

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `OPENROUTER_API_KEY` | Yes | — | LLM API authentication (SEC-001) |
| `LISTEN_ADDR` | No | `:8080` | Server bind address |
| `CORS_ORIGINS` | No | `*` | Comma-separated allowed origins (SEC-002) |
| `READ_TIMEOUT` | No | `10s` | HTTP read timeout |
| `IDLE_TIMEOUT` | No | `120s` | HTTP idle timeout |
| `SHUTDOWN_TIMEOUT` | No | `30s` | Graceful shutdown window (REQ-004) |

---

## 9. Examples & Edge Cases

### 9.1 SSE Event Stream Example

A connected client observing a session would see:

```
: heartbeat

id: 1
event: transition_fired
data: {"session_id":"abc123","cpn_id":"root-001","cpn_depth":0,"transition_id":"t-classify","transition_kind":"llm"}

id: 2
event: stream_chunk
data: {"session_id":"abc123","cpn_id":"root-001","cpn_role":"coordinator","content":"Hello","done":false}

id: 3
event: stream_chunk
data: {"session_id":"abc123","cpn_id":"root-001","cpn_role":"coordinator","content":" there!","done":false}

id: 4
event: stream_chunk
data: {"session_id":"abc123","cpn_id":"root-001","cpn_role":"coordinator","content":"","done":true}

: heartbeat

id: 5
event: session_completed
data: {"session_id":"abc123"}

```

### 9.2 Slow Client Handling

```go
// Non-blocking send with drop-on-full (PAT-004)
select {
case client.events <- eventBytes:
    // delivered
default:
    // buffer full — drop event, log warning
    b.logger.Warn("dropping event for slow SSE client",
        "session_id", sessionID,
        "event_type", eventType,
    )
}
```

### 9.3 Graceful Shutdown

```go
// cmd/server/main.go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Error("server error", "err", err)
    }
}()

<-ctx.Done()
logger.Info("shutting down", "timeout", shutdownTimeout)

shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
defer cancel()

if err := srv.Shutdown(shutdownCtx); err != nil {
    logger.Error("shutdown error", "err", err)
}
```

### 9.4 SSE Write Deadline Management

```go
// handler_sse.go — disable write deadline for long-lived SSE connections
rc := http.NewResponseController(w)
rc.SetWriteDeadline(time.Time{}) // no deadline
```

### 9.5 Edge Cases

| Edge Case | Behavior |
|----------|----------|
| SSE connect to nonexistent session | Return 404 JSON before setting SSE headers. |
| Send message to completed/failed session | Return 410 Gone. |
| HITL resolve with wrong transition ID | Return 409 Conflict (maps from `cpn.ErrNoHITLWaiting`). |
| Multiple SSE clients on same session | All receive the same events via broker fan-out. |
| SSE reconnect with `Last-Event-ID` | Server acknowledges but does not replay (Phase 1). Future: bounded ring buffer replay. |
| Server shutdown with active SSE | Context cancellation triggers SSE handler cleanup and connection close. |
| Request body exceeds 1 MB | `http.MaxBytesReader` returns error; handler returns 413. |
| Concurrent message sends to same session | `SessionService` is thread-safe; messages are queued via `HTTPChannelAdapter.incoming` channel. |
| CPN panics during execution | Recovery middleware catches panics in synchronous handlers. CPN goroutine panics are recovered within the goroutine and mapped to `session_failed` events. |

---

## 10. Validation Criteria

The specification is considered fully implemented when ALL of the following pass:

1. **Build:** `make build-server` produces a binary with zero compilation errors.
2. **Unit tests:** `go test -race -count=1 ./internal/app/... ./internal/driving/httpapi/...` passes with 80%+ coverage.
3. **Integration tests:** Full lifecycle test (create session, send message, receive SSE events, resolve HITL) passes.
4. **Lint:** `golangci-lint run` reports zero issues.
5. **Health check:** `curl localhost:8080/api/v1/health` returns `{"status":"ok"}`.
6. **Version endpoint:** `curl localhost:8080/api/v1/version` returns valid JSON with version, commit, built.
7. **Session lifecycle:** Create session (201), get session (200), send message (202), receive SSE stream_chunk events.
8. **SSE heartbeat:** Heartbeat comments arrive every ~15 seconds on idle SSE connections.
9. **Graceful shutdown:** `kill -SIGTERM <pid>` results in clean exit after draining connections.
10. **CORS:** Preflight OPTIONS returns correct headers for configured origins.
11. **Error privacy:** 500 responses contain `{"error":"internal error"}` only; no stack traces.
12. **Hexagonal compliance:** `go vet` confirms no import cycles. `internal/driving/httpapi/` does not import `infra/`. `internal/app/` does not import `internal/driving/` or `infra/`.
13. **Request ID:** All responses include `X-Request-ID` header; all log lines include matching `request_id`.
14. **Body limit:** Requests exceeding 1 MB are rejected with 413.

---

## 11. Related Specifications & References

### Internal

- `cpn/ports.go` — Port interfaces (`LLMClient`, `ChannelAdapter`, `CostProvider`)
- `cpn/session.go` — `Session`, `StreamChunk`, `Message`, `HITLResponse`, `ChannelType`
- `cpn/event.go` — `Event`, `EventType` constants
- `cpn/cpn.go` — `CPN` struct, `emit()`, `EventSink`
- `cpn/mode.go` — `Mode` (`mas`, `centaurian`), `State` (`idle`, `running`, `waiting`, `completed`, `failed`)
- `infra/openrouter/` — Driven adapter implementing `cpn.LLMClient`

### External

- [Go 1.22 Enhanced Routing](https://go.dev/blog/routing-enhancements) — Method patterns and path variables in `net/http`
- [MDN: Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) — SSE protocol specification
- [HTML Living Standard: SSE](https://html.spec.whatwg.org/multipage/server-sent-events.html) — Normative SSE specification
- [`http.ResponseController`](https://pkg.go.dev/net/http#ResponseController) — Go 1.20+ flush and deadline control
