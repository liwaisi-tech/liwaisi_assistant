package synthesis

import (
	"errors"
	"fmt"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// LintResult aggregates every linter finding for a single topology.
//
// We return slices (not first-error-wins) so the LLM can self-correct on
// every issue in a single round-trip — GUD-002.
type LintResult struct {
	PassedFlag       bool
	UnsafePrimitives []string
	SizeViolations   []string
	SpaceViolations  []string
	UnresolvedArcs   []string
	DisallowedKinds  []string
	MissingTerminal  bool
	Other            []string
}

// Passed satisfies cpn.LintResultPort.
func (r LintResult) Passed() bool { return r.PassedFlag }

// Err returns a single error summarising the lint result (or nil when
// Passed is true). The returned error wraps the most-specific sentinel so
// callers can errors.Is-dispatch.
func (r LintResult) Err() error {
	if r.PassedFlag {
		return nil
	}
	var parts []string
	sentinel := error(nil)
	if len(r.UnsafePrimitives) > 0 {
		sort.Strings(r.UnsafePrimitives)
		parts = append(parts, fmt.Sprintf("%s: %v", cpn.ErrUnsafePrimitive.Error(), r.UnsafePrimitives))
		sentinel = cpn.ErrUnsafePrimitive
	}
	if len(r.SizeViolations) > 0 {
		parts = append(parts, fmt.Sprintf("%s: %v", cpn.ErrTopologyTooLarge.Error(), r.SizeViolations))
		if sentinel == nil {
			sentinel = cpn.ErrTopologyTooLarge
		}
	}
	if len(r.SpaceViolations) > 0 {
		sort.Strings(r.SpaceViolations)
		parts = append(parts, fmt.Sprintf("%s: %v", cpn.ErrSpaceIsolationViolated.Error(), r.SpaceViolations))
		if sentinel == nil {
			sentinel = cpn.ErrSpaceIsolationViolated
		}
	}
	if len(r.UnresolvedArcs) > 0 {
		sort.Strings(r.UnresolvedArcs)
		parts = append(parts, fmt.Sprintf("%s: %v", cpn.ErrUnresolvedArc.Error(), r.UnresolvedArcs))
		if sentinel == nil {
			sentinel = cpn.ErrUnresolvedArc
		}
	}
	if len(r.DisallowedKinds) > 0 {
		sort.Strings(r.DisallowedKinds)
		parts = append(parts, fmt.Sprintf("%s: %v", cpn.ErrDisallowedKind.Error(), r.DisallowedKinds))
		if sentinel == nil {
			sentinel = cpn.ErrDisallowedKind
		}
	}
	if r.MissingTerminal {
		parts = append(parts, cpn.ErrNoTerminal.Error())
		if sentinel == nil {
			sentinel = cpn.ErrNoTerminal
		}
	}
	if len(r.Other) > 0 {
		parts = append(parts, r.Other...)
	}
	if sentinel == nil {
		sentinel = errors.New("lint failed")
	}
	return fmt.Errorf("%w: %v", sentinel, parts)
}

// Lint validates a topology against the GAP-4 rules:
//
//   - Every referenced guard/executor/factory name MUST live in the safe
//     registry (REQ-031 → ErrUnsafePrimitive).
//   - Size caps (CON-001 → ErrTopologyTooLarge).
//   - Arcs resolve to declared places (ErrUnresolvedArc).
//   - At least one terminal place exists (CON-002 → ErrNoTerminal).
//   - No NodeKind in the `forbidden` set (SEC-002 → ErrDisallowedKind).
//   - Space isolation: no surface → computation edge except via HITL
//     (CON-003 → ErrSpaceIsolationViolated).
//
// Pure function — safe for concurrent use and fuzz tests.
func Lint(topo *persist.CPNTopology, safe cpn.SafeRegistryPort, sizeCap cpn.SizeCap) LintResult {
	res := LintResult{}
	if topo == nil {
		res.Other = append(res.Other, "topology is nil")
		return res
	}
	sizeCap = sizeCap.Resolved()

	// ── Size caps (CON-001) ───────────────────────────────────────────
	if n := len(topo.Places); n > sizeCap.MaxPlaces {
		res.SizeViolations = append(res.SizeViolations, fmt.Sprintf("places=%d > max=%d", n, sizeCap.MaxPlaces))
	}
	if n := len(topo.Transitions); n > sizeCap.MaxTransitions {
		res.SizeViolations = append(res.SizeViolations, fmt.Sprintf("transitions=%d > max=%d", n, sizeCap.MaxTransitions))
	}
	arcs := 0
	for _, tr := range topo.Transitions {
		arcs += len(tr.InputPlaces) + len(tr.OutputPlaces)
		if tr.ErrorPlace != "" {
			arcs++
		}
	}
	if arcs > sizeCap.MaxArcs {
		res.SizeViolations = append(res.SizeViolations, fmt.Sprintf("arcs=%d > max=%d", arcs, sizeCap.MaxArcs))
	}

	// Pre-index places for arc + space checks.
	placeIdx := make(map[string]persist.PlaceTopology, len(topo.Places))
	for id, p := range topo.Places {
		placeIdx[id] = p
	}

	inputRefs := make(map[string]bool, len(topo.Places))

	// ── Per-transition checks ─────────────────────────────────────────
	forbiddenKinds := map[cpn.NodeKind]bool{
		cpn.NodeKindRegisterTool: true, // SEC-002
	}

	seen := make(map[string]bool) // dedupe unsafe primitive reports
	for id, tr := range topo.Transitions {
		if forbiddenKinds[cpn.NodeKind(tr.Kind)] {
			res.DisallowedKinds = append(res.DisallowedKinds, fmt.Sprintf("%s:%s", id, tr.Kind))
		}

		// Arc resolution.
		for _, pid := range tr.InputPlaces {
			inputRefs[pid] = true
			if _, ok := placeIdx[pid]; !ok {
				res.UnresolvedArcs = append(res.UnresolvedArcs, fmt.Sprintf("%s.in=%s", id, pid))
			}
		}
		for _, pid := range tr.OutputPlaces {
			if _, ok := placeIdx[pid]; !ok {
				res.UnresolvedArcs = append(res.UnresolvedArcs, fmt.Sprintf("%s.out=%s", id, pid))
			}
		}
		if tr.ErrorPlace != "" {
			if _, ok := placeIdx[tr.ErrorPlace]; !ok {
				res.UnresolvedArcs = append(res.UnresolvedArcs, fmt.Sprintf("%s.err=%s", id, tr.ErrorPlace))
			}
		}

		// Space isolation: disallow surface→computation (direct). HITL
		// kinds are exempt — they are the only sanctioned crossing.
		if cpn.NodeKind(tr.Kind) != cpn.NodeKindHITL {
			for _, inID := range tr.InputPlaces {
				in, ok := placeIdx[inID]
				if !ok {
					continue
				}
				if cpn.SpaceKind(in.Space) != cpn.SpaceSurface {
					continue
				}
				for _, outID := range tr.OutputPlaces {
					out, ok := placeIdx[outID]
					if !ok {
						continue
					}
					if cpn.SpaceKind(out.Space) == cpn.SpaceComputation {
						res.SpaceViolations = append(res.SpaceViolations,
							fmt.Sprintf("%s: %s(surface)→%s(computation)", id, inID, outID))
					}
				}
			}
		}

		// Safe primitive references. Topologies that name a primitive
		// MUST resolve it in the sealed SafeRegistry. An empty name is
		// legal — means "no custom logic, built-in handler".
		if safe != nil {
			if tr.GuardFunc != "" {
				if kind, ok := safe.Lookup(tr.GuardFunc); !ok || kind != PrimitiveKindGuard {
					if !seen[tr.GuardFunc] {
						res.UnsafePrimitives = append(res.UnsafePrimitives, tr.GuardFunc)
						seen[tr.GuardFunc] = true
					}
				}
			}
			if tr.ExecutorFunc != "" {
				if kind, ok := safe.Lookup(tr.ExecutorFunc); !ok || kind != PrimitiveKindExecutor {
					if !seen[tr.ExecutorFunc] {
						res.UnsafePrimitives = append(res.UnsafePrimitives, tr.ExecutorFunc)
						seen[tr.ExecutorFunc] = true
					}
				}
			}
			if tr.FactoryFunc != "" {
				if kind, ok := safe.Lookup(tr.FactoryFunc); !ok || kind != PrimitiveKindFactory {
					if !seen[tr.FactoryFunc] {
						res.UnsafePrimitives = append(res.UnsafePrimitives, tr.FactoryFunc)
						seen[tr.FactoryFunc] = true
					}
				}
			}
		}
	}

	// ── Terminal check (CON-002) ──────────────────────────────────────
	terminals := 0
	for id := range placeIdx {
		if !inputRefs[id] {
			terminals++
		}
	}
	if terminals == 0 && len(placeIdx) > 0 {
		res.MissingTerminal = true
	}

	res.PassedFlag = len(res.UnsafePrimitives) == 0 &&
		len(res.SizeViolations) == 0 &&
		len(res.SpaceViolations) == 0 &&
		len(res.UnresolvedArcs) == 0 &&
		len(res.DisallowedKinds) == 0 &&
		!res.MissingTerminal &&
		len(res.Other) == 0
	return res
}
