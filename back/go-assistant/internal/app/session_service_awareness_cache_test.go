package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

type countingToolboxLister struct {
	boxes []tools.ToolboxManifest
	calls int64
}

func (l *countingToolboxLister) Toolboxes(_ context.Context) []tools.ToolboxManifest {
	atomic.AddInt64(&l.calls, 1)
	return l.boxes
}

type countingRepo struct {
	persist.HostCapabilityRepository
	snap  persist.HostCapabilitySnapshot
	calls int64
}

func (r *countingRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	atomic.AddInt64(&r.calls, 1)
	return r.snap, nil
}

func (r *countingRepo) Save(_ context.Context, _ persist.HostCapabilitySnapshot) error { return nil }
func (r *countingRepo) AppendProbeResult(_ context.Context, _ string, _ persist.BinaryProbe) error {
	return nil
}

func TestEnvironmentAwarenessBlock_CachesByDigest(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-cache-07")
	defer restore()

	repo := &countingRepo{
		snap: persist.HostCapabilitySnapshot{
			ID:         "snap-07",
			HostID:     "machine-cache-07",
			CapturedAt: time.Now(),
			Kernel:     persist.HostKernel{OS: "Linux", Arch: "amd64", OSRelease: map[string]string{"NAME": "Linux"}},
			Identity:   persist.HostIdentity{Shell: "/bin/bash"},
			Binaries:   []persist.BinaryProbe{{Name: "git", Present: true}},
		},
	}
	lister := &countingToolboxLister{
		boxes: []tools.ToolboxManifest{
			{Namespace: "git", Title: "Git", Summary: "Git tools", ToolCount: 3},
		},
	}
	cache := awakens.NewMemoryPersonalityCache(4)

	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		toolboxLister:      lister,
		personalityCache:   cache,
	}

	block1 := svc.environmentAwarenessBlock(context.Background())
	if block1 == "" {
		t.Fatalf("expected non-empty block on first call")
	}
	if !strings.Contains(block1, "## Environment awareness") {
		t.Fatalf("block missing awareness header: %q", block1)
	}
	if !strings.Contains(block1, "### Toolboxes") {
		t.Fatalf("block missing toolboxes subsection")
	}

	block2 := svc.environmentAwarenessBlock(context.Background())
	if block1 != block2 {
		t.Fatalf("block must be stable across turns with same digest")
	}
	if cache.Len() != 1 {
		t.Fatalf("expected exactly one cache entry, got %d", cache.Len())
	}
}

func TestEnvironmentAwarenessBlock_NoRepo_NoBlock(t *testing.T) {
	svc := &SessionService{logger: testLogger()}
	if got := svc.environmentAwarenessBlock(context.Background()); got != "" {
		t.Fatalf("expected empty block when no repo wired, got %q", got)
	}
}
