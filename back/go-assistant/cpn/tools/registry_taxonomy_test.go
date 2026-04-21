package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Test doubles ──────────────────────────────────────────────────────────

// fakeLexicon is a tiny in-memory cpn.Lexicon for tests.
type fakeLexicon struct {
	known map[string]string // tag → kind
	order []string
}

func newFakeLexicon(entries ...[2]string) *fakeLexicon {
	f := &fakeLexicon{known: make(map[string]string, len(entries))}
	for _, kv := range entries {
		f.known[kv[0]] = kv[1]
		f.order = append(f.order, kv[0])
	}
	return f
}

func (f *fakeLexicon) IsKnown(tag string) bool {
	_, ok := f.known[tag]
	return ok
}

func (f *fakeLexicon) Kind(tag string) (string, bool) {
	k, ok := f.known[tag]
	return k, ok
}

func (f *fakeLexicon) Describe(tag string) (string, bool) {
	_, ok := f.known[tag]
	return "", ok
}

func (f *fakeLexicon) All() []cpn.LexiconEntry {
	out := make([]cpn.LexiconEntry, 0, len(f.order))
	for _, t := range f.order {
		out = append(out, cpn.LexiconEntry{Tag: t, Kind: f.known[t]})
	}
	return out
}

// eventSink collects registry events for assertions.
type eventSink struct {
	mu     sync.Mutex
	events []ToolRegistryEvent
}

func (s *eventSink) emit(e ToolRegistryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *eventSink) byType(t string) []ToolRegistryEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ToolRegistryEvent
	for _, e := range s.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// ── Helpers ───────────────────────────────────────────────────────────────

func mkEntry(ns, name, version string, hashtags []string) *ToolEntry {
	return &ToolEntry{
		Namespace:    ns,
		Name:         name,
		Version:      version,
		JSONSchema:   json.RawMessage(`{"type":"object"}`),
		Origin:       OriginBuiltin,
		RegisteredBy: "test",
		Hashtags:     hashtags,
	}
}

func mkEntryTB(ns, name, version, toolbox string, hashtags []string) *ToolEntry {
	e := mkEntry(ns, name, version, hashtags)
	e.Toolbox = toolbox
	return e
}

// ── AC-002: ListByHashtag returns latest non-deprecated entry ────────────

func TestRegistry_ListByHashtag_AC002(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntry("pdf", "pdf-to-text", "0.1.0", []string{"tools", "read", "pdf"})); err != nil {
		t.Fatalf("RegisterEntry: %v", err)
	}
	got := reg.ListByHashtag(ctx, "pdf")
	if len(got) != 1 {
		t.Fatalf("ListByHashtag pdf: want 1, got %d", len(got))
	}
	if got[0].QualifiedName() != "pdf/pdf-to-text@0.1.0" {
		t.Errorf("unexpected entry: %s", got[0].QualifiedName())
	}
}

