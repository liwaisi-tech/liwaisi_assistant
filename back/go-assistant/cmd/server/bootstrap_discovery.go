package main

// bootstrap_discovery.go — implementation of the CLI flag
// --bootstrap-discovery from spec §3 / AC-001. Running the binary with this
// flag probes the host, persists one snapshot, prints a summary, and exits
// without starting the HTTP server.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// bootstrapDiscoveryFlag is the exact flag string the CLI accepts.
const bootstrapDiscoveryFlag = "--bootstrap-discovery"

// hasBootstrapDiscoveryFlag reports whether args contain the flag.
func hasBootstrapDiscoveryFlag(args []string) bool {
	for _, a := range args {
		if a == bootstrapDiscoveryFlag {
			return true
		}
	}
	return false
}

// runBootstrapDiscovery executes the host-discovery CPN synchronously and
// prints a one-line summary per AC-001. Returns an error on discovery
// failure so main can exit non-zero.
func runBootstrapDiscovery(logger *slog.Logger, runtime *cpn.HostRuntime, repo persist.HostCapabilityRepository) error {
	if repo == nil {
		return fmt.Errorf("host capability repository required")
	}
	logger.Info("bootstrap discovery starting")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	subCPN := hostDiscoveryTopologyFactory("bootstrap", HostDiscoveryDeps{
		Repository: repo,
		Source:     persist.HostSnapshotSourceBootstrap,
	})
	subCPN.HostRuntime = runtime

	if err := subCPN.Run(ctx); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	snap, ok := extractSnapshot(subCPN)
	if !ok {
		return fmt.Errorf("no snapshot produced")
	}

	out, _ := json.MarshalIndent(summarySnapshot(snap), "", "  ")
	fmt.Println(string(out))
	logger.Info("bootstrap discovery complete",
		slog.String("host_id", snap.HostID),
		slog.String("snapshot_id", snap.ID),
		slog.Int("binaries", len(snap.Binaries)),
		slog.Int("capabilities", countSatisfiedCaps(snap)),
	)
	return nil
}

// extractSnapshot walks the CPN for the terminal p-snapshot place and
// returns the first HostCapabilitySnapshot token it finds.
func extractSnapshot(c *cpn.CPN) (persist.HostCapabilitySnapshot, bool) {
	for _, p := range c.Places {
		if p.ID != "p-snapshot" {
			continue
		}
		tokens, ok := p.Peek()
		if !ok || len(tokens) == 0 {
			continue
		}
		if snap, ok := tokens[0].Payload.(persist.HostCapabilitySnapshot); ok {
			return snap, true
		}
	}
	return persist.HostCapabilitySnapshot{}, false
}

// summarySnapshot is the trimmed, human-readable view printed by the
// bootstrap CLI. Verbose fields (raw env, full probe results) are skipped
// — operators can read the JSON from the repo when debugging.
type summaryPayload struct {
	SnapshotID   string   `json:"snapshot_id"`
	HostID       string   `json:"host_id"`
	CapturedAt   string   `json:"captured_at"`
	User         string   `json:"user"`
	OS           string   `json:"os"`
	Kernel       string   `json:"kernel"`
	Arch         string   `json:"arch"`
	CPUCount     int      `json:"cpu_count"`
	MemMB        int      `json:"mem_mb"`
	Present      []string `json:"present"`
	Missing      []string `json:"missing"`
	Capabilities []string `json:"capabilities"`
}

func summarySnapshot(s persist.HostCapabilitySnapshot) summaryPayload {
	out := summaryPayload{
		SnapshotID: s.ID,
		HostID:     s.HostID,
		CapturedAt: s.CapturedAt.Format(time.RFC3339),
		User:       s.Identity.User,
		OS:         s.Kernel.OS,
		Kernel:     s.Kernel.Kernel,
		Arch:       s.Kernel.Arch,
		CPUCount:   s.Kernel.CPUCount,
		MemMB:      s.Kernel.MemMB,
	}
	for _, b := range s.Binaries {
		if b.Present {
			out.Present = append(out.Present, b.Name)
		} else {
			out.Missing = append(out.Missing, b.Name)
		}
	}
	for _, c := range s.Capabilities {
		if c.Satisfied {
			out.Capabilities = append(out.Capabilities, c.Name)
		}
	}
	return out
}

func countSatisfiedCaps(s persist.HostCapabilitySnapshot) int {
	n := 0
	for _, c := range s.Capabilities {
		if c.Satisfied {
			n++
		}
	}
	return n
}

// ── HTTP adapters ──────────────────────────────────────────────────────────
//
// The handler in internal/driving/httpapi takes thin ports (HostIDResolver,
// HostDiscoveryRunner). We implement them here so main.go can inject the
// concrete runtime + repo without leaking domain types into the HTTP layer.

// hostIDResolverAdapter implements httpapi.HostIDResolver.
type hostIDResolverAdapter struct {
	runtime *cpn.HostRuntime
}

// ResolveHostID returns /etc/machine-id (trimmed) or a deterministic
// hostname-hash fallback. Mirrors
// internal/app/session_service.go::resolveHostID.
func (a *hostIDResolverAdapter) ResolveHostID(ctx context.Context) string {
	if a.runtime != nil && a.runtime.Adapter != nil {
		if data, err := a.runtime.Adapter.ReadFile(ctx, "/etc/machine-id"); err == nil {
			id := trimBytesForMachineID(data)
			if id != "" {
				return id
			}
		}
	}
	// Fallback to os.ReadFile (the jail rejects /etc/machine-id by
	// default). Ignore errors; the hostname hash is the last resort.
	if b, err := readOSFile("/etc/machine-id"); err == nil {
		if id := trimBytesForMachineID(b); id != "" {
			return id
		}
	}
	host, _ := osHostnameForCmd()
	return fallbackMachineID(host)
}

// hostDiscoveryRunnerAdapter implements httpapi.HostDiscoveryRunner.
type hostDiscoveryRunnerAdapter struct {
	repo    persist.HostCapabilityRepository
	runtime *cpn.HostRuntime
}

// Rediscover runs host-discovery-cpn synchronously and returns the
// deposited snapshot. Source = "manual" per spec §REQ-012.
func (a *hostDiscoveryRunnerAdapter) Rediscover(ctx context.Context) (persist.HostCapabilitySnapshot, error) {
	if a.repo == nil {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("host capability repo not configured")
	}
	subCPN := hostDiscoveryTopologyFactory("rediscover", HostDiscoveryDeps{
		Repository: a.repo,
		Source:     persist.HostSnapshotSourceManual,
	})
	subCPN.HostRuntime = a.runtime
	if err := subCPN.Run(ctx); err != nil {
		return persist.HostCapabilitySnapshot{}, err
	}
	snap, ok := extractSnapshot(subCPN)
	if !ok {
		return persist.HostCapabilitySnapshot{}, fmt.Errorf("no snapshot produced")
	}
	return snap, nil
}

// Small OS-seam helpers so tests can stub. Mirrors
// internal/app/host_bootstrap.go.
var (
	readOSFile        = osReadFile
	osHostnameForCmd  = osHostname
	trimBytesForMachineID = func(data []byte) string {
		out := make([]byte, 0, len(data))
		for _, b := range data {
			if b == '\n' || b == '\r' || b == ' ' || b == '\t' {
				continue
			}
			out = append(out, b)
		}
		return string(out)
	}
)
