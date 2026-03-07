// Package subagent provides application-layer components for subagent
// orchestration, including output validation and result contracts.
package subagent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationError holds a structured validation failure.
// Field identifies which aspect failed (e.g. "format", "length", a key name).
// Message is written to be both human-readable and suitable as LLM retry feedback.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface. When Field is non-empty the output
// includes the field name so callers (and LLMs) can locate the problem.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation failed on %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation failed: %s", e.Message)
}

// ResultValidator validates a subagent's raw output string.
// Implementations must be safe for concurrent use.
type ResultValidator interface {
	// Validate checks the output and returns nil if valid,
	// or a *ValidationError with a message suitable for LLM feedback.
	Validate(output string) error

	// Description returns a human-readable description of the expected
	// output format. This text is included in the subagent's system prompt
	// so the LLM knows the expected structure upfront.
	Description() string
}

// NoOpValidator accepts any output without validation.
// Use it for subagents that return free-form text.
type NoOpValidator struct{}

// Validate always returns nil.
func (NoOpValidator) Validate(string) error { return nil }

// Description returns an empty string because no format is enforced.
func (NoOpValidator) Description() string { return "" }

// JSONValidator validates that the output is well-formed JSON and optionally
// checks for required top-level keys and a maximum character length.
type JSONValidator struct {
	RequiredKeys []string
	MaxLength    int // 0 means no limit.
}

// Validate checks that the output is valid JSON, respects MaxLength, and
// contains every RequiredKeys entry as a top-level key.
func (v *JSONValidator) Validate(output string) error {
	if v.MaxLength > 0 && len(output) > v.MaxLength {
		return &ValidationError{
			Field: "length",
			Message: fmt.Sprintf(
				"output exceeds maximum length of %d characters (got %d)",
				v.MaxLength, len(output)),
		}
	}

	trimmed := strings.TrimSpace(output)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return &ValidationError{
			Field: "format",
			Message: fmt.Sprintf(
				"output is not valid JSON: %s. Please respond with ONLY a valid JSON object.", err.Error()),
		}
	}

	for _, key := range v.RequiredKeys {
		if _, ok := parsed[key]; !ok {
			return &ValidationError{
				Field:   key,
				Message: fmt.Sprintf("required key %q is missing from the JSON response", key),
			}
		}
	}

	return nil
}

// Description returns a prompt-friendly format description that lists the
// required keys when present.
func (v *JSONValidator) Description() string {
	if len(v.RequiredKeys) == 0 {
		return "Respond with ONLY a valid JSON object."
	}
	return fmt.Sprintf(
		"Respond with ONLY a valid JSON object containing these required keys: %s",
		strings.Join(v.RequiredKeys, ", "))
}

// TypedValidator validates output by attempting to unmarshal it into a
// concrete Go type T. An optional CustomCheck function runs after successful
// unmarshal to enforce domain-specific constraints.
type TypedValidator[T any] struct {
	CustomCheck func(parsed *T) error
}

// Validate unmarshals the output into T and, if CustomCheck is set, runs it
// against the parsed value.
func (v *TypedValidator[T]) Validate(output string) error {
	trimmed := strings.TrimSpace(output)

	var result T
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return &ValidationError{
			Field: "format",
			Message: fmt.Sprintf(
				"output does not match expected schema: %s. Please fix and respond with ONLY valid JSON.",
				err.Error()),
		}
	}

	if v.CustomCheck != nil {
		return v.CustomCheck(&result)
	}

	return nil
}

// Description returns a generic schema prompt. Callers that need a more
// specific description should compose their own system prompt instead.
func (v *TypedValidator[T]) Description() string {
	return "Respond with ONLY a valid JSON object matching the expected schema."
}
