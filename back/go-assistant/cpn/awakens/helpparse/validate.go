package helpparse

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed helpschema.json
var embeddedSchema []byte

// SchemaBytes returns the embedded JSON schema text. Exposed so tests and
// SC-12 can reference the same authoritative source.
func SchemaBytes() []byte {
	out := make([]byte, len(embeddedSchema))
	copy(out, embeddedSchema)
	return out
}

// ValidateSchemaJSON decodes raw (LLM output) with DisallowUnknownFields=false
// into HelpSchema, then enforces the structural invariants encoded in the
// embedded JSON-schema:
//
//   - top-level object with required fields: binary, flags, subcommands, examples
//   - binary is a non-empty string
//   - every flag entry has a non-empty "long"
//   - every subcommand entry has a non-empty "name"
//
// Strict decode via json.Decoder catches type mismatches (e.g. flags being an
// object instead of an array) that manual field walks would miss.
func ValidateSchemaJSON(raw []byte) (HelpSchema, error) {
	var probe map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&probe); err != nil {
		return HelpSchema{}, fmt.Errorf("%w: top-level decode: %v", ErrSchemaValidation, err)
	}
	for _, k := range []string{"binary", "flags", "subcommands", "examples"} {
		if _, ok := probe[k]; !ok {
			return HelpSchema{}, fmt.Errorf("%w: missing required field %q", ErrSchemaValidation, k)
		}
	}
	var schema HelpSchema
	strict := json.NewDecoder(bytes.NewReader(raw))
	if err := strict.Decode(&schema); err != nil {
		return HelpSchema{}, fmt.Errorf("%w: decode: %v", ErrSchemaValidation, err)
	}
	if strings.TrimSpace(schema.Binary) == "" {
		return HelpSchema{}, fmt.Errorf("%w: binary is empty", ErrSchemaValidation)
	}
	for i, f := range schema.Flags {
		if strings.TrimSpace(f.Long) == "" {
			return HelpSchema{}, fmt.Errorf("%w: flags[%d].long is empty", ErrSchemaValidation, i)
		}
	}
	for i, s := range schema.Subcommands {
		if strings.TrimSpace(s.Name) == "" {
			return HelpSchema{}, fmt.Errorf("%w: subcommands[%d].name is empty", ErrSchemaValidation, i)
		}
	}
	return schema, nil
}
