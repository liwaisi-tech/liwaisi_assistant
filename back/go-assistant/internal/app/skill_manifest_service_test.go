package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── stub implementations ──────────────────────────────────────────────────────

type stubToolRepo struct {
	entries []persist.ToolRegistryEntry
}

func (r *stubToolRepo) Upsert(_ context.Context, e persist.ToolRegistryEntry) error { return nil }
func (r *stubToolRepo) Get(_ context.Context, _ string) (persist.ToolRegistryEntry, error) {
	return persist.ToolRegistryEntry{}, persist.ErrToolNotFound
}
func (r *stubToolRepo) GetVersion(_ context.Context, _, _, _ string) (persist.ToolRegistryEntry, error) {
	return persist.ToolRegistryEntry{}, persist.ErrToolNotFound
}
func (r *stubToolRepo) ListByNamespace(_ context.Context, _ string) ([]persist.ToolRegistryEntry, error) {
	return nil, nil
}
func (r *stubToolRepo) ListAll(_ context.Context) ([]persist.ToolRegistryEntry, error) {
	return r.entries, nil
}
func (r *stubToolRepo) Deprecate(_ context.Context, _, _ string) error { return nil }
func (r *stubToolRepo) Latest(_ context.Context, _, _ string) (persist.ToolRegistryEntry, error) {
	return persist.ToolRegistryEntry{}, persist.ErrToolNotFound
}
func (r *stubToolRepo) Delete(_ context.Context, _ string) error { return nil }

type stubCapRepo struct {
	snap persist.HostCapabilitySnapshot
	err  error
}

func (r *stubCapRepo) Save(_ context.Context, _ persist.HostCapabilitySnapshot) error { return nil }
func (r *stubCapRepo) LatestForHost(_ context.Context, _ string) (persist.HostCapabilitySnapshot, error) {
	return r.snap, r.err
}
func (r *stubCapRepo) AppendProbeResult(_ context.Context, _ string, _ persist.BinaryProbe) error {
	return nil
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestBuild_AggregatesAllSources(t *testing.T) {
	toolRepo := &stubToolRepo{entries: []persist.ToolRegistryEntry{
		{ID: "1", Namespace: "brae", Name: "http-get", Version: "1.0.0", Origin: "agent-authored", HelpText: "HTTP GET → stdout"},
	}}
	capRepo := &stubCapRepo{snap: persist.HostCapabilitySnapshot{
		Capabilities: []persist.Capability{
			{Name: "can-compile-c", Satisfied: true},
			{Name: "can-run-python", Satisfied: false},
		},
	}}

	svc := NewSkillManifestService(toolRepo, capRepo, "host-1", []string{"classifier", "tool-forge"})
	m, err := svc.Build(context.Background())
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	kindCount := map[SkillKind]int{}
	for _, sk := range m.Skills {
		kindCount[sk.Kind]++
	}

	if kindCount[SkillKindBuiltin] != 2 {
		t.Errorf("want 2 builtins, got %d", kindCount[SkillKindBuiltin])
	}
	if kindCount[SkillKindTool] != 1 {
		t.Errorf("want 1 tool, got %d", kindCount[SkillKindTool])
	}
	if kindCount[SkillKindHostCap] != 2 {
		t.Errorf("want 2 host caps, got %d", kindCount[SkillKindHostCap])
	}
}

func TestBuildCompact_TruncatesAtCap(t *testing.T) {
	toolRepo := &stubToolRepo{entries: []persist.ToolRegistryEntry{
		{ID: "1", Namespace: "brae", Name: "http-get", Version: "1.0.0", Origin: "agent-authored"},
		{ID: "2", Namespace: "brae", Name: "curl-post", Version: "1.0.0", Origin: "agent-authored"},
		{ID: "3", Namespace: "brae", Name: "jq-filter", Version: "1.0.0", Origin: "agent-authored"},
	}}
	capRepo := &stubCapRepo{snap: persist.HostCapabilitySnapshot{
		Capabilities: []persist.Capability{
			{Name: "can-compile-c", Satisfied: true},
		},
	}}

	svc := NewSkillManifestService(toolRepo, capRepo, "host-1", []string{"classifier"})
	compact, err := svc.BuildCompact(context.Background(), 80)
	if err != nil {
		t.Fatalf("BuildCompact error: %v", err)
	}
	if len(compact) > 80 {
		t.Errorf("want len ≤ 80, got %d: %q", len(compact), compact)
	}
}

func TestInvalidate_ClearsCache(t *testing.T) {
	toolRepo := &stubToolRepo{}
	svc := NewSkillManifestService(toolRepo, nil, "", []string{"classifier"})

	m1, _ := svc.Build(context.Background())
	svc.Invalidate("test")

	// Mutate the stub so next build returns different data.
	toolRepo.entries = append(toolRepo.entries, persist.ToolRegistryEntry{
		ID: "99", Namespace: "brae", Name: "new-tool", Version: "1.0.0", Origin: "agent-authored",
	})

	m2, _ := svc.Build(context.Background())
	if len(m2.Skills) <= len(m1.Skills) {
		t.Errorf("expected more skills after invalidation; m1=%d m2=%d", len(m1.Skills), len(m2.Skills))
	}
}

func TestBuildCompact_StableOrdering(t *testing.T) {
	toolRepo := &stubToolRepo{entries: []persist.ToolRegistryEntry{
		{ID: "1", Namespace: "brae", Name: "alpha", Version: "1.0.0", Origin: "builtin"},
		{ID: "2", Namespace: "brae", Name: "beta", Version: "1.0.0", Origin: "agent-authored"},
	}}
	svc := NewSkillManifestService(toolRepo, nil, "", []string{"classifier", "tool-forge"})

	results := make([]string, 5)
	for i := range results {
		svc.Invalidate("force-rebuild")
		c, err := svc.BuildCompact(context.Background(), 0)
		if err != nil {
			t.Fatalf("BuildCompact iteration %d: %v", i, err)
		}
		results[i] = c
	}
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Errorf("compact output not stable: iteration 0 vs %d differ", i)
		}
	}
}

func TestBuildCompact_DeprecatedToolsOmitted(t *testing.T) {
	toolRepo := &stubToolRepo{entries: []persist.ToolRegistryEntry{
		{ID: "1", Namespace: "brae", Name: "live", Version: "1.0.0", Origin: "agent-authored"},
		{ID: "2", Namespace: "brae", Name: "gone", Version: "1.0.0", Origin: "agent-authored", Deprecated: true, DeprecatedAt: time.Now()},
	}}
	svc := NewSkillManifestService(toolRepo, nil, "", []string{"classifier"})
	compact, err := svc.BuildCompact(context.Background(), 0)
	if err != nil {
		t.Fatalf("BuildCompact error: %v", err)
	}
	if strings.Contains(compact, "gone") {
		t.Error("deprecated tool 'gone' should not appear in compact form")
	}
}
