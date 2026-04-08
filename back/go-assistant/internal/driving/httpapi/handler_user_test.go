package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// fakeUserRepo is a minimal in-memory UserRepository for handler tests.
type fakeUserRepo struct {
	rec                  *persist.UserRecord
	updatePrefsCalls     []persist.UserPreferences
	completeOnboardingOK bool
}

func (f *fakeUserRepo) Upsert(_ context.Context, u *persist.UserRecord) error {
	f.rec = u
	return nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, _ string) (*persist.UserRecord, error) {
	if f.rec == nil {
		return nil, persist.ErrUserNotFound
	}
	return f.rec, nil
}

func (f *fakeUserRepo) GetByEmail(_ context.Context, _ string) (*persist.UserRecord, error) {
	return nil, persist.ErrUserNotFound
}

func (f *fakeUserRepo) UpdatePreferences(_ context.Context, _ string, prefs *persist.UserPreferences) error {
	f.updatePrefsCalls = append(f.updatePrefsCalls, *prefs)
	if f.rec == nil {
		f.rec = &persist.UserRecord{}
	}
	f.rec.PreferredLanguage = prefs.PreferredLanguage
	f.rec.PreferredModel = prefs.PreferredModel
	f.rec.ModelOverrides = prefs.ModelOverrides
	if prefs.RegionalVariant != "" {
		f.rec.RegionalVariant = prefs.RegionalVariant
	}
	return nil
}

func (f *fakeUserRepo) CompleteOnboarding(_ context.Context, _ string) error {
	f.completeOnboardingOK = true
	return nil
}

func TestHandleCompleteOnboarding_RegionalVariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		body               string
		wantStatus         int
		wantPersistedVar   string
		wantOnboardingDone bool
	}{
		{
			name:               "valid es-CO",
			body:               `{"preferred_language":"es","regional_variant":"es-CO","preferred_model":"x"}`,
			wantStatus:         http.StatusOK,
			wantPersistedVar:   "es-CO",
			wantOnboardingDone: true,
		},
		{
			name:               "valid en-GB",
			body:               `{"preferred_language":"en","regional_variant":"en-GB","preferred_model":"x"}`,
			wantStatus:         http.StatusOK,
			wantPersistedVar:   "en-GB",
			wantOnboardingDone: true,
		},
		{
			name:               "empty defaults to es-CO when language es",
			body:               `{"preferred_language":"es","regional_variant":"","preferred_model":"x"}`,
			wantStatus:         http.StatusOK,
			wantPersistedVar:   "es-CO",
			wantOnboardingDone: true,
		},
		{
			name:               "empty defaults to en-GB when language en",
			body:               `{"preferred_language":"en","preferred_model":"x"}`,
			wantStatus:         http.StatusOK,
			wantPersistedVar:   "en-GB",
			wantOnboardingDone: true,
		},
		{
			name:       "invalid pt-BR rejected",
			body:       `{"preferred_language":"es","regional_variant":"pt-BR","preferred_model":"x"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "uppercase ES-CO rejected",
			body:       `{"preferred_language":"es","regional_variant":"ES-CO","preferred_model":"x"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "injection rejected",
			body:       `{"preferred_language":"es","regional_variant":"es-CO'); DROP TABLE users;--","preferred_model":"x"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{}
			h := &Handlers{
				UserRepo: repo,
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/user/onboarding/complete", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user-1", Email: "u@x"}))
			rec := httptest.NewRecorder()

			h.HandleCompleteOnboarding(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantStatus != http.StatusOK {
				if len(repo.updatePrefsCalls) > 0 {
					t.Errorf("expected no persistence on rejected request, got %d calls", len(repo.updatePrefsCalls))
				}
				if repo.completeOnboardingOK {
					t.Errorf("expected onboarding NOT marked complete on rejected request")
				}
				return
			}

			if len(repo.updatePrefsCalls) != 1 {
				t.Fatalf("expected 1 UpdatePreferences call, got %d", len(repo.updatePrefsCalls))
			}
			if got := repo.updatePrefsCalls[0].RegionalVariant; got != tc.wantPersistedVar {
				t.Errorf("persisted regional_variant = %q, want %q", got, tc.wantPersistedVar)
			}
			if repo.completeOnboardingOK != tc.wantOnboardingDone {
				t.Errorf("CompleteOnboarding called=%v, want %v", repo.completeOnboardingOK, tc.wantOnboardingDone)
			}

			// Decode body for sanity.
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Errorf("invalid JSON response: %v", err)
			}
		})
	}
}
