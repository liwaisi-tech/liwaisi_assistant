package httpapi

// Admin model-registry REST API (workstream B3).
//
// Spec: spec-architecture-model-registry-and-a2ui-management.md §5 (REQ-API-*)
//
// All mutation endpoints in this file are wired behind AdminMiddleware in
// routes.go. They share the following invariants:
//
//   - Optimistic concurrency (REQ-API-004): PATCH/set-default/license-review
//     accept an `if_match` RFC3339Nano timestamp and reject with 409
//     when the current `updated_at` differs.
//
//   - License + lifecycle are NOT writable from POST /admin/models — the
//     registry adapter forces `lifecycle=registered, license=unreviewed`
//     on insert regardless of the request body (REQ-LIC-003 defense in depth).
//     Admins drive state by calling license-review / set-default afterwards.
//
//   - Error codes map 1:1 to the cpn sentinel errors so the A2UI flow can
//     branch on them deterministically (REQ-API-008):
//       ErrModelNotFound          → 404 model_not_found
//       ErrModelNotInvokable      → 409 model_not_invokable
//       ErrRegistryConflict       → 409 if_match_mismatch
//       ErrCannotDeleteDefault    → 409 cannot_delete_default
//       ErrInvalidInput           → 400 invalid_input

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// ── Response types ───────────────────────────────────────────────────────────

