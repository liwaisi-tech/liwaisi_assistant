package awakens

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// sampleToolboxes returns a small deterministic fixture for shape /
// determinism / digest tests.
func sampleToolboxes() []tools.ToolboxManifest {
	return []tools.ToolboxManifest{
		{
			Namespace: "system",
			Title:     "System",
			Summary:   "POSIX shell, filesystem, and process tooling",
			Hashtags:  []string{"tools", "read", "write", "shell", "fs", "proc"},
			ToolCount: 4,
		},
		{
			Namespace: "awakens",
			Title:     "Awakens",
			Summary:   "Tools minted during this session's awakening",
			Hashtags:  []string{"tools", "shell"},
			ToolCount: 2,
		},
		{
			Namespace: "general",
			Title:     "General",
			Summary:   "Long-tail utilities not specific to a domain",
			Hashtags:  []string{"tools", "compute"},
			ToolCount: 1,
		},
	}
}

const (
	osLine    = "OS: Alpine Linux 3.21 (aarch64)."
	shellLine = "Shell: /bin/sh."
)

// TestBuildToolboxCatalogue_Shape asserts AC-004: three toolboxes render
// with a `### Toolboxes` subsection and one bullet per toolbox.
func TestBuildToolboxCatalogue_Shape(t *testing.T) {
	t.Parallel()
	block, digest, trunc, err := BuildToolboxCatalogue(
		osLine, shellLine,
		[]string{"sh", "awk"}, []string{"git"},
		sampleToolboxes(), []byte("lex"), 0,
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if digest == "" {
		t.Fatal("digest must be non-empty")
	}
	if len(trunc) != 0 {
		t.Fatalf("no truncation expected with cap=0; got %+v", trunc)
	}
	for _, want := range []string{
		EnvironmentAwarenessHeader,
		osLine, shellLine,
		"Available: sh, awk.",
		"NOT available: git.",
		"### Toolboxes",
		"- **system** (4 tools):",
		"- **awakens** (2 tools):",
		"- **general** (1 tools):",
		"Hashtags: tools, read, write, shell, fs, proc.",
		"To request tools beyond what is listed",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
	// Exactly three bullets.
	if got := strings.Count(block, "\n- **"); got != 3 {
		t.Errorf("want 3 toolbox bullets, got %d", got)
	}
}

// TestBuildToolboxCatalogue_Empty asserts an empty toolbox slice still
// renders a usable OS/shell block without the `### Toolboxes` subsection.
func TestBuildToolboxCatalogue_Empty(t *testing.T) {
	t.Parallel()
	block, digest, trunc, err := BuildToolboxCatalogue(
		osLine, shellLine, nil, nil, nil, nil, 0,
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if digest == "" {
		t.Fatal("digest must still be computed on empty catalogue")
	}
	if len(trunc) != 0 {
		t.Fatalf("no truncation expected; got %+v", trunc)
	}
	if strings.Contains(block, "### Toolboxes") {
		t.Errorf("empty toolbox list must NOT render ### Toolboxes:\n%s", block)
	}
	if !strings.Contains(block, osLine) || !strings.Contains(block, shellLine) {
		t.Errorf("os/shell lines missing:\n%s", block)
	}
}

// TestBuildToolboxCatalogue_Determinism asserts byte-for-byte reproducibility
// across two renders with identical inputs (AC-012 spirit for catalogue).
func TestBuildToolboxCatalogue_Determinism(t *testing.T) {
	t.Parallel()
	b1, d1, _, _ := BuildToolboxCatalogue(osLine, shellLine, []string{"sh"}, []string{"git"}, sampleToolboxes(), []byte("lex"), 0)
	b2, d2, _, _ := BuildToolboxCatalogue(osLine, shellLine, []string{"sh"}, []string{"git"}, sampleToolboxes(), []byte("lex"), 0)
	if b1 != b2 {
		t.Errorf("non-deterministic block render:\n--a--\n%s\n--b--\n%s", b1, b2)
	}
	if d1 != d2 {
		t.Errorf("non-deterministic digest: %s vs %s", d1, d2)
	}
}

// TestBuildToolboxCatalogue_Digest asserts AC-013: same inputs yield
// identical digests; changing any input changes the digest.
func TestBuildToolboxCatalogue_Digest(t *testing.T) {
	t.Parallel()
	base := func() (string, string) {
		b, d, _, _ := BuildToolboxCatalogue(osLine, shellLine, nil, nil, sampleToolboxes(), []byte("lex"), 0)
		return b, d
	}
	_, d0 := base()
	_, d1 := base()
	if d0 != d1 {
		t.Fatalf("determinism broken: %s vs %s", d0, d1)
	}

	// osLine change.
	_, d, _, _ := BuildToolboxCatalogue("OS: Other.", shellLine, nil, nil, sampleToolboxes(), []byte("lex"), 0)
	if d == d0 {
		t.Error("digest did not change when osLine changed")
	}
	// shellLine change.
	_, d, _, _ = BuildToolboxCatalogue(osLine, "Shell: /bin/bash.", nil, nil, sampleToolboxes(), []byte("lex"), 0)
	if d == d0 {
		t.Error("digest did not change when shellLine changed")
	}
	// catalogue content change.
	alt := append([]tools.ToolboxManifest{}, sampleToolboxes()...)
	alt[0].ToolCount = 99
	_, d, _, _ = BuildToolboxCatalogue(osLine, shellLine, nil, nil, alt, []byte("lex"), 0)
	if d == d0 {
		t.Error("digest did not change when catalogue bytes changed")
	}
	// lexicon excerpt change.
	_, d, _, _ = BuildToolboxCatalogue(osLine, shellLine, nil, nil, sampleToolboxes(), []byte("LEX-DIFFERENT"), 0)
	if d == d0 {
		t.Error("digest did not change when lexicon excerpt changed")
	}
}

// TestBuildToolboxCatalogue_TruncationOrder asserts AC-005: 22 toolboxes
// at cap 4096 drops `general` first, then smallest non-general by
// ToolCount ascending, ties broken by Namespace ascending; one event
// per drop.
func TestBuildToolboxCatalogue_TruncationOrder(t *testing.T) {
	t.Parallel()
	// Build 22 toolboxes with fat summaries so the block blows past 4 KiB.
	// ns-00..ns-20 are non-general with varying ToolCount; `general` has
	// count 1 so it is dropped first by the inverted render order.
	fat := strings.Repeat("x", 220)
	var boxes []tools.ToolboxManifest
	boxes = append(boxes, tools.ToolboxManifest{
		Namespace: "general", Title: "General", Summary: fat,
		Hashtags: []string{"tools", "compute"}, ToolCount: 1,
	})
	// 21 non-general boxes; ns-00 ToolCount=0 (smallest) so it would drop
	// second. Two boxes at ToolCount=1 to exercise the namespace
	// ascending tie-break.
	for i := 0; i < 21; i++ {
		ns := fmt.Sprintf("ns-%02d", i)
		count := i // 0..20
		boxes = append(boxes, tools.ToolboxManifest{
			Namespace: ns, Title: ns, Summary: fat,
			Hashtags: []string{"tools", "x" + ns}, ToolCount: count,
		})
	}
	// Duplicate ns-00 with different namespace to force tie-break on
	// namespace ascending at ToolCount=0. "aa-zero" sorts BEFORE "ns-00"
	// so it should render BEFORE ns-00 (higher in render order, dropped
	// AFTER ns-00).
	boxes = append(boxes, tools.ToolboxManifest{
		Namespace: "aa-zero", Title: "aa-zero", Summary: fat,
		Hashtags: []string{"tools", "zero"}, ToolCount: 0,
	})

	block, digest, trunc, err := BuildToolboxCatalogue(
		osLine, shellLine, nil, nil, boxes, []byte("lex"), 4096,
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if digest == "" {
		t.Fatal("digest must be non-empty")
	}
	if len(block) > 4096 {
		t.Fatalf("block exceeds cap: %d > 4096", len(block))
	}
	if len(trunc) == 0 {
		t.Fatal("expected truncation events with 22 fat toolboxes at cap 4096")
	}
	// First event MUST be `general`.
	if trunc[0].Namespace != "general" {
		t.Errorf("first dropped toolbox must be 'general', got %q", trunc[0].Namespace)
	}
	// Each event carries the canonical reason.
	for _, e := range trunc {
		if e.Reason != catalogueTruncationReason {
			t.Errorf("event %q has unexpected reason %q", e.Namespace, e.Reason)
		}
	}
	// Second event should be ns-00 (ToolCount=0 and namespace "ns-00"
	// sorts AFTER "aa-zero" under ASC tie-break, so ns-00 lives at the
	// tail and drops before aa-zero).
	if len(trunc) >= 2 && trunc[1].Namespace != "ns-00" {
		t.Errorf("second dropped toolbox must be 'ns-00' (smallest non-general, namespace tie-break), got %q", trunc[1].Namespace)
	}
	// Scan truncation events: no `general` bullets must appear in block.
	if strings.Contains(block, "- **general**") {
		t.Error("dropped toolbox `general` still appears in block")
	}
}

// TestBuildToolboxCatalogue_Performance asserts AC-009 / CON-003: render
// latency < 5 ms at 16 toolboxes.
func TestBuildToolboxCatalogue_Performance(t *testing.T) {
	t.Parallel()
	var boxes []tools.ToolboxManifest
	for i := 0; i < 16; i++ {
		boxes = append(boxes, tools.ToolboxManifest{
			Namespace: fmt.Sprintf("box-%02d", i),
			Title:     fmt.Sprintf("Title %02d", i),
			Summary:   "A representative summary line describing this toolbox.",
			Hashtags:  []string{"tools", "read", "write", "query"},
			ToolCount: i + 1,
		})
	}
	const iterations = 50
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_, _, _, err := BuildToolboxCatalogue(osLine, shellLine, nil, nil, boxes, []byte("lex"), 4096)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	elapsed := time.Since(start)
	per := elapsed / iterations
	if per > 5*time.Millisecond {
		t.Errorf("render too slow: %v per call (> 5 ms, CON-003)", per)
	}
	t.Logf("BuildToolboxCatalogue@16 toolboxes: %v per call over %d iterations", per, iterations)
}

func BenchmarkBuildToolboxCatalogue(b *testing.B) {
	var boxes []tools.ToolboxManifest
	for i := 0; i < 16; i++ {
		boxes = append(boxes, tools.ToolboxManifest{
			Namespace: fmt.Sprintf("box-%02d", i),
			Title:     fmt.Sprintf("Title %02d", i),
			Summary:   "A representative summary line describing this toolbox.",
			Hashtags:  []string{"tools", "read", "write", "query"},
			ToolCount: i + 1,
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = BuildToolboxCatalogue(osLine, shellLine, nil, nil, boxes, []byte("lex"), 4096)
	}
}
