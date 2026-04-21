package synthesis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// CanonicaliseTopology produces a byte-stable JSON serialisation of the
// topology (AC-008). Map keys are emitted in sorted order; places and
// transitions are sorted by ID before emission; optional zero-value fields
// are elided. SHA-256 over the returned bytes is the flow_id hash.
//
// We walk the topology ourselves rather than leaning on encoding/json's
// default marshaller because Go map iteration order is non-deterministic —
// two Marshals of the same topology can produce different bytes, which
// would defeat idempotent SaveAuthored. The output remains valid JSON and
// round-trips through json.Unmarshal back into persist.CPNTopology.
func CanonicaliseTopology(topo *persist.CPNTopology) ([]byte, error) {
	if topo == nil {
		return nil, fmt.Errorf("synthesis: canonicalise nil topology")
	}
	root := canonicalTopology(topo)
	// Use a json.Encoder configured for stable output. We already pre-sort
	// by emitting slices in sorted order; encoder flags keep whitespace
	// deterministic.
	return marshalStable(root)
}

// CanonicalHash returns the hex SHA-256 of CanonicaliseTopology(topo).
func CanonicalHash(topo *persist.CPNTopology) (string, error) {
	b, err := CanonicaliseTopology(topo)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalTopology walks persist.CPNTopology and emits an ordered map
// that json.Marshal will render deterministically — json.Marshal on a
// map[string]any still uses Go map iteration order, so we build an
// []orderedEntry and render it via marshalStable below.

type orderedEntry struct {
	Key   string
	Value any
}

func canonicalTopology(t *persist.CPNTopology) []orderedEntry {
	entries := []orderedEntry{
		{"id", t.ID},
		{"role", t.Role},
		{"depth", t.Depth},
		{"mode", t.Mode},
	}

	// Places — sorted map, each rendered as ordered entries.
	placeIDs := make([]string, 0, len(t.Places))
	for id := range t.Places {
		placeIDs = append(placeIDs, id)
	}
	sort.Strings(placeIDs)
	places := make([]orderedEntry, 0, len(placeIDs))
	for _, id := range placeIDs {
		p := t.Places[id]
		places = append(places, orderedEntry{id, []orderedEntry{
			{"id", p.ID},
			{"color", p.Color},
			{"space", p.Space},
		}})
	}
	entries = append(entries, orderedEntry{"places", places})

	// Transitions.
	trIDs := make([]string, 0, len(t.Transitions))
	for id := range t.Transitions {
		trIDs = append(trIDs, id)
	}
	sort.Strings(trIDs)
	transitions := make([]orderedEntry, 0, len(trIDs))
	for _, id := range trIDs {
		tr := t.Transitions[id]
		transitions = append(transitions, orderedEntry{id, canonicalTransition(tr)})
	}
	entries = append(entries, orderedEntry{"transitions", transitions})

	return entries
}

func canonicalTransition(tr persist.TransitionTopology) []orderedEntry {
	out := []orderedEntry{
		{"id", tr.ID},
		{"kind", tr.Kind},
	}
	// Input / output places: preserve order (caller's arc order is part
	// of semantics); we still canonicalise at the topology-map level.
	if len(tr.InputPlaces) > 0 {
		out = append(out, orderedEntry{"inputPlaces", append([]string(nil), tr.InputPlaces...)})
	}
	if len(tr.OutputPlaces) > 0 {
		out = append(out, orderedEntry{"outputPlaces", append([]string(nil), tr.OutputPlaces...)})
	}
	if tr.ErrorPlace != "" {
		out = append(out, orderedEntry{"errorPlace", tr.ErrorPlace})
	}
	if tr.GuardFunc != "" {
		out = append(out, orderedEntry{"guardFunc", tr.GuardFunc})
	}
	if tr.ExecutorFunc != "" {
		out = append(out, orderedEntry{"executorFunc", tr.ExecutorFunc})
	}
	if tr.FactoryFunc != "" {
		out = append(out, orderedEntry{"factoryFunc", tr.FactoryFunc})
	}
	if tr.SystemPrompt != "" {
		out = append(out, orderedEntry{"systemPrompt", tr.SystemPrompt})
	}
	if tr.ToolName != "" {
		out = append(out, orderedEntry{"toolName", tr.ToolName})
	}
	if tr.ObservedCPNID != "" {
		out = append(out, orderedEntry{"observedCPNID", tr.ObservedCPNID})
	}
	if tr.EventFilterFunc != "" {
		out = append(out, orderedEntry{"eventFilterFunc", tr.EventFilterFunc})
	}
	if tr.RetryOnFunc != "" {
		out = append(out, orderedEntry{"retryOnFunc", tr.RetryOnFunc})
	}
	if len(tr.LLMTools) > 0 {
		sorted := append([]string(nil), tr.LLMTools...)
		sort.Strings(sorted)
		out = append(out, orderedEntry{"llmTools", sorted})
	}
	if len(tr.ToolParameters) > 0 {
		// tr.ToolParameters is json.RawMessage — re-encode through
		// canonicalJSON so arbitrary nested maps become stable.
		canon, err := canonicaliseRawJSON(tr.ToolParameters)
		if err == nil {
			out = append(out, orderedEntry{"toolParameters", json.RawMessage(canon)})
		}
	}
	// LLMConfig / HITLConfig / ValidateConfig / Retry: re-encode via
	// canonicaliseRawJSON so their nested maps remain stable.
	if tr.LLMConfig != nil {
		if b, err := json.Marshal(tr.LLMConfig); err == nil {
			if canon, err := canonicaliseRawJSON(b); err == nil {
				out = append(out, orderedEntry{"llmConfig", json.RawMessage(canon)})
			}
		}
	}
	if tr.HITLConfig != nil {
		if b, err := json.Marshal(tr.HITLConfig); err == nil {
			if canon, err := canonicaliseRawJSON(b); err == nil {
				out = append(out, orderedEntry{"hitlConfig", json.RawMessage(canon)})
			}
		}
	}
	if tr.ValidateConfig != nil {
		if b, err := json.Marshal(tr.ValidateConfig); err == nil {
			if canon, err := canonicaliseRawJSON(b); err == nil {
				out = append(out, orderedEntry{"validateConfig", json.RawMessage(canon)})
			}
		}
	}
	if tr.Retry != nil {
		if b, err := json.Marshal(tr.Retry); err == nil {
			if canon, err := canonicaliseRawJSON(b); err == nil {
				out = append(out, orderedEntry{"retry", json.RawMessage(canon)})
			}
		}
	}
	if tr.SubNetTopology != nil {
		// Recurse through the same canonicaliser.
		sub := canonicalTopology(tr.SubNetTopology)
		out = append(out, orderedEntry{"subNetTopology", sub})
	}
	return out
}

// marshalStable renders an orderedEntry tree as deterministic JSON. Nested
// arrays and maps of basic types round-trip through encoding/json.Marshal.
func marshalStable(v any) ([]byte, error) {
	switch node := v.(type) {
	case []orderedEntry:
		buf := []byte{'{'}
		for i, e := range node {
			if i > 0 {
				buf = append(buf, ',')
			}
			k, err := json.Marshal(e.Key)
			if err != nil {
				return nil, err
			}
			buf = append(buf, k...)
			buf = append(buf, ':')
			child, err := marshalStable(e.Value)
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, '}')
		return buf, nil
	case json.RawMessage:
		if len(node) == 0 {
			return []byte("null"), nil
		}
		return append([]byte(nil), node...), nil
	case []any:
		buf := []byte{'['}
		for i, child := range node {
			if i > 0 {
				buf = append(buf, ',')
			}
			b, err := marshalStable(child)
			if err != nil {
				return nil, err
			}
			buf = append(buf, b...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(v)
	}
}

// canonicaliseRawJSON decodes raw JSON and re-emits it with sorted keys
// at every object level. Arrays keep their original order (semantic).
func canonicaliseRawJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("null"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return canonicaliseValue(v)
}

func canonicaliseValue(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kj, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kj...)
			buf = append(buf, ':')
			child, err := canonicaliseValue(x[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := []byte{'['}
		for i, c := range x {
			if i > 0 {
				buf = append(buf, ',')
			}
			child, err := canonicaliseValue(c)
			if err != nil {
				return nil, err
			}
			buf = append(buf, child...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(x)
	}
}
