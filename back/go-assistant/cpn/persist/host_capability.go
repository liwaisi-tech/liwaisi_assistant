// Package persist — host_capability.go defines the HostCapabilityRepository
// port used by GAP-2 (host discovery). The adapter lives in
// store/postgres/host_capability_store.go; the in-memory test adapter is in
// host_capability_memory.go.
//
// Spec: spec/spec-architecture-host-discovery-capability-registry.md §4 & §7.
//
// The port deliberately keeps to stdlib types + json.RawMessage so it stays
// platform-neutral and does not drag any storage package into the domain.
package persist

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrHostSnapshotNotFound signals "no snapshot yet for this host". Session
// bootstrap and the HTTP endpoint use errors.Is to branch on it.
var ErrHostSnapshotNotFound = errors.New("persist: host capability snapshot not found")

// HostIdentity is the part of the snapshot produced by t-who. Fields mirror
// spec §4.
type HostIdentity struct {
	MachineID       string `json:"machine_id"`
	MachineIDSource string `json:"machine_id_source,omitempty"` // "file" | "fallback"
	Hostname        string `json:"hostname"`
	User            string `json:"user"`
	UID             int    `json:"uid"`
	GID             int    `json:"gid"`
	Home            string `json:"home"`
	Shell           string `json:"shell"`
	// Env is the filtered, redacted environment — no TOKEN/KEY/PASSWORD/SECRET
	// keys (GUD-002 / AC-008). Map form so the persisted JSON remains
	// human-readable.
	Env map[string]string `json:"env,omitempty"`
}

// HostKernel is the part of the snapshot produced by t-uname. Fields mirror
// spec §4.
type HostKernel struct {
	OS        string            `json:"os"`
	Kernel    string            `json:"kernel"`
	Arch      string            `json:"arch"`
	OSRelease map[string]string `json:"os_release"`
	CPUCount  int               `json:"cpu_count"`
	MemMB     int               `json:"mem_mb"`
}

// BinaryProbe is the result of one probe. Missing binaries are NOT errors —
// Present=false is a normal outcome.
type BinaryProbe struct {
	Name         string `json:"name"`
	Path         string `json:"path,omitempty"`
	Present      bool   `json:"present"`
	Version      string `json:"version,omitempty"`
	DurationMs   int64  `json:"duration_ms"`
	DetectionErr string `json:"detection_err,omitempty"`
}

// Capability is a derived atomic fact. DerivedFrom lists the probe names that
// contributed to Satisfied.
type Capability struct {
	Name        string   `json:"name"`
	Satisfied   bool     `json:"satisfied"`
	DerivedFrom []string `json:"derived_from"`
}

// Sources of a snapshot (REQ-012).
const (
	HostSnapshotSourceBootstrap = "bootstrap"
	HostSnapshotSourceSession   = "session"
	HostSnapshotSourceManual    = "manual"
)

// HostCapabilitySnapshot is one row of host_capability_snapshots.
type HostCapabilitySnapshot struct {
	ID           string          `json:"id"`
	HostID       string          `json:"host_id"`
	CapturedAt   time.Time       `json:"captured_at"`
	Source       string          `json:"source"`
	Identity     HostIdentity    `json:"identity"`
	Kernel       HostKernel      `json:"kernel"`
	Binaries     []BinaryProbe   `json:"binaries"`
	Capabilities []Capability    `json:"capabilities"`
	RawProbes    json.RawMessage `json:"raw_probes,omitempty"`
}

// HasCapability is a small consumer helper so downstream CPNs don't have to
// iterate the slice themselves.
func (s HostCapabilitySnapshot) HasCapability(name string) bool {
	for _, c := range s.Capabilities {
		if c.Name == name {
			return c.Satisfied
		}
	}
	return false
}

// HostCapabilityRepository is the domain port for host-discovery persistence.
// The Postgres adapter lives in store/postgres/host_capability_store.go; the
// in-memory adapter in this package powers tests (REQ-010).
type HostCapabilityRepository interface {
	// Save inserts an immutable snapshot row and invalidates any cached
	// LatestForHost result for the same host (REQ-011 append-only).
	Save(ctx context.Context, s HostCapabilitySnapshot) error

	// LatestForHost returns the most recent snapshot for hostID, or
	// ErrHostSnapshotNotFound when the host has never been probed.
	//
	// Adapters MAY serve reads from a 60 s in-memory cache (REQ-013).
	LatestForHost(ctx context.Context, hostID string) (HostCapabilitySnapshot, error)

	// AppendProbeResult mutates the JSONB `binaries` array on the latest
	// row for hostID, replacing any existing entry with the same probe.Name.
	// Returns ErrHostSnapshotNotFound if there is no baseline snapshot yet.
	AppendProbeResult(ctx context.Context, hostID string, probe BinaryProbe) error
}
