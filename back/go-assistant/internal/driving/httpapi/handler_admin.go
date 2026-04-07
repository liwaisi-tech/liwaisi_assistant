package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
)

// HandleListConfig returns all platform config entries with masked secrets.
// GET /api/v1/admin/config
func (h *Handlers) HandleListConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config provider not enabled")
		return
	}

	items := h.ConfigProvider.ListAll()
	missing := h.ConfigProvider.MissingRequired()

	writeJSON(w, http.StatusOK, map[string]any{
		"items":          items,
		"setup_required": len(missing) > 0,
	})
}

// HandleSetConfig sets a config value.
// PUT /api/v1/admin/config/{key}
func (h *Handlers) HandleSetConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config provider not enabled")
		return
	}

	key := r.PathValue("key")
	def := config.DefByKey(key)
	if def == nil {
		writeError(w, http.StatusBadRequest, "unknown config key: "+key)
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user := auth.UserFromContext(r.Context())
	updatedBy := ""
	if user != nil {
		updatedBy = user.Email
	}

	if err := h.ConfigProvider.Set(r.Context(), key, req.Value, updatedBy); err != nil {
		h.Logger.Error("set config", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.Logger.Info("config updated", "key", key, "by", updatedBy)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"key":    key,
		"source": "db",
	})
}

// HandleDeleteConfig removes a DB-stored config value (falls back to env/default).
// DELETE /api/v1/admin/config/{key}
func (h *Handlers) HandleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "config provider not enabled")
		return
	}

	key := r.PathValue("key")
	def := config.DefByKey(key)
	if def == nil {
		writeError(w, http.StatusBadRequest, "unknown config key: "+key)
		return
	}

	user := auth.UserFromContext(r.Context())
	updatedBy := ""
	if user != nil {
		updatedBy = user.Email
	}

	if err := h.ConfigProvider.Delete(r.Context(), key); err != nil {
		h.Logger.Error("delete config", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.Logger.Info("config deleted", "key", key, "by", updatedBy)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"key": key,
	})
}

// HandleConfigStatus returns platform readiness status.
// GET /api/v1/admin/config/status (public, no auth required)
func (h *Handlers) HandleConfigStatus(w http.ResponseWriter, r *http.Request) {
	if h.ConfigProvider == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":            true,
			"missing_required": []string{},
		})
		return
	}

	missing := h.ConfigProvider.MissingRequired()
	writeJSON(w, http.StatusOK, map[string]any{
		"ready":            len(missing) == 0,
		"missing_required": missing,
	})
}
