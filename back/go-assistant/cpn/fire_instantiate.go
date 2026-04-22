package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// fireInstantiate executes a NodeKindInstantiate transition (GAP-4).
//
// Flow:
//  1. Pull the FlowRef from the consumed token.
//  2. Load the topology via FlowRepository.GetByID; fail fast on rejected.
//  3. Re-lint (primitive may have been deprecated since persist).
//  4. First instantiation per session → topology-approval HITL.
//  5. Materialise into a *CPN using only safe primitives.
//  6. Spawn as a sub-CPN using the same machinery as fireSubNet.
//
// Spec: spec/spec-architecture-cpn-synthesis-instantiate.md §3.
func fireInstantiate(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if c.FlowRepository == nil {
		return nil, 0, fmt.Errorf("transition %s: no FlowRepository on CPN", t.ID)
	}
	if c.SafeRegistry == nil {
		return nil, 0, fmt.Errorf("transition %s: no SafeRegistry on CPN", t.ID)
	}
	flowRef, err := extractFlowRef(consumed)
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: %w", t.ID, err)
	}

	record, err := c.FlowRepository.GetByID(ctx, flowRef.FlowID)
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: load flow %s: %w", t.ID, flowRef.FlowID, err)
	}
	if record.Rejected {
		return nil, 0, fmt.Errorf("%w: %s (%s)", ErrTopologyRejected, flowRef.FlowID, record.RejectedReason)
	}

	// Re-validate with current safe registry (AC-007).
	sizeCap := DefaultSizeCap()
	lintRes := lintTopology(record.TopologyJSON, c.SafeRegistry, sizeCap)
	if !lintRes.Passed() {
		return nil, 0, fmt.Errorf("%w: %v", ErrDeprecatedDependency, lintRes.Err())
	}

	digest := topologyDigest(record.TopologyJSON)

	// ── HITL (first instantiation this session) ───────────────────────
	skipHITL := t.InstantiateConfig != nil && t.InstantiateConfig.SkipHITL
	if !skipHITL && !c.HasApprovedFlow(flowRef.FlowID) {
		if c.TopologyRouter != nil {
			summary := AuthoredTopologySummary{
				FlowID:               flowRef.FlowID,
				Name:                 digest.Name,
				Summary:              flowRef.Summary,
				SizePlaces:           digest.SizePlaces,
				SizeTransitions:      digest.SizeTransitions,
				ReferencedPrimitives: digest.ReferencedPrimitives,
			}
			approved, err := c.TopologyRouter.ApproveTopology(ctx, summary)
			if err != nil {
				return nil, 0, fmt.Errorf("transition %s: topology approval: %w", t.ID, err)
			}
			if !approved {
				return nil, 0, ErrHITLRejected
			}
			c.MarkFlowApproved(flowRef.FlowID)
		} else {
			slog.WarnContext(ctx, "instantiate: no TopologyRouter — falling open (dev-mode)",
				"transition_id", t.ID, "flow_id", flowRef.FlowID)
			c.MarkFlowApproved(flowRef.FlowID)
		}
	}

	// ── Materialise and spawn ─────────────────────────────────────────
	child, err := materialiseTopology(ctx, record.TopologyJSON, c.SafeRegistry)
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: materialise: %w", t.ID, err)
	}

	childID, err := newUUID()
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: %w", t.ID, err)
	}
	child.ID = childID
	child.Depth = c.Depth + 1
	child.SessionID = c.SessionID
	child.RegionalVariant = c.RegionalVariant
	child.LLMClient = c.LLMClient
	child.HostRuntime = c.HostRuntime
	child.ToolRegistry = c.ToolRegistry
	child.FlowRepository = c.FlowRepository
	child.SafeRegistry = c.SafeRegistry
	child.TopologyRouter = c.TopologyRouter
	child.FirstRunLedger = c.FirstRunLedger
	child.Cost = c.Cost

	// Expose the materialised child on the instantiate transition so the
	// topology snapshot the monitor serialises (persist.MarshalCPN →
	// TransitionTopology.SubNetTopology) includes the live sub-CPN graph.
	// Without this assignment the monitor would render t-instantiate as a
	// leaf box and the composed structure (3 bash clones + aggregate)
	// would be invisible to the user — defeating the "evolve like a
	// biological being" goal (plan Phase 4).
	t.SubNet = child
	// JIT sub-CPN plan Phase 3: when the synthesized child references
	// tools by name (kind:"tool", toolName:"bash_exec"), persist.UnmarshalCPN
	// preserves the ToolName field but does NOT attach an executor. Ask the
	// ToolRegistry (when it exposes the wider *tools.Registry surface) to
	// inject executors + ToolMeta so the tool transitions can fire.
	// Typed as an anonymous interface to avoid widening the narrow
	// cpn.ToolRegistry port — that would break test fakes that only
	// implement RegisterManifest.
	if child.ToolRegistry != nil {
		if injector, ok := any(child.ToolRegistry).(interface{ InjectIntoCPN(*CPN) }); ok {
			injector.InjectIntoCPN(child)
		}
	}

	childCtx, cancel := withOptionalTimeout(ctx, t.InstantiateConfig)
	defer cancel()

	injectMappedTokens(child, consumed, t.InstantiateConfig)

	bus := make(chan Event, SubNetEventBusCapacity)
	child.EventEmitter = bus
	c.registerSubNetBus(child.ID, bus)

	runErr := child.Run(childCtx)
	close(bus)

	if runErr != nil {
		if t.ErrorPlace != "" {
			if ep, ok := c.Places[t.ErrorPlace]; ok {
				errToken := &Token{
					Color:       ColorError,
					Payload:     fmt.Sprintf("%s: %v", ErrSubNetFailed, runErr),
					Space:       ep.Space,
					OriginID:    child.ID,
					OriginDepth: child.Depth,
					OriginKind:  NodeKindInstantiate,
					SessionID:   c.SessionID,
					Timestamp:   time.Now(),
				}
				_ = ep.Deposit(errToken)
				return nil, 0, nil
			}
		}
		return nil, 0, fmt.Errorf("%w: %v", ErrSubNetFailed, runErr)
	}

	terminals := child.TerminalPlaces()
	outMap := outputMappingOrDefault(t, terminals)

	var snaps []TokenSnapshot
	for _, tp := range terminals {
		toks, ok := tp.Peek()
		if !ok {
			continue
		}
		parentIDs := outMap[tp.ID]
		if len(parentIDs) == 0 {
			continue
		}
		for _, tok := range toks {
			for _, pid := range parentIDs {
				pp, ok := c.Places[pid]
				if !ok {
					continue
				}
				copyTok := *tok
				copyTok.OriginID = child.ID
				copyTok.OriginDepth = child.Depth
				copyTok.OriginKind = NodeKindInstantiate
				copyTok.Space = pp.Space
				copyTok.Color = pp.Color
				if err := pp.Deposit(&copyTok); err != nil {
					return snaps, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
				}
				snaps = append(snaps, copyTok.Snapshot())
			}
		}
	}
	return snaps, 0, nil
}

