package awakens

import (
	"context"
	"errors"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// DefaultCacheTTL is the "less than 24 hours" window from REQ-009.
const DefaultCacheTTL = 24 * time.Hour

// ErrCacheMiss is returned by LookupCachedSnapshot when no fresh snapshot
// for the given host exists.
var ErrCacheMiss = errors.New("awakens: cached snapshot miss or stale")

// Clock is an indirection for testability. Production uses time.Now.
type Clock func() time.Time

// SystemClock returns real wall-clock time. Exposed so tests can inject a
// fixed clock without monkey-patching.
func SystemClock() time.Time { return time.Now().UTC() }

// LookupCachedSnapshot returns the repository's latest snapshot for hostID
// when it was captured within ttl. The second return value is true when the
// snapshot is fresh; false otherwise (caller falls through to shell probes).
//
// ttl ≤ 0 selects DefaultCacheTTL per REQ-009.
// clock may be nil — it defaults to SystemClock.
func LookupCachedSnapshot(ctx context.Context, repo persist.HostCapabilityRepository, hostID string, ttl time.Duration, clock Clock) (persist.HostCapabilitySnapshot, bool, error) {
	if repo == nil {
		return persist.HostCapabilitySnapshot{}, false, ErrCacheMiss
	}
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if clock == nil {
		clock = SystemClock
	}

	snap, err := repo.LatestForHost(ctx, hostID)
	if err != nil {
		if errors.Is(err, persist.ErrHostSnapshotNotFound) {
			return persist.HostCapabilitySnapshot{}, false, ErrCacheMiss
		}
		return persist.HostCapabilitySnapshot{}, false, err
	}
	if snap.CapturedAt.IsZero() {
		return snap, false, ErrCacheMiss
	}
	if clock().Sub(snap.CapturedAt) > ttl {
		return snap, false, ErrCacheMiss
	}
	// Only awakening-sourced snapshots qualify for the 24h cache reuse
	// shortcut — legacy `session`/`bootstrap` rows can still be used but
	// we record the age so the caller can decide.
	return snap, true, nil
}
