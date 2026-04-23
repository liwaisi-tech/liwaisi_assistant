package architect

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

func mkTool(reg *tools.Registry, ns, name, version string, hashtags []string) error {
	return reg.RegisterEntry(context.Background(), &tools.ToolEntry{
		Namespace:    ns,
		Name:         name,
		Version:      version,
		JSONSchema:   json.RawMessage(`{"type":"object"}`),
		Origin:       tools.OriginBuiltin,
		RegisteredBy: "test",
		Hashtags:     hashtags,
	})
}

func TestHashtagRetriever_Match_Empty(t *testing.T) {
	reg := tools.NewRegistry()
	r := NewHashtagRetriever(reg)
	set, err := r.Match(ArchitectRequest{Hashtags: []string{"shell"}})
	if err != nil {
		t.Fatalf("Match err: %v", err)
	}
	if len(set.Matches) != 0 {
		t.Errorf("expected zero matches on empty registry, got %+v", set)
	}
}

func TestHashtagRetriever_Match_RanksByOverlap(t *testing.T) {
	reg := tools.NewRegistry()
	if err := mkTool(reg, "system", "bash-exec", "1.0.0", []string{"shell", "host", "exec"}); err != nil {
		t.Fatal(err)
	}
	if err := mkTool(reg, "system", "file-read", "1.0.0", []string{"fs", "read"}); err != nil {
		t.Fatal(err)
	}
	if err := mkTool(reg, "net", "http-fetch", "1.0.0", []string{"http", "network"}); err != nil {
		t.Fatal(err)
	}

	r := NewHashtagRetriever(reg)
	set, err := r.Match(ArchitectRequest{Hashtags: []string{"shell", "host"}})
	if err != nil {
		t.Fatalf("Match err: %v", err)
	}
	if len(set.Matches) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(set.Matches), set.Matches)
	}
	m := set.Matches[0]
	if m.QualifiedName != "system/bash-exec@1.0.0" {
		t.Errorf("QualifiedName = %q", m.QualifiedName)
	}
	if m.Score != 2.0 {
		t.Errorf("Score = %v, want 2.0 (two hashtag hits)", m.Score)
	}
	if set.Digest == "" {
		t.Error("Digest must be populated when Matches non-empty")
	}
}

func TestHashtagRetriever_Match_DeduplicatesAcrossTags(t *testing.T) {
	reg := tools.NewRegistry()
	if err := mkTool(reg, "system", "bash-exec", "1.0.0", []string{"shell", "host"}); err != nil {
		t.Fatal(err)
	}
	r := NewHashtagRetriever(reg)
	set, _ := r.Match(ArchitectRequest{Hashtags: []string{"shell", "host"}})
	if len(set.Matches) != 1 {
		t.Errorf("a single tool matching two tags must appear once, got %d", len(set.Matches))
	}
	if len(set.Matches[0].Hashtags) != 2 {
		t.Errorf("matched hashtags must be de-duplicated yet preserved, got %v", set.Matches[0].Hashtags)
	}
}

func TestHashtagRetriever_Match_CapsBoostsScore(t *testing.T) {
	reg := tools.NewRegistry()
	if err := mkTool(reg, "system", "bash-exec", "1.0.0", []string{"shell"}); err != nil {
		t.Fatal(err)
	}
	r := NewHashtagRetriever(reg)
	r.CapsFor = func(qn string) []string {
		if qn == "system/bash-exec@1.0.0" {
			return []string{"host.bash"}
		}
		return nil
	}
	set, _ := r.Match(ArchitectRequest{
		Hashtags:     []string{"shell"},
		RequiredCaps: []string{"host.bash"},
	})
	if len(set.Matches) != 1 || set.Matches[0].Score != 2.0 {
		t.Errorf("CapsFor contribution missing, got %+v", set.Matches)
	}
}

func TestHashtagRetriever_Match_MaxResults(t *testing.T) {
	reg := tools.NewRegistry()
	for _, n := range []string{"a", "b", "c", "d"} {
		if err := mkTool(reg, "pkg", n, "1.0.0", []string{"shell"}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewHashtagRetriever(reg)
	r.MaxResults = 2
	set, _ := r.Match(ArchitectRequest{Hashtags: []string{"shell"}})
	if len(set.Matches) != 2 {
		t.Errorf("MaxResults cap broken, got %d", len(set.Matches))
	}
}

func TestHashtagRetriever_Match_Deterministic(t *testing.T) {
	reg := tools.NewRegistry()
	for _, n := range []string{"c", "a", "b"} {
		if err := mkTool(reg, "pkg", n, "1.0.0", []string{"shell"}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewHashtagRetriever(reg)
	req := ArchitectRequest{Hashtags: []string{"shell"}}
	s1, _ := r.Match(req)
	s2, _ := r.Match(req)
	if s1.Digest != s2.Digest {
		t.Errorf("digest not stable: %s vs %s", s1.Digest, s2.Digest)
	}
	// Lex order tie-break: a, b, c.
	want := []string{"pkg/a@1.0.0", "pkg/b@1.0.0", "pkg/c@1.0.0"}
	for i, m := range s1.Matches {
		if m.QualifiedName != want[i] {
			t.Errorf("position %d = %q, want %q", i, m.QualifiedName, want[i])
		}
	}
}

func TestHashtagRetriever_NilSafe(t *testing.T) {
	var r *HashtagRetriever
	set, err := r.Match(ArchitectRequest{Hashtags: []string{"shell"}})
	if err != nil {
		t.Errorf("nil retriever should not error: %v", err)
	}
	if len(set.Matches) != 0 {
		t.Errorf("nil retriever should yield zero matches, got %+v", set)
	}
}
