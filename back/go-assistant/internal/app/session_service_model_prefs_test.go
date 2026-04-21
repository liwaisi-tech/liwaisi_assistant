package app

import (
	"context"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
)

// stubUserRepo is a minimal in-memory persist.UserRepository used by
// applyUserModelPreferences tests. Only GetByID is exercised.
type stubUserRepo struct {
	rec *persist.UserRecord
	err error
}

func (s *stubUserRepo) Upsert(context.Context, *persist.UserRecord) error { return nil }
func (s *stubUserRepo) GetByID(_ context.Context, _ string) (*persist.UserRecord, error) {
	return s.rec, s.err
}
func (s *stubUserRepo) GetByEmail(context.Context, string) (*persist.UserRecord, error) {
	return nil, persist.ErrUserNotFound
}
func (s *stubUserRepo) UpdatePreferences(context.Context, string, *persist.UserPreferences) error {
	return nil
}
func (s *stubUserRepo) CompleteOnboarding(context.Context, string) error { return nil }

// buildTestCPN returns a CPN with four transitions exercising the full role
// matrix (classifier role, reasoning role, empty role, non-LLM kind).
// Mirrors the authoring convention introduced by REQ-CFG-003: Model literals
// on authored topologies MUST be empty; intent is expressed via Role.
func buildTestCPN() *cpn.CPN {
	return &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"tClassify": {ID: "tClassify", Kind: cpn.NodeKindLLM, LLMConfig: &cpn.LLMConfig{Role: "classifier"}},
			"tReason":   {ID: "tReason", Kind: cpn.NodeKindLLM, LLMConfig: &cpn.LLMConfig{Role: "reasoning"}},
			"tDirect":   {ID: "tDirect", Kind: cpn.NodeKindLLM, LLMConfig: &cpn.LLMConfig{}},
			"tNoLLM":    {ID: "tNoLLM", Kind: cpn.NodeKindHITL}, // must be skipped
		},
	}
}

func modelOf(c *cpn.CPN, id string) string { return c.Transitions[id].LLMConfig.Model }

// TestApplyUserModelPreferences exercises REQ-CFG-005: three-level precedence
// cascade (per-role override → global preference → ProductDefaultModel),
// and asserts REQ-CFG-003 (Model is written by the resolver, never by the
// authored topology).
func TestApplyUserModelPreferences(t *testing.T) {
	def := openrouter.ProductDefaultModel

	t.Run("nil persist stamps product default everywhere", func(t *testing.T) {
		s := &SessionService{}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		for _, id := range []string{"tClassify", "tReason", "tDirect"} {
			if got := modelOf(c, id); got != def {
				t.Fatalf("transition %s: got %q, want %q", id, got, def)
			}
		}
		if c.Transitions["tNoLLM"].LLMConfig != nil {
			t.Fatal("nil-LLMConfig transition must stay nil")
		}
	})

	t.Run("empty userID stamps product default", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{PreferredModel: "X"}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "")
		for _, id := range []string{"tClassify", "tReason", "tDirect"} {
			if got := modelOf(c, id); got != def {
				t.Fatalf("transition %s: got %q, want %q", id, got, def)
			}
		}
	})

	t.Run("GetByID error stamps product default", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{err: persist.ErrUserNotFound}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if got := modelOf(c, "tClassify"); got != def {
			t.Fatalf("classifier: got %q, want %q", got, def)
		}
		if got := modelOf(c, "tDirect"); got != def {
			t.Fatalf("direct: got %q, want %q", got, def)
		}
	})

	t.Run("empty preferences stamp product default", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		for _, id := range []string{"tClassify", "tReason", "tDirect"} {
			if got := modelOf(c, id); got != def {
				t.Fatalf("transition %s: got %q, want %q", id, got, def)
			}
		}
	})

	t.Run("only PreferredModel rewrites every transition", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{PreferredModel: "anthropic/claude-haiku-4-5"}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		for _, id := range []string{"tClassify", "tReason", "tDirect"} {
			if got := modelOf(c, id); got != "anthropic/claude-haiku-4-5" {
				t.Fatalf("transition %s: got %q, want preferred", id, got)
			}
		}
	})

	t.Run("ModelOverrides win for matching role; rest fall to default", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{
			ModelOverrides: map[string]string{"classifier": "google/gemini-2.0-flash-001"},
		}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if got := modelOf(c, "tClassify"); got != "google/gemini-2.0-flash-001" {
			t.Fatalf("classifier: got %q", got)
		}
		// Reasoning and Direct have no override and no preferred → product default.
		if got := modelOf(c, "tReason"); got != def {
			t.Fatalf("reasoning: got %q, want %q", got, def)
		}
		if got := modelOf(c, "tDirect"); got != def {
			t.Fatalf("direct: got %q, want %q", got, def)
		}
	})

	t.Run("both set: override wins per-role, preferred fills the rest (AC-003)", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{
			PreferredModel: "anthropic/claude-sonnet-4-6",
			ModelOverrides: map[string]string{"classifier": "google/gemini-2.0-flash-001"},
		}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if got := modelOf(c, "tClassify"); got != "google/gemini-2.0-flash-001" {
			t.Fatalf("classifier override lost: %q", got)
		}
		if got := modelOf(c, "tReason"); got != "anthropic/claude-sonnet-4-6" {
			t.Fatalf("reasoning should fall back to preferred, got %q", got)
		}
		if got := modelOf(c, "tDirect"); got != "anthropic/claude-sonnet-4-6" {
			t.Fatalf("empty-role transition should take preferred, got %q", got)
		}
	})

	t.Run("empty-string override is ignored, falls to preferred", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{
			PreferredModel: "anthropic/claude-sonnet-4-6",
			ModelOverrides: map[string]string{"classifier": ""},
		}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if got := modelOf(c, "tClassify"); got != "anthropic/claude-sonnet-4-6" {
			t.Fatalf("empty override must fall through to preferred, got %q", got)
		}
	})
}

// TestResolveModelForUser exercises the pure precedence helper directly.
func TestResolveModelForUser(t *testing.T) {
	def := openrouter.ProductDefaultModel
	tests := []struct {
		name string
		rec  *persist.UserRecord
		role string
		want string
	}{
		{"nil record → product default", nil, "classifier", def},
		{"empty record → product default", &persist.UserRecord{}, "classifier", def},
		{"preferred only, any role", &persist.UserRecord{PreferredModel: "X"}, "classifier", "X"},
		{"preferred only, empty role", &persist.UserRecord{PreferredModel: "X"}, "", "X"},
		{"override wins over preferred", &persist.UserRecord{PreferredModel: "X", ModelOverrides: map[string]string{"classifier": "Y"}}, "classifier", "Y"},
		{"override for non-matching role ignored", &persist.UserRecord{PreferredModel: "X", ModelOverrides: map[string]string{"structured": "Y"}}, "classifier", "X"},
		{"empty role cannot look up overrides", &persist.UserRecord{PreferredModel: "X", ModelOverrides: map[string]string{"": "Y"}}, "", "X"},
		{"empty-string override ignored", &persist.UserRecord{PreferredModel: "X", ModelOverrides: map[string]string{"classifier": ""}}, "classifier", "X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveModelForUser(tt.rec, tt.role); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
