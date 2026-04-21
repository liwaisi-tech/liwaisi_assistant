package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
)

// stubRegistry is an in-memory cpn.ModelRegistry used to exercise the
// REQ-GATE-001 fallback path in SessionService.resolveModelWithGate.
// Only the four methods the resolver calls (GetInvokable, GetRoleDefault,
// GetProductDefault, plus the minimal set to satisfy the interface) are
// exercised; the rest return nil/ErrModelNotFound.
type stubRegistry struct {
	invokable      map[string]*cpn.ModelRegistryEntry
	roleDefaults   map[string]string
	productDefault *cpn.ModelRegistryEntry
	productErr     error
}

func (s *stubRegistry) GetInvokable(_ context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	if e, ok := s.invokable[id]; ok {
		return e, nil
	}
	return nil, cpn.ErrModelNotInvokable
}
func (s *stubRegistry) GetByID(_ context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	if e, ok := s.invokable[id]; ok {
		return e, nil
	}
	return nil, cpn.ErrModelNotFound
}
func (s *stubRegistry) GetProductDefault(context.Context) (*cpn.ModelRegistryEntry, error) {
	if s.productErr != nil {
		return nil, s.productErr
	}
	return s.productDefault, nil
}
func (s *stubRegistry) ListInvokable(context.Context) ([]*cpn.ModelRegistryEntry, error) {
	return nil, nil
}
func (s *stubRegistry) ListAll(context.Context, cpn.ModelListFilter) ([]*cpn.ModelRegistryEntry, int, error) {
	return nil, 0, nil
}
func (s *stubRegistry) Insert(context.Context, *cpn.ModelRegistryEntry) error {
	return nil
}
func (s *stubRegistry) Update(context.Context, *cpn.ModelRegistryEntry, cpn.UpdatedAt) error {
	return nil
}
func (s *stubRegistry) Delete(context.Context, string) error                              { return nil }
func (s *stubRegistry) SetProductDefault(context.Context, string, string) error           { return nil }
func (s *stubRegistry) SetLicenseReview(context.Context, string, cpn.LicenseReview) error { return nil }
func (s *stubRegistry) GetRoleDefault(_ context.Context, role string) (string, error) {
	if id, ok := s.roleDefaults[role]; ok {
		return id, nil
	}
	return "", cpn.ErrModelNotFound
}
func (s *stubRegistry) SetRoleDefault(context.Context, string, string) error { return nil }

func invokableEntry(registryID string) *cpn.ModelRegistryEntry {
	return &cpn.ModelRegistryEntry{
		RegistryID: registryID,
		Lifecycle:  cpn.Lifecycle{State: cpn.LifecycleActive},
		License:    cpn.License{Status: cpn.LicenseApprovedCommercial},
	}
}

func newJSONLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// TestResolveModelWithGate_InvokableCandidate covers the happy path — the
// user's preferred model passes REQ-GATE-001 and is returned unchanged.
func TestResolveModelWithGate_InvokableCandidate(t *testing.T) {
	logger, buf := newJSONLogger()
	reg := &stubRegistry{
		invokable: map[string]*cpn.ModelRegistryEntry{
			"anthropic/claude-haiku-4-5": invokableEntry("anthropic/claude-haiku-4-5"),
		},
	}
	s := &SessionService{logger: logger, modelRegistry: reg}
	rec := &persist.UserRecord{PreferredModel: "anthropic/claude-haiku-4-5"}

	got := s.resolveModelWithGate(context.Background(), rec, "")
	if got != "anthropic/claude-haiku-4-5" {
		t.Fatalf("resolved %q, want haiku", got)
	}
	if strings.Contains(buf.String(), "falling back") {
		t.Fatalf("no fallback should have been logged: %s", buf.String())
	}
}

