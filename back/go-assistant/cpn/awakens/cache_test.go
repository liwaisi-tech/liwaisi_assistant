package awakens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

type stubRepo struct {
	snap persist.HostCapabilitySnapshot
	err  error
}

func (s *stubRepo) Save(_ context.Context, _ persist.HostCapabilitySnapshot) error { return nil }
func (s *stubRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	return s.snap, s.err
}
func (s *stubRepo) AppendProbeResult(_ context.Context, _ string, _ persist.BinaryProbe) error {
	return nil
}

func TestLookupCachedSnapshot_Fresh(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	repo := &stubRepo{
		snap: persist.HostCapabilitySnapshot{
			HostID:     "h1",
			CapturedAt: now.Add(-2 * time.Hour),
			Source:     SourceAwakening,
		},
	}
	snap, fresh, err := LookupCachedSnapshot(
		context.Background(), repo, "h1", 0, func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !fresh {
		t.Fatalf("expected fresh=true for 2h-old snapshot under 24h TTL")
	}
	if snap.HostID != "h1" {
		t.Fatalf("got snap %+v", snap)
	}
}

func TestLookupCachedSnapshot_Stale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	repo := &stubRepo{
		snap: persist.HostCapabilitySnapshot{
			HostID:     "h1",
			CapturedAt: now.Add(-48 * time.Hour),
		},
	}
	_, fresh, err := LookupCachedSnapshot(
		context.Background(), repo, "h1", DefaultCacheTTL, func() time.Time { return now },
	)
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("want ErrCacheMiss, got %v", err)
	}
	if fresh {
		t.Fatalf("stale snap must return fresh=false")
	}
}

func TestLookupCachedSnapshot_NotFound(t *testing.T) {
	t.Parallel()
	repo := &stubRepo{err: persist.ErrHostSnapshotNotFound}
	_, fresh, err := LookupCachedSnapshot(context.Background(), repo, "h1", 0, nil)
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("want ErrCacheMiss, got %v", err)
	}
	if fresh {
		t.Fatalf("fresh must be false on miss")
	}
}

func TestLookupCachedSnapshot_NilRepo(t *testing.T) {
	t.Parallel()
	_, _, err := LookupCachedSnapshot(context.Background(), nil, "h1", 0, nil)
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("want ErrCacheMiss, got %v", err)
	}
}

func TestLookupCachedSnapshot_RepoError(t *testing.T) {
	t.Parallel()
	boom := errors.New("db exploded")
	repo := &stubRepo{err: boom}
	_, _, err := LookupCachedSnapshot(context.Background(), repo, "h1", 0, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped db error, got %v", err)
	}
}
