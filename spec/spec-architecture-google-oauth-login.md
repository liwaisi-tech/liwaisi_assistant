---
title: Google OAuth Login - Full-Stack Authentication
version: 1.0
date_created: 2026-04-04
last_updated: 2026-04-04
owner: liwaisi
tags: [architecture, security, authentication, oauth, google, full-stack]
---

# Introduction

This specification defines the implementation of secure Google OAuth 2.0 authentication across the liwaisi_assistant full stack. The system currently has a client-side-only Google Sign-In implementation that extracts user info from JWTs but never validates tokens on the backend. All API routes are unprotected. This spec closes that gap by adding backend token verification, auth middleware, user management, and frontend token propagation.

## 1. Purpose & Scope

**Purpose**: Implement end-to-end authentication so that every API request is verified against a valid Google identity, preventing impersonation and unauthorized access.

**Scope**:
- Backend (Go): Google `id_token` verification, auth middleware, user entity, DB migration
- Frontend (React): Token persistence, Authorization header injection, SSE auth, token refresh
- Shared: Error contract for 401/403 responses

**Audience**: AI agents and developers implementing the auth layer.

**Assumptions**:
- Google Cloud project with OAuth 2.0 Client ID is already configured
- The backend follows hexagonal/ports-and-adapters architecture
- Dev-mode (no auth) must remain functional when `GOOGLE_CLIENT_ID` env var is unset
- No custom JWT issuance; the Google `id_token` is used directly as the Bearer token

## 2. Definitions

| Term | Definition |
|------|-----------|
| **GSI** | Google Sign-In / Google Identity Services - client-side JS library |
| **id_token** | JWT issued by Google containing user identity claims (email, name, picture, sub) |
| **Bearer token** | The Google `id_token` sent in the `Authorization: Bearer <token>` header |
| **JWKS** | JSON Web Key Set - Google's public keys for verifying `id_token` signatures |
| **sub** | Google's unique, stable user identifier (never changes, unlike email) |
| **CPN** | Coloured Petri Net - the domain execution engine |
| **SSE** | Server-Sent Events - real-time streaming protocol |
| **HITL** | Human-In-The-Loop - user approval gate in CPN execution |
| **Hexagonal Architecture** | Ports-and-adapters pattern separating domain from infrastructure |

## 3. Requirements, Constraints & Guidelines

### Authentication Requirements

