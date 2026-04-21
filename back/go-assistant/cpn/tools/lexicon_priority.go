package tools

import (
	"math"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// LexiconPriorityStats carries the runtime signals feeding the REQ-002a
// priority formula. All maps are keyed by the pre-normalised lexicon tag.
// Any missing key is treated as zero — callers MAY pass a nil pointer to
// opt out of runtime signals entirely, in which case the selection falls
// back to deterministic curation-only ranking.
type LexiconPriorityStats struct {
	// ToolCountByTag: number of registered tools tagged with this tag.
	ToolCountByTag map[string]int
	// CoverageJaccardByTag: jaccard(tag, registered_tools) in [0,1].
	CoverageJaccardByTag map[string]float64
	// RecencyScoreByTag: recency_score(last_used_days_ago) in [0,1].
	RecencyScoreByTag map[string]float64
}

// SelectTopTags returns the top-N lexicon entries ordered by the deterministic
// priority formula defined in spec-architecture-brae-awakening-toolbox-
// extension.md REQ-002a:
//
//	priority = 0.4 * log1p(tool_count_per_tag)
//	         + 0.3 * coverage_jaccard(tag, registered_tools)
//	         + 0.2 * recency_score(last_used_days_ago)
//	         + 0.1 * human_curation_rank
//
// `human_curation_rank` is derived from the entry's YAML index so that earlier
// entries score higher: rank = (N - yaml_index) / N. When `stats` is nil, the
// tool_count / coverage / recency terms all contribute zero and curation
// drives the ordering (this is the v0.1 cut, before runtime signals exist).
//
// Ties on priority are broken by tag ascending so two identical inputs always
// produce byte-for-byte identical output (REQ-012, AC-012). If n <= 0 or the
// lexicon is nil the function returns nil.
func SelectTopTags(lex cpn.Lexicon, n int, stats *LexiconPriorityStats) []cpn.LexiconEntry {
	if lex == nil || n <= 0 {
		return nil
	}
	all := lex.All()
	if len(all) == 0 {
		return nil
	}
	total := float64(len(all))

	type scored struct {
		entry    cpn.LexiconEntry
		priority float64
	}
	ranked := make([]scored, len(all))
	for i, e := range all {
		curationRank := (total - float64(i)) / total

		var toolCount float64
		var coverage float64
		var recency float64
		if stats != nil {
			if stats.ToolCountByTag != nil {
				toolCount = float64(stats.ToolCountByTag[e.Tag])
			}
			if stats.CoverageJaccardByTag != nil {
				coverage = stats.CoverageJaccardByTag[e.Tag]
			}
			if stats.RecencyScoreByTag != nil {
				recency = stats.RecencyScoreByTag[e.Tag]
			}
		}

		priority := 0.4*math.Log1p(toolCount) +
			0.3*coverage +
			0.2*recency +
			0.1*curationRank

		ranked[i] = scored{entry: e, priority: priority}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].priority != ranked[j].priority {
			return ranked[i].priority > ranked[j].priority
		}
		return ranked[i].entry.Tag < ranked[j].entry.Tag
	})

	if n > len(ranked) {
		n = len(ranked)
	}
	out := make([]cpn.LexiconEntry, n)
	for i := 0; i < n; i++ {
		out[i] = ranked[i].entry
	}
	return out
}

// TagStat is the input row for the SC-18 closeout priority selector
// (spec-architecture-brae-awakening.md REQ-1801/REQ-1802). Freq is a raw
// usage count; Recency is a normalised score in [0,1].
type TagStat struct {
	Tag     string  `json:"tag"`
	Freq    float64 `json:"freq"`
	Recency float64 `json:"recency"`
}

// SelectTopTagsByFreqRecency returns the top-n tag names ordered by
//
//	p = freq*0.6 + recency*0.4
//
// Ties on p are broken by tag ascending (lexicographic). The function is
// pure — no I/O, no package-level state read — and the input slice is not
// mutated. When n <= 0 or input is empty the result is nil. When n exceeds
// len(input) every tag is returned.
func SelectTopTagsByFreqRecency(input []TagStat, n int) []string {
	if n <= 0 || len(input) == 0 {
		return nil
	}
	ranked := make([]TagStat, len(input))
	copy(ranked, input)

	sort.Slice(ranked, func(i, j int) bool {
		pi := ranked[i].Freq*0.6 + ranked[i].Recency*0.4
		pj := ranked[j].Freq*0.6 + ranked[j].Recency*0.4
		if pi != pj {
			return pi > pj
		}
		return ranked[i].Tag < ranked[j].Tag
	})

	if n > len(ranked) {
		n = len(ranked)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = ranked[i].Tag
	}
	return out
}
