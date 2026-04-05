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

// ── Personality Response Types ──────────────────────────────────────────────

// PersonalityResponse is the JSON response for personality endpoints.
type PersonalityResponse struct {
	UserID     string              `json:"user_id"`
	Principles []PrincipleResponse `json:"principles"`
	Hierarchy  []string            `json:"hierarchy"`
	Tensions   []TensionResponse   `json:"tensions"`
	Version    int                 `json:"version"`
	UpdatedAt  string              `json:"updated_at"`
}

// PrincipleResponse is a single principle in the personality.
type PrincipleResponse struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Rules       []string `json:"rules"`
}

// TensionResponse is a tension rule in the personality.
type TensionResponse struct {
	Between    [2]string `json:"between"`
	Friction   string    `json:"friction"`
	Resolution string    `json:"resolution"`
}

// ── Personality Request Types ───────────────────────────────────────────────

// UpdatePrincipleRequest is the body for PATCH /api/v1/personality/principles/{kind}.
type UpdatePrincipleRequest struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Rules       []string `json:"rules"`
}

// SetHierarchyRequest is the body for PUT /api/v1/personality/hierarchy.
type SetHierarchyRequest struct {
	Hierarchy []string `json:"hierarchy"`
}

// PreviewPersonalityRequest is the body for POST /api/v1/personality/preview.
type PreviewPersonalityRequest struct {
	Principles []PrincipleResponse `json:"principles"`
	Hierarchy  []string            `json:"hierarchy"`
	Tensions   []TensionResponse   `json:"tensions"`
}

// PreviewPersonalityResponse is the response for POST /api/v1/personality/preview.
type PreviewPersonalityResponse struct {
	SystemPrompt string `json:"system_prompt"`
}

// ── Handler ─────────────────────────────────────────────────────────────────

