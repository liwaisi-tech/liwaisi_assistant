package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/driving/httpapi"
)

// mockVerifier is a test double for auth.TokenVerifier.
type mockVerifier struct {
	user *auth.AuthenticatedUser
	err  error
}

func (m *mockVerifier) Verify(_ context.Context, _ string) (*auth.AuthenticatedUser, error) {
	return m.user, m.err
}

// echoHandler writes the authenticated user's Sub from context.
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"sub": user.Sub})
	})
}

func TestAuthMiddleware_NilVerifier_DevMode(t *testing.T) {
	t.Parallel()

	handler := httpapi.AuthMiddleware(nil, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["sub"] != "dev-user" {
		t.Errorf("sub = %q; want %q", body["sub"], "dev-user")
	}
}

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	t.Parallel()

	verifier := &mockVerifier{
		user: &auth.AuthenticatedUser{
			Sub:   "google-123",
			Email: "user@example.com",
			Name:  "Test User",
		},
	}

	handler := httpapi.AuthMiddleware(verifier, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["sub"] != "google-123" {
		t.Errorf("sub = %q; want %q", body["sub"], "google-123")
	}
}

func TestAuthMiddleware_ValidQueryParamToken(t *testing.T) {
	t.Parallel()

	verifier := &mockVerifier{
		user: &auth.AuthenticatedUser{
			Sub:   "google-456",
			Email: "sse@example.com",
		},
	}

	handler := httpapi.AuthMiddleware(verifier, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/abc/events?token=valid-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["sub"] != "google-456" {
		t.Errorf("sub = %q; want %q", body["sub"], "google-456")
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	t.Parallel()

	verifier := &mockVerifier{}
	handler := httpapi.AuthMiddleware(verifier, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusUnauthorized)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unauthorized" {
		t.Errorf("error = %q; want %q", body["error"], "unauthorized")
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	t.Parallel()

	verifier := &mockVerifier{
		err: errors.New("invalid token"),
	}
	handler := httpapi.AuthMiddleware(verifier, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_BearerPrefixRequired(t *testing.T) {
	t.Parallel()

	verifier := &mockVerifier{
		user: &auth.AuthenticatedUser{Sub: "ignored"},
	}
	handler := httpapi.AuthMiddleware(verifier, nil)(echoHandler())

	// Authorization header without "Bearer " prefix.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Basic some-other-auth")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want %d (Basic auth should not be accepted)", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_HeaderTakesPrecedenceOverQuery(t *testing.T) {
	t.Parallel()

	callCount := 0
	verifier := &mockVerifier{
		user: &auth.AuthenticatedUser{Sub: "from-header"},
	}
	// Wrap to count calls.
	countingVerifier := &countingMockVerifier{inner: verifier, count: &callCount}

	handler := httpapi.AuthMiddleware(countingVerifier, nil)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
	}
	// Should only call Verify once (header token).
	if callCount != 1 {
		t.Errorf("Verify called %d times; want 1", callCount)
	}
}

type countingMockVerifier struct {
	inner auth.TokenVerifier
	count *int
}

func (c *countingMockVerifier) Verify(ctx context.Context, token string) (*auth.AuthenticatedUser, error) {
	*c.count++
	return c.inner.Verify(ctx, token)
}
