package httpapi

// Admin host-capabilities REST API (GAP-2).
//
// Spec: spec/spec-architecture-host-discovery-capability-registry.md §4
// (REQ-030, REQ-031, REQ-032).
//
// Both endpoints share the AdminMiddleware wrapper in routes.go.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// HostCapabilityAccessor is the narrow port the handler needs. Production
// wires it to the Postgres adapter; tests inject a fake.
type HostCapabilityAccessor interface {
	LatestForHost(ctx context.Context, hostID string) (persist.HostCapabilitySnapshot, error)
}

// HostDiscoveryRunner runs an on-demand host-discovery CPN and returns the
// produced snapshot. Used by POST /rediscover.
type HostDiscoveryRunner interface {
	Rediscover(ctx context.Context) (persist.HostCapabilitySnapshot, error)
}

// HostIDResolver returns the machine-id the current process is probing.
type HostIDResolver interface {
	ResolveHostID(ctx context.Context) string
}

// HandleGetHostCapabilities serves the latest snapshot for the current host.
//
// GET /api/v1/admin/host/capabilities
//
//	200 → JSON snapshot
//	404 → {"reason":"no-snapshot-yet"}
func (h *Handlers) HandleGetHostCapabilities(w http.ResponseWriter, r *http.Request) {
	if h.HostCapability == nil {
		writeError(w, http.StatusServiceUnavailable, "host capability registry not enabled")
		return
	}
	hostID := ""
	if h.HostIDResolver != nil {
		hostID = h.HostIDResolver.ResolveHostID(r.Context())
	}
	if hostID == "" {
		writeError(w, http.StatusInternalServerError, "could not resolve host id")
		return
	}
	snap, err := h.HostCapability.LatestForHost(r.Context(), hostID)
	if err != nil {
		if errors.Is(err, persist.ErrHostSnapshotNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"reason": "no-snapshot-yet"})
			return
		}
		h.Logger.Error("get host capabilities", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// HandlePostHostCapabilitiesRediscover triggers a synchronous discovery run.
//
// POST /api/v1/admin/host/capabilities/rediscover
//
//	202 → {"snapshot_id":"...", "host_id":"...", "queued_at":"..."}
func (h *Handlers) HandlePostHostCapabilitiesRediscover(w http.ResponseWriter, r *http.Request) {
	if h.HostDiscoveryRunner == nil {
		writeError(w, http.StatusServiceUnavailable, "host discovery not enabled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	snap, err := h.HostDiscoveryRunner.Rediscover(ctx)
	if err != nil {
		h.Logger.Error("rediscover host", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"snapshot_id": snap.ID,
		"host_id":     snap.HostID,
		"queued_at":   time.Now().UTC().Format(time.RFC3339),
	})
}

// _ prevents the `cpn` package import from being unused on builds that
// dead-code-eliminate all references. Kept so the import stays honest
// across refactors.
var _ = cpn.ColorHostFact
