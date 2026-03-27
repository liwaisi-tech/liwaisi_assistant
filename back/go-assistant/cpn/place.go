package cpn

import (
	"fmt"
	"sync"
)

// Place is a typed buffer with spatial identity. Thread-safe.
//
// ID, Color, and Space are immutable after construction via NewPlace.
// All token operations (Deposit, Consume, Peek, Len) are mutex-protected.
//
// WARNING: Do not access the Tokens field directly in concurrent code.
// Use Deposit, Consume, Peek, or Len instead.
type Place struct {
	ID    string
	Color ColorSet
	Space SpaceKind

	// Tokens holds the current token buffer. Exported per spec.
	// Direct access is NOT thread-safe — use Deposit/Consume/Peek/Len.
	Tokens []*Token

	mu sync.Mutex
}

// NewPlace creates a Place with the given identity, color constraint, and space.
// The returned Place has an empty token buffer and is ready for concurrent use.
func NewPlace(id string, color ColorSet, space SpaceKind) *Place {
	return &Place{
		ID:    id,
		Color: color,
		Space: space,
	}
}

// Deposit adds a token to this place's buffer.
//
// Validation order:
//  1. ErrSpaceViolation — Surface token cannot skip to Computation place
//  2. ErrColorMismatch — token color must match place color
//  3. ErrSpaceMismatch — token space must match place space
//
// Thread-safe. The validation checks read immutable fields (no lock needed);
// the buffer append is mutex-protected.
func (p *Place) Deposit(t *Token) error {
	if t.Space == SpaceSurface && p.Space == SpaceComputation {
		return fmt.Errorf("%w: place=%s token_space=%s place_space=%s",
			ErrSpaceViolation, p.ID, t.Space, p.Space)
	}
	if t.Color != p.Color {
		return fmt.Errorf("%w: place=%s expected=%s got=%s",
			ErrColorMismatch, p.ID, p.Color, t.Color)
	}
	if t.Space != p.Space {
		return fmt.Errorf("%w: place=%s expected=%s got=%s",
			ErrSpaceMismatch, p.ID, p.Space, t.Space)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.Tokens = append(p.Tokens, t)
	return nil
}

// Consume removes and returns the oldest token (FIFO).
// Returns ErrEmptyPlace if no tokens are available.
// Thread-safe.
func (p *Place) Consume() (*Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.Tokens) == 0 {
		return nil, ErrEmptyPlace
	}
	t := p.Tokens[0]
	p.Tokens[0] = nil // allow GC of consumed token
	p.Tokens = p.Tokens[1:]
	return t, nil
}

// Peek returns a copy of all tokens without removing them.
// Returns (nil, false) if the place is empty.
// The returned slice is safe to read without synchronization.
// Thread-safe.
func (p *Place) Peek() ([]*Token, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.Tokens) == 0 {
		return nil, false
	}
	cp := make([]*Token, len(p.Tokens))
	copy(cp, p.Tokens)
	return cp, true
}

// Len returns the number of tokens currently in this place.
// Thread-safe.
func (p *Place) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Tokens)
}
