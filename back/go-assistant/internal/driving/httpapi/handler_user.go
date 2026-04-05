package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// ── User Response Types ────────────────────────────────────────────────────

// UserProfileResponse is the JSON response for GET /api/v1/user/profile.
type UserProfileResponse struct {
	ID                  string              `json:"id"`
	Email               string              `json:"email"`
	Name                string              `json:"name"`
	Picture             string              `json:"picture"`
	Preferences         UserPreferencesJSON `json:"preferences"`
	OnboardingCompleted bool                `json:"onboarding_completed"`
	CreatedAt           string              `json:"created_at"`
}

// UserPreferencesJSON is the JSON representation of user preferences.
type UserPreferencesJSON struct {
	PreferredLanguage string            `json:"preferred_language"`
	PreferredModel    string            `json:"preferred_model"`
	ModelOverrides    map[string]string `json:"model_overrides"`
}

// ── User Request Types ─────────────────────────────────────────────────────

// UpdatePreferencesRequest is the body for PUT /api/v1/user/preferences.
type UpdatePreferencesRequest struct {
	PreferredLanguage string            `json:"preferred_language"`
	PreferredModel    string            `json:"preferred_model"`
	ModelOverrides    map[string]string `json:"model_overrides"`
}

// CompleteOnboardingRequest is the body for POST /api/v1/user/onboarding/complete.
type CompleteOnboardingRequest struct {
	PreferredLanguage string            `json:"preferred_language"`
	PreferredModel    string            `json:"preferred_model"`
	ModelOverrides    map[string]string `json:"model_overrides"`
	PersonalityPreset string            `json:"personality_preset"`
}

// ── Handlers ───────────────────────────────────────────────────────────────

// HandleGetProfile returns the authenticated user's profile and preferences.
// GET /api/v1/user/profile
func (h *Handlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	if h.UserRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rec, err := h.UserRepo.GetByID(r.Context(), user.Sub)
	if errors.Is(err, persist.ErrUserNotFound) {
		// User authenticated but not yet in DB (first request after sign-in).
		// Return a synthetic profile indicating onboarding is needed.
		writeJSON(w, http.StatusOK, UserProfileResponse{
			ID:                  user.Sub,
			Email:               user.Email,
			Name:                user.Name,
			Picture:             user.Picture,
			Preferences:         UserPreferencesJSON{PreferredLanguage: "en", ModelOverrides: map[string]string{}},
			OnboardingCompleted: false,
			CreatedAt:           time.Now().Format(time.RFC3339),
		})
		return
	}
	if err != nil {
		h.Logger.Error("get user profile", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	overrides := rec.ModelOverrides
	if overrides == nil {
		overrides = map[string]string{}
	}

	writeJSON(w, http.StatusOK, UserProfileResponse{
		ID:      rec.ID,
		Email:   rec.Email,
		Name:    rec.Name,
		Picture: rec.Picture,
		Preferences: UserPreferencesJSON{
			PreferredLanguage: rec.PreferredLanguage,
			PreferredModel:    rec.PreferredModel,
			ModelOverrides:    overrides,
		},
		OnboardingCompleted: rec.OnboardingCompletedAt != nil,
		CreatedAt:           rec.CreatedAt.Format(time.RFC3339),
	})
}

// HandleUpdatePreferences updates the authenticated user's preferences.
// PUT /api/v1/user/preferences
func (h *Handlers) HandleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	if h.UserRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req UpdatePreferencesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.PreferredLanguage != "" && req.PreferredLanguage != "en" && req.PreferredLanguage != "es" {
		writeError(w, http.StatusBadRequest, "preferred_language must be 'en' or 'es'")
		return
	}

	prefs := &persist.UserPreferences{
		PreferredLanguage: req.PreferredLanguage,
		PreferredModel:    req.PreferredModel,
		ModelOverrides:    req.ModelOverrides,
	}

	if err := h.UserRepo.UpdatePreferences(r.Context(), user.Sub, prefs); err != nil {
		h.Logger.Error("update preferences", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleCompleteOnboarding marks onboarding as complete and saves preferences + optional personality preset.
// POST /api/v1/user/onboarding/complete
func (h *Handlers) HandleCompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	if h.UserRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req CompleteOnboardingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save preferences.
	lang := req.PreferredLanguage
	if lang == "" {
		lang = "en"
	}
	prefs := &persist.UserPreferences{
		PreferredLanguage: lang,
		PreferredModel:    req.PreferredModel,
		ModelOverrides:    req.ModelOverrides,
	}
	if err := h.UserRepo.UpdatePreferences(r.Context(), user.Sub, prefs); err != nil {
		h.Logger.Error("save onboarding preferences", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Apply personality preset if specified and persistence is available.
	if req.PersonalityPreset != "" && req.PersonalityPreset != "custom" && h.PersonalityRepo != nil {
		if preset, ok := cpn.PersonalityPresets[req.PersonalityPreset]; ok {
			p := preset // copy
			p.UserID = user.Sub
			p.Version = 1
			p.UpdatedAt = time.Now()

			principlesJSON, err := json.Marshal(p.Principles)
			if err != nil {
				h.Logger.Error("marshal personality preset", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			hierarchyJSON, err := json.Marshal(p.Hierarchy)
			if err != nil {
				h.Logger.Error("marshal personality preset hierarchy", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			tensionsJSON, err := json.Marshal(p.Tensions)
			if err != nil {
				h.Logger.Error("marshal personality preset tensions", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}

			if err := h.PersonalityRepo.Save(r.Context(), &persist.PersonalityRecord{
				UserID:     user.Sub,
				Principles: principlesJSON,
				Hierarchy:  hierarchyJSON,
				Tensions:   tensionsJSON,
				Version:    1,
				UpdatedAt:  p.UpdatedAt,
			}); err != nil {
				h.Logger.Error("save personality preset", "user_id", user.Sub, "preset", req.PersonalityPreset, "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
	}

	// Mark onboarding complete.
	if err := h.UserRepo.CompleteOnboarding(r.Context(), user.Sub); err != nil {
		h.Logger.Error("complete onboarding", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Welcome to Liwaisi!",
	})
}
