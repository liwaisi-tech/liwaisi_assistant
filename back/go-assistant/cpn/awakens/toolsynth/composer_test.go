package toolsynth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

func schemaFor(bin string) helpparse.HelpSchema {
	return helpparse.HelpSchema{
		Binary:       bin,
		Flags:        []helpparse.HelpFlag{{Long: "--help", Doc: "help"}},
		Subcommands:  []helpparse.HelpSub{},
		Examples:     []string{},
		SourceSHA256: "src-" + bin,
	}
}

func TestCompose_ErrorOnEmpty(t *testing.T) {
	t.Parallel()
	_, err := Compose("sess", nil, Deps{Store: NewMemoryPendingToolStore()})
	if err == nil {
		t.Fatal("want ErrNoSchemas")
	}
}

func TestCompose_ErrorOnMissingStore(t *testing.T) {
	t.Parallel()
	_, err := Compose("sess", []helpparse.HelpSchema{schemaFor("git")}, Deps{})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestCompose_OneBranchPerSchema(t *testing.T) {
	t.Parallel()
	store := NewMemoryPendingToolStore()
	schemas := []helpparse.HelpSchema{schemaFor("git"), schemaFor("rg"), schemaFor("jq")}
	c, err := Compose("sess", schemas, Deps{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		if _, ok := c.Transitions[TxMaterialisePrefix+s.Binary]; !ok {
			t.Errorf("missing materialise for %s", s.Binary)
		}
		if _, ok := c.Transitions[TxStagePrefix+s.Binary]; !ok {
			t.Errorf("missing stage for %s", s.Binary)
		}
		if _, ok := c.Places[PlacePendingPrefix+s.Binary]; !ok {
			t.Errorf("missing pending place for %s", s.Binary)
		}
		if _, ok := c.Places[PlaceStagedPrefix+s.Binary]; !ok {
			t.Errorf("missing staged place for %s", s.Binary)
		}
	}
}

func TestCompose_DeduplicatesBinaries(t *testing.T) {
	t.Parallel()
	store := NewMemoryPendingToolStore()
	schemas := []helpparse.HelpSchema{schemaFor("git"), schemaFor("git")}
	c, err := Compose("sess", schemas, Deps{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for id := range c.Transitions {
		if id == TxMaterialisePrefix+"git" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want 1 branch for dedup'd binary, got %d", count)
	}
}

func TestCompose_FailureInOneBranchDoesNotAbortOthers(t *testing.T) {
	t.Parallel()
	store := NewMemoryPendingToolStore()

	// Schema 2 has a malformed flag → SynthesiseManifest errors.
	bad := helpparse.HelpSchema{
		Binary:       "badbin",
		Flags:        []helpparse.HelpFlag{{Long: "", Doc: "empty long"}},
		SourceSHA256: "src-bad",
	}
	schemas := []helpparse.HelpSchema{schemaFor("git"), bad, schemaFor("rg")}

	var failures []string
	var completed []string
	var mu sync.Mutex
	deps := Deps{
		Store: store,
		Clock: func() time.Time { return time.Unix(1700000000, 0).UTC() },
		OnSynthesisFailed: func(_ context.Context, b, _ string) {
			mu.Lock()
			failures = append(failures, b)
			mu.Unlock()
		},
		OnSynthesisCompleted: func(_ context.Context, b, _ string) {
			mu.Lock()
			completed = append(completed, b)
			mu.Unlock()
		},
	}
	c, err := Compose("sess", schemas, deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Fatalf("run must not abort (REQ-1205): %v", err)
	}

	staged, _ := store.List(ctx, "sess")
	if len(staged) != 2 {
		t.Fatalf("want 2 staged, got %d: %+v", len(staged), staged)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(failures) != 1 || failures[0] != "badbin" {
		t.Fatalf("failures = %+v", failures)
	}
	if len(completed) != 2 {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestReduceSyntheses_FiltersFailures(t *testing.T) {
	t.Parallel()
	in := []SynthesisResult{
		{Binary: "git", Pending: PendingTool{SourceSHA256: "g"}},
		{Binary: "bad", Err: "boom"},
		{Binary: "rg", Pending: PendingTool{SourceSHA256: "r"}},
	}
	out := ReduceSyntheses(in)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].SourceSHA256 != "g" || out[1].SourceSHA256 != "r" {
		t.Fatalf("wrong order/contents: %+v", out)
	}
}

func TestStagedPlaceIDs_Deterministic(t *testing.T) {
	t.Parallel()
	ids := StagedPlaceIDs([]helpparse.HelpSchema{schemaFor("zz"), schemaFor("aa")})
	if len(ids) != 2 {
		t.Fatalf("len=%d", len(ids))
	}
	if ids[0] != PlaceStagedPrefix+"aa" || ids[1] != PlaceStagedPrefix+"zz" {
		t.Fatalf("not sorted: %v", ids)
	}
}
