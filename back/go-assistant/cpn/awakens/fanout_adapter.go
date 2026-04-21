package awakens

import (
	"context"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
)

// This file is the typed interface adapter between the awakening topology
// and the probe-fanout composer defined under cpn/awakens/fanout. It exists
// so that:
//
//   1. cpn/awakens/topology.go never imports the fanout package directly —
//      keeping the bootstrap compile-path independent of fanout package
//      completeness.
//   2. The Composer Engineer can implement cpn/awakens/fanout/composer.go
//      against a small, stable surface (the ComposerFunc signature below)
//      without coordinating type names with the topology wire-up.
//
// Wiring: session_service_awakening.go constructs a Deps with Composer set
// to fanout.Compose (wrapped in a closure that forwards Deps). topology.go
// only sees the ComposerFunc.
//
// Spec: spec-architecture-brae-awakening-probe-fanout.md §4.3, §4.4.

// ComposerFunc is the public surface the topology depends on. It accepts a
// validated AwakeningProbePlan payload (tolerated shapes: typed struct,
// []byte, json.RawMessage, or string) and returns a ready-to-Run *cpn.CPN
// whose terminal p-probe-egress place receives a single AwakeningReport
// token.
//
// Implementations MUST:
//   - Validate the plan (SEC-002, CON-002, CON-003).
//   - Return a *cpn.CPN with one input plan-seeded place, N per-probe
//     transitions, and a reducer transition (see fanout.Compose docs).
//   - NOT persist anything (CON-001).
type ComposerFunc func(ctx context.Context, sessionID string, planPayload any, deps ComposerDeps) (*cpn.CPN, error)

// ComposerDeps mirrors fanout.Deps but is defined here so topology.go can
// assemble it without importing the fanout package.
type ComposerDeps struct {
	HostAdapter cpn.HostAdapter
	HostGate    cpn.HostGate
	Clock       func() time.Time
	// Sandbox carries the detected wrapper argv prefix (SC-10 / SEC-004).
	// The composer forwards it to fanout.Deps so every probe subprocess is
	// wrapped.
	Sandbox fanout.Sandbox
	// OnProbeFired / OnProbeReduced carry the SC-08 observability hooks
	// down into the fanout sub-CPN. Both are optional — the composer must
	// wire them onto its Deps only when present.
	OnProbeFired   func(ctx context.Context, slug string, duration time.Duration, exitCode int)
	OnProbeReduced func(ctx context.Context, success, fail int)
}
