package toolsynth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func mkPending(name, sess, prov string) PendingTool {
	return PendingTool{
		Manifest:         cpn.ToolManifest{Namespace: DefaultNamespace, Name: name, Version: DefaultVersion, Kind: KindSynthesized, Origin: OriginHelpParser},
		SourceSHA256:     "src-" + name,
		ProvenanceSHA256: prov,
		SessionID:        sess,
		CreatedAt:        time.Unix(1700000000, 0).UTC(),
	}
}

func TestMemoryPendingToolStore_StageAndGet(t *testing.T) {
	t.Parallel()
	s := NewMemoryPendingToolStore()
	ctx := context.Background()
	if err := s.Stage(ctx, mkPending("git", "sess-1", "p1")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "git")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.ProvenanceSHA256 != "p1" {
		t.Errorf("got %q", got.ProvenanceSHA256)
	}
	if _, ok, _ := s.Get(ctx, "nope"); ok {
		t.Error("unexpected hit")
	}
}

func TestMemoryPendingToolStore_ListBySession(t *testing.T) {
	t.Parallel()
	s := NewMemoryPendingToolStore()
	ctx := context.Background()
	_ = s.Stage(ctx, mkPending("git", "sess-a", "pa"))
	_ = s.Stage(ctx, mkPending("rg", "sess-a", "pb"))
	_ = s.Stage(ctx, mkPending("jq", "sess-b", "pc"))

	a, err := s.List(ctx, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 {
		t.Fatalf("sess-a len=%d", len(a))
	}
	if a[0].Manifest.Name != "git" || a[1].Manifest.Name != "rg" {
		t.Fatalf("sorting broken: %v", a)
	}

	all, _ := s.List(ctx, "")
	if len(all) != 3 {
		t.Fatalf("all len=%d", len(all))
	}
}

func TestMemoryPendingToolStore_Delete(t *testing.T) {
	t.Parallel()
	s := NewMemoryPendingToolStore()
	ctx := context.Background()
	_ = s.Stage(ctx, mkPending("git", "sess-a", "p"))
	if err := s.Delete(ctx, "git"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "git"); ok {
		t.Fatal("still present after delete")
	}
	if err := s.Delete(ctx, "git"); err == nil {
		t.Fatal("want error on second delete")
	}
}

func TestMemoryPendingToolStore_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	s := NewMemoryPendingToolStore()
	ctx := context.Background()
	const N = 40
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("tool-%d", i)
			_ = s.Stage(ctx, mkPending(name, "sess", fmt.Sprintf("prov-%d", i)))
			_, _, _ = s.Get(ctx, name)
			_, _ = s.List(ctx, "sess")
		}(i)
	}
	wg.Wait()
	all, _ := s.List(ctx, "sess")
	if len(all) != N {
		t.Fatalf("want %d staged, got %d", N, len(all))
	}
}

func TestMemoryPendingToolStore_StageWithoutNameFails(t *testing.T) {
	t.Parallel()
	s := NewMemoryPendingToolStore()
	p := mkPending("", "sess", "p")
	p.Manifest.Name = ""
	if err := s.Stage(context.Background(), p); err == nil {
		t.Fatal("want error")
	}
}
