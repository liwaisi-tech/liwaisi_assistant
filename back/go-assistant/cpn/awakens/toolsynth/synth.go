package toolsynth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

// SynthesiseManifest converts one HelpSchema into a PendingTool. Pure and
// deterministic: identical input yields byte-identical output (REQ-1204).
// Sets Kind=synthesized, Origin=brae-awakens/help-parser (REQ-1201) and
// stamps ProvenanceSHA256 over the canonical JSON (REQ-1202).
//
// CreatedAt is NOT baked into the provenance hash — callers set it after
// synthesis so re-synthesis at a different wall-clock instant still compares
// byte-stable.
func SynthesiseManifest(schema helpparse.HelpSchema) (PendingTool, error) {
	binary := strings.TrimSpace(schema.Binary)
	if binary == "" {
		return PendingTool{}, ErrEmptyBinary
	}

	flags := append([]helpparse.HelpFlag(nil), schema.Flags...)
	for _, f := range flags {
		if strings.TrimSpace(f.Long) == "" {
			return PendingTool{}, fmt.Errorf("%w: binary=%s", ErrMalformedFlag, binary)
		}
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Long < flags[j].Long })

	subs := append([]helpparse.HelpSub(nil), schema.Subcommands...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })

	examples := append([]string(nil), schema.Examples...)

	paramsJSON, err := buildParametersSchema(flags, subs)
	if err != nil {
		return PendingTool{}, fmt.Errorf("toolsynth: params schema: %w", err)
	}

	helpText := renderHelpText(binary, flags, subs, examples)
	manifest := cpn.ToolManifest{
		Namespace:    DefaultNamespace,
		Name:         binary,
		Version:      DefaultVersion,
		Schema:       paramsJSON,
		HelpText:     helpText,
		Origin:       OriginHelpParser,
		Kind:         KindSynthesized,
		RegisteredBy: OriginHelpParser,
		Provenance: cpn.ProvenanceSnapshot{
			SourceSHA256: schema.SourceSHA256,
		},
	}

	provSum, err := canonicalManifestSHA256(manifest)
	if err != nil {
		return PendingTool{}, fmt.Errorf("toolsynth: canonicalise manifest: %w", err)
	}

	return PendingTool{
		Manifest:         manifest,
		SourceSHA256:     schema.SourceSHA256,
		ProvenanceSHA256: provSum,
	}, nil
}

// buildParametersSchema emits a draft-2020-12-style JSON object with one
// property per flag and one "subcommand" enum when subcommands exist. The
// map-based nature of encoding/json already gives us sorted keys, but we
// build explicit ordered maps for determinism clarity.
func buildParametersSchema(flags []helpparse.HelpFlag, subs []helpparse.HelpSub) (json.RawMessage, error) {
	props := make(map[string]any, len(flags)+1)
	for _, f := range flags {
		key := strings.TrimPrefix(f.Long, "--")
		entry := map[string]any{}
		if f.Arg {
			entry["type"] = "string"
		} else {
			entry["type"] = "boolean"
		}
		if f.Short != "" {
			entry["short"] = f.Short
		}
		if f.Doc != "" {
			entry["description"] = f.Doc
		}
		props[key] = entry
	}
	if len(subs) > 0 {
		names := make([]string, 0, len(subs))
		for _, s := range subs {
			if strings.TrimSpace(s.Name) == "" {
				continue
			}
			names = append(names, s.Name)
		}
		sort.Strings(names)
		props["subcommand"] = map[string]any{
			"type": "string",
			"enum": names,
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	return canonicalJSON(schema)
}

func renderHelpText(binary string, flags []helpparse.HelpFlag, subs []helpparse.HelpSub, examples []string) string {
	var b strings.Builder
	b.WriteString(binary)
	b.WriteString("\n")
	if len(flags) > 0 {
		b.WriteString("Flags:\n")
		for _, f := range flags {
			b.WriteString("  ")
			b.WriteString(f.Long)
			if f.Short != "" {
				b.WriteString(", ")
				b.WriteString(f.Short)
			}
			if f.Doc != "" {
				b.WriteString("\t")
				b.WriteString(f.Doc)
			}
			b.WriteString("\n")
		}
	}
	if len(subs) > 0 {
		b.WriteString("Subcommands:\n")
		for _, s := range subs {
			b.WriteString("  ")
			b.WriteString(s.Name)
			if s.Doc != "" {
				b.WriteString("\t")
				b.WriteString(s.Doc)
			}
			b.WriteString("\n")
		}
	}
	if len(examples) > 0 {
		b.WriteString("Examples:\n")
		for _, ex := range examples {
			b.WriteString("  ")
			b.WriteString(ex)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// canonicalJSON serialises v with sorted keys. encoding/json already sorts
// map keys; we round-trip through (encode → decode-to-map → re-encode) to
// flatten any struct-declared ordering and guarantee sorted keys end-to-end.
func canonicalJSON(v any) ([]byte, error) {
	first, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var probe any
	if err := json.Unmarshal(first, &probe); err != nil {
		return nil, err
	}
	probe = sortMaps(probe)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(probe); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// json.Encoder appends a trailing \n; strip it so the hash input is
	// exactly the canonical object bytes.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

// sortMaps walks v and sorts every map[string]any via the JSON-key order
// encoding/json will use. encoding/json already sorts map keys when
// marshalling; the explicit pass here just normalises any nested structures
// before re-marshal.
func sortMaps(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = sortMaps(x[k])
		}
		return out
	case []any:
		for i, e := range x {
			x[i] = sortMaps(e)
		}
		return x
	default:
		return v
	}
}

// canonicalManifestSHA256 returns the hex-encoded sha256 over the canonical
// JSON of the manifest. Excludes timestamps and registration metadata that
// can legitimately vary across re-synthesis.
func canonicalManifestSHA256(m cpn.ToolManifest) (string, error) {
	canonical, err := canonicalJSON(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
