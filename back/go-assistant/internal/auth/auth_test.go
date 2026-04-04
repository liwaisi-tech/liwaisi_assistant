package auth_test

import (
	"context"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

func TestNewContext_and_UserFromContext(t *testing.T) {
	t.Parallel()

	user := &auth.AuthenticatedUser{
		Sub:     "google-123",
		Email:   "test@example.com",
		Name:    "Test User",
		Picture: "https://example.com/pic.jpg",
	}

	ctx := auth.NewContext(context.Background(), user)
	got := auth.UserFromContext(ctx)

	if got == nil {
		t.Fatal("UserFromContext returned nil; want non-nil user")
	}
	if got.Sub != user.Sub {
		t.Errorf("Sub = %q; want %q", got.Sub, user.Sub)
	}
	if got.Email != user.Email {
		t.Errorf("Email = %q; want %q", got.Email, user.Email)
	}
	if got.Name != user.Name {
		t.Errorf("Name = %q; want %q", got.Name, user.Name)
	}
	if got.Picture != user.Picture {
		t.Errorf("Picture = %q; want %q", got.Picture, user.Picture)
	}
}

func TestUserFromContext_empty(t *testing.T) {
	t.Parallel()

	got := auth.UserFromContext(context.Background())
	if got != nil {
		t.Errorf("UserFromContext on empty context = %v; want nil", got)
	}
}

func TestNoopVerifier(t *testing.T) {
	t.Parallel()

	verifier := &auth.NoopVerifier{}

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"random token", "some-random-string"},
		{"jwt-like token", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjMifQ.sig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user, err := verifier.Verify(context.Background(), tt.token)
			if err != nil {
				t.Fatalf("Verify(%q) error = %v; want nil", tt.token, err)
			}
			if user.Sub != "dev-user" {
				t.Errorf("Sub = %q; want %q", user.Sub, "dev-user")
			}
			if user.Email != "dev@localhost" {
				t.Errorf("Email = %q; want %q", user.Email, "dev@localhost")
			}
			if user.Name != "Developer" {
				t.Errorf("Name = %q; want %q", user.Name, "Developer")
			}
			if user.Picture != "" {
				t.Errorf("Picture = %q; want empty", user.Picture)
			}
		})
	}
}

func TestNoopVerifier_implements_TokenVerifier(t *testing.T) {
	t.Parallel()

	// Compile-time check is in auth.go; this confirms assignability at test time too.
	var v auth.TokenVerifier = &auth.NoopVerifier{}
	_, err := v.Verify(context.Background(), "any")
	if err != nil {
		t.Fatalf("NoopVerifier.Verify() error = %v; want nil", err)
	}
}
