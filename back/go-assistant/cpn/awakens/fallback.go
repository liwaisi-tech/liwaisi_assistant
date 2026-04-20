package awakens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// SourceAwakening is the `source` value written to host_capability_snapshots
// on the happy path (REQ-004).
const SourceAwakening = "awakening"

// SourceAwakeningFallback is the `source` value written when the LLM is
// unreachable and we fall back to the legacy deterministic path (REQ-010).
const SourceAwakeningFallback = "awakening-fallback"

// LegacyDiscoveryFunc is the shape of the legacy host-discovery topology
// runner. The cmd/server package wires this to hostDiscoveryTopologyFactory
// + CPN.Run() so the awakens package does not have to import cmd/server.
type LegacyDiscoveryFunc func(ctx context.Context) (persist.HostCapabilitySnapshot, error)

// ErrLLMUnavailable is the sentinel callers use to signal "primary LLM path
// is not usable; go to fallback". It unwraps cleanly via errors.Is.
var ErrLLMUnavailable = errors.New("awakens: LLM provider unavailable")

// FallbackResult carries what the fallback produced. Keeping snapshot +
// flag together lets callers emit a differently-worded first-turn message
// without re-reading the DB.
type FallbackResult struct {
	Snapshot persist.HostCapabilitySnapshot
	// UsedFallback is true when the legacy path produced the snapshot.
	UsedFallback bool
}

// RunFallback invokes legacy host-discovery, stamps the resulting snapshot
// with source=awakening-fallback, and persists it. On success returns a
// usable snapshot; on failure the error wraps whatever the legacy path
// reported — the caller may then decide to boot the session without any
// host-capability seed (downstream consumers PEEK defensively).
//
// REQ-010 + CON-006. The legacy topology stays compilable and runnable.
func RunFallback(ctx context.Context, legacy LegacyDiscoveryFunc, repo persist.HostCapabilityRepository) (FallbackResult, error) {
	if legacy == nil {
		return FallbackResult{}, fmt.Errorf("%w: no legacy discovery factory", ErrLLMUnavailable)
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snap, err := legacy(runCtx)
	if err != nil {
		return FallbackResult{}, fmt.Errorf("awakens: legacy host-discovery: %w", err)
	}
	// Tag the snapshot per REQ-010 without touching the legacy factory.
	snap.Source = SourceAwakeningFallback
	if snap.CapturedAt.IsZero() {
		snap.CapturedAt = time.Now().UTC()
	}
	if repo != nil {
		if saveErr := repo.Save(ctx, snap); saveErr != nil {
			// Surfaced but non-fatal — the caller may still seed the
			// session with the in-memory snapshot.
			return FallbackResult{Snapshot: snap, UsedFallback: true},
				fmt.Errorf("awakens: save fallback snapshot: %w", saveErr)
		}
	}
	return FallbackResult{Snapshot: snap, UsedFallback: true}, nil
}
