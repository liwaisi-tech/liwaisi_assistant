package helpparse

import (
	"errors"
	"testing"
)

func TestValidateSchemaJSON_Good(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"binary": "git",
		"flags": [{"long": "--version", "doc": "show version"}],
		"subcommands": [{"name": "push"}],
		"examples": ["git push origin main"]
	}`)
	s, err := ValidateSchemaJSON(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s.Binary != "git" {
		t.Fatalf("binary = %q", s.Binary)
	}
	if len(s.Flags) != 1 || s.Flags[0].Long != "--version" {
		t.Fatalf("flags = %+v", s.Flags)
	}
	if len(s.Subcommands) != 1 || s.Subcommands[0].Name != "push" {
		t.Fatalf("subs = %+v", s.Subcommands)
	}
	if len(s.Examples) != 1 {
		t.Fatalf("examples = %+v", s.Examples)
	}
}

func TestValidateSchemaJSON_MissingBinary(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"flags": [], "subcommands": [], "examples": []}`)
	_, err := ValidateSchemaJSON(raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSchemaValidation) {
		t.Fatalf("want ErrSchemaValidation, got %v", err)
	}
}

func TestValidateSchemaJSON_EmptyBinary(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"binary": "", "flags": [], "subcommands": [], "examples": []}`)
	_, err := ValidateSchemaJSON(raw)
	if err == nil || !errors.Is(err, ErrSchemaValidation) {
		t.Fatalf("want ErrSchemaValidation, got %v", err)
	}
}

func TestValidateSchemaJSON_BadFlagsType(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"binary": "git", "flags": {"bad": true}, "subcommands": [], "examples": []}`)
	_, err := ValidateSchemaJSON(raw)
	if err == nil || !errors.Is(err, ErrSchemaValidation) {
		t.Fatalf("want ErrSchemaValidation, got %v", err)
	}
}

func TestValidateSchemaJSON_FlagMissingLong(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"binary": "git", "flags": [{"short": "-v"}], "subcommands": [], "examples": []}`)
	_, err := ValidateSchemaJSON(raw)
	if err == nil || !errors.Is(err, ErrSchemaValidation) {
		t.Fatalf("want ErrSchemaValidation, got %v", err)
	}
}

func TestValidateSchemaJSON_SubMissingName(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"binary": "git", "flags": [], "subcommands": [{"doc":"no name"}], "examples": []}`)
	_, err := ValidateSchemaJSON(raw)
	if err == nil || !errors.Is(err, ErrSchemaValidation) {
		t.Fatalf("want ErrSchemaValidation, got %v", err)
	}
}

func TestValidateSchemaJSON_NotAnObject(t *testing.T) {
	t.Parallel()
	_, err := ValidateSchemaJSON([]byte(`["array"]`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSchemaBytes(t *testing.T) {
	t.Parallel()
	b := SchemaBytes()
	if len(b) == 0 {
		t.Fatal("empty schema bytes")
	}
	b[0] = 0 // should not mutate embedded copy
	b2 := SchemaBytes()
	if b2[0] == 0 {
		t.Fatal("schema bytes aliased")
	}
}