func extractFlowRef(consumed []Token) (FlowRef, error) {
	if len(consumed) == 0 {
		return FlowRef{}, errors.New("no consumed tokens")
	}
	for _, tok := range consumed {
		if tok.Color != ColorFlowRef {
			continue
		}
		switch v := tok.Payload.(type) {
		case FlowRef:
			return v, nil
		case *FlowRef:
			if v == nil {
				continue
			}
			return *v, nil
		case map[string]any:
			id, _ := v["flow_id"].(string)
			sum, _ := v["summary"].(string)
			if id != "" {
				return FlowRef{FlowID: id, Summary: sum}, nil
			}
		case string:
			s := strings.TrimSpace(v)
			if s != "" {
				return FlowRef{FlowID: s}, nil
			}
		case json.RawMessage:
			var r FlowRef
			if err := json.Unmarshal(v, &r); err == nil && r.FlowID != "" {
				return r, nil
			}
		}
	}
	return FlowRef{}, errors.New("no ColorFlowRef token consumed")
}

func withOptionalTimeout(parent context.Context, cfg *InstantiateConfig) (context.Context, context.CancelFunc) {
	if cfg == nil || cfg.Timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, cfg.Timeout)
}

// injectMappedTokens deposits consumed tokens into the child's source
// places.
func injectMappedTokens(child *CPN, consumed []Token, cfg *InstantiateConfig) {
	outputRefs := make(map[string]bool)
	for _, t := range child.Transitions {
		for _, pid := range t.OutputPlaces {
			outputRefs[pid] = true
		}
	}
	var sources []*Place
	for id, p := range child.Places {
		if !outputRefs[id] {
			sources = append(sources, p)
		}
	}
	if len(sources) == 0 {
		return
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })

	colorIdx := make(map[ColorSet]*Place, len(sources))
	for _, sp := range sources {
		colorIdx[sp.Color] = sp
	}

	childByID := make(map[string]*Place, len(child.Places))
	for id, p := range child.Places {
		childByID[id] = p
	}

	for i := range consumed {
		tok := consumed[i]
		var target *Place
		if cfg != nil {
			for parentPID, childPID := range cfg.InputMapping {
				_ = parentPID
				if p, ok := childByID[childPID]; ok {
					target = p
					break
				}
			}
		}
		if target == nil {
			target = colorIdx[tok.Color]
		}
		if target == nil {
			target = sources[0]
		}
		tok.Color = target.Color
		tok.Space = target.Space
		_ = target.Deposit(&tok)
	}
}

func outputMappingOrDefault(t *Transition, terminals []*Place) map[string][]string {
	m := make(map[string][]string)
	if t.InstantiateConfig != nil && len(t.InstantiateConfig.OutputMapping) > 0 {
		for childPID, parentPID := range t.InstantiateConfig.OutputMapping {
			m[childPID] = append(m[childPID], parentPID)
		}
		return m
	}
	for _, tp := range terminals {
		m[tp.ID] = append([]string(nil), t.OutputPlaces...)
	}
	return m
}
