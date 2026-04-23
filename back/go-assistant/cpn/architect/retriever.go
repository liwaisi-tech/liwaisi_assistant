package architect

import (
	"context"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// HashtagRetriever is the v1 deterministic implementation of ToolRetriever.
// It ranks tools using pure set arithmetic over the registry's hashtag index
// — no BM25, no embeddings, no LLM. Closes the TODO in cpn/tool_match_set.go.
//
// Scoring formula (spec §11 slice 2, deterministic tie-break):
//
//	score = |matchedHashtags| * α + |matchedCaps| * β
//	ties:  fewer extra hashtags first, then qualified-name lexicographically
//
// α and β default to 1.0 each; the ratio does not matter for v1 because the
// scoring is sparse integer-valued. Keep them configurable so later slices
// can weigh capability match higher than hashtag match without re-wiring.
type HashtagRetriever struct {
	Registry      *tools.Registry
	HashtagWeight float64 // default 1.0
	CapWeight     float64 // default 1.0
	// CapsFor maps a qualified tool name to the host capabilities it
	// requires. Optional — when nil, RequiredCaps scoring contributes zero.
	// This plug-in exists because host capabilities are not yet persisted
	// on ToolEntry; the production wiring will pass a closure that reads
	// from cpn.DeriveHostGateOp + the tool's manifest.
	CapsFor func(qualifiedTool string) []string
	// MaxResults caps the returned slice length. Zero means "no cap".
	MaxResults int
}

// NewHashtagRetriever constructs a HashtagRetriever with default weights.
func NewHashtagRetriever(reg *tools.Registry) *HashtagRetriever {
	return &HashtagRetriever{
		Registry:      reg,
		HashtagWeight: 1.0,
		CapWeight:     1.0,
	}
}

// Match implements ToolRetriever. Returns an empty ToolMatchSet (non-nil,
// zero-length Matches slice) when no request hashtags are supplied, so
// callers can treat the zero value uniformly.
func (h *HashtagRetriever) Match(req ArchitectRequest) (cpn.ToolMatchSet, error) {
	if h == nil || h.Registry == nil {
		return cpn.ToolMatchSet{}, nil
	}
	if len(req.Hashtags) == 0 && len(req.RequiredCaps) == 0 {
		return cpn.ToolMatchSet{}, nil
	}

	// Collect candidate entries keyed by qualified name — a tool listed
	// under several matching hashtags must only appear once.
	type cand struct {
		entry    *tools.ToolEntry
		matched  map[string]struct{}
		capHits  int
	}
	candidates := make(map[string]*cand)

	ctx := context.Background()
	for _, raw := range req.Hashtags {
		tag, ok := tools.NormalizeHashtag(raw)
		if !ok {
			continue
		}
		for _, entry := range h.Registry.ListByHashtag(ctx, tag) {
			qn := entry.QualifiedName()
			c, exists := candidates[qn]
			if !exists {
				c = &cand{entry: entry, matched: make(map[string]struct{})}
				candidates[qn] = c
			}
			c.matched[tag] = struct{}{}
		}
	}

	// Capability contribution: when CapsFor is wired, add candidates that
	// provide a required capability even if no hashtag matched, and bump
	// capHits for existing candidates.
	if h.CapsFor != nil && len(req.RequiredCaps) > 0 {
		capSet := make(map[string]struct{}, len(req.RequiredCaps))
		for _, cap := range req.RequiredCaps {
			capSet[cap] = struct{}{}
		}
		// We cannot enumerate "all tools that provide capability X" from
		// the registry directly; instead, walk current candidates and any
		// user-authored tools — this is the v1 minimum and keeps the
		// scoring pure-function deterministic.
		walk := func(entry *tools.ToolEntry) {
			qn := entry.QualifiedName()
			hits := 0
			for _, cap := range h.CapsFor(qn) {
				if _, ok := capSet[cap]; ok {
					hits++
				}
			}
			if hits == 0 {
				return
			}
			c, exists := candidates[qn]
			if !exists {
				c = &cand{entry: entry, matched: make(map[string]struct{})}
				candidates[qn] = c
			}
			c.capHits = hits
		}
		for _, e := range h.Registry.ListUserAuthored() {
			walk(e)
		}
		// Existing candidates from hashtag pass get capHits too.
		for _, c := range candidates {
			if c.capHits != 0 {
				continue
			}
			for _, cap := range h.CapsFor(c.entry.QualifiedName()) {
				if _, ok := capSet[cap]; ok {
					c.capHits++
				}
			}
		}
	}

	if len(candidates) == 0 {
		return cpn.ToolMatchSet{}, nil
	}

	matches := make([]cpn.ToolMatch, 0, len(candidates))
	for qn, c := range candidates {
		matchedTags := sortedTagSet(c.matched)
		score := float64(len(matchedTags))*h.HashtagWeight + float64(c.capHits)*h.CapWeight
		matches = append(matches, cpn.ToolMatch{
			QualifiedName: qn,
			Score:         score,
			Hashtags:      matchedTags,
		})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].QualifiedName < matches[j].QualifiedName
	})

	if h.MaxResults > 0 && len(matches) > h.MaxResults {
		matches = matches[:h.MaxResults]
	}

	set := cpn.ToolMatchSet{Matches: matches}
	set.Digest = matchSetDigest(set)
	return set, nil
}

func sortedTagSet(s map[string]struct{}) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
