// Package auth defines the authentication port for the hexagonal architecture.
// It provides the TokenVerifier interface (port) that infrastructure adapters
// implement, along with context helpers for propagating authenticated user
// identity through the request lifecycle.
package auth

import (
	"context"
	"errors"
)

// ── Sentinel Errors ────────────────────────────────────────────────────────

// ErrInvalidToken indicates the token is malformed or has an invalid signature.
var ErrInvalidToken = errors.New("invalid token")

// ErrTokenExpired indicates the token's exp claim is in the past.
var ErrTokenExpired = errors.New("token expired")

// ErrWrongAudience indicates the token's aud claim does not match the expected client ID.
var ErrWrongAudience = errors.New("wrong audience")

// ── Domain Types ───────────────────────────────────────────────────────────

// AuthenticatedUser represents a verified user identity extracted from a token.
type AuthenticatedUser struct {
	// Sub is Google's stable, unique user identifier (immutable).
	Sub string

	// Email is the user's email address (may change over time).
	Email string

	// Name is the user's display name.
	Name string

	// Picture is the URL of the user's profile picture.
	Picture string
}

// ── Port Interface ─────────────────────────────────────────────────────────

// TokenVerifier is the port interface for token verification.
// Production implementation: infra/googleauth.GoogleTokenVerifier
// Dev-mode implementation: NoopVerifier (always returns a synthetic dev-user).
type TokenVerifier interface {
	// Verify validates the token string and returns the authenticated user.
	// Returns ErrInvalidToken, ErrTokenExpired, or ErrWrongAudience on failure.
	Verify(ctx context.Context, tokenString string) (*AuthenticatedUser, error)
}

// ── Context Helpers ────────────────────────────────────────────────────────

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey string

const userKey contextKey = "authenticated_user"

// NewContext returns a new context with the authenticated user stored in it.
func NewContext(ctx context.Context, user *AuthenticatedUser) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext extracts the authenticated user from the context.
// Returns nil if no user is present (e.g., dev-mode or unauthenticated path).
func UserFromContext(ctx context.Context) *AuthenticatedUser {
	user, _ := ctx.Value(userKey).(*AuthenticatedUser)
	return user
}

// ── Noop Verifier (Dev-Mode) ───────────────────────────────────────────────

// NoopVerifier is a TokenVerifier that always succeeds with a synthetic dev-user.
// Used when GOOGLE_CLIENT_ID is not set (local development).
type NoopVerifier struct{}

// Compile-time interface check.
var _ TokenVerifier = (*NoopVerifier)(nil)

// Verify always returns a synthetic dev-user regardless of the token value.
func (n *NoopVerifier) Verify(_ context.Context, _ string) (*AuthenticatedUser, error) {
	return &AuthenticatedUser{
		Sub:   "dev-user",
		Email: "dev@localhost",
		Name:  "Developer",
	}, nil
}
