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
	"time"

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

func TestHandleUpdatePreferences(t *testing.T) {
	t.Parallel()

	type want struct {
		status       int
		variant      string
		model        string
		overrides    map[string]string
		persisted    bool
		persistEmpty bool // when true, ModelOverrides must be empty map (not nil)
	}

	cases := []struct {
		name   string
		body   string
		authed bool
		noRepo bool
		seed   *persist.UserRecord
		want   want
	}{
		{
			name:   "valid full payload",
			body:   `{"preferred_language":"es","regional_variant":"es-MX","preferred_model":"m1","model_overrides":{"classifier":"m2"}}`,
			authed: true,
			want: want{
				status:    http.StatusOK,
				variant:   "es-MX",
				model:     "m1",
				overrides: map[string]string{"classifier": "m2"},
				persisted: true,
			},
		},
		{
			name:   "valid lang only resolves default variant",
			body:   `{"preferred_language":"es"}`,
			authed: true,
			want: want{
				status:    http.StatusOK,
				variant:   "es-CO",
				persisted: true,
			},
		},
		{
			name:   "empty variant with english resolves to en default",
			body:   `{"preferred_language":"en","regional_variant":""}`,
			authed: true,
			want: want{
				status:    http.StatusOK,
				variant:   "en-GB",
				persisted: true,
			},
		},
		{
			name:   "invalid variant rejected",
			body:   `{"preferred_language":"es","regional_variant":"pt-BR"}`,
			authed: true,
			want:   want{status: http.StatusBadRequest},
		},
		{
			name:   "invalid language rejected",
			body:   `{"preferred_language":"fr"}`,
			authed: true,
			want:   want{status: http.StatusBadRequest},
		},
		{
			name: "empty overrides overwrites existing",
			body: `{"preferred_language":"es","regional_variant":"es-CO","preferred_model":"m1","model_overrides":{}}`,
			seed: &persist.UserRecord{
				ID:             "user-1",
				ModelOverrides: map[string]string{"classifier": "old"},
				PreferredModel: "old-model",
			},
			authed: true,
			want: want{
				status:       http.StatusOK,
				variant:      "es-CO",
				model:        "m1",
				overrides:    map[string]string{},
				persisted:    true,
				persistEmpty: true,
			},
		},
		{
			name: "empty preferred_model overwrites existing",
			body: `{"preferred_language":"es","regional_variant":"es-CO","preferred_model":""}`,
			seed: &persist.UserRecord{
				ID:             "user-1",
				PreferredModel: "old-model",
			},
			authed: true,
			want: want{
				status:    http.StatusOK,
				variant:   "es-CO",
				model:     "",
				persisted: true,
			},
		},
		{
			name:   "persistence disabled",
			body:   `{"preferred_language":"es"}`,
			authed: true,
			noRepo: true,
			want:   want{status: http.StatusServiceUnavailable},
		},
		{
			name: "unauthenticated",
			body: `{"preferred_language":"es"}`,
			want: want{status: http.StatusUnauthorized},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeUserRepo{}
			if tc.seed != nil {
				repo.rec = tc.seed
			}
			h := &Handlers{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if !tc.noRepo {
				h.UserRepo = repo
			}

			req := httptest.NewRequest(http.MethodPut, "/api/v1/user/preferences", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authed {
				req = req.WithContext(auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user-1", Email: "u@x"}))
			}
			rec := httptest.NewRecorder()

			h.HandleUpdatePreferences(rec, req)

			if rec.Code != tc.want.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want.status, rec.Body.String())
			}

			if !tc.want.persisted {
				if len(repo.updatePrefsCalls) != 0 {
					t.Errorf("expected no persistence, got %d calls", len(repo.updatePrefsCalls))
				}
				return
			}

			if len(repo.updatePrefsCalls) != 1 {
				t.Fatalf("expected 1 UpdatePreferences call, got %d", len(repo.updatePrefsCalls))
			}
			got := repo.updatePrefsCalls[0]
			if got.RegionalVariant != tc.want.variant {
				t.Errorf("regional_variant = %q, want %q", got.RegionalVariant, tc.want.variant)
			}
			if got.PreferredModel != tc.want.model {
				t.Errorf("preferred_model = %q, want %q", got.PreferredModel, tc.want.model)
			}
			if tc.want.persistEmpty {
				if got.ModelOverrides == nil || len(got.ModelOverrides) != 0 {
					t.Errorf("expected empty (non-nil) overrides map, got %#v", got.ModelOverrides)
				}
			} else if tc.want.overrides != nil {
				if len(got.ModelOverrides) != len(tc.want.overrides) {
					t.Errorf("overrides len = %d, want %d", len(got.ModelOverrides), len(tc.want.overrides))
				}
				for k, v := range tc.want.overrides {
					if got.ModelOverrides[k] != v {
						t.Errorf("overrides[%q] = %q, want %q", k, got.ModelOverrides[k], v)
					}
				}
			}
		})
	}
}

