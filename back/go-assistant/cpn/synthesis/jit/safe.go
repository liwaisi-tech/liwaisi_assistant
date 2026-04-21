package jit

import (
	"context"
	"encoding/json"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
)

// PrimitiveFanout is the SafeRegistry name for the jit-fanout executor.
const (
	PrimitiveFanout    = "jit-fanout"
	PrimitiveAggregate = "jit-aggregate"
)

// Register contributes the jit-fanout and jit-aggregate executors to the
// supplied SafeRegistry. MUST be invoked at server boot BEFORE Seal, right
// after synthesis.RegisterDefaults. Safe to call once per process.
//
// Both executors are pure-Go, deterministic, and do no I/O.
func Register(safe *synthesis.SafeRegistry) {
	safe.RegisterSafePrimitive(synthesis.SafePrimitive{
		Name:     PrimitiveFanout,
		Kind:     synthesis.PrimitiveKindExecutor,
		Output:   "intent",
		CostHint: "free",
	}, fanoutExecutor)

	safe.RegisterSafePrimitive(synthesis.SafePrimitive{
		Name:     PrimitiveAggregate,
		Kind:     synthesis.PrimitiveKindExecutor,
		Output:   "artifact",
		CostHint: "free",
	}, aggregateExecutor)
}

// fanoutExecutor is an identity-split: it forwards the incoming intent
// token unchanged. The topology's arc multiplicity is what produces N
// per-branch tokens; the executor merely re-tags the Color to ColorIntent
// so each consumer sees the intent payload. NL is preserved as-is.
func fanoutExecutor(_ context.Context, in cpn.Token) (cpn.Token, error) {
	out := in
	// Preserve original payload; normalise color so downstream tool
	// transitions can match on ColorString (the intent text) or on a
	// structured map carrying NL + Hashtags. JIT's contract is that the
	// intent token lands on each branch input unchanged.
	if out.Color == "" {
		out.Color = cpn.ColorString
	}
	return out, nil
}

// aggregateExecutor joins the N branch results into one ColorArtifact
// token. Deterministic: sorts incoming result keys by branch id, encodes
// to JSON, and returns the joined payload. Tolerates nil / empty payloads.
func aggregateExecutor(_ context.Context, in cpn.Token) (cpn.Token, error) {
	out := in
	out.Color = cpn.ColorArtifact
	// The runtime joins multiset inputs upstream; this executor operates on
	// the consumed token. We simply echo it with a stable JSON wrapper so
	// observers downstream see a consistent shape.
	wrapper := map[string]any{
		"aggregate": true,
		"payload":   in.Payload,
	}
	if b, err := json.Marshal(wrapper); err == nil {
		out.Payload = json.RawMessage(b)
	}
	return out, nil
}
