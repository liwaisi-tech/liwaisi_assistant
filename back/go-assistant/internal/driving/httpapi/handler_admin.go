package httpapi

import (
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
)

// ── Admin Config API ────────────────────────────────────────────────────────

// AdminConfigResponse is the response for GET /api/v1/admin/config.
type AdminConfigResponse struct {
	Items         []config.ConfigItem `json:"items"`
	SetupRequired bool                `json:"setup_required"`
}

// AdminSetConfigRequest is the request body for PUT /api/v1/admin/config/{key}.
type AdminSetConfigRequest struct {
	Value string `json:"value"`
}

// PlatformStatusResponse is the response for GET /api/v1/admin/config/status.
type PlatformStatusResponse struct {
	Ready           bool     `json:"ready"`
	MissingRequired []string `json:"missing_required"`
}

// HandleListConfig returns all config items with their effective values and sources.
// GET /api/v1/admin/config
func (h *Handlers) HandleListConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config not available")
		return
	}

	items := h.ConfigProvider.List(r.Context())
	setupRequired, _ := h.ConfigProvider.SetupRequired()

	writeJSON(w, http.StatusOK, AdminConfigResponse{
		Items:         items,
		SetupRequired: setupRequired,
	})
}

// HandleSetConfig sets a config value.
// PUT /api/v1/admin/config/{key}
func (h *Handlers) HandleSetConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config not available")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	def := config.LookupConfigDef(key)
	if def == nil {
		writeError(w, http.StatusBadRequest, "unknown config key: "+key)
		return
	}

	var req AdminSetConfigRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	updatedBy := ""
	if user := auth.UserFromContext(r.Context()); user != nil {
		updatedBy = user.Email
	}

	entry := &config.ConfigEntry{
		Key:       key,
		Value:     req.Value,
		IsSecret:  def.IsSecret,
		UpdatedBy: updatedBy,
		UpdatedAt: time.Now(),
	}

	if err := h.ConfigProvider.Set(r.Context(), entry); err != nil {
		h.Logger.Error("set config", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleDeleteConfig removes a config value (reverts to env or default).
// DELETE /api/v1/admin/config/{key}
func (h *Handlers) HandleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config not available")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	if config.LookupConfigDef(key) == nil {
		writeError(w, http.StatusBadRequest, "unknown config key: "+key)
		return
	}

	if err := h.ConfigProvider.Delete(r.Context(), key); err != nil {
		h.Logger.Error("delete config", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleConfigStatus returns the platform readiness status (public, no auth).
// GET /api/v1/admin/config/status
func (h *Handlers) HandleConfigStatus(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeJSON(w, http.StatusOK, PlatformStatusResponse{Ready: false, MissingRequired: []string{"config_not_initialized"}})
		return
	}

	setupRequired, missing := h.ConfigProvider.SetupRequired()

	writeJSON(w, http.StatusOK, PlatformStatusResponse{
		Ready:           !setupRequired,
		MissingRequired: missing,
	})
}