// TestHandleCompleteOnboarding_RequiresPreferredModel asserts REQ-ONB-003:
// POST /api/v1/user/onboarding/complete with empty (or missing) preferred_model
// MUST return HTTP 400 with error code "ONBOARDING_MODEL_REQUIRED".
func TestHandleCompleteOnboarding_RequiresPreferredModel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"empty preferred_model", `{"preferred_language":"en","preferred_model":""}`},
		{"missing preferred_model", `{"preferred_language":"en"}`},
	}

	for _, tc := range cases {
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

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if body["code"] != "ONBOARDING_MODEL_REQUIRED" {
				t.Errorf("code = %q, want %q", body["code"], "ONBOARDING_MODEL_REQUIRED")
			}
			if len(repo.updatePrefsCalls) > 0 {
				t.Errorf("preferences must NOT be persisted on rejection")
			}
			if repo.completeOnboardingOK {
				t.Error("onboarding must NOT be marked complete on rejection")
			}
		})
	}
}

// TestHandleGetProfile_LegacyUserRoutedToOnboarding asserts REQ-ONB-004 /
// AC-009: a user whose preferred_model is empty AND model_overrides are
// empty is treated as mid-onboarding even if OnboardingCompletedAt is set.
func TestHandleGetProfile_LegacyUserRoutedToOnboarding(t *testing.T) {
	t.Parallel()

	completedAt := time.Now().Add(-24 * time.Hour)
	repo := &fakeUserRepo{
		rec: &persist.UserRecord{
			ID:                    "user-1",
			Email:                 "u@x",
			CreatedAt:             completedAt.Add(-time.Hour),
			OnboardingCompletedAt: &completedAt,
			PreferredLanguage:     "en",
			PreferredModel:        "", // legacy: never picked a model
			ModelOverrides:        map[string]string{},
		},
	}
	h := &Handlers{
		UserRepo: repo,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/profile", nil)
	req = req.WithContext(auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user-1", Email: "u@x"}))
	rec := httptest.NewRecorder()

	h.HandleGetProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp UserProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.OnboardingCompleted {
		t.Error("legacy user with empty preferred_model must report onboarding_completed=false")
	}
}

// TestHandleGetProfile_UserWithPreferenceStaysCompleted asserts the inverse
// of REQ-ONB-004: a user with a non-empty preferred_model keeps the
// OnboardingCompletedAt-derived true flag.
func TestHandleGetProfile_UserWithPreferenceStaysCompleted(t *testing.T) {
	t.Parallel()

	completedAt := time.Now().Add(-24 * time.Hour)
	repo := &fakeUserRepo{
		rec: &persist.UserRecord{
			ID:                    "user-1",
			Email:                 "u@x",
			CreatedAt:             completedAt.Add(-time.Hour),
			OnboardingCompletedAt: &completedAt,
			PreferredLanguage:     "en",
			PreferredModel:        "google/gemma-4-31b-it",
			ModelOverrides:        map[string]string{},
		},
	}
	h := &Handlers{
		UserRepo: repo,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/profile", nil)
	req = req.WithContext(auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user-1", Email: "u@x"}))
	rec := httptest.NewRecorder()

	h.HandleGetProfile(rec, req)

	var resp UserProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.OnboardingCompleted {
		t.Error("user with non-empty preferred_model must report onboarding_completed=true")
	}
}

// TestHandleGetProfile_UserWithOnlyOverridesStaysCompleted asserts REQ-ONB-004:
// empty preferred_model but NON-empty model_overrides keeps the user completed
// (they expressed a preference, just per-role rather than globally).
func TestHandleGetProfile_UserWithOnlyOverridesStaysCompleted(t *testing.T) {
	t.Parallel()

	completedAt := time.Now().Add(-24 * time.Hour)
	repo := &fakeUserRepo{
		rec: &persist.UserRecord{
			ID:                    "user-1",
			Email:                 "u@x",
			CreatedAt:             completedAt.Add(-time.Hour),
			OnboardingCompletedAt: &completedAt,
			PreferredLanguage:     "en",
			PreferredModel:        "",
			ModelOverrides:        map[string]string{"classifier": "google/gemini-2.0-flash-001"},
		},
	}
	h := &Handlers{
		UserRepo: repo,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/profile", nil)
	req = req.WithContext(auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user-1", Email: "u@x"}))
	rec := httptest.NewRecorder()

	h.HandleGetProfile(rec, req)

	var resp UserProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.OnboardingCompleted {
		t.Error("user with non-empty model_overrides must report onboarding_completed=true")
	}
}
