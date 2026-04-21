package httpapi

// Admin host-policy REST API (GAP-6).
//
// Spec: spec/spec-architecture-host-gate-security-policy.md §4.
//
// The admin endpoints let operators inspect and edit the currently-loaded
// HostPolicy, browse the gate-decision audit log, and review/revoke
// first-run ledger entries. All routes are admin-gated by the existing
// AdminMiddleware, mirroring /api/v1/admin/models.

import (
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host/gate"
)

// HandleHostPolicyGet returns the current policy as YAML.
// GET /api/v1/admin/host/policy
func (h *Handlers) HandleHostPolicyGet(w http.ResponseWriter, r *http.Request) {
	if h.HostPolicy == nil {
		writeError(w, http.StatusServiceUnavailable, "host policy not enabled")
		return
	}
	p := h.HostPolicy.Get()
	if p == nil {
		writeError(w, http.StatusNotFound, "no policy loaded")
		return
	}
	out, err := p.ToYAML()
	if err != nil {
		h.Logger.Error("marshal policy", "error", err)
		writeError(w, http.StatusInternalServerError, "marshal error")
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	_, _ = w.Write(out)
}

// HandleHostPolicyPut replaces the policy atomically.
// PUT /api/v1/admin/host/policy  (body: raw YAML document)
func (h *Handlers) HandleHostPolicyPut(w http.ResponseWriter, r *http.Request) {
	if h.HostPolicy == nil {
		writeError(w, http.StatusServiceUnavailable, "host policy not enabled")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	p, err := gate.LoadFromBytes(body)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_policy", err.Error())
		return
	}
	h.HostPolicy.Put(p)
	h.Logger.Info("host policy reloaded", "by", currentAdminEmail(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// HandleGateDecisionsList returns the most recent audit rows.
// GET /api/v1/admin/host/gate-decisions?limit=100
func (h *Handlers) HandleGateDecisionsList(w http.ResponseWriter, r *http.Request) {
	if h.GateDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "gate decisions store not enabled")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "limit must be 1..1000")
			return
		}
		limit = n
	}
	recs, err := h.GateDecisions.List(r.Context(), limit)
	if err != nil {
		h.Logger.Error("list gate decisions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": mapDecisions(recs)})
}

// HandleFirstRunList returns the first-run ledger.
// GET /api/v1/admin/host/first-run-ledger?limit=100
func (h *Handlers) HandleFirstRunList(w http.ResponseWriter, r *http.Request) {
	if h.FirstRun == nil {
		writeError(w, http.StatusServiceUnavailable, "first-run ledger not enabled")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "limit must be 1..1000")
			return
		}
		limit = n
	}
	entries, err := h.FirstRun.List(r.Context(), limit)
	if err != nil {
		h.Logger.Error("list first-run", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": mapFirstRun(entries)})
}

// HandleFirstRunRevoke flips a ledger row to revoked.
// POST /api/v1/admin/host/first-run-ledger/{sha}/revoke
func (h *Handlers) HandleFirstRunRevoke(w http.ResponseWriter, r *http.Request) {
	if h.FirstRun == nil {
		writeError(w, http.StatusServiceUnavailable, "first-run ledger not enabled")
		return
	}
	sha := r.PathValue("sha")
	if sha == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "sha required")
		return
	}
	hostID := r.URL.Query().Get("host")
	if hostID == "" {
		// Gate exposes its HostID; callers may omit to revoke on the
		// local host.
		hostID = hostHostID()
	}
	if err := h.FirstRun.Revoke(r.Context(), hostID, sha); err != nil {
		h.Logger.Error("revoke first-run", "sha", sha, "error", err)
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	h.Logger.Info("first-run revoked", "sha", sha, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "sha": sha})
}

// mapDecisions shapes GateDecisionRecord for JSON output.
func mapDecisions(recs []*persist.GateDecisionRecord) []map[string]any {
	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		row := map[string]any{
			"id":           r.ID,
			"session_id":   r.SessionID,
			"op_kind":      r.OpKind,
			"command_hash": r.CommandHash,
			"path":         r.Path,
			"sandbox":      r.Sandbox,
			"decision":     r.Decision,
			"reason":       r.Reason,
			"risk_band":    r.RiskBand,
			"decided_at":   r.DecidedAt,
		}
		if r.HITLResponseID != nil {
			row["hitl_response_id"] = *r.HITLResponseID
		}
		out = append(out, row)
	}
	return out
}

func mapFirstRun(entries []*persist.FirstRunLedgerEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row := map[string]any{
			"id":            e.ID,
			"host_id":       e.HostID,
			"binary_path":   e.BinaryPath,
			"binary_sha256": e.BinarySHA256,
			"first_seen":    e.FirstSeen,
			"revoked":       e.Revoked,
		}
		if e.FirstApprovedBy != "" {
			row["first_approved_by"] = e.FirstApprovedBy
		}
		if e.FirstApprovedAt != nil {
			row["first_approved_at"] = *e.FirstApprovedAt
		}
		out = append(out, row)
	}
	return out
}

// hostHostID returns the local hostname or "localhost" as a fallback.
func hostHostID() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}
