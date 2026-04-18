package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// helper — rich entry factory for table tests.
func newEntry(ns, name, version string) *ToolEntry {
	return &ToolEntry{
		Namespace:  ns,
		Name:       name,
		Version:    version,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
		Origin:     OriginAgentAuthored,
		HelpText:   "test tool",
	}
}

// AC-001: duplicate (namespace,name,version) → ErrDuplicate.
func TestRegisterEntry_DuplicateRejected(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	if err := r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.0.0")); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.0.0"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

// AC-002: Latest returns highest semver among non-deprecated.
func TestRegisterEntry_Latest_HighestSemver(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	if err := r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.0.0")); err != nil {
		t.Fatalf("register v1: %v", err)
	}
	if err := r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.1.0")); err != nil {
		t.Fatalf("register v1.1: %v", err)
	}
	if err := r.RegisterEntry(ctx, newEntry("brae", "http-get", "2.0.0")); err != nil {
		t.Fatalf("register v2: %v", err)
	}

	latest, err := r.Latest(ctx, "brae", "http-get")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Version != "2.0.0" {
		t.Fatalf("latest = %s, want 2.0.0", latest.Version)
	}
}

// AC-003: After deprecation, Latest skips the deprecated entry.
func TestDeprecate_SkippedByLatest(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	_ = r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.0.0"))
	_ = r.RegisterEntry(ctx, newEntry("brae", "http-get", "2.0.0"))

	if err := r.Deprecate(ctx, "brae/http-get@2.0.0", "rollback"); err != nil {
		t.Fatalf("deprecate: %v", err)
	}

	latest, err := r.Latest(ctx, "brae", "http-get")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Version != "1.0.0" {
		t.Fatalf("latest = %s, want 1.0.0 (2.0.0 deprecated)", latest.Version)
	}

	// Anchor-only resolution picks the same.
	resolved, err := r.ResolveByName(ctx, "brae/http-get")
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	if resolved.Version != "1.0.0" {
		t.Fatalf("ResolveByName = %s, want 1.0.0", resolved.Version)
	}
}

// AC-008: resolution — bare, anchor, version.
func TestResolveByName_Table(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	_ = r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.0.0"))
	_ = r.RegisterEntry(ctx, newEntry("brae", "http-get", "1.1.0"))
	_ = r.RegisterEntry(ctx, newEntry("aaa", "http-get", "3.0.0"))

	cases := []struct {
		query string
		want  string
	}{
		{"brae/http-get@1.0.0", "1.0.0"},
		{"brae/http-get", "1.1.0"},
		// Bare name — deterministic tie-break alphabetical "aaa" < "brae".
		{"http-get", "3.0.0"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got, err := r.ResolveByName(ctx, tc.query)
			if err != nil {
				t.Fatalf("resolve %q: %v", tc.query, err)
			}
			if got.Version != tc.want {
				t.Fatalf("resolve %q = %s, want %s", tc.query, got.Version, tc.want)
			}
		})
	}
}

// AC-006 race test: 1000 parallel RegisterEntry calls yield exactly 1000 entries.
func TestRegisterEntry_Race1000(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	const goroutines = 1000

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := newEntry("race", "tool-"+strconv.Itoa(idx), "1.0.0")
			if err := r.RegisterEntry(ctx, e); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	list := r.ListFiltered(ctx, ToolFilter{Namespace: "race"})
	if len(list) != goroutines {
		t.Fatalf("expected %d entries, got %d", goroutines, len(list))
	}
}

// AC-009: VerifyBinary returns ErrBinaryDrift on sha mismatch.
func TestVerifyBinary_DriftDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.bin")
	original := []byte("good content")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sum := sha256.Sum256(original)
	expected := hex.EncodeToString(sum[:])

	e := &ToolEntry{BinaryPath: path, BinarySHA256: expected}
	r := NewRegistry()

	if err := r.VerifyBinary(e); err != nil {
		t.Fatalf("expected match, got %v", err)
	}

	// Rewrite → drift.
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	err := r.VerifyBinary(e)
	if err == nil {
		t.Fatal("expected ErrBinaryDrift, got nil")
	}
	var regErr *RegistryError
	if !errors.As(err, &regErr) || regErr.Code != CodeBinaryDrift {
		t.Fatalf("expected ErrBinaryDrift, got %v", err)
	}
}