- **REQ-001**: The backend MUST verify Google `id_token` signatures using Google's JWKS endpoint (`https://www.googleapis.com/oauth2/v3/certs`) with local key caching.
- **REQ-002**: The backend MUST validate `id_token` claims: `iss` (accounts.google.com or https://accounts.google.com), `aud` (matches configured client ID), `exp` (not expired).
- **REQ-003**: All API routes except `GET /api/v1/health` and `GET /api/v1/version` MUST require a valid Bearer token.
- **REQ-004**: The frontend MUST persist the Google `id_token` (credential string) and send it as `Authorization: Bearer <id_token>` on every API request.
- **REQ-005**: SSE connections MUST be authenticated via `?token=<id_token>` query parameter (EventSource does not support custom headers).
- **REQ-006**: The backend MUST create or update a user record upon first successful token verification (upsert by Google `sub`).
- **REQ-007**: The frontend MUST handle 401 responses by triggering Google re-authentication (prompt).
- **REQ-008**: The backend MUST extract the authenticated user identity from the verified token and inject it into the request context for downstream handlers.
- **REQ-009**: Session creation MUST use the verified `sub` from the token as `user_id`, NOT a client-supplied value.
- **REQ-010**: Session operations (get, delete, send message, SSE, HITL) MUST verify that the authenticated user owns the session.

### Security Requirements

- **SEC-001**: The `id_token` query parameter for SSE MUST be validated with the same rigor as the Authorization header.
- **SEC-002**: Token verification MUST use cryptographic signature validation (RS256), NOT the tokeninfo HTTP endpoint (to avoid per-request latency and external dependency).
- **SEC-003**: JWKS keys MUST be cached locally with a TTL of 24 hours and refreshed on signature verification failure (key rotation support).
- **SEC-004**: Failed authentication attempts MUST be logged with request ID, IP, and failure reason (but NOT the token itself).
- **SEC-005**: The `id_token` MUST NOT be logged, stored in the database, or included in error responses.

### Dev-Mode Requirements

- **DEV-001**: When `GOOGLE_CLIENT_ID` environment variable is NOT set, the auth middleware MUST be skipped entirely (pass-through).
- **DEV-002**: In dev-mode, requests without an Authorization header MUST be allowed, and a synthetic user context with `user_id=dev-user` MUST be injected.
- **DEV-003**: The frontend MUST continue to support the existing behavior: skip login screen and use `'dev-user'` when `VITE_GOOGLE_CLIENT_ID` is unset.

### Constraints

- **CON-001**: The backend MUST NOT issue its own JWTs or session tokens. The Google `id_token` is the sole authentication credential.
- **CON-002**: Auth components MUST follow the existing hexagonal architecture: token verification as a port interface, Google implementation as an infrastructure adapter.
- **CON-003**: The `users` table migration MUST be additive (new migration file, no modification of existing migrations).
- **CON-004**: The auth middleware MUST be inserted into the existing middleware chain in `middleware.go` after CORS and before any handler logic.
- **CON-005**: No new frontend dependencies. The Google `id_token` is a standard JWT; no library needed for sending it.

### Guidelines

- **GUD-001**: Use Google's `sub` claim (not email) as the stable user identifier. Email can change; `sub` is immutable.
- **GUD-002**: Cache the decoded token claims in the request context to avoid re-parsing in handlers.
- **GUD-003**: Log authentication events at `info` level (success) and `warn` level (failure).
- **GUD-004**: Return `401 Unauthorized` for missing/invalid/expired tokens. Return `403 Forbidden` for valid tokens that lack access to a specific resource (e.g., wrong session owner).

### Patterns

- **PAT-001**: Define a `TokenVerifier` port interface in the domain/application layer. Implement `GoogleTokenVerifier` in the infrastructure layer.
- **PAT-002**: Use Go's `context.Context` to propagate authenticated user identity from middleware to handlers via `context.WithValue`.
- **PAT-003**: Define a context key type and accessor functions: `UserFromContext(ctx) *AuthenticatedUser`.
- **PAT-004**: The auth middleware returns an `http.Handler` wrapper consistent with the existing `Chain()` pattern in `middleware.go`.

## 4. Interfaces & Data Contracts

### 4.1 Token Verifier Port (Backend)

```go
// Package: internal/auth (new package)

// AuthenticatedUser represents a verified user identity extracted from a token.
type AuthenticatedUser struct {
    Sub     string // Google's stable unique user ID
    Email   string // User's email address
    Name    string // Display name
    Picture string // Profile picture URL
}

// TokenVerifier is the port interface for token verification.
// Implementations: GoogleTokenVerifier (production), NoopTokenVerifier (dev-mode).
type TokenVerifier interface {
    // Verify validates the token string and returns the authenticated user.
    // Returns error if the token is invalid, expired, or has wrong audience.
    Verify(ctx context.Context, tokenString string) (*AuthenticatedUser, error)
}
```

### 4.2 Google Token Verifier Adapter (Backend)

```go
// Package: infra/googleauth (new package)

// GoogleTokenVerifier implements auth.TokenVerifier using Google JWKS.
type GoogleTokenVerifier struct {
    clientID string          // Expected audience claim
    jwksURL  string          // https://www.googleapis.com/oauth2/v3/certs
    cache    *jwksCache      // Cached public keys with TTL
}

func NewGoogleTokenVerifier(clientID string) *GoogleTokenVerifier
```

### 4.3 Auth Middleware (Backend)

```go
// Package: internal/driving/httpapi

// AuthMiddleware returns middleware that validates Bearer tokens.
// If verifier is nil (dev-mode), all requests pass through with a synthetic user.
func AuthMiddleware(verifier auth.TokenVerifier, logger *slog.Logger) func(http.Handler) http.Handler
```

**Middleware behavior**:
1. Extract token from `Authorization: Bearer <token>` header
2. If no header, check `?token=<token>` query parameter (for SSE)
3. If no token found, return `401 Unauthorized`
4. Call `verifier.Verify(ctx, token)`
5. If verification fails, return `401 Unauthorized` with JSON error body
6. Store `*AuthenticatedUser` in request context
7. Call next handler

### 4.4 User Table Migration (Backend)

```sql
-- Migration: 006_users.up.sql

CREATE TABLE users (
    id         TEXT PRIMARY KEY,          -- Google 'sub' claim
    email      TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    picture    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_users_email ON users (email);

-- Update sessions table to reference users
-- Note: NOT a foreign key constraint to avoid breaking existing sessions
ALTER TABLE sessions ADD COLUMN google_sub TEXT;
CREATE INDEX idx_sessions_google_sub ON sessions (google_sub);
```

### 4.5 User Repository Port (Backend)

```go
// Package: cpn/persist (extend existing interfaces file)

type UserRecord struct {
    ID        string    // Google 'sub'
    Email     string
    Name      string
    Picture   string
    CreatedAt time.Time
    UpdatedAt time.Time
}

type UserRepository interface {
    Upsert(ctx context.Context, user *UserRecord) error
    GetByID(ctx context.Context, id string) (*UserRecord, error)
    GetByEmail(ctx context.Context, email string) (*UserRecord, error)
}
```

### 4.6 HTTP Error Responses (Shared Contract)

```json
// 401 Unauthorized
{
    "error": "unauthorized",
    "message": "missing or invalid authentication token"
}

// 403 Forbidden
{
    "error": "forbidden",
    "message": "you do not have access to this session"
}
```

### 4.7 Frontend API Changes

```typescript
// services/api.ts - Updated request function

// Token getter function injected from AuthContext
let getToken: (() => string | null) | null = null;

export function setTokenGetter(fn: () => string | null) {
    getToken = fn;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const headers: Record<string, string> = {
        'Content-Type': 'application/json',
    };
    
    const token = getToken?.();
    if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    }
    
    const response = await fetch(`${BASE_URL}${path}`, {
        headers,
        ...options,
    });
    
    if (response.status === 401) {
        // Trigger re-authentication
        window.dispatchEvent(new Event('auth:expired'));
        throw new Error('Authentication expired');
    }
    
    // ... existing error handling
}
```

### 4.8 Frontend SSE Authentication

```typescript
// hooks/useSSE.ts - Updated EventSource URL

const url = token
    ? `${BASE_URL}/sessions/${sessionId}/events?token=${encodeURIComponent(token)}`
    : `${BASE_URL}/sessions/${sessionId}/events`;

const eventSource = new EventSource(url);
```

### 4.9 Frontend AuthContext Changes

```typescript
// contexts/AuthContext.tsx - Updated context value

interface AuthContextType {
    user: User | null;
    token: string | null;       // NEW: raw Google id_token
    isAuthenticated: boolean;
    logout: () => void;
}

// Store credential (id_token) in addition to decoded user info
// On Google callback: store both token and user data
// On 'auth:expired' event: clear state and re-prompt Google Sign-In
```

### 4.10 Session Creation Change

```typescript
// Frontend: Remove user_id from CreateSessionRequest body
// Backend derives user_id from the verified token (REQ-009)

// OLD: POST /api/v1/sessions { "user_id": "email@example.com", "channel": "web" }
// NEW: POST /api/v1/sessions { "channel": "web" }
// Backend extracts user identity from Authorization header
```

```go
// Backend handler change:
// OLD: req.UserID from request body
// NEW: user := auth.UserFromContext(r.Context()); userID = user.Sub
```

### 4.11 Updated Route Table

| Method | Path | Auth Required | Notes |
|--------|------|:---:|-------|
| GET | `/api/v1/health` | No | Health check |
| GET | `/api/v1/version` | No | Version info |
| GET | `/api/v1/billing/balance` | Yes | |
| POST | `/api/v1/sessions` | Yes | user_id from token |
| GET | `/api/v1/sessions/{id}` | Yes | ownership check |
| DELETE | `/api/v1/sessions/{id}` | Yes | ownership check |
| GET | `/api/v1/sessions/{id}/events` | Yes | token via query param |
| POST | `/api/v1/sessions/{id}/messages` | Yes | ownership check |
| POST | `/api/v1/sessions/{id}/hitl/{tid}` | Yes | ownership check |

## 5. Acceptance Criteria

### Authentication Flow

- **AC-001**: Given a user with a valid Google account, When they click "Sign in with Google" on the login screen, Then the frontend stores the `id_token` and user info, and all subsequent API requests include `Authorization: Bearer <id_token>`.
- **AC-002**: Given a request with a valid Bearer token, When the auth middleware processes it, Then the request proceeds with an `AuthenticatedUser` in the context and a user record is upserted in the database.
- **AC-003**: Given a request with an expired or invalid token, When the auth middleware processes it, Then a `401 Unauthorized` JSON response is returned and the request is not forwarded to the handler.
- **AC-004**: Given a request with no Authorization header and no `?token` query parameter, When the auth middleware processes it, Then a `401 Unauthorized` response is returned.

### Session Ownership

- **AC-005**: Given an authenticated user A, When user A attempts to access a session created by user B, Then a `403 Forbidden` response is returned.
- **AC-006**: Given an authenticated user, When they create a session via `POST /sessions`, Then the session's `user_id` is set to the verified `sub` from the token (not from the request body).

### SSE Authentication

- **AC-007**: Given a valid token passed as `?token=<id_token>` query parameter, When the SSE endpoint is requested, Then the connection is established and events stream normally.
- **AC-008**: Given no token or an invalid token on the SSE endpoint, When the connection is attempted, Then a `401 Unauthorized` response is returned before the SSE stream begins.

### Token Refresh

- **AC-009**: Given a frontend receiving a `401` response from any API call, When the `auth:expired` event fires, Then the Google Sign-In prompt is re-displayed and, upon success, the new token is stored and used for subsequent requests.

### Dev-Mode

- **AC-010**: Given `GOOGLE_CLIENT_ID` is NOT set on the backend, When any request arrives (with or without Authorization header), Then the request passes through with a synthetic `dev-user` context.
- **AC-011**: Given `VITE_GOOGLE_CLIENT_ID` is NOT set on the frontend, When the app loads, Then no login screen is shown and `'dev-user'` is used as the userId.

### User Management

- **AC-012**: Given the first successful authentication of a Google user, When the middleware verifies the token, Then a new row is inserted into the `users` table with `id=sub`, `email`, `name`, `picture`.
- **AC-013**: Given a returning Google user whose name or picture has changed, When they authenticate, Then the `users` table row is updated with the latest values and `updated_at` is refreshed.

## 6. Test Automation Strategy

### Test Levels

| Level | Scope | Framework |
|-------|-------|-----------|
| Unit (Backend) | TokenVerifier, middleware, context helpers | Go `testing` + table-driven tests |
| Unit (Frontend) | AuthContext, api.ts token injection | Vitest + React Testing Library |
| Integration (Backend) | Middleware chain with mock verifier, DB user upsert | Go `testing` + `httptest` + `pgxmock` |
| Integration (Frontend) | Login flow, 401 handling, SSE auth | Vitest + MSW (Mock Service Worker) |
| E2E | Full login → session → message flow | Playwright (future, out of scope for this spec) |

### Backend Test Strategy

1. **TokenVerifier Unit Tests**:
   - Valid token → returns AuthenticatedUser
   - Expired token → returns error
   - Wrong audience → returns error
   - Malformed token → returns error
   - JWKS cache refresh on unknown kid

2. **Auth Middleware Unit Tests**:
   - Valid Bearer header → next handler called with user in context
   - Valid query param token → next handler called with user in context
   - Missing token → 401 response
   - Invalid token → 401 response
   - Nil verifier (dev-mode) → pass-through with synthetic user

3. **Session Ownership Tests**:
   - User A creates session → User A can access it
   - User A creates session → User B gets 403
   - Dev-mode → no ownership check

4. **User Repository Tests**:
   - Insert new user → row created
   - Upsert existing user → row updated
   - Get by ID → returns correct user
   - Get by email → returns correct user

### Frontend Test Strategy

1. **AuthContext Tests**:
   - Google callback stores token and user
   - Token getter returns stored token
   - Logout clears token and user
   - `auth:expired` event triggers re-prompt

2. **API Service Tests**:
   - Requests include Authorization header when token exists
   - Requests omit Authorization header when no token
   - 401 response dispatches `auth:expired` event

3. **SSE Tests**:
   - EventSource URL includes `?token=` when token exists
   - EventSource URL has no token param when no token

### Coverage Requirements

- Backend: >= 80% line coverage on `internal/auth/` and auth middleware
- Frontend: >= 80% line coverage on `AuthContext.tsx` and `api.ts` changes

## 7. Rationale & Context

### Why Google `id_token` directly (no custom JWT)?

Issuing a custom JWT adds complexity (secret management, rotation, refresh tokens, token store) without meaningful benefit for this use case. The Google `id_token` has a 1-hour expiry, is cryptographically signed, and contains all needed claims. The frontend can re-acquire tokens via Google's silent re-auth (`auto_select: true`). This approach minimizes backend state and avoids a token exchange endpoint.

### Why `sub` instead of `email` as user ID?

Google's `sub` claim is immutable and unique per Google account. Emails can change (e.g., Google Workspace admin renames a user). Using `sub` prevents identity fragmentation if a user's email changes.

### Why JWKS verification instead of tokeninfo endpoint?

The `https://oauth2.googleapis.com/tokeninfo?id_token=...` endpoint adds ~100ms latency per request and creates an external dependency on every API call. Local JWKS verification is ~0.1ms after initial key fetch and works offline.

### Why query parameter for SSE auth?

The `EventSource` Web API does not support custom headers. The only alternatives are cookies (introduces CSRF concerns and complicates the stateless token model) or query parameters. The query parameter approach is widely used (e.g., Firebase, Supabase) and is acceptable because: (a) the token has a short TTL (1 hour), (b) HTTPS encrypts the URL in transit, (c) server logs should be configured to not log query parameters.

### Why no foreign key from sessions to users?

Existing sessions in the database have `user_id` values that don't correspond to the new `users` table (they were plain email strings or 'dev-user'). A foreign key constraint would break existing data. The `google_sub` column is added as an optional field for gradual migration.

## 8. Dependencies & External Integrations

### External Systems

- **EXT-001**: Google Identity Services (GSI) - Frontend JavaScript library for Google Sign-In button and credential response. Loaded via `<script src="https://accounts.google.com/gsi/client">`.
- **EXT-002**: Google JWKS Endpoint - `https://www.googleapis.com/oauth2/v3/certs` - Public keys for verifying `id_token` RS256 signatures. Cached locally with 24h TTL.

### Third-Party Services

- **SVC-001**: Google OAuth 2.0 - Requires a configured OAuth Client ID in Google Cloud Console. The client ID must have the correct authorized JavaScript origins and redirect URIs.

### Infrastructure Dependencies

- **INF-001**: PostgreSQL - New `users` table (migration 006). Existing `sessions` table altered to add `google_sub` column.
- **INF-002**: HTTPS - Required in production. Google `id_token` in query parameters MUST be encrypted in transit.

### Technology Platform Dependencies

- **PLT-001**: Go standard library `crypto/rsa`, `encoding/json`, `math/big` - For RS256 JWT signature verification. No external JWT library required.
- **PLT-002**: Go `net/http` - Auth middleware follows existing middleware pattern.

## 9. Examples & Edge Cases

### Example: Successful Authentication Flow

```
1. User clicks "Sign in with Google" on LoginScreen
2. Google GSI returns credential (id_token JWT)
3. Frontend stores: { token: "eyJhbG...", user: { email, name, picture } }
4. Frontend calls: POST /api/v1/sessions
   Headers: { Authorization: "Bearer eyJhbG..." }
   Body: { "channel": "web" }
5. Backend middleware:
   a. Extracts "eyJhbG..." from Authorization header
   b. Fetches Google JWKS (cached)
   c. Verifies RS256 signature
   d. Validates: iss, aud, exp claims
   e. Extracts: sub, email, name, picture
   f. Upserts user record in DB
   g. Stores AuthenticatedUser in request context
6. Handler creates session with user_id = sub from context
7. Returns 201 with session details
```

### Example: SSE Connection with Auth

```
1. Frontend establishes SSE:
   new EventSource("/api/v1/sessions/abc123/events?token=eyJhbG...")
2. Backend auth middleware:
   a. No Authorization header found
   b. Checks ?token query parameter → finds "eyJhbG..."
   c. Verifies token (same as header flow)
   d. Verifies session "abc123" belongs to authenticated user
3. SSE stream established, events flow normally
```

### Example: Token Expiration Handling

```
1. User has been active for 1+ hour
2. Google id_token expires (exp claim in the past)
3. Frontend calls POST /api/v1/sessions/{id}/messages
4. Backend returns: 401 { "error": "unauthorized", "message": "token expired" }
5. Frontend receives 401:
   a. Dispatches 'auth:expired' event
   b. AuthContext listener triggers Google re-prompt
   c. Google auto_select silently refreshes token (if session still valid)
   d. New token stored, retried request succeeds
6. If Google session expired too:
   a. Google shows interactive sign-in prompt
   b. User clicks to re-authenticate
   c. New token stored, app resumes
```

### Edge Case: Dev-Mode (No Google Client ID)

```
Backend: GOOGLE_CLIENT_ID not set
  → AuthMiddleware created with nil verifier
  → All requests pass through
  → Synthetic user injected: { Sub: "dev-user", Email: "dev@localhost", Name: "Developer" }
  → Sessions created with user_id = "dev-user"

Frontend: VITE_GOOGLE_CLIENT_ID not set
  → hasGoogleAuth = false
  → LoginScreen not rendered
  → userId = "dev-user"
  → No Authorization header sent
```

### Edge Case: Session Ownership Violation

```
1. User A (sub: "google-123") creates session "sess-abc"
2. User B (sub: "google-456") calls GET /api/v1/sessions/sess-abc
3. Backend:
   a. Token verified → user is "google-456"
   b. Session "sess-abc" has user_id = "google-123"
   c. Mismatch → returns 403 { "error": "forbidden", "message": "you do not have access to this session" }
```

### Edge Case: JWKS Key Rotation

```
1. Google rotates signing keys
2. Incoming token signed with new key (kid not in cache)
3. Verifier attempts verification → signature fails
4. Verifier refreshes JWKS cache from Google endpoint
5. Retries verification with new keys → succeeds
6. If still fails after refresh → token genuinely invalid → 401
```

## 10. Validation Criteria

1. All existing tests continue to pass (no regression).
2. Backend unit tests for TokenVerifier achieve >= 80% coverage.
3. Backend integration test: full middleware chain with mock JWKS server.
4. Frontend unit tests for AuthContext token management achieve >= 80% coverage.
5. Manual validation: Google Sign-In → session creation → message send → SSE stream → all authenticated.
6. Manual validation: Dev-mode (no env vars) → app works without login.
7. Manual validation: Modified `user_id` in request body is ignored; session uses `sub` from token.
8. Manual validation: Accessing another user's session returns 403.
9. Database migration 006 applies cleanly on existing database with data.

## 11. Related Specifications / Further Reading

- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md) - SSE streaming architecture (affected by SSE auth)
- [spec-design-sse-streaming-bugfixes.md](spec-design-sse-streaming-bugfixes.md) - SSE implementation details
- [Google Identity Services documentation](https://developers.google.com/identity/gsi/web)
- [Google OAuth 2.0 for Web Server Applications](https://developers.google.com/identity/protocols/oauth2)
- [Verifying Google ID tokens](https://developers.google.com/identity/gsi/web/guides/verify-google-id-tokens)