// TestResolveModelWithGate_FallbackOnNonInvokable covers REQ-OBS-004: when
// the user's preferred model is not invokable, the resolver falls back to
// the product default and emits a WARN with the reason.
func TestResolveModelWithGate_FallbackOnNonInvokable(t *testing.T) {
	logger, buf := newJSONLogger()
	reg := &stubRegistry{
		invokable: map[string]*cpn.ModelRegistryEntry{
			"google/gemma-4-31b-it": invokableEntry("google/gemma-4-31b-it"),
		},
		productDefault: invokableEntry("google/gemma-4-31b-it"),
	}
	s := &SessionService{logger: logger, modelRegistry: reg}
	rec := &persist.UserRecord{PreferredModel: "disabled/model"}

	got := s.resolveModelWithGate(context.Background(), rec, "")
	if got != "google/gemma-4-31b-it" {
		t.Fatalf("resolved %q, want gemma (product default)", got)
	}

	// Verify the WARN was emitted (REQ-OBS-004).
	logs := buf.String()
	if !strings.Contains(logs, `"level":"WARN"`) {
		t.Fatalf("expected WARN level in logs, got: %s", logs)
	}
	if !strings.Contains(logs, `"candidate":"disabled/model"`) {
		t.Fatalf("expected candidate=disabled/model in logs, got: %s", logs)
	}
	if !strings.Contains(logs, `"source":"user preferred"`) {
		t.Fatalf("expected source=user preferred, got: %s", logs)
	}
}

// TestResolveModelWithGate_RoleDefaultInsertedBetweenPreferredAndProductDefault
// covers the new cascade rung (REQ-REG-004): when a role-specific default
// exists in the registry and the user has no preferences, the role default
// is used (not the product default).
func TestResolveModelWithGate_RoleDefault(t *testing.T) {
	logger, _ := newJSONLogger()
	reg := &stubRegistry{
		invokable: map[string]*cpn.ModelRegistryEntry{
			"anthropic/claude-opus-4-6": invokableEntry("anthropic/claude-opus-4-6"),
			"google/gemma-4-31b-it":     invokableEntry("google/gemma-4-31b-it"),
		},
		roleDefaults:   map[string]string{"reasoning": "anthropic/claude-opus-4-6"},
		productDefault: invokableEntry("google/gemma-4-31b-it"),
	}
	s := &SessionService{logger: logger, modelRegistry: reg}

	got := s.resolveModelWithGate(context.Background(), &persist.UserRecord{}, "reasoning")
	if got != "anthropic/claude-opus-4-6" {
		t.Fatalf("resolved %q, want opus (role default)", got)
	}
}

// TestResolveModelWithGate_UserOverrideHonoredWhenInvokable asserts the
// per-role override still wins over preferred when both are invokable.
func TestResolveModelWithGate_UserOverrideWins(t *testing.T) {
	logger, _ := newJSONLogger()
	reg := &stubRegistry{
		invokable: map[string]*cpn.ModelRegistryEntry{
			"anthropic/claude-opus-4-6":  invokableEntry("anthropic/claude-opus-4-6"),
			"anthropic/claude-haiku-4-5": invokableEntry("anthropic/claude-haiku-4-5"),
		},
		productDefault: invokableEntry("google/gemma-4-31b-it"),
	}
	s := &SessionService{logger: logger, modelRegistry: reg}
	rec := &persist.UserRecord{
		PreferredModel: "anthropic/claude-haiku-4-5",
		ModelOverrides: map[string]string{"reasoning": "anthropic/claude-opus-4-6"},
	}

	got := s.resolveModelWithGate(context.Background(), rec, "reasoning")
	if got != "anthropic/claude-opus-4-6" {
		t.Fatalf("resolved %q, want opus (override)", got)
	}
}

// TestResolveModelWithGate_ProductDefaultUnreachable covers the infra-failure
// path: when the registry itself errors on GetProductDefault, the resolver
// falls back to the compile-time constant so the session still boots.
func TestResolveModelWithGate_ProductDefaultUnreachable(t *testing.T) {
	logger, buf := newJSONLogger()
	reg := &stubRegistry{
		// No invokable entries; no role defaults.
		productErr: cpn.ErrModelNotFound, // simulate registry outage
	}
	s := &SessionService{logger: logger, modelRegistry: reg}

	got := s.resolveModelWithGate(context.Background(), &persist.UserRecord{PreferredModel: "nope"}, "")
	if got != openrouter.ProductDefaultModel {
		t.Fatalf("resolved %q, want compile-time fallback %q", got, openrouter.ProductDefaultModel)
	}

	// The unreachability should have been logged as ERROR.
	var sawError bool
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["level"] == "ERROR" {
			sawError = true
			break
		}
	}
	if !sawError {
		t.Fatalf("expected ERROR level log on product-default unreachable, got: %s", buf.String())
	}
}