// ListByHashtag excludes deprecated entries.
func TestRegistry_ListByHashtag_ExcludesDeprecated(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntry("pdf", "old", "0.1.0", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.Deprecate(ctx, "pdf/old@0.1.0", "retired"); err != nil {
		t.Fatal(err)
	}
	if got := reg.ListByHashtag(ctx, "pdf"); len(got) != 0 {
		t.Errorf("expected 0 non-deprecated, got %d", len(got))
	}
}

// ── AC-003: Toolboxes returns one manifest per distinct toolbox ──────────

func TestRegistry_Toolboxes_AC003(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntryTB("pdf", "pdf-to-text", "0.1.0", "pdf", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterEntry(ctx, mkEntryTB("web", "fetch", "0.1.0", "web", []string{"network"})); err != nil {
		t.Fatal(err)
	}
	tbs := reg.Toolboxes(ctx)
	if len(tbs) != 2 {
		t.Fatalf("want 2 toolboxes, got %d", len(tbs))
	}
	for _, tb := range tbs {
		if tb.ToolCount != 1 {
			t.Errorf("toolbox %s: ToolCount = %d, want 1", tb.Namespace, tb.ToolCount)
		}
	}
}

// Toolboxes deterministic ordering under shuffled inserts.
func TestRegistry_Toolboxes_DeterministicOrder(t *testing.T) {
	want := []string{"alpha", "beta", "gamma"}
	for trial, order := range [][]string{
		{"gamma", "alpha", "beta"},
		{"beta", "gamma", "alpha"},
		{"alpha", "beta", "gamma"},
	} {
		reg := NewRegistry()
		ctx := context.Background()
		for _, ns := range order {
			if err := reg.RegisterEntry(ctx, mkEntryTB(ns, "t", "0.1.0", ns, []string{"tools"})); err != nil {
				t.Fatal(err)
			}
		}
		tbs := reg.Toolboxes(ctx)
		got := make([]string, len(tbs))
		for i, tb := range tbs {
			got[i] = tb.Namespace
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("trial %d (insert %v): got %v, want %v", trial, order, got, want)
		}
	}
}

// ── AC-004: known + unknown hashtag → drop + event, registration succeeds ─

func TestRegistry_LexiconDrift_AC004(t *testing.T) {
	sink := &eventSink{}
	lex := newFakeLexicon([2]string{"tools", "kind"})
	reg := NewRegistry(WithLexicon(lex))
	reg.OnEvent(sink.emit)
	ctx := context.Background()

	e := mkEntry("developer", "git-blame", "0.1.0", []string{"tools", "frobnicate"})
	if err := reg.RegisterEntry(ctx, e); err != nil {
		t.Fatalf("RegisterEntry: %v", err)
	}

	// Unknown dropped from persisted set.
	if len(e.Hashtags) != 1 || e.Hashtags[0] != "tools" {
		t.Errorf("persisted hashtags = %v, want [tools]", e.Hashtags)
	}
	// Queryable by known tag.
	if got := reg.ListByHashtag(ctx, "tools"); len(got) != 1 {
		t.Errorf("ListByHashtag(tools) got %d, want 1", len(got))
	}
	// Exactly one lexicon.entry.dropped event naming the dropped tag.
	drops := sink.byType("lexicon.entry.dropped")
	if len(drops) != 1 {
		t.Fatalf("want 1 lexicon.entry.dropped, got %d", len(drops))
	}
	if !strings.Contains(drops[0].Reason, "frobnicate") {
		t.Errorf("reason should name frobnicate; got %q", drops[0].Reason)
	}
	// SEC-003: reason must not carry HelpText/Provenance.
	if strings.Contains(drops[0].Reason, "HelpText") {
		t.Errorf("reason leaked HelpText: %q", drops[0].Reason)
	}
}

// AC-014: many unknown, one known → registration succeeds, all unknown dropped.
func TestRegistry_LexiconDrift_ManyUnknown_AC014(t *testing.T) {
	sink := &eventSink{}
	lex := newFakeLexicon([2]string{"tools", "kind"})
	reg := NewRegistry(WithLexicon(lex))
	reg.OnEvent(sink.emit)
	ctx := context.Background()

	unknowns := []string{"a1", "b2", "c3", "d4", "e5", "f6", "g7", "h8", "i9", "j0"}
	tags := append([]string{"tools"}, unknowns...)
	e := mkEntry("dev", "many", "0.1.0", tags)
	if err := reg.RegisterEntry(ctx, e); err != nil {
		t.Fatalf("RegisterEntry: %v", err)
	}
	if len(e.Hashtags) != 1 || e.Hashtags[0] != "tools" {
		t.Errorf("persisted hashtags = %v, want [tools]", e.Hashtags)
	}
	drops := sink.byType("lexicon.entry.dropped")
	if len(drops) != 1 {
		t.Fatalf("want exactly 1 lexicon.entry.dropped, got %d", len(drops))
	}
	for _, u := range unknowns {
		if !strings.Contains(drops[0].Reason, u) {
			t.Errorf("reason should enumerate %q; got %q", u, drops[0].Reason)
		}
	}
}

// ── AC-005: invalid hashtag → ErrInvalidHashtag, no entry inserted ───────

func TestRegistry_InvalidHashtag_AC005(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	err := reg.RegisterEntry(ctx, mkEntry("x", "y", "0.1.0", []string{"Foo!"}))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidHashtag) {
		t.Errorf("want ErrInvalidHashtag, got %v", err)
	}
	// Registry must not hold the entry.
	if _, ok := reg.Resolve("x/y@0.1.0"); ok {
		t.Errorf("entry should not have been inserted")
	}
}

// ── AC-006: >12 hashtags → ErrTooManyHashtags ────────────────────────────

func TestRegistry_TooManyHashtags_AC006(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	tags := make([]string, 13)
	for i := range tags {
		tags[i] = "t" + string(rune('a'+i))
	}
	err := reg.RegisterEntry(ctx, mkEntry("x", "y", "0.1.0", tags))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTooManyHashtags) {
		t.Errorf("want ErrTooManyHashtags, got %v", err)
	}
}

