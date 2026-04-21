package persist

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newToolEntryTest(ns, name, ver string) ToolRegistryEntry {
	return ToolRegistryEntry{
		Namespace: ns,
		Name:      name,
		Version:   ver,
		Origin:    "agent-authored",
		Schema:    json.RawMessage(`{"type":"object"}`),
	}
}

func TestMemoryToolRepo_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryToolRegistryRepository()

	if err := r.Upsert(ctx, newToolEntryTest("brae", "http", "1.0.0")); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Duplicate
	err := r.Upsert(ctx, newToolEntryTest("brae", "http", "1.0.0"))
	if !errors.Is(err, ErrToolDuplicate) {
		t.Fatalf("expected ErrToolDuplicate, got %v", err)
	}

	// Exact
	got, err := r.Get(ctx, "brae/http@1.0.0")
	if err != nil {
		t.Fatalf("get exact: %v", err)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("version = %s", got.Version)
	}
}

func TestMemoryToolRepo_LatestAndDeprecate(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryToolRegistryRepository()

	_ = r.Upsert(ctx, newToolEntryTest("brae", "http", "1.0.0"))
	_ = r.Upsert(ctx, newToolEntryTest("brae", "http", "2.0.0"))

	latest, err := r.Latest(ctx, "brae", "http")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Version != "2.0.0" {
		t.Fatalf("latest = %s want 2.0.0", latest.Version)
	}

	if err := r.Deprecate(ctx, "brae/http@2.0.0", "drift"); err != nil {
		t.Fatalf("deprecate: %v", err)
	}

	latest, err = r.Latest(ctx, "brae", "http")
	if err != nil {
		t.Fatalf("latest after deprecate: %v", err)
	}
	if latest.Version != "1.0.0" {
		t.Fatalf("latest after deprecate = %s want 1.0.0", latest.Version)
	}

	// Explicit version still resolvable.
	v, err := r.Get(ctx, "brae/http@2.0.0")
	if err != nil {
		t.Fatalf("get explicit: %v", err)
	}
	if !v.Deprecated {
		t.Fatalf("expected deprecated=true")
	}
}

func TestMemoryToolRepo_ListAndDelete(t *testing.T) {
	ctx := context.Background()
	r := NewMemoryToolRegistryRepository()

	_ = r.Upsert(ctx, newToolEntryTest("ns1", "a", "1.0.0"))
	_ = r.Upsert(ctx, newToolEntryTest("ns1", "b", "1.0.0"))
	_ = r.Upsert(ctx, newToolEntryTest("ns2", "a", "1.0.0"))

	all, err := r.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	byNS, err := r.ListByNamespace(ctx, "ns1")
	if err != nil {
		t.Fatalf("list by ns: %v", err)
	}
	if len(byNS) != 2 {
		t.Fatalf("len(ns1) = %d, want 2", len(byNS))
	}

	if err := r.Delete(ctx, "ns1/a@1.0.0"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	err = r.Delete(ctx, "ns1/a@1.0.0")
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound on redelete, got %v", err)
	}
}

func TestSplitQualifiedName(t *testing.T) {
	cases := []struct {
		input   string
		wantOK  bool
		wantNs  string
		wantNm  string
		wantVer string
	}{
		{"brae/http@1.0.0", true, "brae", "http", "1.0.0"},
		{"brae/http", true, "brae", "http", ""},
		{"brae", false, "", "", ""},
		{"/http", false, "", "", ""},
		{"brae/", false, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			ns, nm, ver, ok := SplitQualifiedName(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v want %v", ok, tc.wantOK)
			}
			if ok {
				if ns != tc.wantNs || nm != tc.wantNm || ver != tc.wantVer {
					t.Fatalf("got (%q,%q,%q) want (%q,%q,%q)", ns, nm, ver, tc.wantNs, tc.wantNm, tc.wantVer)
				}
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.9.9", 1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			got := CompareSemver(tc.a, tc.b)
			if (got == 0 && tc.want != 0) ||
				(got > 0 && tc.want <= 0) ||
				(got < 0 && tc.want >= 0) {
				t.Fatalf("CompareSemver(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
