package jit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
)

// newTestHarness builds a sealed SafeRegistry with the JIT primitives plus
// defaults, and a resolver that accepts a fixed allow-list of tool names.
func newTestHarness(t *testing.T, allowedTools ...string) (*synthesis.SafeRegistry, ResolveFunc) {
	t.Helper()
	safe := synthesis.NewSafeRegistry()
	synthesis.RegisterDefaults(safe)
	Register(safe)
	safe.Seal()

	allow := make(map[string]struct{}, len(allowedTools))
	for _, n := range allowedTools {
		allow[n] = struct{}{}
	}
	resolver := func(_ context.Context, qn string) error {
		if _, ok := allow[qn]; ok {
			return nil
		}
		return errors.New("not found")
	}
	return safe, resolver
}

func twoToolMatchSet() cpn.ToolMatchSet {
	return cpn.ToolMatchSet{
		Matches: []cpn.ToolMatch{
			{QualifiedName: "pdf/pdf-to-text@0.1.0", Score: 0.9, Hashtags: []string{"pdf"}},
			{QualifiedName: "pdf/pdf-info@0.1.0", Score: 0.8, Hashtags: []string{"pdf"}},
		},
		Digest: "digest-aa",
	}
}

// TestCompose_AC001_ParallelFanoutShape asserts AC-001.
func TestCompose_AC001_ParallelFanoutShape(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0")

	draft, blob, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "extract and count"}, ComposeOptions{
		SessionID:    "s1",
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got := draft.Metadata["template"]; got != string(TemplateParallelFanout) {
		t.Fatalf("template = %q, want parallel-fanout", got)
	}
	if len(draft.Places) != 4 {
		t.Fatalf("places = %d, want 4", len(draft.Places))
	}
	if len(draft.Transitions) != 4 {
		t.Fatalf("transitions = %d, want 4", len(draft.Transitions))
	}
	if !bytes.Contains(blob, []byte(`"jit":"true"`)) {
		t.Errorf("metadata.jit missing in blob: %s", blob)
	}
}

// TestCompose_AC002_CacheHit asserts byte-equal blob on second compose and a
// jit.cache.hit event.
func TestCompose_AC002_CacheHit(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0")
	cache := NewCache()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	opts := ComposeOptions{
		SessionID:         "s1",
		PersonalityDigest: "p1",
		Resolver:          resolver,
		SafeRegistry:      safe,
		Cache:             cache,
		Logger:            logger,
	}
	_, blob1, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, opts)
	if err != nil {
		t.Fatalf("compose1: %v", err)
	}
	_, blob2, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, opts)
	if err != nil {
		t.Fatalf("compose2: %v", err)
	}
	if !bytes.Equal(blob1, blob2) {
		t.Fatalf("blobs differ between compose runs")
	}
	if !strings.Contains(buf.String(), "jit.cache.hit") {
		t.Errorf("jit.cache.hit not logged: %s", buf.String())
	}
}

// TestCompose_AC003_PersonalityDigestInvalidatesCache asserts cache miss when
// personality digest changes.
func TestCompose_AC003_PersonalityDigestInvalidatesCache(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0")
	cache := NewCache()
	baseOpts := ComposeOptions{
		SessionID:    "s1",
		Resolver:     resolver,
		SafeRegistry: safe,
		Cache:        cache,
	}
	o1 := baseOpts
	o1.PersonalityDigest = "alpha"
	_, blob1, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, o1)
	if err != nil {
		t.Fatalf("compose1: %v", err)
	}
	o2 := baseOpts
	o2.PersonalityDigest = "beta"
	_, blob2, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, o2)
	if err != nil {
		t.Fatalf("compose2: %v", err)
	}
	// Different cache keys → both blobs are the same byte-for-byte (topology
	// does not depend on personality yet). What we care about is that the
	// cache stored both entries and did NOT return a hit from the alpha run
	// when beta asked.
	if cache.Len() != 2 {
		t.Fatalf("cache len = %d, want 2 (alpha + beta)", cache.Len())
	}
	if !bytes.Equal(blob1, blob2) {
		t.Errorf("blob body unexpectedly differs across personality digests")
	}
}

