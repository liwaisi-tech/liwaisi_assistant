package httpapi

// Admin toolbox aggregate read (Brae Toolbox Taxonomy REQ-010, AC-009).
//
// Returns a JSON array of ToolboxManifest derived from the tool registry. The
// endpoint sits behind AdminMiddleware (see routes.go) so anonymous callers
// receive 401 from AuthMiddleware before admin filtering runs; authenticated
// non-admins receive 403 from AdminMiddleware. The handler itself only runs
// after both checks have passed.

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// HandleAdminListToolboxes returns every toolbox grouping currently present in
// the tool registry. Response shape is a JSON array of ToolboxManifest.
// GET /api/v1/admin/toolboxes
func (h *Handlers) HandleAdminListToolboxes(w http.ResponseWriter, r *http.Request) {
	if h.ToolboxLister == nil {
		writeError(w, http.StatusServiceUnavailable, "toolbox aggregate not configured")
		return
	}
	boxes := h.ToolboxLister.Toolboxes(r.Context())
	if boxes == nil {
		boxes = []tools.ToolboxManifest{}
	}
	writeJSON(w, http.StatusOK, boxes)
}
