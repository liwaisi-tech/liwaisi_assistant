// Package cpn — host_snapshot.go exposes a read-only helper for downstream
// CPNs to access the host-capability snapshot seeded at session bootstrap.
//
// The snapshot lives in the well-known place "p-host-capabilities" (color
// ColorHostFact, space SpaceComputation). Consumers MUST use Peek — never
// Consume — so the token remains visible to every transition for the
// lifetime of the session (spec GAP-2 REQ-022).
package cpn

// WellKnownHostCapabilitiesPlace is the place id every root CPN uses to
// expose the latest HostCapabilitySnapshot to its transitions.
const WellKnownHostCapabilitiesPlace = "p-host-capabilities"

// PeekHostSnapshot returns (snapshot, true) when c has a
// p-host-capabilities place with a non-empty token whose payload is a host
// capability snapshot. The returned value is an opaque any; callers decode
// via a type assertion against persist.HostCapabilitySnapshot.
//
// The helper is defined on *CPN (not on Place) so topology factories and
// guards can read the facts without importing the persist package here. The
// domain stays free of a storage-package dependency; the concrete type lives
// in cpn/persist.
func PeekHostSnapshot(c *CPN) (any, bool) {
	if c == nil {
		return nil, false
	}
	p, ok := c.Places[WellKnownHostCapabilitiesPlace]
	if !ok {
		return nil, false
	}
	tokens, ok := p.Peek()
	if !ok || len(tokens) == 0 {
		return nil, false
	}
	// Latest-wins: a bootstrap placeholder may be deposited at session
	// creation so the terminal check passes; the real snapshot from
	// awakening (or a fresh cache) is deposited later. Callers expect the
	// most recent snapshot, so return the tail token.
	return tokens[len(tokens)-1].Payload, true
}

// SeedHostSnapshot creates (if needed) the well-known p-host-capabilities
// place and deposits a single ColorHostFact token with payload=snap. The
// place is added to every root CPN at session bootstrap regardless of the
// topology factory (spec GAP-2 REQ-022).
//
// The accepted payload type is intentionally `any` so cpn stays free of a
// runtime dependency on the persist package (persist already imports cpn
// for ColorSet constants; the reverse would be a cycle).
func SeedHostSnapshot(c *CPN, snap any) {
	if c == nil || snap == nil {
		return
	}
	if _, ok := c.Places[WellKnownHostCapabilitiesPlace]; !ok {
		c.Places[WellKnownHostCapabilitiesPlace] = NewPlace(
			WellKnownHostCapabilitiesPlace, ColorHostFact, SpaceComputation,
		)
	}
	_ = c.Places[WellKnownHostCapabilitiesPlace].Deposit(&Token{
		Color:   ColorHostFact,
		Space:   SpaceComputation,
		Payload: snap,
	})
}