// TestCompose_AC004_UnknownPrimitive asserts ErrUnknownPrimitive on miss.
func TestCompose_AC004_UnknownPrimitive(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0") // pdf-info missing
	_, _, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, ComposeOptions{
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if !errors.Is(err, ErrUnknownPrimitive) {
		t.Fatalf("err = %v, want ErrUnknownPrimitive", err)
	}
}

// TestCompose_AC005_CapViolationLint builds a 25-match topology to exceed
// MaxTransitions=24 and asserts lint rejects it.
func TestCompose_AC005_CapViolationLint(t *testing.T) {
	allowed := make([]string, 0, 25)
	matches := make([]cpn.ToolMatch, 0, 25)
	for i := 0; i < 25; i++ {
		qn := "ns/tool@" + string(rune('a'+i))
		allowed = append(allowed, qn)
		matches = append(matches, cpn.ToolMatch{QualifiedName: qn})
	}
	safe, resolver := newTestHarness(t, allowed...)
	_, _, err := Compose(context.Background(), cpn.ToolMatchSet{Matches: matches, Digest: "big"}, cpn.Intent{NL: "x"}, ComposeOptions{
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if !errors.Is(err, ErrLintFailed) {
		t.Fatalf("err = %v, want ErrLintFailed", err)
	}
}

// TestCompose_AC008_NoMatches asserts ErrNoMatches.
func TestCompose_AC008_NoMatches(t *testing.T) {
	safe, resolver := newTestHarness(t)
	_, _, err := Compose(context.Background(), cpn.ToolMatchSet{}, cpn.Intent{}, ComposeOptions{
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if !errors.Is(err, ErrNoMatches) {
		t.Fatalf("err = %v, want ErrNoMatches", err)
	}
}

// TestCompose_AC010_MetadataPresent asserts metadata.jit and intent_digest are
// both in the emitted JSON.
func TestCompose_AC010_MetadataPresent(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0")
	_, blob, err := Compose(context.Background(), twoToolMatchSet(), cpn.Intent{NL: "x"}, ComposeOptions{
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	var parsed struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(blob, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Metadata["jit"] != "true" {
		t.Errorf("metadata.jit = %q", parsed.Metadata["jit"])
	}
	if parsed.Metadata["intent_digest"] == "" {
		t.Errorf("metadata.intent_digest missing")
	}
}

// TestCompose_AC014_SpanishFallback asserts Spanish intents default to
// parallel-fanout even when they contain a translated cue.
func TestCompose_AC014_SpanishFallback(t *testing.T) {
	safe, resolver := newTestHarness(t, "pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0")
	intent := cpn.Intent{
		NL:   "descarga el archivo y luego extrae el título",
		Lang: "es-CO",
	}
	draft, _, err := Compose(context.Background(), twoToolMatchSet(), intent, ComposeOptions{
		Resolver:     resolver,
		SafeRegistry: safe,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got := draft.Metadata["template"]; got != string(TemplateParallelFanout) {
		t.Fatalf("template = %q, want parallel-fanout (Spanish fallback)", got)
	}
}

// TestCompose_AC015_PersonalityDigest asserts the awakens.PersonalityDigest
// formula (bytes concatenation, sha256 hex).
func TestCompose_AC015_PersonalityDigest(t *testing.T) {
	cat := []byte(`[{"name":"exec-noop"}]`)
	osLine := []byte("linux-6.17")
	shellLine := []byte("zsh")
	lex := []byte("lexicon")

	d1 := awakens.PersonalityDigest(cat, osLine, shellLine, lex)
	d2 := awakens.PersonalityDigest(cat, osLine, shellLine, lex)
	if d1 != d2 {
		t.Fatalf("digest not stable: %s vs %s", d1, d2)
	}

	// Changing any input must change the digest.
	for _, mut := range []func(){
		func() { cat = append(cat, 'x') },
		func() { osLine = append(osLine, 'x') },
		func() { shellLine = append(shellLine, 'x') },
		func() { lex = append(lex, 'x') },
	} {
		prev := awakens.PersonalityDigest(cat, osLine, shellLine, lex)
		mut()
		cur := awakens.PersonalityDigest(cat, osLine, shellLine, lex)
		if prev == cur {
			t.Fatalf("mutation did not alter digest")
		}
	}
}

// TestIntent_DigestStable asserts the Intent digest is deterministic and
// changes with any field mutation.
func TestIntent_DigestStable(t *testing.T) {
	a := cpn.Intent{NL: "hi", Hashtags: []string{"b", "a"}, Lang: "en"}
	b := cpn.Intent{NL: "hi", Hashtags: []string{"a", "b"}, Lang: "en"}
	if a.Digest() != b.Digest() {
		t.Fatalf("digest sensitive to tag order")
	}
	c := cpn.Intent{NL: "hi!", Hashtags: []string{"a", "b"}, Lang: "en"}
	if a.Digest() == c.Digest() {
		t.Fatalf("digest should change when NL changes")
	}
}
