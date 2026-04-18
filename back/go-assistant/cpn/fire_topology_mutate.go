package cpn

import (
	"context"
	"encoding/json"
	"fmt"
)

// fireTopologyMutate handles NodeKindTopologyMutate transitions (GAP-7).
//
// The consumed token must carry a JSON-serialised Mutation as its payload.
// The transition deposits a result token into each output place indicating
// success or the rejection reason.
func fireTopologyMutate(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if len(consumed) == 0 {
		return nil, 0, fmt.Errorf("transition %s: no consumed tokens", t.ID)
	}

	raw, err := json.Marshal(consumed[0].Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("transition %s: marshal mutation payload: %w", t.ID, err)
	}
	var m Mutation
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, 0, fmt.Errorf("transition %s: unmarshal mutation: %w", t.ID, err)
	}

	mutErr := c.Mutate(ctx, m)

	result := &Token{
		Color:       ColorArtifact,
		Space:       SpaceObservation,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindTopologyMutate,
		SessionID:   c.SessionID,
	}
	if mutErr != nil {
		result.Color = ColorError
		result.Payload = map[string]any{"error": mutErr.Error(), "mutation_kind": string(m.Kind)}
	} else {
		result.Payload = map[string]any{"applied": true, "mutation_kind": string(m.Kind)}
	}

	outputSnaps := make([]TokenSnapshot, 0, len(t.OutputPlaces))
	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, 0, fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := *result
		tok.Space = p.Space
		outputSnaps = append(outputSnaps, tok.Snapshot())
		if err := p.Deposit(&tok); err != nil {
			return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	// If mutation was rejected, propagate error.
	if mutErr != nil {
		return outputSnaps, 0, mutErr
	}
	return outputSnaps, 0, nil
}