// ModelRegistryEntryResponse is the admin-API representation of one model row.
// It exposes every field the admin UI needs to render the model card,
// including derived IsProductDefault + Invokable so the client doesn't
// duplicate the REQ-GATE-001 predicate.
type ModelRegistryEntryResponse struct {
	ID               string           `json:"id"`
	RegistryID       string           `json:"registry_id"`
	Vendor           string           `json:"vendor"`
	Family           string           `json:"family"`
	Version          string           `json:"version"`
	Variant          *string          `json:"variant,omitempty"`
	DisplayName      string           `json:"display_name"`
	Description      string           `json:"description"`
	HuggingFaceID    *string          `json:"hugging_face_id,omitempty"`
	Modalities       cpn.Modalities   `json:"modalities"`
	Capabilities     cpn.Capabilities `json:"capabilities"`
	Context          cpn.ContextInfo  `json:"context"`
	Pricing          cpn.Pricing      `json:"pricing"`
	SupportedParams  []string         `json:"supported_params"`
	DefaultParams    map[string]any   `json:"default_params,omitempty"`
	License          cpn.License      `json:"license"`
	Lifecycle        cpn.Lifecycle    `json:"lifecycle"`
	Routes           []cpn.Route      `json:"routes"`
	SourceMetadata   map[string]any   `json:"source_metadata,omitempty"`
	IsProductDefault bool             `json:"is_product_default"`
	Invokable        bool             `json:"invokable"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
}

// AdminModelListResponse is the response for GET /api/v1/admin/models.
type AdminModelListResponse struct {
	Items []ModelRegistryEntryResponse `json:"items"`
	Total int                          `json:"total"`
	Page  int                          `json:"page"`
	Size  int                          `json:"size"`
}

// ModelRegistryEntryFromDomain maps the domain type to the API response.
func modelRegistryEntryResponse(m *cpn.ModelRegistryEntry) ModelRegistryEntryResponse {
	return ModelRegistryEntryResponse{
		ID:               m.ID,
		RegistryID:       m.RegistryID,
		Vendor:           m.Vendor,
		Family:           m.Family,
		Version:          m.Version,
		Variant:          m.Variant,
		DisplayName:      m.DisplayName,
		Description:      m.Description,
		HuggingFaceID:    m.HuggingFaceID,
		Modalities:       m.Modalities,
		Capabilities:     m.Capabilities,
		Context:          m.Context,
		Pricing:          m.Pricing,
		SupportedParams:  orEmptyStrings(m.SupportedParams),
		DefaultParams:    m.DefaultParams,
		License:          m.License,
		Lifecycle:        m.Lifecycle,
		Routes:           orEmptyRoutes(m.Routes),
		SourceMetadata:   m.SourceMetadata,
		IsProductDefault: m.IsProductDefault,
		Invokable:        m.Invokable(),
		CreatedAt:        m.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        m.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyRoutes(r []cpn.Route) []cpn.Route {
	if r == nil {
		return []cpn.Route{}
	}
	return r
}

// ── Request types ────────────────────────────────────────────────────────────

// RegisterModelRequest is the body for POST /api/v1/admin/models.
// The lifecycle + license fields are intentionally absent: the adapter
// forces the initial state to (registered, unreviewed) per REQ-LIC-003.
type RegisterModelRequest struct {
	RegistryID      string           `json:"registry_id"`
	Vendor          string           `json:"vendor"`
	Family          string           `json:"family"`
	Version         string           `json:"version"`
	Variant         *string          `json:"variant,omitempty"`
	DisplayName     string           `json:"display_name"`
	Description     string           `json:"description"`
	HuggingFaceID   *string          `json:"hugging_face_id,omitempty"`
	Modalities      cpn.Modalities   `json:"modalities"`
	Capabilities    cpn.Capabilities `json:"capabilities"`
	Context         cpn.ContextInfo  `json:"context"`
	Pricing         cpn.Pricing      `json:"pricing"`
	SupportedParams []string         `json:"supported_params,omitempty"`
	DefaultParams   map[string]any   `json:"default_params,omitempty"`
	LicenseKind     string           `json:"license_kind,omitempty"`    // metadata only — status is fixed to "unreviewed" on insert
	LicenseSPDXID   *string          `json:"license_spdx_id,omitempty"` // ditto
	LicenseName     *string          `json:"license_name,omitempty"`    // ditto
	LicenseURL      *string          `json:"license_url,omitempty"`     // ditto
	LicenseSource   string           `json:"license_source,omitempty"`  // "huggingface" | "manual"
	CommunitySlug   *string          `json:"community_slug,omitempty"`  // ditto
	Routes          []cpn.Route      `json:"routes,omitempty"`
	SourceMetadata  map[string]any   `json:"source_metadata,omitempty"`
}

// UpdateModelRequest is the body for PATCH /api/v1/admin/models/{registryID}.
// Every field is optional; nil pointers leave the column untouched.
// License and lifecycle are NOT updatable here — use the dedicated endpoints.
type UpdateModelRequest struct {
	IfMatch         string            `json:"if_match"`
	DisplayName     *string           `json:"display_name,omitempty"`
	Description     *string           `json:"description,omitempty"`
	HuggingFaceID   *string           `json:"hugging_face_id,omitempty"`
	Modalities      *cpn.Modalities   `json:"modalities,omitempty"`
	Capabilities    *cpn.Capabilities `json:"capabilities,omitempty"`
	Context         *cpn.ContextInfo  `json:"context,omitempty"`
	Pricing         *cpn.Pricing      `json:"pricing,omitempty"`
	SupportedParams *[]string         `json:"supported_params,omitempty"`
	DefaultParams   *map[string]any   `json:"default_params,omitempty"`
	Routes          *[]cpn.Route      `json:"routes,omitempty"`
	SourceMetadata  *map[string]any   `json:"source_metadata,omitempty"`
}

// SetDefaultRequest is the body for POST /api/v1/admin/models/set-default.
type SetDefaultRequest struct {
	RegistryID string `json:"registry_id"`
}

// LicenseReviewRequest is the body for POST /api/v1/admin/models/license-review.
type LicenseReviewRequest struct {
	RegistryID string `json:"registry_id"`
	Status     string `json:"status"`
	Note       string `json:"note,omitempty"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

// HandleAdminListModels returns a paginated list of model registry entries.
// GET /api/v1/admin/models?vendor=&lifecycle=&license=&invokable=&search=&page=&size=&order_by=
func (h *Handlers) HandleAdminListModels(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}

	q := r.URL.Query()
	filter := cpn.ModelListFilter{
		Vendor:         q.Get("vendor"),
		LifecycleState: q.Get("lifecycle"),
		LicenseStatus:  q.Get("license"),
		Search:         q.Get("search"),
	}
	if inv := q.Get("invokable"); inv != "" {
		b, err := strconv.ParseBool(inv)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "invokable must be true|false")
			return
		}
		filter.Invokable = &b
	}
	if p := q.Get("page"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "page must be a non-negative integer")
			return
		}
		filter.Page = n
	}
	if s := q.Get("size"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "size must be a non-negative integer")
			return
		}
		filter.PageSize = n
	}
	if ob := q["order_by"]; len(ob) > 0 {
		filter.OrderBy = ob
	}

	entries, total, err := h.ModelRegistry.ListAll(r.Context(), filter)
	if err != nil {
		h.Logger.Error("admin list models", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]ModelRegistryEntryResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, modelRegistryEntryResponse(e))
	}

	writeJSON(w, http.StatusOK, AdminModelListResponse{
		Items: items,
		Total: total,
		Page:  filter.Page,
		Size:  filter.PageSize,
	})
}