// ── AC-010: empty Toolbox defaults to Namespace ──────────────────────────

func TestRegistry_ToolboxDefault_AC010(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntry("developer", "lint", "0.1.0", nil)); err != nil {
		t.Fatal(err)
	}
	e, err := reg.Get(ctx, "developer/lint@0.1.0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.Toolbox != "developer" {
		t.Errorf("Toolbox = %q, want %q", e.Toolbox, "developer")
	}
}

// ── BEH-002: toolbox.conflict.detected across versions at same anchor ───

func TestRegistry_ToolboxConflict(t *testing.T) {
	sink := &eventSink{}
	reg := NewRegistry()
	reg.OnEvent(sink.emit)
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntryTB("developer", "lint", "0.1.0", "general", nil)); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterEntry(ctx, mkEntryTB("developer", "lint", "0.2.0", "developer", nil)); err != nil {
		t.Fatal(err)
	}
	events := sink.byType("toolbox.conflict.detected")
	if len(events) != 1 {
		t.Fatalf("want 1 conflict event, got %d", len(events))
	}
	if !strings.Contains(events[0].Reason, "general") || !strings.Contains(events[0].Reason, "developer") {
		t.Errorf("reason should mention both toolboxes; got %q", events[0].Reason)
	}
}

// ListByToolbox is case-insensitive + returns latest only.
func TestRegistry_ListByToolbox(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntryTB("pdf", "a", "0.1.0", "PDF", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterEntry(ctx, mkEntryTB("pdf", "b", "0.1.0", "pdf", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	got := reg.ListByToolbox(ctx, "pdf")
	if len(got) != 2 {
		t.Fatalf("ListByToolbox: want 2, got %d", len(got))
	}
	names := make([]string, len(got))
	for i, e := range got {
		names[i] = e.QualifiedName()
	}
	sort.Strings(names)
	if names[0] != "pdf/a@0.1.0" || names[1] != "pdf/b@0.1.0" {
		t.Errorf("unexpected entries: %v", names)
	}
}

// latest non-deprecated hashtag semantics: after registering v0.2.0 the
// older v0.1.0 no longer participates in byHashtag.
func TestRegistry_ListByHashtag_LatestOnly(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	if err := reg.RegisterEntry(ctx, mkEntry("pdf", "x", "0.1.0", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterEntry(ctx, mkEntry("pdf", "x", "0.2.0", []string{"pdf"})); err != nil {
		t.Fatal(err)
	}
	got := reg.ListByHashtag(ctx, "pdf")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 entry, got %d", len(got))
	}
	if got[0].Version != "0.2.0" {
		t.Errorf("want latest (0.2.0), got %s", got[0].Version)
	}
	_ = time.Now // silence unused import if we refactor later
}
