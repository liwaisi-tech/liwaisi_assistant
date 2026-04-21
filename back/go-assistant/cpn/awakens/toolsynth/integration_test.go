package toolsynth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

// TestIntegration_EndToEnd feeds 3 HelpSchemas (1 malformed) into the
// composer, runs the CPN, and asserts: 2 PendingTools staged, 1 failure
// event emitted, zero registry writes (REQ-1203 — there is no registry
// handle available to the toolsynth package; if any code tried to call
// RegisterManifest, it would need a cross-package import that isn't there).
func TestIntegration_EndToEnd(t *testing.T) {
	t.Parallel()
	store := NewMemoryPendingToolStore()

	var started, completed, failed int
	var mu sync.Mutex
	deps := Deps{
		Store: store,
		Clock: func() time.Time { return time.Unix(1700000001, 0).UTC() },
		OnSynthesisStarted: func(_ context.Context, _ string) {
			mu.Lock()
			started++
			mu.Unlock()
		},
		OnSynthesisCompleted: func(_ context.Context, _, _ string) {
			mu.Lock()
			completed++
			mu.Unlock()
		},
		OnSynthesisFailed: func(_ context.Context, _, _ string) {
			mu.Lock()
			failed++
			mu.Unlock()
		},
	}
	schemas := []helpparse.HelpSchema{
		{
			Binary:       "git",
			Flags:        []helpparse.HelpFlag{{Long: "--version", Short: "-v"}},
			Subcommands:  []helpparse.HelpSub{{Name: "push"}},
			Examples:     []string{"git push"},
			SourceSHA256: "src-git",
		},
		{
			Binary:       "broken",
			Flags:        []helpparse.HelpFlag{{Long: "   "}},
			SourceSHA256: "src-broken",
		},
		{
			Binary:       "rg",
			Flags:        []helpparse.HelpFlag{{Long: "--color", Arg: true}},
			SourceSHA256: "src-rg",
		},
	}
	c, err := Compose("sess-int", schemas, deps)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	staged, err := store.List(ctx, "sess-int")
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 2 {
		t.Fatalf("want 2 staged PendingTools, got %d: %+v", len(staged), staged)
	}
	if staged[0].Manifest.Name != "git" || staged[1].Manifest.Name != "rg" {
		t.Fatalf("wrong staging order: %+v", staged)
	}
	for _, p := range staged {
		if p.Manifest.Kind != KindSynthesized {
			t.Errorf("Kind=%q", p.Manifest.Kind)
		}
		if p.Manifest.Origin != OriginHelpParser {
			t.Errorf("Origin=%q", p.Manifest.Origin)
		}
		if p.ProvenanceSHA256 == "" {
			t.Errorf("missing provenance for %s", p.Manifest.Name)
		}
		if p.SessionID != "sess-int" {
			t.Errorf("SessionID=%q", p.SessionID)
		}
		if p.CreatedAt.IsZero() {
			t.Errorf("CreatedAt zero for %s", p.Manifest.Name)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 3 {
		t.Errorf("started=%d want 3", started)
	}
	if completed != 2 {
		t.Errorf("completed=%d want 2", completed)
	}
	if failed != 1 {
		t.Errorf("failed=%d want 1", failed)
	}
}
