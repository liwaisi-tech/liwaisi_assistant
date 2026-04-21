package cpn

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

// MutationKind identifies the type of topology mutation.
type MutationKind string

const (
	MutationAddPlace            MutationKind = "add_place"
	MutationAddTransition       MutationKind = "add_transition"
	MutationDeprecateTransition MutationKind = "deprecate_transition"
)

// Mutation is the union type for all topology mutations (REQ-002).
// Exactly one of AddPlace, AddTransition, DeprecateTransition must be set.
type Mutation struct {
	Kind        MutationKind `json:"kind"`
	RequestedBy string       `json:"requested_by"` // userID or agent ID
	Reason      string       `json:"reason"`

	// AddPlace fields (MutationAddPlace)
	AddPlace *PlaceDef `json:"add_place,omitempty"`

	// AddTransition fields (MutationAddTransition)
	AddTransition *TransitionDef `json:"add_transition,omitempty"`

	// DeprecateTransition fields (MutationDeprecateTransition)
	DeprecateTransitionID string `json:"deprecate_transition_id,omitempty"`
	DeprecateReason       string `json:"deprecate_reason,omitempty"`
}

// PlaceDef is the spec for a new place in an add_place mutation (CON-001).
type PlaceDef struct {
	ID    string    `json:"id"`
	Color ColorSet  `json:"color"`
	Space SpaceKind `json:"space"`
}

// TransitionDef is the spec for a new transition in an add_transition mutation (CON-002).
type TransitionDef struct {
	ID           string   `json:"id"`
	Kind         NodeKind `json:"kind"`
	InputPlaces  []string `json:"input_places"`
	OutputPlaces []string `json:"output_places"`
}

// ErrMutationNotPermitted is returned when Mutate is called on a CPN that
// does not have MutableAfterStart set.
var ErrMutationNotPermitted = errors.New("topology mutation not permitted: set MutableAfterStart=true")

// MutationAuditLog is the port for persisting mutation records.
// Implemented by the Postgres adapter in store/postgres/.
type MutationAuditLog interface {
	LogMutation(ctx context.Context, cpnID, sessionID string, m Mutation, approved bool, rejectedReason string) error
}

// Mutate applies a topology mutation to the CPN at runtime (GAP-7 REQ-003).
//
// Protocol:
//  1. Reject if MutableAfterStart==false → ErrMutationNotPermitted
//  2. If TrustMutations==false, surface a HITL gate for operator approval.
//     (In this implementation, HITL gate is delegated to the transition that
//     invokes Mutate; the caller has already resolved HITL before calling Mutate.)
//  3. Acquire write lock on mu (pauses concurrent reads).
//  4. Validate the mutation against current topology (CON-001/002/003).
//  5. Apply the mutation.
//  6. Run Validate(c.Places, c.Transitions) — rollback on failure.
//  7. Emit EventTopologyMutated or EventTopologyMutationRejected.
//  8. Log to MutationAuditLog if set.
func (c *CPN) Mutate(ctx context.Context, m Mutation) error {
	if !c.MutableAfterStart {
		c.emit(&Event{Type: EventTopologyMutationRejected, Payload: map[string]any{
			"reason": "MutableAfterStart not set", "mutation_kind": string(m.Kind),
		}})
		return ErrMutationNotPermitted
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Take a rollback snapshot.
	placesSnapshot := make(map[string]*Place, len(c.Places))
	maps.Copy(placesSnapshot, c.Places)
	transitionsSnapshot := make(map[string]*Transition, len(c.Transitions))
	maps.Copy(transitionsSnapshot, c.Transitions)

	// Apply mutation.
	var applyErr error
	switch m.Kind {
	case MutationAddPlace:
		applyErr = c.applyAddPlace(m)
	case MutationAddTransition:
		applyErr = c.applyAddTransition(m)
	case MutationDeprecateTransition:
		applyErr = c.applyDeprecateTransition(m)
	default:
		applyErr = fmt.Errorf("unknown mutation kind %q", m.Kind)
	}

	if applyErr != nil {
		c.Places = placesSnapshot
		c.Transitions = transitionsSnapshot
		c.emit(&Event{Type: EventTopologyMutationRejected, Payload: map[string]any{
			"reason": applyErr.Error(), "mutation_kind": string(m.Kind),
		}})
		if c.MutationLog != nil {
			_ = c.MutationLog.LogMutation(ctx, c.ID, c.SessionID, m, false, applyErr.Error())
		}
		return applyErr
	}

	// Validate post-mutation topology (rollback on failure).
	if err := Validate(c.Places, c.Transitions); err != nil {
		c.Places = placesSnapshot
		c.Transitions = transitionsSnapshot
		c.emit(&Event{Type: EventTopologyMutationRejected, Payload: map[string]any{
			"reason": err.Error(), "mutation_kind": string(m.Kind),
		}})
		if c.MutationLog != nil {
			_ = c.MutationLog.LogMutation(ctx, c.ID, c.SessionID, m, false, err.Error())
		}
		return err
	}

	c.emit(&Event{Type: EventTopologyMutated, Payload: map[string]any{
		"mutation_kind": string(m.Kind), "requested_by": m.RequestedBy, "reason": m.Reason,
	}})
	if c.MutationLog != nil {
		_ = c.MutationLog.LogMutation(ctx, c.ID, c.SessionID, m, true, "")
	}
	return nil
}

func (c *CPN) applyAddPlace(m Mutation) error {
	if m.AddPlace == nil {
		return fmt.Errorf("add_place mutation missing PlaceDef")
	}
	def := m.AddPlace
	if def.ID == "" {
		return fmt.Errorf("add_place: place ID must not be empty")
	}
	if _, exists := c.Places[def.ID]; exists {
		return fmt.Errorf("add_place: place %q already exists (CON-001)", def.ID)
	}
	c.Places[def.ID] = &Place{ID: def.ID, Color: def.Color, Space: def.Space}
	return nil
}

func (c *CPN) applyAddTransition(m Mutation) error {
	if m.AddTransition == nil {
		return fmt.Errorf("add_transition mutation missing TransitionDef")
	}
	def := m.AddTransition
	if def.ID == "" {
		return fmt.Errorf("add_transition: transition ID must not be empty")
	}
	if _, exists := c.Transitions[def.ID]; exists {
		return fmt.Errorf("add_transition: transition %q already exists", def.ID)
	}
	// CON-002: all referenced places must exist.
	for _, pid := range def.InputPlaces {
		if _, ok := c.Places[pid]; !ok {
			return fmt.Errorf("add_transition: input place %q does not exist (CON-002)", pid)
		}
	}
	for _, pid := range def.OutputPlaces {
		if _, ok := c.Places[pid]; !ok {
			return fmt.Errorf("add_transition: output place %q does not exist (CON-002)", pid)
		}
	}
	c.Transitions[def.ID] = &Transition{
		ID:           def.ID,
		Kind:         def.Kind,
		InputPlaces:  def.InputPlaces,
		OutputPlaces: def.OutputPlaces,
	}
	return nil
}

func (c *CPN) applyDeprecateTransition(m Mutation) error {
	if m.DeprecateTransitionID == "" {
		return fmt.Errorf("deprecate_transition: transition ID must not be empty")
	}
	t, exists := c.Transitions[m.DeprecateTransitionID]
	if !exists {
		return fmt.Errorf("deprecate_transition: transition %q not found", m.DeprecateTransitionID)
	}
	// CON-003: mark non-firable; tokens drain naturally.
	t.Deprecated = true
	t.DeprecateReason = m.DeprecateReason
	return nil
}
