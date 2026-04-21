package httpapi

// Admin authored-artefact REST API (GAP-10).
//
// Spec: spec-architecture-authored-artifact-provenance.md §5.
//
// Endpoints (all gated by AdminMiddleware in routes.go):
//   GET    /api/v1/admin/artefacts?forge_run_id=&set_id=&host_id=&limit=&offset=
//   GET    /api/v1/admin/artefacts/{id}
//   POST   /api/v1/admin/artefacts/sets/{set_id}/rollback
//   POST   /api/v1/admin/artefacts/sets/{set_id}/restore
//   POST   /api/v1/admin/artefacts/purge        body: {older_than_days?:int}
//
// The handlers hold their own dependencies (ledger + rollback service)
// rather than sitting on the giant Handlers struct so the wiring in
// cmd/server/main.go stays localised.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// ── Response DTOs ────────────────────────────────────────────────────────

type adminArtefactResponse struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	Classification string `json:"classification"`
	SetID          string `json:"set_id"`
	ForgeRunID     string `json:"forge_run_id,omitempty"`
	HostID         string `json:"host_id,omitempty"`
	FlowHash       string `json:"flow_hash,omitempty"`
	AuthoringCPNID string `json:"authoring_cpn_id,omitempty"`
	TransitionID   string `json:"transition_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Size           int64  `json:"size_bytes"`
	MIME           string `json:"mime,omitempty"`
	Mode           uint32 `json:"mode"`
	State          string `json:"state"`
	CreatedAt      string `json:"created_at"`
	QuarantinedAt  string `json:"quarantined_at,omitempty"`
	RestoredAt     string `json:"restored_at,omitempty"`
	PurgedAt       string `json:"purged_at,omitempty"`
	QuarantinePath string `json:"quarantine_path,omitempty"`
}

type adminArtefactListResponse struct {
	Items  []adminArtefactResponse `json:"items"`
	Total  int                     `json:"total"`
	Limit  int                     `json:"limit,omitempty"`
	Offset int                     `json:"offset,omitempty"`
}

type adminPurgeRequest struct {
	OlderThanDays *int `json:"older_than_days,omitempty"`
}

func artefactResponse(a persist.Artefact) adminArtefactResponse {
	out := adminArtefactResponse{
		ID:             a.ID,
		Path:           a.Path,
		Classification: string(a.Classification),
		SetID:          a.SetID,
		ForgeRunID:     a.ForgeRunID,
		HostID:         a.HostID,
		FlowHash:       a.FlowHash,
		AuthoringCPNID: a.AuthoringCPNID,
		TransitionID:   a.TransitionID,
		SessionID:      a.SessionID,
		SHA256:         a.SHA256,
		Size:           a.Size,
		MIME:           a.MIME,
		Mode:           a.Mode,
		State:          string(a.State),
		CreatedAt:      a.CreatedAt.UTC().Format(time.RFC3339Nano),
		QuarantinePath: a.QuarantinePath,
	}
	if a.QuarantinedAt != nil {
		out.QuarantinedAt = a.QuarantinedAt.UTC().Format(time.RFC3339Nano)
	}
	if a.RestoredAt != nil {
		out.RestoredAt = a.RestoredAt.UTC().Format(time.RFC3339Nano)
	}
	if a.PurgedAt != nil {
		out.PurgedAt = a.PurgedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// ── Ports the admin handler consumes ─────────────────────────────────────

// ArtefactRollbackService is the minimal surface the admin endpoint uses
// to trigger a rollback / restore. RollbackService in cpn/persist satisfies
// it unchanged.
type ArtefactRollbackService interface {
	Rollback(ctx context.Context, setID, actor string) error
	Restore(ctx context.Context, setID, actor string) error
}

// ArtefactPurgeRunner is the minimal surface the admin endpoint uses to
// force a purge. PurgeService in cpn/persist satisfies it.
type ArtefactPurgeRunner interface {
	Run(ctx context.Context, cutoff time.Time) (int, error)
}

// ── Handlers ─────────────────────────────────────────────────────────────

// HandleAdminListArtefacts returns authored artefact rows.
func (h *Handlers) HandleAdminListArtefacts(w http.ResponseWriter, r *http.Request) {
	if h.ArtefactLedger == nil {
		writeError(w, http.StatusServiceUnavailable, "artefact ledger not configured")
		return
	}
	q := r.URL.Query()
	filter := persist.ArtefactFilter{
		ForgeRunID: q.Get("forge_run_id"),
		SetID:      q.Get("set_id"),
	}
	if state := q.Get("state"); state != "" {
		filter.State = persist.ArtefactState(state)
	}
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if cs := q.Get("classification"); cs != "" {
		for _, c := range strings.Split(cs, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				filter.ClassificationIn = append(filter.ClassificationIn, persist.ArtefactClassification(c))
			}
		}
	}

	items, err := h.ArtefactLedger.ListByHost(r.Context(), q.Get("host_id"), filter)
	if err != nil {
		h.Logger.Error("admin list artefacts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]adminArtefactResponse, 0, len(items))
	for _, a := range items {
		out = append(out, artefactResponse(a))
	}
	writeJSON(w, http.StatusOK, adminArtefactListResponse{
		Items:  out,
		Total:  len(out),
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// HandleAdminGetArtefact returns a single artefact row.
func (h *Handlers) HandleAdminGetArtefact(w http.ResponseWriter, r *http.Request) {
	if h.ArtefactLedger == nil {
		writeError(w, http.StatusServiceUnavailable, "artefact ledger not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "id is required")
		return
	}
	a, err := h.ArtefactLedger.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, persist.ErrArtefactNotFound) {
			writeErrorCode(w, http.StatusNotFound, "not_found", "artefact not found")
			return
		}
		h.Logger.Error("admin get artefact", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, artefactResponse(a))
}

// HandleAdminRollbackArtefactSet triggers a rollback of every artefact
// sharing set_id.
func (h *Handlers) HandleAdminRollbackArtefactSet(w http.ResponseWriter, r *http.Request) {
	if h.ArtefactRollback == nil {
		writeError(w, http.StatusServiceUnavailable, "rollback service not configured")
		return
	}
	setID := r.PathValue("set_id")
	if setID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "set_id is required")
		return
	}
	actor := currentAdminEmail(r)
	if err := h.ArtefactRollback.Rollback(r.Context(), setID, actor); err != nil {
		h.writeArtefactError(w, "rollback", setID, err)
		return
	}
	h.Logger.Info("artefact rollback", "set_id", setID, "by", actor)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "set_id": setID, "action": "rollback"})
}

// HandleAdminRestoreArtefactSet restores a previously-quarantined set.
func (h *Handlers) HandleAdminRestoreArtefactSet(w http.ResponseWriter, r *http.Request) {
	if h.ArtefactRollback == nil {
		writeError(w, http.StatusServiceUnavailable, "rollback service not configured")
		return
	}
	setID := r.PathValue("set_id")
	if setID == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "set_id is required")
		return
	}
	actor := currentAdminEmail(r)
	if err := h.ArtefactRollback.Restore(r.Context(), setID, actor); err != nil {
		h.writeArtefactError(w, "restore", setID, err)
		return
	}
	h.Logger.Info("artefact restore", "set_id", setID, "by", actor)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "set_id": setID, "action": "restore"})
}

// HandleAdminPurgeArtefacts triggers the purge job out-of-schedule. Body is
// optional; when omitted the default grace window applies.
func (h *Handlers) HandleAdminPurgeArtefacts(w http.ResponseWriter, r *http.Request) {
	if h.ArtefactPurge == nil {
		writeError(w, http.StatusServiceUnavailable, "purge service not configured")
		return
	}
	var req adminPurgeRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
	}
	var cutoff time.Time
	if req.OlderThanDays != nil && *req.OlderThanDays > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(*req.OlderThanDays) * 24 * time.Hour)
	}
	n, err := h.ArtefactPurge.Run(r.Context(), cutoff)
	if err != nil {
		h.Logger.Error("admin purge", "error", err)
		writeError(w, http.StatusInternalServerError, "purge failed")
		return
	}
	h.Logger.Info("artefact purge", "rows", n, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purged": n})
}

// writeArtefactError translates persist sentinel errors into HTTP responses.
func (h *Handlers) writeArtefactError(w http.ResponseWriter, op, setID string, err error) {
	switch {
	case errors.Is(err, persist.ErrArtefactSetEmpty):
		writeErrorCode(w, http.StatusNotFound, "not_found", "artefact set is empty")
	case errors.Is(err, persist.ErrArtefactAlreadyRolledBack):
		writeErrorCode(w, http.StatusConflict, "already_rolled_back", err.Error())
	case errors.Is(err, persist.ErrArtefactNotQuarantined):
		writeErrorCode(w, http.StatusConflict, "not_quarantined", err.Error())
	default:
		h.Logger.Error("admin artefact", "op", op, "set_id", setID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// currentAdminEmail is defined in handler_admin_models.go — we reuse it
// here. The auth import is therefore unused in this file, but kept to keep
// the file self-documenting for future changes that touch auth context.
var _ = auth.UserFromContext
