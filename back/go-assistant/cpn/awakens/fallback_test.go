package awakens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

type recordRepo struct {
	saved []persist.HostCapabilitySnapshot
}

func (r *recordRepo) Save(_ context.Context, s persist.HostCapabilitySnapshot) error {
	r.saved = append(r.saved, s)
	return nil
}
func (r *recordRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	return persist.HostCapabilitySnapshot{}, persist.ErrHostSnapshotNotFound
}
func (r *recordRepo) AppendProbeResult(_ context.Context, _ string, _ persist.BinaryProbe) error {
	return nil
}

func TestRunFallback_TagsSource(t *testing.T) {
	t.Parallel()
	legacy := func(_ context.Context) (persist.HostCapabilitySnapshot, error) {
		return persist.HostCapabilitySnapshot{
			HostID:     "h1",
			CapturedAt: time.Now().Add(-time.Minute),
			Source:     persist.HostSnapshotSourceSession, // wrong — RunFallback must overwrite
			Kernel:     persist.HostKernel{OS: "Linux", Arch: "amd64"},
		}, nil
	}
	repo := &recordRepo{}
	res, err := RunFallback(context.Background(), legacy, repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Snapshot.Source != SourceAwakeningFallback {
		t.Errorf("snap source: got %q want %q", res.Snapshot.Source, SourceAwakeningFallback)
	}
	if !res.UsedFallback {
		t.Errorf("UsedFallback must be true")
	}
	if len(repo.saved) != 1 || repo.saved[0].Source != SourceAwakeningFallback {
		t.Errorf("repo save mismatch: %+v", repo.saved)
	}
}

func TestRunFallback_NilLegacy(t *testing.T) {
	t.Parallel()
	_, err := RunFallback(context.Background(), nil, nil)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("want ErrLLMUnavailable, got %v", err)
	}
}

func TestRunFallback_LegacyError(t *testing.T) {
	t.Parallel()
	boom := errors.New("legacy exploded")
	legacy := func(_ context.Context) (persist.HostCapabilitySnapshot, error) {
		return persist.HostCapabilitySnapshot{}, boom
	}
	_, err := RunFallback(context.Background(), legacy, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped boom, got %v", err)
	}
}
