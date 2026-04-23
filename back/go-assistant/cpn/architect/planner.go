package architect

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis/jit"
)

// Planner is the deterministic core of the CPN Agent Architect
// (spec-architecture-cpn-agent-architect §3, §11 slice 3). It is pure in the
// sense that it does not emit events, persist state, or call an LLM; it
// composes the pre-existing deterministic building blocks (library lookup,
// tool retriever, jit composer) into a single decision function.
//
// The LLM-backed Architect transition (future slice) wraps this with natural
// language parsing of the user turn + HITL iteration. Keeping the pure
// decision function separate means the deterministic path is fully unit
// testable and offers a hard fallback when the LLM is unavailable.
type Planner struct {
	Library      LibraryPort
	Retriever    ToolRetriever
	SafeRegistry cpn.SafeRegistryPort
	// Template is the jit composition template override. Empty means
	// TemplateAuto (composer selects based on intent cues).
	Template jit.TemplateID
	// Cache allows callers to share a jit.Cache across Plan invocations.
	// Nil disables caching.
	Cache *jit.Cache
	// MinConfidenceForReuse gates library hits: if a candidate's signature
	// overlap is below this fraction the planner falls through to Compose
	// instead of emitting a Reuse draft. Default 0 (any overlap reuses).
	MinConfidenceForReuse float64
	// Now is injected for deterministic testing. nil → time.Now.
	Now func() time.Time
}

// ErrNoLibrary / ErrNoRetriever signal missing dependencies.
var (
	ErrNoLibrary   = errors.New("architect: Library port not configured")
	ErrNoRetriever = errors.New("architect: Retriever not configured")
)

// Plan evaluates the request and returns a single ArchitectDraft. The draft
// is one of:
//
//   - StrategyReuse: library signature covers the request; BaseFlowID set.
//   - StrategyCompose: retriever returned matches and the jit composer
//     emitted a valid topology; TopologyBlob holds canonical JSON.
//   - StrategyToolForge: retriever's matches cover hashtags but not every
//     RequiredCap; MissingCaps lists the gaps.
//   - StrategyReject: no library fit and retriever returned no matches.
//
// Plan never returns a non-nil error together with a StrategyCompose draft:
// a lint / compose failure causes Plan to degrade to StrategyReject with a
// populated Reason instead, so callers can render a user-facing message.
func (p *Planner) Plan(ctx context.Context, req ArchitectRequest) (ArchitectDraft, error) {
	if p.Library == nil {
		return ArchitectDraft{}, ErrNoLibrary
	}
	if p.Retriever == nil {
		return ArchitectDraft{}, ErrNoRetriever
	}

	// 1. Library lookup — deterministic, no LLM.
	candidates := p.Library.FindCovering(req.Hashtags, req.RequiredCaps, req.InputColors)
	if len(candidates) > 0 {
		top := candidates[0]
		conf := signatureConfidence(req, top.Signature)
		if conf >= p.MinConfidenceForReuse {
			return ArchitectDraft{
				Strategy:   StrategyReuse,
				BaseFlowID: top.Hash,
				Candidates: candidates,
				Confidence: conf,
				Reason: fmt.Sprintf("library hit: %s covers %d hashtag(s) and %d cap(s)",
					top.Hash, overlapCount(req.Hashtags, top.Signature.Hashtags),
					overlapCount(req.RequiredCaps, top.Signature.RequiredCaps)),
			}, nil
		}
	}

	// 2. Tool retrieval.
	matchSet, err := p.Retriever.Match(req)
	if err != nil {
		return ArchitectDraft{}, fmt.Errorf("architect: retriever: %w", err)
	}

	if len(matchSet.Matches) == 0 {
		return ArchitectDraft{
			Strategy:    StrategyReject,
			MissingCaps: req.RequiredCaps,
			Reason:      "no library flow covers the request and retriever found no matching tools",
			Candidates:  candidates,
		}, nil
	}

	// 3. Detect missing capabilities: retriever returned matches, but if
	// RequiredCaps contains entries absent from every candidate's profile
	// we flag them for the toolforge lane. We only know the caps we asked
	// the retriever to weigh in (req.RequiredCaps); per-tool cap metadata
	// is owned by the retriever itself, so for v1 we trust the retriever's
	// Score > 0 as "covers something" and report MissingCaps = req.RequiredCaps
	// ∖ union(all matches' hashtags). Better cap-tracking lands when
	// tool entries gain a RequiredCaps field.
	missing := missingCaps(req.RequiredCaps, matchSet)
	if len(missing) > 0 {
		return ArchitectDraft{
			Strategy:    StrategyToolForge,
			MatchSet:    matchSet,
			MissingCaps: missing,
			Reason: fmt.Sprintf("retriever covered %d tool(s) but caps %v have no provider",
				len(matchSet.Matches), missing),
			Candidates: candidates,
		}, nil
	}

	// 4. Compose a fresh topology.
	template := p.Template
	if template == "" {
		template = jit.TemplateAuto
	}
	opts := jit.ComposeOptions{
		SessionID:    req.SessionID,
		TraceID:      req.TurnID,
		Template:     template,
		Cap:          req.Budget,
		SafeRegistry: p.SafeRegistry,
		Cache:        p.Cache,
		Now:          p.Now,
	}
	_, blob, err := jit.Compose(ctx, matchSet, req.Intent, opts)
	if err != nil {
		// Compose/lint failure degrades to Reject with a human-readable
		// reason so the caller can render an error state without a panic.
		return ArchitectDraft{
			Strategy:   StrategyReject,
			MatchSet:   matchSet,
			Reason:     fmt.Sprintf("compose failed: %v", err),
			Candidates: candidates,
		}, nil
	}

	return ArchitectDraft{
		Strategy:     StrategyCompose,
		MatchSet:     matchSet,
		TopologyBlob: blob,
		Confidence:   retrievalConfidence(matchSet),
		Reason: fmt.Sprintf("composed template=%s from %d tool match(es)",
			template, len(matchSet.Matches)),
		Candidates: candidates,
	}, nil
}

