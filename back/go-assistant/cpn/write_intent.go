// Package cpn — write_intent.go plumbs authored-artefact provenance
// intents through context.Value so callers of HostAdapter.WriteFile (forge,
// synth, manifest) can attach metadata the host recorder consumes.
//
// GAP-10. Spec: spec-architecture-authored-artifact-provenance.md.
//
// The intent *value* (persist.WriteIntent) lives in cpn/persist. To avoid a
// cpn ↔ cpn/persist import cycle the context key is a distinct exported
// type defined here; persist.WithWriteIntent and the infra/host adapter
// use that same type symbol by importing cpn.
package cpn

import "context"

// WriteIntentKeyType is the exported context-key type used by
// cpn.WithWriteIntent. Kept as a named type (rather than an unkeyed
// struct{}) so it is self-describing in go/doc and in log dumps of the
// request context.
type WriteIntentKeyType struct{}

// WriteIntentKey is the single well-known context key used to plumb
// persist.WriteIntent values through HostAdapter.WriteFile. Callers that
// already depend on cpn/persist should use persist.WithWriteIntent (typed
// wrapper). Callers that only depend on cpn can use cpn.WithWriteIntent
// with an `any`-typed payload — the infra/host adapter performs the type
// assertion on retrieval.
var WriteIntentKey = WriteIntentKeyType{}

// WithWriteIntent returns a child context carrying the given intent. The
// intent is passed as `any` so this helper stays usable from any package
// without dragging cpn/persist into the import graph of cpn itself. The
// HostAdapter in infra/host performs the type assertion to
// persist.WriteIntent — callers should pass a persist.WriteIntent value.
func WithWriteIntent(ctx context.Context, intent any) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, WriteIntentKey, intent)
}
