// Package observability exports metric helpers for the toolbox-taxonomy
// subsystem (spec-architecture-brae-toolbox-taxonomy.md REQ-LEX-002).
//
// The codebase does not currently vendor a metrics library; the spec calls
// this logical metric `lexicon.coverage.gauge`, but Prometheus-style names do
// not allow dots, so the exported handle is `lexicon_coverage_ratio`. The
// gauge is a plain atomic float holder that downstream code can scrape (HTTP,
// log pump, or a future promauto adapter) without coupling the tools package
// to any particular metrics backend.
package observability

import (
	"context"
	"math"
	"sync/atomic"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// LexiconCoverageGauge holds the current lexicon coverage ratio in the range
// [0.0, 1.0]. Thread-safe; readers may poll without locking.
type LexiconCoverageGauge struct {
	// value stores the float64 as a uint64 bit pattern for atomic access.
	value atomic.Uint64
}

// Name is the metric handle, safe for Prometheus ingestion.
const Name = "lexicon_coverage_ratio"

// NewLexiconCoverageGauge constructs a fresh gauge initialised to 1.0
// (vacuous truth: with zero tools registered, coverage is 100%).
func NewLexiconCoverageGauge() *LexiconCoverageGauge {
	g := &LexiconCoverageGauge{}
	g.Set(1.0)
	return g
}

// Set overrides the current value. Out-of-range inputs are clamped to [0,1].
func (g *LexiconCoverageGauge) Set(v float64) {
	if math.IsNaN(v) || v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	g.value.Store(math.Float64bits(v))
}

// Value returns the most recent ratio.
func (g *LexiconCoverageGauge) Value() float64 {
	return math.Float64frombits(g.value.Load())
}

// Observe recomputes the coverage ratio from a fresh snapshot of the registry
// and lexicon and stores the result. Both arguments MUST be non-nil; a nil
// argument leaves the gauge unchanged.
//
// The ratio is:
//
//	covered_tools / total_non_deprecated_tools
//
// A tool is "covered" when every one of its persisted hashtags is a known
// entry in the lexicon. A tool with an empty hashtag set counts as covered
// (nothing to miss). When there are no non-deprecated tools, the gauge is
// 1.0 (vacuous).
func (g *LexiconCoverageGauge) Observe(reg *tools.Registry, lex cpn.Lexicon) {
	if g == nil || reg == nil || lex == nil {
		return
	}
	g.Set(Coverage(reg, lex))
}

// Coverage computes the coverage ratio without mutating any gauge. Exposed so
// callers can sample on demand (e.g. from an admin endpoint) without owning a
// gauge instance.
func Coverage(reg *tools.Registry, lex cpn.Lexicon) float64 {
	if reg == nil || lex == nil {
		return 1.0
	}
	entries := reg.ListFiltered(context.Background(), tools.ToolFilter{})
	var total, covered int
	for _, e := range entries {
		if e == nil || e.Deprecated {
			continue
		}
		total++
		if entryCovered(e, lex) {
			covered++
		}
	}
	if total == 0 {
		return 1.0
	}
	return float64(covered) / float64(total)
}

func entryCovered(e *tools.ToolEntry, lex cpn.Lexicon) bool {
	for _, tag := range e.Hashtags {
		if !lex.IsKnown(tag) {
			return false
		}
	}
	return true
}
