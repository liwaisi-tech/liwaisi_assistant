package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Mock repo ────────────────────────────────────────────────────────────

type mockPersonalityRepo struct {
	store map[string]*persist.PersonalityRecord
	mu    sync.Mutex
}

func newMockRepo() *mockPersonalityRepo {
	return &mockPersonalityRepo{store: make(map[string]*persist.PersonalityRecord)}
}

func (m *mockPersonalityRepo) Get(_ context.Context, userID string) (*persist.PersonalityRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.store[userID]
	if !ok {
		return nil, nil // not found — matches real Postgres behavior
	}
	return rec, nil
}

func (m *mockPersonalityRepo) Save(_ context.Context, rec *persist.PersonalityRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[rec.UserID] = rec
	return nil
}

func (m *mockPersonalityRepo) Delete(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, userID)
	return nil
}

// errRepo always returns errors on Get.
type errRepo struct{ mockPersonalityRepo }

func (e *errRepo) Get(context.Context, string) (*persist.PersonalityRecord, error) {
	return nil, errors.New("db down")
}

// ── Helpers ──────────────────────────────────────────────────────────────

func seedRepo(t *testing.T, repo *mockPersonalityRepo, userID string, p *cpn.Personality) {
	t.Helper()
	p.UserID = userID
	rec, err := personalityToRecord(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = repo.Save(context.Background(), rec)
}

func makeDeps(repo persist.PersonalityRepository) *PersonalityToolDeps {
	return &PersonalityToolDeps{
		Repo:        repo,
		DefaultPers: cpn.DefaultPersonality(),
	}
}

func jsonToken(t *testing.T, v any) cpn.Token {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonToken: %v", err)
	}
	return cpn.Token{
		Color:   cpn.ColorJSON,
		Payload: json.RawMessage(raw),
	}
}

func stringToken(s string) cpn.Token {
	return cpn.Token{
		Color:   cpn.ColorString,
		Payload: s,
	}
}

// ── Tests ────────────────────────────────────────────────────────────────

func TestGetIdentity_FromRepo(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)

	custom := cpn.DefaultPersonality()
	custom.Principles[1].Title = "Custom Conducta"
	custom.Version = 5
	seedRepo(t, repo, "user-1", custom)

	exec := makeGetIdentity(deps)
	out, err := exec(context.Background(), stringToken("user-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Color != cpn.ColorIdentity {
		t.Errorf("color = %s, want IDENTITY", out.Color)
	}

	p, ok := out.Payload.(*cpn.Personality)
	if !ok {
		t.Fatalf("payload type = %T, want *cpn.Personality", out.Payload)
	}
	if p.Principles[1].Title != "Custom Conducta" {
		t.Errorf("title = %q, want %q", p.Principles[1].Title, "Custom Conducta")
	}
	if p.Version != 5 {
		t.Errorf("version = %d, want 5", p.Version)
	}
}

