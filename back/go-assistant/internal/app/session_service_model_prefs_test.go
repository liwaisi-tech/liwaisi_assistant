package app

import (
	"context"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
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

// buildTestCPN returns a CPN with three transitions carrying distinct
// LLMConfig role keys for precedence assertions.
func buildTestCPN() *cpn.CPN {
	return &cpn.CPN{
		Transitions: map[string]*cpn.Transition{
			"tClassify": {ID: "tClassify", LLMConfig: &cpn.LLMConfig{Model: "classifier"}},
			"tReason":   {ID: "tReason", LLMConfig: &cpn.LLMConfig{Model: "reasoning"}},
			"tDirect":   {ID: "tDirect", LLMConfig: &cpn.LLMConfig{Model: ""}},
			"tNoLLM":    {ID: "tNoLLM"}, // LLMConfig nil → must be skipped
		},
	}
}

func modelOf(c *cpn.CPN, id string) string { return c.Transitions[id].LLMConfig.Model }

func TestApplyUserModelPreferences(t *testing.T) {
	t.Run("nil persist is a no-op", func(t *testing.T) {
		s := &SessionService{}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if modelOf(c, "tClassify") != "classifier" || modelOf(c, "tReason") != "reasoning" || modelOf(c, "tDirect") != "" {
			t.Fatalf("unexpected mutation: %+v", c.Transitions)
		}
	})

	t.Run("empty userID is a no-op", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{PreferredModel: "X"}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "")
		if modelOf(c, "tClassify") != "classifier" {
			t.Fatalf("expected no mutation for empty userID")
		}
	})

	t.Run("GetByID error is a no-op", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{err: persist.ErrUserNotFound}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if modelOf(c, "tClassify") != "classifier" || modelOf(c, "tDirect") != "" {
			t.Fatalf("expected no mutation on fetch error")
		}
	})

	t.Run("both fields empty is a no-op", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if modelOf(c, "tClassify") != "classifier" || modelOf(c, "tReason") != "reasoning" || modelOf(c, "tDirect") != "" {
			t.Fatalf("expected no mutation when both prefs empty")
		}
	})

	t.Run("only PreferredModel rewrites every transition including empty", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{PreferredModel: "anthropic/claude-haiku-4-5"}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		for _, id := range []string{"tClassify", "tReason", "tDirect"} {
			if got := modelOf(c, id); got != "anthropic/claude-haiku-4-5" {
				t.Fatalf("transition %s: got %q, want preferred", id, got)
			}
		}
		if c.Transitions["tNoLLM"].LLMConfig != nil {
			t.Fatalf("nil-LLMConfig transition must stay nil")
		}
	})

	t.Run("only ModelOverrides rewrites matching role keys only", func(t *testing.T) {
		s := &SessionService{persist: &PersistDeps{Users: &stubUserRepo{rec: &persist.UserRecord{
			ModelOverrides: map[string]string{"classifier": "google/gemini-2.0-flash-001"},
		}}}}
		c := buildTestCPN()
		s.applyUserModelPreferences(context.Background(), c, "u1")
		if got := modelOf(c, "tClassify"); got != "google/gemini-2.0-flash-001" {
			t.Fatalf("classifier: got %q", got)
		}
		if modelOf(c, "tReason") != "reasoning" || modelOf(c, "tDirect") != "" {
			t.Fatalf("non-matching transitions should be untouched")
		}
	})

	t.Run("both set: override wins per-role, preferred fills the rest", func(t *testing.T) {
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
			t.Fatalf("empty role key should take preferred, got %q", got)
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
