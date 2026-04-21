package awakens

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

type fakeSnapRepo struct {
	snap  persist.HostCapabilitySnapshot
	err   error
	calls int64
}

func (f *fakeSnapRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	atomic.AddInt64(&f.calls, 1)
	return f.snap, f.err
}

func mkSnap(os, shell string, bins ...persist.BinaryProbe) persist.HostCapabilitySnapshot {
	return persist.HostCapabilitySnapshot{
		HostID:   "h1",
		Identity: persist.HostIdentity{User: "u", Shell: shell},
		Kernel: persist.HostKernel{
			OS:        os,
			OSRelease: map[string]string{"NAME": os},
		},
		Binaries: bins,
	}
}

func mkBoxes() []tools.ToolboxManifest {
	return []tools.ToolboxManifest{
		{Namespace: "git", Title: "git", Summary: "Git tools", ToolCount: 3, Hashtags: []string{"#vcs"}},
		{Namespace: "general", Title: "general", Summary: "General", ToolCount: 2},
	}
}

func TestBuildEnvironmentAwareness_CacheHit(t *testing.T) {
	t.Parallel()
	repo := &fakeSnapRepo{snap: mkSnap("Linux", "zsh", persist.BinaryProbe{Name: "jq", Present: true})}
	cache := NewMemoryPersonalityCache(4)
	boxes := mkBoxes()

	block1, d1, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", boxes, []byte("lex"), cache)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if block1 == "" || d1 == "" {
		t.Fatalf("expected block+digest, got empty")
	}
	if !strings.Contains(block1, "## Environment awareness") {
		t.Fatalf("block missing header: %q", block1)
	}
	if !strings.Contains(block1, "### Toolboxes") {
		t.Fatalf("block missing toolboxes subsection")
	}

	block2, d2, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", boxes, []byte("lex"), cache)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d1 != d2 || block1 != block2 {
		t.Fatalf("digest/block must be stable on repeat call")
	}
	if cache.Len() != 1 {
		t.Fatalf("cache should hold exactly one entry, got %d", cache.Len())
	}
}

func TestBuildEnvironmentAwareness_DigestChange(t *testing.T) {
	t.Parallel()
	repo := &fakeSnapRepo{snap: mkSnap("Linux", "zsh")}
	cache := NewMemoryPersonalityCache(4)
	boxes := mkBoxes()

	_, d1, _ := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", boxes, []byte("lex-v1"), cache)

	repo.snap = mkSnap("Linux", "bash")
	_, d2, _ := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", boxes, []byte("lex-v2"), cache)

	if d1 == d2 {
		t.Fatalf("digest must change when snapshot/lexicon change")
	}
	if cache.Len() != 2 {
		t.Fatalf("both digests should coexist until LRU pressure, got %d", cache.Len())
	}
}

func TestBuildEnvironmentAwareness_NoSnapshot(t *testing.T) {
	t.Parallel()
	repo := &fakeSnapRepo{err: errors.New("not found")}
	cache := NewMemoryPersonalityCache(4)

	block, digest, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", mkBoxes(), nil, cache)
	if err != nil {
		t.Fatalf("should degrade gracefully, got err: %v", err)
	}
	if block != "" || digest != "" {
		t.Fatalf("expected empty block on missing snapshot, got %q / %q", block, digest)
	}
}

func TestBuildEnvironmentAwareness_NilRepo(t *testing.T) {
	t.Parallel()
	block, d, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), nil, "h1", nil, nil, nil)
	if err != nil || block != "" || d != "" {
		t.Fatalf("nil repo must be a silent no-op")
	}
}

func TestBuildEnvironmentAwareness_EmptySnapshot(t *testing.T) {
	t.Parallel()
	repo := &fakeSnapRepo{snap: persist.HostCapabilitySnapshot{}}
	block, _, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", nil, nil, nil)
	if err != nil || block != "" {
		t.Fatalf("empty snapshot must produce no block")
	}
}

func TestBuildEnvironmentAwareness_LegacyPathNoBoxes(t *testing.T) {
	t.Parallel()
	repo := &fakeSnapRepo{snap: mkSnap("Linux", "zsh")}
	cache := NewMemoryPersonalityCache(4)
	block, d, err := BuildEnvironmentAwarenessFromSnapshot(context.Background(), repo, "h1", nil, nil, cache)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(block, "## Environment awareness") {
		t.Fatalf("legacy path must still render the awareness header")
	}
	if d == "" {
		t.Fatalf("digest expected even on legacy path")
	}
	if _, ok := cache.Get(d); !ok {
		t.Fatalf("legacy path should populate cache")
	}
}

func TestFlushAwakeningCache_InvalidatesPersonality(t *testing.T) {
	t.Parallel()
	cache := NewMemoryPersonalityCache(4)
	cache.Put("digest-zombie", "block")
	cache.Put("digest-keep", "block")

	err := FlushAwakeningCache(context.Background(), nil, "h1",
		WithPersonalityCache(cache, "digest-zombie"))
	if err != nil {
		t.Fatalf("flush err: %v", err)
	}
	if _, ok := cache.Get("digest-zombie"); ok {
		t.Fatalf("zombie digest must have been invalidated")
	}
	if _, ok := cache.Get("digest-keep"); !ok {
		t.Fatalf("non-targeted digest must survive flush")
	}
}