func TestGetIdentity_Default(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)

	exec := makeGetIdentity(deps)
	out, err := exec(context.Background(), stringToken("unknown-user"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, ok := out.Payload.(*cpn.Personality)
	if !ok {
		t.Fatalf("payload type = %T, want *cpn.Personality", out.Payload)
	}

	def := cpn.DefaultPersonality()
	if p.Principles[0].Title != def.Principles[0].Title {
		t.Errorf("got %q, want default %q", p.Principles[0].Title, def.Principles[0].Title)
	}
}

func TestGetIdentity_RepoError(t *testing.T) {
	repo := &errRepo{}
	deps := makeDeps(repo)

	exec := makeGetIdentity(deps)
	_, err := exec(context.Background(), stringToken("user-1"))
	if err == nil {
		t.Fatal("expected error for repo failure, got nil")
	}
}

func TestSetPrinciple_Conducta(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	input := setPrincipleInput{
		UserID:      "user-1",
		Kind:        "conducta",
		Title:       "Warmth Plus",
		Description: "Even warmer tone",
	}
	exec := makeSetPrinciple(deps)
	out, err := exec(context.Background(), jsonToken(t, input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := out.Payload.(*cpn.Personality)
	if p.Principles[1].Title != "Warmth Plus" {
		t.Errorf("title = %q, want %q", p.Principles[1].Title, "Warmth Plus")
	}
	if p.Principles[1].Description != "Even warmer tone" {
		t.Errorf("description mismatch")
	}
	if p.Version != 1 {
		t.Errorf("version = %d, want 1", p.Version)
	}

	// Verify persisted.
	rec, err := repo.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("not persisted: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("persisted version = %d, want 1", rec.Version)
	}
}

func TestSetPrinciple_EticaExtend(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	def := cpn.DefaultPersonality()
	eticaRules := def.Principles[2].Rules
	extended := make([]string, len(eticaRules), len(eticaRules)+1)
	copy(extended, eticaRules)
	extended = append(extended, "Extra rule: always log privacy decisions")

	input := setPrincipleInput{
		UserID: "user-1",
		Kind:   "etica",
		Rules:  extended,
	}
	exec := makeSetPrinciple(deps)
	out, err := exec(context.Background(), jsonToken(t, input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := out.Payload.(*cpn.Personality)
	if len(p.Principles[2].Rules) != len(extended) {
		t.Errorf("rules len = %d, want %d", len(p.Principles[2].Rules), len(extended))
	}
}

func TestSetPrinciple_EticaRemoveRule(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	// Try to set etica with only one rule (missing core rules).
	input := setPrincipleInput{
		UserID: "user-1",
		Kind:   "etica",
		Rules:  []string{"Only this rule"},
	}
	exec := makeSetPrinciple(deps)
	_, err := exec(context.Background(), jsonToken(t, input))
	if err == nil {
		t.Fatal("expected ErrEticaViolation, got nil")
	}
	if !errors.Is(err, cpn.ErrEticaViolation) {
		t.Errorf("error = %v, want ErrEticaViolation", err)
	}
}

func TestReset(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	exec := makeReset(deps)
	out, err := exec(context.Background(), stringToken("user-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Color != cpn.ColorIdentity {
		t.Errorf("color = %s, want IDENTITY", out.Color)
	}

	p := out.Payload.(*cpn.Personality)
	def := cpn.DefaultPersonality()
	if p.Principles[0].Title != def.Principles[0].Title {
		t.Errorf("got %q, want default %q", p.Principles[0].Title, def.Principles[0].Title)
	}

	// Verify deleted — Get returns nil record, nil error (not-found).
	rec, err := repo.Get(context.Background(), "user-1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rec != nil {
		t.Error("expected nil record after delete, got non-nil")
	}
}

func TestGetTensions(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	exec := makeGetTensions(deps)
	out, err := exec(context.Background(), stringToken("user-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Color != cpn.ColorJSON {
		t.Errorf("color = %s, want JSON", out.Color)
	}

	raw, ok := out.Payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type = %T, want json.RawMessage", out.Payload)
	}

	var tensions [3]cpn.TensionRule
	if err := json.Unmarshal(raw, &tensions); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	def := cpn.DefaultPersonality()
	if tensions[0].Friction != def.Tensions[0].Friction {
		t.Errorf("friction = %q, want %q", tensions[0].Friction, def.Tensions[0].Friction)
	}
}

func TestSetHierarchy_Valid(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	input := setHierarchyInput{
		UserID:    "user-1",
		Hierarchy: []string{"nucleo", "etica", "conducta"},
	}
	exec := makeSetHierarchy(deps)
	out, err := exec(context.Background(), jsonToken(t, input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := out.Payload.(*cpn.Personality)
	want := [3]cpn.PrincipleKind{cpn.PrincipleNucleo, cpn.PrincipleEtica, cpn.PrincipleConducta}
	if p.Hierarchy != want {
		t.Errorf("hierarchy = %v, want %v", p.Hierarchy, want)
	}
	if p.Version != 1 {
		t.Errorf("version = %d, want 1", p.Version)
	}
}

func TestSetHierarchy_EticaLast(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	seedRepo(t, repo, "user-1", cpn.DefaultPersonality())

	input := setHierarchyInput{
		UserID:    "user-1",
		Hierarchy: []string{"nucleo", "conducta", "etica"},
	}
	exec := makeSetHierarchy(deps)
	_, err := exec(context.Background(), jsonToken(t, input))
	if err == nil {
		t.Fatal("expected ErrEticaCannotBeLast, got nil")
	}
	if !errors.Is(err, cpn.ErrEticaCannotBeLast) {
		t.Errorf("error = %v, want ErrEticaCannotBeLast", err)
	}
}

func TestRegisterPersonalityTools(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	reg := NewRegistry()

	if err := RegisterPersonalityTools(reg, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	expected := []string{
		"system/personality.get_identity",
		"system/personality.set_principle",
		"system/personality.reset",
		"system/personality.get_tensions",
		"system/personality.set_hierarchy",
		"system/identity.about_liwaisi",
	}
	for _, qn := range expected {
		entry, ok := reg.Resolve(qn)
		if !ok {
			t.Errorf("tool %q not registered", qn)
			continue
		}
		if entry.Executor == nil {
			t.Errorf("tool %q has nil executor", qn)
		}
	}

	schemas := reg.List("system")
	if len(schemas) != 6 {
		t.Errorf("system tools = %d, want 6", len(schemas))
	}
}

func TestPersonalityTools_HaveParameters(t *testing.T) {
	repo := newMockRepo()
	deps := makeDeps(repo)
	reg := NewRegistry()

	if err := RegisterPersonalityTools(reg, deps); err != nil {
		t.Fatalf("register: %v", err)
	}

	expected := []string{
		"system/personality.get_identity",
		"system/personality.set_principle",
		"system/personality.reset",
		"system/personality.get_tensions",
		"system/personality.set_hierarchy",
		"system/identity.about_liwaisi",
	}
	for _, qn := range expected {
		entry, ok := reg.Resolve(qn)
		if !ok {
			t.Errorf("tool %q not registered", qn)
			continue
		}
		if entry.Schema.Parameters == nil {
			t.Errorf("tool %q has nil Parameters", qn)
			continue
		}
		// Verify it's valid JSON.
		var parsed map[string]interface{}
		if err := json.Unmarshal(entry.Schema.Parameters, &parsed); err != nil {
			t.Errorf("tool %q Parameters is not valid JSON: %v", qn, err)
		}
		if parsed["type"] != "object" {
			t.Errorf("tool %q Parameters type = %v, want 'object'", qn, parsed["type"])
		}
	}
}