// HandleAdminGetModel returns one entry.
// GET /api/v1/admin/models/{registryID...}
func (h *Handlers) HandleAdminGetModel(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}
	registryID := r.PathValue("registryID")
	if registryID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id is required")
		return
	}

	entry, err := h.ModelRegistry.GetByID(r.Context(), registryID)
	if err != nil {
		if errors.Is(err, cpn.ErrModelNotFound) {
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
			return
		}
		h.Logger.Error("admin get model", "registry_id", registryID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, modelRegistryEntryResponse(entry))
}

// HandleAdminRegisterModel inserts a new row.
// POST /api/v1/admin/models
func (h *Handlers) HandleAdminRegisterModel(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}

	var req RegisterModelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if req.RegistryID == "" || req.Vendor == "" || req.DisplayName == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id, vendor, display_name are required")
		return
	}
	if len(req.Routes) == 0 {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "at least one route is required")
		return
	}

	licenseSource := req.LicenseSource
	if licenseSource == "" {
		licenseSource = "manual"
	}

	entry := &cpn.ModelRegistryEntry{
		RegistryID:      req.RegistryID,
		Vendor:          req.Vendor,
		Family:          req.Family,
		Version:         req.Version,
		Variant:         req.Variant,
		DisplayName:     req.DisplayName,
		Description:     req.Description,
		HuggingFaceID:   req.HuggingFaceID,
		Modalities:      req.Modalities,
		Capabilities:    req.Capabilities,
		Context:         req.Context,
		Pricing:         req.Pricing,
		SupportedParams: req.SupportedParams,
		DefaultParams:   req.DefaultParams,
		License: cpn.License{
			Kind:          req.LicenseKind,
			SPDXID:        req.LicenseSPDXID,
			CommunitySlug: req.CommunitySlug,
			Name:          req.LicenseName,
			URL:           req.LicenseURL,
			Source:        licenseSource,
			// Status + Lifecycle are overridden by the adapter per REQ-LIC-003.
		},
		Routes:         req.Routes,
		SourceMetadata: req.SourceMetadata,
	}

	if err := h.ModelRegistry.Insert(r.Context(), entry); err != nil {
		if errors.Is(err, cpn.ErrInvalidInput) {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		if errors.Is(err, cpn.ErrDuplicateRegistryID) {
			writeErrorCode(w, http.StatusConflict, "duplicate_registry_id", "registry_id already exists")
			return
		}
		h.Logger.Error("admin register model", "registry_id", req.RegistryID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	fresh, err := h.ModelRegistry.GetByID(r.Context(), req.RegistryID)
	if err != nil {
		h.Logger.Error("admin re-fetch after insert", "registry_id", req.RegistryID, "error", err)
		writeJSON(w, http.StatusCreated, map[string]string{"registry_id": req.RegistryID})
		return
	}
	actor := currentAdminEmail(r)
	h.Logger.Info("model registered", "registry_id", req.RegistryID, "by", actor)
	writeJSON(w, http.StatusCreated, modelRegistryEntryResponse(fresh))
}

// HandleAdminUpdateModel applies a partial update.
// PATCH /api/v1/admin/models/{registryID...}
func (h *Handlers) HandleAdminUpdateModel(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}
	registryID := r.PathValue("registryID")
	if registryID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id is required")
		return
	}

	var req UpdateModelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	ifMatch, err := parseIfMatch(req.IfMatch, r.Header.Get("If-Match"))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	current, err := h.ModelRegistry.GetByID(r.Context(), registryID)
	if err != nil {
		if errors.Is(err, cpn.ErrModelNotFound) {
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
			return
		}
		h.Logger.Error("admin update: get", "registry_id", registryID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if req.DisplayName != nil {
		current.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		current.Description = *req.Description
	}
	if req.HuggingFaceID != nil {
		current.HuggingFaceID = req.HuggingFaceID
	}
	if req.Modalities != nil {
		current.Modalities = *req.Modalities
	}
	if req.Capabilities != nil {
		current.Capabilities = *req.Capabilities
	}
	if req.Context != nil {
		current.Context = *req.Context
	}
	if req.Pricing != nil {
		current.Pricing = *req.Pricing
	}
	if req.SupportedParams != nil {
		current.SupportedParams = *req.SupportedParams
	}
	if req.DefaultParams != nil {
		current.DefaultParams = *req.DefaultParams
	}
	if req.Routes != nil {
		current.Routes = *req.Routes
	}
	if req.SourceMetadata != nil {
		current.SourceMetadata = *req.SourceMetadata
	}

	if err := h.ModelRegistry.Update(r.Context(), current, ifMatch); err != nil {
		switch {
		case errors.Is(err, cpn.ErrModelNotFound):
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
		case errors.Is(err, cpn.ErrRegistryConflict):
			writeErrorCode(w, http.StatusConflict, "if_match_mismatch", "row modified by another writer; refetch and retry")
		case errors.Is(err, cpn.ErrInvalidInput):
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		default:
			h.Logger.Error("admin update", "registry_id", registryID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	fresh, err := h.ModelRegistry.GetByID(r.Context(), registryID)
	if err != nil {
		h.Logger.Error("admin re-fetch after update", "registry_id", registryID, "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"registry_id": registryID})
		return
	}
	h.Logger.Info("model updated", "registry_id", registryID, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusOK, modelRegistryEntryResponse(fresh))
}

// HandleAdminDeleteModel removes a row (cannot delete the product default).
// DELETE /api/v1/admin/models/{registryID...}
func (h *Handlers) HandleAdminDeleteModel(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}
	registryID := r.PathValue("registryID")
	if registryID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id is required")
		return
	}

	if err := h.ModelRegistry.Delete(r.Context(), registryID); err != nil {
		switch {
		case errors.Is(err, cpn.ErrModelNotFound):
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
		case errors.Is(err, cpn.ErrCannotDeleteDefault):
			writeErrorCode(w, http.StatusConflict, "cannot_delete_default", "reassign the product default first")
		default:
			h.Logger.Error("admin delete", "registry_id", registryID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.Logger.Info("model deleted", "registry_id", registryID, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registry_id": registryID})
}

// HandleAdminSetDefault atomically promotes a model to product default.
// POST /api/v1/admin/models/set-default
func (h *Handlers) HandleAdminSetDefault(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}

	var req SetDefaultRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if req.RegistryID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id is required")
		return
	}

	actor := currentAdminEmail(r)
	if err := h.ModelRegistry.SetProductDefault(r.Context(), req.RegistryID, actor); err != nil {
		switch {
		case errors.Is(err, cpn.ErrModelNotFound):
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
		case errors.Is(err, cpn.ErrModelNotInvokable):
			writeErrorCode(w, http.StatusConflict, "model_not_invokable", "model cannot be default — license must be approved first")
		case errors.Is(err, cpn.ErrInvalidInput):
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		default:
			h.Logger.Error("admin set-default", "registry_id", req.RegistryID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	fresh, err := h.ModelRegistry.GetByID(r.Context(), req.RegistryID)
	if err != nil {
		h.Logger.Error("admin re-fetch after set-default", "registry_id", req.RegistryID, "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"registry_id": req.RegistryID})
		return
	}
	h.Logger.Info("product default reassigned", "registry_id", req.RegistryID, "by", actor)
	writeJSON(w, http.StatusOK, modelRegistryEntryResponse(fresh))
}

// HandleAdminLicenseReview records a license decision.
// POST /api/v1/admin/models/license-review
func (h *Handlers) HandleAdminLicenseReview(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "model registry not enabled")
		return
	}

	var req LicenseReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if req.RegistryID == "" || req.Status == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "registry_id and status are required")
		return
	}
	if !isValidLicenseStatus(req.Status) {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "invalid license status")
		return
	}

	actor := currentAdminEmail(r)
	review := cpn.LicenseReview{
		Status:     req.Status,
		ReviewerID: actor,
		Note:       req.Note,
	}
	if err := h.ModelRegistry.SetLicenseReview(r.Context(), req.RegistryID, review); err != nil {
		switch {
		case errors.Is(err, cpn.ErrModelNotFound):
			writeErrorCode(w, http.StatusNotFound, "model_not_found", "model not found")
		case errors.Is(err, cpn.ErrInvalidInput):
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		default:
			h.Logger.Error("admin license-review", "registry_id", req.RegistryID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	fresh, err := h.ModelRegistry.GetByID(r.Context(), req.RegistryID)
	if err != nil {
		h.Logger.Error("admin re-fetch after license-review", "registry_id", req.RegistryID, "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"registry_id": req.RegistryID})
		return
	}
	h.Logger.Info("license reviewed", "registry_id", req.RegistryID, "status", req.Status, "by", actor)
	writeJSON(w, http.StatusOK, modelRegistryEntryResponse(fresh))
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// parseIfMatch accepts an RFC3339Nano timestamp from either the body
// `if_match` field or the `If-Match` HTTP header and returns a cpn.UpdatedAt.
// Body wins over header when both are supplied.
func parseIfMatch(body, header string) (cpn.UpdatedAt, error) {
	raw := body
	if raw == "" {
		raw = header
	}
	if raw == "" {
		return cpn.UpdatedAt{}, errIfMatchRequired
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		// Try RFC3339 (no sub-second) as a fallback.
		t, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return cpn.UpdatedAt{}, errInvalidIfMatch
		}
	}
	return cpn.UpdatedAt(t.UTC()), nil
}

var (
	errIfMatchRequired = errors.New("if_match is required (RFC3339Nano timestamp)")
	errInvalidIfMatch  = errors.New("if_match must be an RFC3339Nano timestamp")
)

// currentAdminEmail returns the caller's admin email from the auth context,
// falling back to "unknown" in dev-mode or when the header is stripped.
func currentAdminEmail(r *http.Request) string {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		return "unknown"
	}
	return user.Email
}

// isValidLicenseStatus gates the license-review input against the enum.
func isValidLicenseStatus(s string) bool {
	switch s {
	case cpn.LicenseUnreviewed,
		cpn.LicenseReviewInProgress,
		cpn.LicenseApprovedCommercial,
		cpn.LicenseApprovedNonCommerc,
		cpn.LicenseRestricted,
		cpn.LicenseBlocked,
		cpn.LicenseUnknown:
		return true
	}
	return false
}