// TestResolveModelWithGate_LegacyPathPreservedWhenNoRegistry confirms that
// the existing TestApplyUserModelPreferences suite continues to hold: with
// no registry wired, behavior exactly matches resolveModelForUser.
func TestResolveModelWithGate_LegacyPath(t *testing.T) {
	logger, _ := newJSONLogger()
	s := &SessionService{logger: logger} // no modelRegistry
	rec := &persist.UserRecord{PreferredModel: "legacy/model"}

	got := s.resolveModelWithGate(context.Background(), rec, "classifier")
	if got != "legacy/model" {
		t.Fatalf("legacy path returned %q, want legacy/model (no gate)", got)
	}
}

// countingRegistry wraps stubRegistry and counts the methods that
// resolveModelWithGateCached touches. Used to prove REQ-FIX-011's cache.
type countingRegistry struct {
	stubRegistry
	getInvokableCalls   int
	getRoleDefaultCalls int
	getProductCalls     int
}

func (c *countingRegistry) GetInvokable(ctx context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	c.getInvokableCalls++
	return c.stubRegistry.GetInvokable(ctx, id)
}
func (c *countingRegistry) GetRoleDefault(ctx context.Context, role string) (string, error) {
	c.getRoleDefaultCalls++
	return c.stubRegistry.GetRoleDefault(ctx, role)
}
func (c *countingRegistry) GetProductDefault(ctx context.Context) (*cpn.ModelRegistryEntry, error) {
	c.getProductCalls++
	return c.stubRegistry.GetProductDefault(ctx)
}

// TestResolveModelWithGate_CachesAcrossCalls covers REQ-FIX-011 (AC-011):
// repeated resolution for the same (candidate, role) tuple in one session
// build must hit the registry at most once — not once per transition.
func TestResolveModelWithGate_CachesAcrossCalls(t *testing.T) {
	logger, _ := newJSONLogger()
	reg := &countingRegistry{
		stubRegistry: stubRegistry{
			invokable: map[string]*cpn.ModelRegistryEntry{
				"anthropic/claude-haiku-4-5": invokableEntry("anthropic/claude-haiku-4-5"),
				"anthropic/claude-opus-4-6":  invokableEntry("anthropic/claude-opus-4-6"),
			},
			roleDefaults:   map[string]string{"reasoning": "anthropic/claude-opus-4-6"},
			productDefault: invokableEntry("anthropic/claude-haiku-4-5"),
		},
	}
	s := &SessionService{logger: logger, modelRegistry: reg}
	rec := &persist.UserRecord{PreferredModel: "anthropic/claude-haiku-4-5"}

	cache := newResolveCache()
	// Simulate 8 transitions across 2 roles — classic worker topology shape.
	for range 8 {
		_ = s.resolveModelWithGateCached(context.Background(), rec, "classifier", cache)
		_ = s.resolveModelWithGateCached(context.Background(), rec, "reasoning", cache)
	}

	// Preferred candidate "claude-haiku" must resolve via cache after the
	// first lookup; role default "reasoning" pulls "claude-opus" once more.
	if reg.getInvokableCalls > 2 {
		t.Fatalf("GetInvokable called %d times, want <= 2 (cache miss once per unique candidate)", reg.getInvokableCalls)
	}
	if reg.getRoleDefaultCalls > 1 {
		t.Fatalf("GetRoleDefault called %d times, want <= 1 per unique role with hits cached", reg.getRoleDefaultCalls)
	}
	if reg.getProductCalls != 0 {
		t.Fatalf("GetProductDefault called %d times, want 0 (preferred resolved first)", reg.getProductCalls)
	}
}
