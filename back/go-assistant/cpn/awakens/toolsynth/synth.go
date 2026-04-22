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

// maxPromotedFlags bounds the curated-flag list rendered into HelpText.
// LLM tool-selection degrades when every option is enumerated — we keep the
// most informative N and collapse the tail into a catch-all sentence. Ten
// tracks the Anthropic "top 3–5 plus compact tail" guidance with headroom
// for multi-modal tools like `grep`/`git`. Pure, deterministic: caller
// pre-sorts flags.
const maxPromotedFlags = 10

// renderHelpText builds an LLM-optimized description from the parsed help
// schema. The layout follows 2026 tool-use guidelines (Anthropic, OpenAI
// function calling, MCP): capability line → when-to-use → when-NOT-to-use →
// curated key flags → subcommands → examples → caveats. Raw `--help` dumps
// are explicitly avoided — flags are curated to `maxPromotedFlags` with a
// count-collapsed tail so the model spends tokens on selection, not scanning.
//
// Pure and deterministic (REQ-1204): inputs must already be sorted by the
// caller; no clock, no map iteration, no randomness.
func renderHelpText(binary string, flags []helpparse.HelpFlag, subs []helpparse.HelpSub, examples []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s — command-line tool registered from its `--help` output during brae awakening.\n\n", binary)

	fmt.Fprintf(&b,
		"When to use: Call this tool when the user asks to run `%s` on the host. Pass only the flags relevant to the user's request; the argument schema mirrors the parsed options.\n\n",
		binary)

	b.WriteString("When NOT to use: Do not use for tasks outside this binary's domain or when a more specific synthesized tool already covers the request. Treat state-mutating flags (e.g. --in-place, -i, --force) as side-effectful — surface them to the user before invoking.\n\n")

	// Curated flag list — drop --help/-h/--version/-V (never load-bearing
	// for selection) and prefer flags with non-empty docs.
	curated, hidden := curateFlags(flags)
	if len(curated) > 0 {
		b.WriteString("Key flags:\n")
		for _, f := range curated {
			b.WriteString("  ")
			b.WriteString(f.Long)
			if f.Short != "" {
				b.WriteString(", ")
				b.WriteString(f.Short)
			}
			if f.Arg {
				b.WriteString(" <value>")
			}
			if f.Doc != "" {
				b.WriteString(" — ")
				b.WriteString(f.Doc)
			}
			b.WriteByte('\n')
		}
		if hidden > 0 {
			fmt.Fprintf(&b, "  (%d more flag(s) available — invoke `%s --help` for the full reference.)\n", hidden, binary)
		}
		b.WriteByte('\n')
	}

	if len(subs) > 0 {
		b.WriteString("Subcommands:\n")
		for _, s := range subs {
			b.WriteString("  ")
			b.WriteString(s.Name)
			if s.Doc != "" {
				b.WriteString(" — ")
				b.WriteString(s.Doc)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if len(examples) > 0 {
		b.WriteString("Examples:\n")
		for _, ex := range examples {
			b.WriteString("  ")
			b.WriteString(ex)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	b.WriteString("Notes: Output goes to stdout/stderr with conventional exit codes. This description was auto-synthesized from parsed help text during awakening; if inaccurate, re-register via the register_tool flow.\n")

	return b.String()
}

// curateFlags drops boilerplate flags (--help/-h/--version/-V) and caps the
// promoted list at maxPromotedFlags, returning the hidden-count so the
// renderer can note the tail. Input must be pre-sorted so the selection is
// deterministic.
func curateFlags(flags []helpparse.HelpFlag) ([]helpparse.HelpFlag, int) {
	out := make([]helpparse.HelpFlag, 0, len(flags))
	for _, f := range flags {
		switch f.Long {
		case "--help", "--version":
			continue
		}
		out = append(out, f)
	}
	if len(out) <= maxPromotedFlags {
		return out, 0
	}
	return out[:maxPromotedFlags], len(out) - maxPromotedFlags
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
