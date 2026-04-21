package observability

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// staticLexicon is the minimal cpn.Lexicon implementation used by the gauge
// tests. Only IsKnown is exercised; the rest satisfy the interface.
type staticLexicon struct {
	known map[string]struct{}
}

func newStaticLexicon(tags ...string) *staticLexicon {
	m := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		m[t] = struct{}{}
	}
	return &staticLexicon{known: m}
}

func (s *staticLexicon) IsKnown(tag string) bool        { _, ok := s.known[tag]; return ok }
func (s *staticLexicon) Kind(string) (string, bool)     { return "", false }
func (s *staticLexicon) Describe(string) (string, bool) { return "", false }
func (s *staticLexicon) All() []cpn.LexiconEntry        { return nil }

// TestLexiconCoverageGauge_TransitionsFromFullToPartial (REQ-LEX-002):
// starting with zero tools the gauge is 1.0, and after a tool with an
// unknown hashtag is registered the gauge drops below 1.0.
func TestLexiconCoverageGauge_TransitionsFromFullToPartial(t *testing.T) {
	lex := newStaticLexicon("kind-fetch")
	reg := tools.NewRegistry()
	g := NewLexiconCoverageGauge()

	g.Observe(reg, lex)
	if got := g.Value(); got != 1.0 {
		t.Fatalf("empty registry gauge = %v want 1.0", got)
	}

	covered := &tools.ToolEntry{
		Namespace:  "brae",
		Name:       "covered",
		Version:    "1.0.0",
		Origin:     tools.OriginAgentAuthored,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
		Hashtags:   []string{"kind-fetch"},
	}
	if err := reg.RegisterEntry(context.Background(), covered); err != nil {
		t.Fatalf("register covered: %v", err)
	}
	g.Observe(reg, lex)
	if got := g.Value(); got != 1.0 {
		t.Fatalf("covered-only gauge = %v want 1.0", got)
	}

	partial := &tools.ToolEntry{
		Namespace:  "brae",
		Name:       "partial",
		Version:    "1.0.0",
		Origin:     tools.OriginAgentAuthored,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
		Hashtags:   []string{"kind-fetch", "unknown-tag"},
	}
	if err := reg.RegisterEntry(context.Background(), partial); err != nil {
		t.Fatalf("register partial: %v", err)
	}
	g.Observe(reg, lex)
	got := g.Value()
	if !(got > 0 && got < 1.0) {
		t.Fatalf("partial-coverage gauge = %v want strictly between 0 and 1", got)
	}
	if got != 0.5 {
		t.Fatalf("partial-coverage gauge = %v want 0.5 (1 of 2 covered)", got)
	}
}

func TestLexiconCoverageGauge_ClampsOutOfRange(t *testing.T) {
	g := NewLexiconCoverageGauge()
	g.Set(-0.5)
	if g.Value() != 0 {
		t.Fatalf("negative set not clamped: %v", g.Value())
	}
	g.Set(2.0)
	if g.Value() != 1.0 {
		t.Fatalf(">1 set not clamped: %v", g.Value())
	}
}
