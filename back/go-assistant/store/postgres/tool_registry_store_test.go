//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func setupToolRegistryStore(t *testing.T) (*ToolRegistryStore, context.Context) {
	t.Helper()
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return NewToolRegistryStore(pool), context.Background()
}

func newToolEntryFixture(ns, name, ver, origin string) persist.ToolRegistryEntry {
	return persist.ToolRegistryEntry{
		Namespace:    ns,
		Name:         name,
		Version:      ver,
		Schema:       json.RawMessage(`{"type":"object"}`),
		Origin:       origin,
		RegisteredBy: "integration-test",
	}
}

func TestToolRegistryStore_Upsert_Duplicate(t *testing.T) {
	s, ctx := setupToolRegistryStore(t)

	if err := s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "1.0.0", "agent-authored")); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	err := s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "1.0.0", "agent-authored"))
	if !errors.Is(err, persist.ErrToolDuplicate) {
		t.Fatalf("expected ErrToolDuplicate, got %v", err)
	}
}

func TestToolRegistryStore_LatestAndDeprecate(t *testing.T) {
	s, ctx := setupToolRegistryStore(t)

	_ = s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "1.0.0", "agent-authored"))
	_ = s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "1.1.0", "agent-authored"))
	_ = s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "2.0.0", "agent-authored"))

	latest, err := s.Latest(ctx, "brae", "http-get")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Version != "2.0.0" {
		t.Fatalf("latest = %s want 2.0.0", latest.Version)
	}

	if err := s.Deprecate(ctx, "brae/http-get@2.0.0", "drift"); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	latest, err = s.Latest(ctx, "brae", "http-get")
	if err != nil {
		t.Fatalf("latest after deprecate: %v", err)
	}
	if latest.Version != "1.1.0" {
		t.Fatalf("latest = %s want 1.1.0", latest.Version)
	}
}

func TestToolRegistryStore_GetAndList(t *testing.T) {
	s, ctx := setupToolRegistryStore(t)

	_ = s.Upsert(ctx, newToolEntryFixture("brae", "http-get", "1.0.0", "agent-authored"))
	_ = s.Upsert(ctx, newToolEntryFixture("ns2", "tool-a", "1.0.0", "user"))

	got, err := s.Get(ctx, "brae/http-get@1.0.0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Origin != "agent-authored" {
		t.Fatalf("origin = %s", got.Origin)
	}

	all, err := s.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d want 2", len(all))
	}

	ns1, err := s.ListByNamespace(ctx, "brae")
	if err != nil {
		t.Fatalf("list by ns: %v", err)
	}
	if len(ns1) != 1 {
		t.Fatalf("len(ns1) = %d want 1", len(ns1))
	}

	_, err = s.Get(ctx, "missing/tool")
	if !errors.Is(err, persist.ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound, got %v", err)
	}
}

// TestToolRegistryStore_TaxonomyRoundTrip verifies REQ-009: hashtags and
// toolbox survive insert→fetch round-trip with values preserved, and that
// legacy rows read back with the NOT NULL defaults ({}, "").
func TestToolRegistryStore_TaxonomyRoundTrip(t *testing.T) {
	s, ctx := setupToolRegistryStore(t)

	taxed := newToolEntryFixture("brae", "http-get", "1.0.0", "agent-authored")
	taxed.Toolbox = "http"
	taxed.Hashtags = []string{"kind-fetch", "domain-web"}
	if err := s.Upsert(ctx, taxed); err != nil {
		t.Fatalf("upsert taxed: %v", err)
	}

	bare := newToolEntryFixture("brae", "echo", "1.0.0", "agent-authored")
	if err := s.Upsert(ctx, bare); err != nil {
		t.Fatalf("upsert bare: %v", err)
	}

	gotTaxed, err := s.Get(ctx, "brae/http-get@1.0.0")
	if err != nil {
		t.Fatalf("get taxed: %v", err)
	}
	if gotTaxed.Toolbox != "http" {
		t.Fatalf("toolbox = %q want http", gotTaxed.Toolbox)
	}
	if len(gotTaxed.Hashtags) != 2 ||
		gotTaxed.Hashtags[0] != "kind-fetch" ||
		gotTaxed.Hashtags[1] != "domain-web" {
		t.Fatalf("hashtags = %v want [kind-fetch domain-web]", gotTaxed.Hashtags)
	}

	gotBare, err := s.Get(ctx, "brae/echo@1.0.0")
	if err != nil {
		t.Fatalf("get bare: %v", err)
	}
	if gotBare.Toolbox != "" {
		t.Fatalf("bare toolbox = %q want empty", gotBare.Toolbox)
	}
	if len(gotBare.Hashtags) != 0 {
		t.Fatalf("bare hashtags = %v want empty", gotBare.Hashtags)
	}
}

func TestToolRegistryStore_Delete(t *testing.T) {
	s, ctx := setupToolRegistryStore(t)
	_ = s.Upsert(ctx, newToolEntryFixture("brae", "tmp", "1.0.0", "agent-authored"))

	if err := s.Delete(ctx, "brae/tmp@1.0.0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.Get(ctx, "brae/tmp@1.0.0")
	if !errors.Is(err, persist.ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound after delete, got %v", err)
	}
	err = s.Delete(ctx, "brae/tmp@1.0.0")
	if !errors.Is(err, persist.ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound on repeat delete, got %v", err)
	}
}