// HandleGetPersonality returns the user's personality or the default.
// GET /api/v1/personality
func (h *Handlers) HandleGetPersonality(w http.ResponseWriter, r *http.Request) {
	if h.PersonalityRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rec, err := h.PersonalityRepo.Get(r.Context(), user.Sub)
	if err != nil {
		h.Logger.Error("get personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if rec == nil {
		// No custom personality; return default.
		writeJSON(w, http.StatusOK, personalityToResponse(user.Sub, cpn.DefaultPersonality()))
		return
	}

	resp, err := personalityRecordToResponse(rec)
	if err != nil {
		h.Logger.Error("unmarshal personality record", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleUpdatePrinciple updates a single principle by kind.
// PATCH /api/v1/personality/principles/{kind}
func (h *Handlers) HandleUpdatePrinciple(w http.ResponseWriter, r *http.Request) {
	if h.PersonalityRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	kind := cpn.PrincipleKind(r.PathValue("kind"))
	if kind != cpn.PrincipleNucleo && kind != cpn.PrincipleConducta && kind != cpn.PrincipleEtica {
		writeError(w, http.StatusBadRequest, "invalid principle kind; must be nucleo, conducta, or etica")
		return
	}

	var req UpdatePrincipleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Load current personality or use default.
	p, err := h.loadPersonality(r, user.Sub)
	if err != nil {
		h.Logger.Error("load personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Find and merge-update the principle.
	pr := findPrinciple(p, kind)
	if pr == nil {
		writeError(w, http.StatusNotFound, "principle not found")
		return
	}

	if req.Title != nil {
		pr.Title = *req.Title
	}
	if req.Description != nil {
		pr.Description = *req.Description
	}
	if req.Rules != nil {
		pr.Rules = req.Rules
	}

	// Validate the update.
	if err := p.ValidatePrincipleUpdate(kind, *pr); err != nil {
		if errors.Is(err, cpn.ErrEticaViolation) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p.Version++
	p.UpdatedAt = time.Now()

	if err := h.savePersonality(r, p); err != nil {
		h.Logger.Error("save personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, personalityToResponse(user.Sub, p))
}

// HandleSetHierarchy replaces the principle hierarchy.
// PUT /api/v1/personality/hierarchy
func (h *Handlers) HandleSetHierarchy(w http.ResponseWriter, r *http.Request) {
	if h.PersonalityRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req SetHierarchyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Hierarchy) != 3 {
		writeError(w, http.StatusBadRequest, "hierarchy must have exactly 3 elements")
		return
	}

	// Load current personality or use default.
	p, err := h.loadPersonality(r, user.Sub)
	if err != nil {
		h.Logger.Error("load personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	p.Hierarchy = [3]cpn.PrincipleKind{
		cpn.PrincipleKind(req.Hierarchy[0]),
		cpn.PrincipleKind(req.Hierarchy[1]),
		cpn.PrincipleKind(req.Hierarchy[2]),
	}

	if err := p.ValidateHierarchy(); err != nil {
		if errors.Is(err, cpn.ErrEticaCannotBeLast) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p.Version++
	p.UpdatedAt = time.Now()

	if err := h.savePersonality(r, p); err != nil {
		h.Logger.Error("save personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, personalityToResponse(user.Sub, p))
}

// HandleResetPersonality deletes a user's personality and returns the default.
// DELETE /api/v1/personality
func (h *Handlers) HandleResetPersonality(w http.ResponseWriter, r *http.Request) {
	if h.PersonalityRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := h.PersonalityRepo.Delete(r.Context(), user.Sub); err != nil {
		h.Logger.Error("delete personality", "user_id", user.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, personalityToResponse(user.Sub, cpn.DefaultPersonality()))
}

// HandlePreviewPersonality renders a personality as a system prompt.
// POST /api/v1/personality/preview
func (h *Handlers) HandlePreviewPersonality(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req PreviewPersonalityRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p := requestToPersonality(user.Sub, &req)
	writeJSON(w, http.StatusOK, PreviewPersonalityResponse{
		SystemPrompt: p.AsSystemPrompt(),
	})
}

// ── Internal helpers ────────────────────────────────────────────────────────

// loadPersonality loads a user's personality from persistence, falling back to the default.
func (h *Handlers) loadPersonality(r *http.Request, userID string) (*cpn.Personality, error) {
	rec, err := h.PersonalityRepo.Get(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		p := cpn.DefaultPersonality()
		p.UserID = userID
		return p, nil
	}
	return personalityRecordToModel(rec)
}

// savePersonality marshals a Personality to a PersonalityRecord and saves it.
func (h *Handlers) savePersonality(r *http.Request, p *cpn.Personality) error {
	principlesJSON, err := json.Marshal(p.Principles)
	if err != nil {
		return err
	}
	hierarchyJSON, err := json.Marshal(p.Hierarchy)
	if err != nil {
		return err
	}
	tensionsJSON, err := json.Marshal(p.Tensions)
	if err != nil {
		return err
	}

	return h.PersonalityRepo.Save(r.Context(), &persist.PersonalityRecord{
		UserID:     p.UserID,
		Principles: principlesJSON,
		Hierarchy:  hierarchyJSON,
		Tensions:   tensionsJSON,
		Version:    p.Version,
		UpdatedAt:  p.UpdatedAt,
	})
}

// findPrinciple is a local helper that mirrors cpn.Personality.findPrinciple
// which is unexported.
func findPrinciple(p *cpn.Personality, kind cpn.PrincipleKind) *cpn.Principle {
	for i := range p.Principles {
		if p.Principles[i].Kind == kind {
			return &p.Principles[i]
		}
	}
	return nil
}

// personalityToResponse converts a cpn.Personality to the API response.
func personalityToResponse(userID string, p *cpn.Personality) PersonalityResponse {
	principles := make([]PrincipleResponse, len(p.Principles))
	for i, pr := range p.Principles {
		rules := pr.Rules
		if rules == nil {
			rules = []string{}
		}
		principles[i] = PrincipleResponse{
			Kind:        string(pr.Kind),
			Title:       pr.Title,
			Description: pr.Description,
			Rules:       rules,
		}
	}

	hierarchy := make([]string, len(p.Hierarchy))
	for i, h := range p.Hierarchy {
		hierarchy[i] = string(h)
	}

	tensions := make([]TensionResponse, len(p.Tensions))
	for i, t := range p.Tensions {
		tensions[i] = TensionResponse{
			Between:    [2]string{string(t.Between[0]), string(t.Between[1])},
			Friction:   t.Friction,
			Resolution: t.Resolution,
		}
	}

	return PersonalityResponse{
		UserID:     userID,
		Principles: principles,
		Hierarchy:  hierarchy,
		Tensions:   tensions,
		Version:    p.Version,
		UpdatedAt:  p.UpdatedAt.Format(time.RFC3339),
	}
}

// personalityRecordToResponse converts a persist.PersonalityRecord to the API response.
func personalityRecordToResponse(rec *persist.PersonalityRecord) (PersonalityResponse, error) {
	var principles []PrincipleResponse
	if err := json.Unmarshal(rec.Principles, &principles); err != nil {
		return PersonalityResponse{}, err
	}

	var hierarchy []string
	if err := json.Unmarshal(rec.Hierarchy, &hierarchy); err != nil {
		return PersonalityResponse{}, err
	}

	var tensions []TensionResponse
	if err := json.Unmarshal(rec.Tensions, &tensions); err != nil {
		return PersonalityResponse{}, err
	}

	return PersonalityResponse{
		UserID:     rec.UserID,
		Principles: principles,
		Hierarchy:  hierarchy,
		Tensions:   tensions,
		Version:    rec.Version,
		UpdatedAt:  rec.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// personalityRecordToModel converts a persist.PersonalityRecord to a cpn.Personality.
func personalityRecordToModel(rec *persist.PersonalityRecord) (*cpn.Personality, error) {
	var principles [3]cpn.Principle
	if err := json.Unmarshal(rec.Principles, &principles); err != nil {
		return nil, err
	}

	var hierarchy [3]cpn.PrincipleKind
	if err := json.Unmarshal(rec.Hierarchy, &hierarchy); err != nil {
		return nil, err
	}

	var tensions [3]cpn.TensionRule
	if err := json.Unmarshal(rec.Tensions, &tensions); err != nil {
		return nil, err
	}

	return &cpn.Personality{
		UserID:     rec.UserID,
		Principles: principles,
		Hierarchy:  hierarchy,
		Tensions:   tensions,
		Version:    rec.Version,
		UpdatedAt:  rec.UpdatedAt,
	}, nil
}

// requestToPersonality builds a cpn.Personality from a preview request.
func requestToPersonality(userID string, req *PreviewPersonalityRequest) *cpn.Personality {
	var principles [3]cpn.Principle
	for i := 0; i < 3 && i < len(req.Principles); i++ {
		principles[i] = cpn.Principle{
			Kind:        cpn.PrincipleKind(req.Principles[i].Kind),
			Title:       req.Principles[i].Title,
			Description: req.Principles[i].Description,
			Rules:       req.Principles[i].Rules,
		}
	}

	var hierarchy [3]cpn.PrincipleKind
	for i := 0; i < 3 && i < len(req.Hierarchy); i++ {
		hierarchy[i] = cpn.PrincipleKind(req.Hierarchy[i])
	}

	var tensions [3]cpn.TensionRule
	for i := 0; i < 3 && i < len(req.Tensions); i++ {
		tensions[i] = cpn.TensionRule{
			Between:    [2]cpn.PrincipleKind{cpn.PrincipleKind(req.Tensions[i].Between[0]), cpn.PrincipleKind(req.Tensions[i].Between[1])},
			Friction:   req.Tensions[i].Friction,
			Resolution: req.Tensions[i].Resolution,
		}
	}

	return &cpn.Personality{
		UserID:     userID,
		Principles: principles,
		Hierarchy:  hierarchy,
		Tensions:   tensions,
	}
}

