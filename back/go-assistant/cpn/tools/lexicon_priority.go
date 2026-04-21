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