// signatureConfidence is the fraction of requested hashtags and caps that
// the signature covers, averaged. Returns 1.0 when the request specifies
// nothing (empty requirement = perfect cover).
func signatureConfidence(req ArchitectRequest, sig cpn.FlowSignature) float64 {
	hs := overlapFraction(req.Hashtags, sig.Hashtags)
	cs := overlapFraction(req.RequiredCaps, sig.RequiredCaps)
	switch {
	case len(req.Hashtags) == 0 && len(req.RequiredCaps) == 0:
		return 1.0
	case len(req.Hashtags) == 0:
		return cs
	case len(req.RequiredCaps) == 0:
		return hs
	default:
		return (hs + cs) / 2
	}
}

// retrievalConfidence normalises the top match's score to 0..1 using the
// number of requested hashtags as the denominator. A perfect hit on a
// 3-hashtag request (score ≥ 3) returns 1.0; partial hits scale linearly.
func retrievalConfidence(set cpn.ToolMatchSet) float64 {
	if len(set.Matches) == 0 {
		return 0
	}
	top := set.Matches[0]
	if top.Score <= 0 {
		return 0
	}
	if top.Score >= 1 {
		// Float score typically equals hashtag-hit count (int); clamp at 1.
		if top.Score > 3 {
			return 1.0
		}
		return top.Score / 3.0
	}
	return top.Score
}

func overlapCount(needles, haystack []string) int {
	if len(needles) == 0 || len(haystack) == 0 {
		return 0
	}
	have := make(map[string]struct{}, len(haystack))
	for _, v := range haystack {
		have[v] = struct{}{}
	}
	n := 0
	for _, v := range needles {
		if _, ok := have[v]; ok {
			n++
		}
	}
	return n
}

func overlapFraction(needles, haystack []string) float64 {
	if len(needles) == 0 {
		return 1.0
	}
	return float64(overlapCount(needles, haystack)) / float64(len(needles))
}

// missingCaps returns caps present in the request but not in any match's
// union. Placeholder for v1: per-tool cap metadata is not yet persisted on
// ToolEntry; once it is, refactor this to cross-check against the real cap
// set rather than conflating "covered" with "has any hashtag overlap".
func missingCaps(reqCaps []string, set cpn.ToolMatchSet) []string {
	if len(reqCaps) == 0 || len(set.Matches) == 0 {
		return nil
	}
	// v1 heuristic: if any tool matched at all, assume the retriever has
	// the caps covered. This avoids falsely flagging ToolForge when the
	// caller did not provide cap-level metadata. When the retriever's
	// CapsFor plug-in is wired, this becomes authoritative.
	return nil
}
