package cpn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// FlowSignature is the deterministic, structural fingerprint of a crystallised
// CPN topology used by the architect lane to answer "does an existing flow fit
// this request?" without invoking an LLM.
//
// All slice fields are stored in sorted, de-duplicated canonical form so that
// value equality over Digest is a safe cache key (PAT-004, spec
// spec-architecture-cpn-agent-architect §4.2).
//
// The signature is intentionally a structural projection of a topology — it
// does not track per-run metrics (that belongs to flow_ranking) nor runtime
// execution state (that belongs to the event log). Keep it small, pure, and
// comparable.
type FlowSignature struct {
	// InputColors is the sorted set of colours the topology accepts on its
	// source places (places with no incoming transition arcs).
	InputColors []string `json:"input_colors"`
	// OutputColors is the sorted set of colours emitted by sink places
	// (places with no outgoing transition arcs).
	OutputColors []string `json:"output_colors"`
	// RequiredTools is the sorted set of qualified tool names (namespace/name)
	// referenced by NodeKindTool/NodeKindBash transitions in the flow.
	RequiredTools []string `json:"required_tools"`
	// RequiredCaps is the sorted set of host capability identifiers the flow
	// transitively needs (e.g. "host.bash", "fs.read"). Populated by the
	// caller via a resolver — ComputeFlowSignature leaves it empty when no
	// resolver is provided so host-gate wiring stays a single source of truth.
	RequiredCaps []string `json:"required_caps"`
	// Hashtags is the union of hashtags across all referenced tools, after
	// NormalizeSet. Populated only when a resolver is supplied.
	Hashtags []string `json:"hashtags"`
	// Template is the synthesis template id (e.g. "parallel-fanout",
	// "sequential-pipeline") when the flow came from the JIT composer, or
	// "custom" for hand-authored / agent-authored topologies.
	Template string `json:"template"`
	// Digest is the sha256 hex over the canonical JSON encoding of the other
	// fields. Stable across calls on the same logical signature.
	Digest string `json:"digest"`
}

// FlowSignatureResolver is the optional plug-in a caller supplies to
// ComputeFlowSignature so the signature can be enriched with per-tool hashtags
// and required host capabilities. The default nil resolver yields a signature
// with only the structural (colour + tool) fields populated.
type FlowSignatureResolver interface {
	// HashtagsFor returns the canonical hashtag set for the tool's latest
	// non-deprecated entry. Empty slice (or nil) means "no hashtags known".
	HashtagsFor(qualifiedTool string) []string
	// CapsFor returns the host capabilities the tool requires (host.bash,
	// fs.read, etc.). Empty means "no host capability".
	CapsFor(qualifiedTool string) []string
}

// ComputeFlowSignature walks a topology and emits its FlowSignature. It is
// pure: given the same *CPN and resolver it returns byte-identical output,
// including Digest. A nil resolver is legal — structural fields are still
// populated, and Hashtags/RequiredCaps are left empty.
//
// Source/sink detection is based on the arc graph: a place is a source if no
// transition lists it in OutputPlaces, and a sink if no transition lists it in
// InputPlaces.
func ComputeFlowSignature(c *CPN, resolver FlowSignatureResolver) FlowSignature {
	sig := FlowSignature{Template: "custom"}
	if c == nil {
		sig.Digest = signatureDigest(sig)
		return sig
	}

	// Fold transition arcs into sets for source/sink classification.
	producers := make(map[string]struct{}, len(c.Places))
	consumers := make(map[string]struct{}, len(c.Places))
	toolSet := make(map[string]struct{})

	for _, t := range c.Transitions {
		if t == nil {
			continue
		}
		for _, p := range t.InputPlaces {
			consumers[p] = struct{}{}
		}
		for _, p := range t.OutputPlaces {
			producers[p] = struct{}{}
		}
		switch t.Kind {
		case NodeKindTool, NodeKindBash:
			if name := qualifiedToolName(t); name != "" {
				toolSet[name] = struct{}{}
			}
		}
	}

	inColors := make(map[string]struct{})
	outColors := make(map[string]struct{})
	for id, p := range c.Places {
		if p == nil {
			continue
		}
		col := string(p.Color)
		if _, hasProducer := producers[id]; !hasProducer {
			inColors[col] = struct{}{}
		}
		if _, hasConsumer := consumers[id]; !hasConsumer {
			outColors[col] = struct{}{}
		}
	}
	sig.InputColors = sortedKeys(inColors)
	sig.OutputColors = sortedKeys(outColors)
	sig.RequiredTools = sortedKeys(toolSet)

	if resolver != nil {
		hashtagSet := make(map[string]struct{})
		capSet := make(map[string]struct{})
		for _, tool := range sig.RequiredTools {
			for _, tag := range resolver.HashtagsFor(tool) {
				if tag = strings.TrimSpace(tag); tag != "" {
					hashtagSet[tag] = struct{}{}
				}
			}
			for _, cap := range resolver.CapsFor(tool) {
				if cap = strings.TrimSpace(cap); cap != "" {
					capSet[cap] = struct{}{}
				}
			}
		}
		sig.Hashtags = sortedKeys(hashtagSet)
		sig.RequiredCaps = sortedKeys(capSet)
	}

	sig.Digest = signatureDigest(sig)
	return sig
}

// WithTemplate returns a copy of sig with Template replaced and Digest
// recomputed. Useful when the JIT composer knows the template id up-front
// and wants to stamp it before persisting.
func (sig FlowSignature) WithTemplate(template string) FlowSignature {
	sig.Template = template
	sig.Digest = signatureDigest(sig)
	return sig
}

// Covers reports whether this signature satisfies the caller's requirements:
// all required hashtags and capabilities are present, and every required input
// colour is produced somewhere in the flow's input surface. Nil / empty
// requirement slices mean "no constraint".
func (sig FlowSignature) Covers(reqHashtags, reqCaps, reqInputColors []string) bool {
	if !isSubset(reqHashtags, sig.Hashtags) {
		return false
	}
	if !isSubset(reqCaps, sig.RequiredCaps) {
		return false
	}
	if !isSubset(reqInputColors, sig.InputColors) {
		return false
	}
	return true
}

// qualifiedToolName prefers ToolMeta.Namespace + ToolName; falls back to
// ToolName alone when no namespace is available.
func qualifiedToolName(t *Transition) string {
	if t == nil {
		return ""
	}
	if t.ToolMeta != nil && t.ToolMeta.Namespace != "" && t.ToolName != "" {
		return t.ToolMeta.Namespace + "/" + t.ToolName
	}
	if t.ToolName != "" {
		return t.ToolName
	}
	return t.ID
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isSubset(needles, haystack []string) bool {
	if len(needles) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(haystack))
	for _, v := range haystack {
		have[v] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := have[n]; !ok {
			return false
		}
	}
	return true
}

func signatureDigest(sig FlowSignature) string {
	// Zero out Digest so it never contributes to its own hash.
	sig.Digest = ""
	blob, err := json.Marshal(sig)
	if err != nil {
		// Impossible in practice (only string slices + strings), but keep
		// the signature shape stable even on a panic path.
		return ""
	}
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}