// Schema validation: malformed JSON → ErrSchemaInvalid.
func TestRegisterEntry_SchemaInvalid(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	e := newEntry("brae", "broken", "1.0.0")
	e.JSONSchema = json.RawMessage(`{"oops":`)

	err := r.RegisterEntry(ctx, e)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

// Unregister enforces origin=agent-authored.
func TestUnregister_OriginGuard(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	user := newEntry("brae", "auth-tool", "1.0.0")
	user.Origin = OriginUser
	_ = r.RegisterEntry(ctx, user)

	err := r.Unregister(ctx, "brae/auth-tool@1.0.0")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden on non-agent origin, got %v", err)
	}

	// Replace with agent-authored → allowed.
	agent := newEntry("brae", "agent-tool", "1.0.0")
	agent.Origin = OriginAgentAuthored
	_ = r.RegisterEntry(ctx, agent)
	if err := r.Unregister(ctx, "brae/agent-tool@1.0.0"); err != nil {
		t.Fatalf("expected delete success for agent-authored: %v", err)
	}
}

// Bootstrap reconciles persisted rows into memory (AC-004).
func TestBootstrap_LoadsPersistedRows(t *testing.T) {
	ctx := context.Background()
	repo := persist.NewMemoryToolRegistryRepository()

	// Seed directly via the repo — simulates a prior process lifetime.
	_ = repo.Upsert(ctx, persist.ToolRegistryEntry{
		Namespace: "brae", Name: "http-get", Version: "1.0.0",
		Origin: OriginAgentAuthored, Schema: json.RawMessage(`{}`),
	})
	_ = repo.Upsert(ctx, persist.ToolRegistryEntry{
		Namespace: "brae", Name: "http-get", Version: "2.0.0",
		Origin: OriginAgentAuthored, Schema: json.RawMessage(`{}`),
	})

	// New process boots and calls Bootstrap.
	r := NewRegistry()
	if err := r.Bootstrap(ctx, repo); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	latest, err := r.Latest(ctx, "brae", "http-get")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Version != "2.0.0" {
		t.Fatalf("latest = %s, want 2.0.0", latest.Version)
	}
}

// Filter by origin (AC-007 building block).
func TestListFiltered_ByOrigin(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()

	a := newEntry("brae", "a-tool", "1.0.0")
	a.Origin = OriginAgentAuthored
	_ = r.RegisterEntry(ctx, a)

	u := newEntry("brae", "u-tool", "1.0.0")
	u.Origin = OriginUser
	_ = r.RegisterEntry(ctx, u)

	list := r.ListFiltered(ctx, ToolFilter{Origin: OriginAgentAuthored})
	if len(list) != 1 || list[0].Name != "a-tool" {
		t.Fatalf("expected only agent-authored, got %+v", list)
	}
}

// Backward-compat: legacy Register path keeps existing Resolve semantics.
func TestLegacyRegisterAndResolve_BackwardCompat(t *testing.T) {
	r := NewRegistry()
	schema := &ToolSchema{
		Name:      "personality.get_identity",
		Namespace: "system",
		Version:   "1.0.0",
	}
	if err := r.Register(schema, dummyExecutor); err != nil {
		t.Fatalf("legacy register: %v", err)
	}

	entry, ok := r.Resolve("system/personality.get_identity")
	if !ok || entry == nil {
		t.Fatal("legacy Resolve failed")
	}
	if entry.Executor == nil {
		t.Fatal("executor missing after legacy register")
	}
	if entry.Origin != OriginBuiltin {
		t.Fatalf("expected origin=builtin, got %q", entry.Origin)
	}
}
