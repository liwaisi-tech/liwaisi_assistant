package cpn

import "context"

// sessionIDCtxKey is the private key under which a CPN's session ID is
// threaded through context.Context values so that infrastructure adapters
// (notably infra/host/gate) can correlate a ctx back to the owning
// interactive session without taking a dependency on the executor.
type sessionIDCtxKey struct{}

// WithSessionID returns a child context carrying id. An empty id returns
// parent unchanged so callers can use it unconditionally.
func WithSessionID(parent context.Context, id string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if id == "" {
		return parent
	}
	return context.WithValue(parent, sessionIDCtxKey{}, id)
}

// SessionIDFromContext extracts the session ID previously attached by
// WithSessionID. Returns "" when ctx carries no session ID (including
// nil ctx) — callers decide whether that is a hard error or a fallback.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionIDCtxKey{}).(string)
	return v
}
