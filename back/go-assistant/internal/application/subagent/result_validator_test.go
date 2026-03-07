package subagent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidationError_Error(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		message string
		want    string
	}{
		{
			name:    "with field",
			field:   "format",
			message: "output is not valid JSON",
			want:    `validation failed on "format": output is not valid JSON`,
		},
		{
			name:    "without field",
			field:   "",
			message: "something went wrong",
			want:    "validation failed: something went wrong",
		},
		{
			name:    "field with special characters",
			field:   "required_key",
			message: `key "severity" is missing`,
			want:    `validation failed on "required_key": key "severity" is missing`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ValidationError{Field: tt.field, Message: tt.message}
			if got := e.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidationError_ImplementsError(t *testing.T) {
	ve := &ValidationError{Field: "f", Message: "m"}

	// Wrap the concrete *ValidationError so errors.As actually unwraps it.
	wrapped := fmt.Errorf("outer: %w", ve)

	var target *ValidationError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should unwrap *ValidationError from wrapped error")
	}
	if target.Field != "f" || target.Message != "m" {
		t.Errorf("unwrapped error has wrong fields: %+v", target)
	}
}

// ---------------------------------------------------------------------------
// NoOpValidator
// ---------------------------------------------------------------------------

func TestNoOpValidator_Validate(t *testing.T) {
	v := NoOpValidator{}
	tests := []struct {
		name   string
		output string
	}{
		{"empty string", ""},
		{"plain text", "some random text"},
		{"valid JSON", `{"key": "value"}`},
		{"invalid JSON", `{not json`},
		{"very long output", strings.Repeat("x", 100_000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Validate(tt.output); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.output, err)
			}
		})
	}
}

func TestNoOpValidator_Description(t *testing.T) {
	v := NoOpValidator{}
	if got := v.Description(); got != "" {
		t.Errorf("Description() = %q, want empty string", got)
	}
}

func TestNoOpValidator_ImplementsInterface(t *testing.T) {
	var _ ResultValidator = NoOpValidator{}
}

// ---------------------------------------------------------------------------
// JSONValidator
// ---------------------------------------------------------------------------

func TestJSONValidator_Validate(t *testing.T) {
	tests := []struct {
		name         string
		requiredKeys []string
		maxLength    int
		output       string
		wantErr      bool
		wantField    string
	}{
		{
			name:    "valid JSON no requirements",
			output:  `{"foo": "bar"}`,
			wantErr: false,
		},
		{
			name:    "valid JSON with whitespace",
			output:  `  {"foo": "bar"}  `,
			wantErr: false,
		},
		{
			name:      "invalid JSON",
			output:    `{not json at all}`,
			wantErr:   true,
			wantField: "format",
		},
		{
			name:      "plain text",
			output:    "hello world",
			wantErr:   true,
			wantField: "format",
		},
		{
			name:      "empty string",
			output:    "",
			wantErr:   true,
			wantField: "format",
		},
		{
			name:      "JSON array instead of object",
			output:    `[1, 2, 3]`,
			wantErr:   true,
			wantField: "format",
		},
		{
			name:         "all required keys present",
			requiredKeys: []string{"severity", "message"},
			output:       `{"severity": "high", "message": "found issue", "extra": true}`,
			wantErr:      false,
		},
		{
			name:         "missing one required key",
			requiredKeys: []string{"severity", "message"},
			output:       `{"severity": "high"}`,
			wantErr:      true,
			wantField:    "message",
		},
		{
			name:         "missing all required keys",
			requiredKeys: []string{"severity", "message"},
			output:       `{"other": "value"}`,
			wantErr:      true,
			wantField:    "severity",
		},
		{
			name:      "within max length",
			maxLength: 100,
			output:    `{"ok": true}`,
			wantErr:   false,
		},
		{
			name:      "exceeds max length",
			maxLength: 10,
			output:    `{"key": "a very long value that exceeds the limit"}`,
			wantErr:   true,
			wantField: "length",
		},
		{
			name:      "exactly at max length",
			maxLength: 12,
			output:    `{"ok": true}`,
			wantErr:   false,
		},
		{
			name:      "zero max length means no limit",
			maxLength: 0,
			output:    strings.Repeat("x", 10_000),
			wantErr:   true,
			wantField: "format",
		},
		{
			name:         "required key with null value is present",
			requiredKeys: []string{"data"},
			output:       `{"data": null}`,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &JSONValidator{
				RequiredKeys: tt.requiredKeys,
				MaxLength:    tt.maxLength,
			}

			err := v.Validate(tt.output)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if ve.Field != tt.wantField {
					t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
				}
				if ve.Message == "" {
					t.Error("ValidationError.Message should not be empty")
				}
			}
		})
	}
}

func TestJSONValidator_Description(t *testing.T) {
	tests := []struct {
		name         string
		requiredKeys []string
		wantContains []string
	}{
		{
			name:         "no required keys",
			requiredKeys: nil,
			wantContains: []string{"JSON"},
		},
		{
			name:         "with required keys",
			requiredKeys: []string{"severity", "message"},
			wantContains: []string{"JSON", "severity", "message"},
		},
		{
			name:         "single required key",
			requiredKeys: []string{"result"},
			wantContains: []string{"result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &JSONValidator{RequiredKeys: tt.requiredKeys}
			desc := v.Description()
			for _, s := range tt.wantContains {
				if !strings.Contains(desc, s) {
					t.Errorf("Description() = %q, want it to contain %q", desc, s)
				}
			}
		})
	}
}

func TestJSONValidator_ImplementsInterface(t *testing.T) {
	var _ ResultValidator = &JSONValidator{}
}

// ---------------------------------------------------------------------------
// TypedValidator
// ---------------------------------------------------------------------------

type testReviewResult struct {
	Severity   string  `json:"severity"`
	Message    string  `json:"message"`
	Confidence float64 `json:"confidence"`
}

func TestTypedValidator_Validate(t *testing.T) {
	tests := []struct {
		name        string
		customCheck func(parsed *testReviewResult) error
		output      string
		wantErr     bool
		wantField   string
	}{
		{
			name:    "valid output matching type",
			output:  `{"severity": "high", "message": "SQL injection", "confidence": 0.95}`,
			wantErr: false,
		},
		{
			name:    "valid with extra fields ignored",
			output:  `{"severity": "low", "message": "ok", "confidence": 0.5, "extra": true}`,
			wantErr: false,
		},
		{
			name:    "valid with missing optional fields",
			output:  `{"severity": "medium"}`,
			wantErr: false,
		},
		{
			name:      "invalid JSON",
			output:    `not json`,
			wantErr:   true,
			wantField: "format",
		},
		{
			name:      "JSON array instead of object",
			output:    `[1, 2, 3]`,
			wantErr:   true,
			wantField: "format",
		},
		{
			name:      "wrong type for field",
			output:    `{"severity": 123, "message": "ok", "confidence": "not_a_number"}`,
			wantErr:   true,
			wantField: "format",
		},
		{
			name:    "valid with whitespace",
			output:  `  {"severity": "high", "message": "found it", "confidence": 0.9}  `,
			wantErr: false,
		},
		{
			name: "custom check passes",
			customCheck: func(parsed *testReviewResult) error {
				if parsed.Confidence < 0 || parsed.Confidence > 1 {
					return &ValidationError{
						Field:   "confidence",
						Message: "confidence must be between 0.0 and 1.0",
					}
				}
				return nil
			},
			output:  `{"severity": "high", "message": "ok", "confidence": 0.8}`,
			wantErr: false,
		},
		{
			name: "custom check fails",
			customCheck: func(parsed *testReviewResult) error {
				if parsed.Confidence < 0 || parsed.Confidence > 1 {
					return &ValidationError{
						Field:   "confidence",
						Message: "confidence must be between 0.0 and 1.0",
					}
				}
				return nil
			},
			output:    `{"severity": "high", "message": "ok", "confidence": 5.0}`,
			wantErr:   true,
			wantField: "confidence",
		},
		{
			name: "custom check returns non-ValidationError",
			customCheck: func(parsed *testReviewResult) error {
				if parsed.Severity == "" {
					return errors.New("severity is required")
				}
				return nil
			},
			output:  `{"severity": "", "message": "ok", "confidence": 0.5}`,
			wantErr: true,
		},
		{
			name:      "empty string",
			output:    "",
			wantErr:   true,
			wantField: "format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &TypedValidator[testReviewResult]{
				CustomCheck: tt.customCheck,
			}

			err := v.Validate(tt.output)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.wantField != "" {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error is not *ValidationError: %T", err)
				}
				if ve.Field != tt.wantField {
					t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
				}
			}
		})
	}
}

func TestTypedValidator_Description(t *testing.T) {
	v := &TypedValidator[testReviewResult]{}
	desc := v.Description()
	if !strings.Contains(desc, "JSON") {
		t.Errorf("Description() = %q, want it to contain %q", desc, "JSON")
	}
}

func TestTypedValidator_ImplementsInterface(t *testing.T) {
	var _ ResultValidator = &TypedValidator[testReviewResult]{}
}

// ---------------------------------------------------------------------------
// LLM-friendly error messages
// ---------------------------------------------------------------------------

func TestErrorMessagesAreLLMFriendly(t *testing.T) {
	tests := []struct {
		name      string
		validator ResultValidator
		output    string
	}{
		{
			name:      "JSONValidator invalid JSON",
			validator: &JSONValidator{},
			output:    "not json",
		},
		{
			name:      "JSONValidator missing key",
			validator: &JSONValidator{RequiredKeys: []string{"severity"}},
			output:    `{"other": "value"}`,
		},
		{
			name:      "JSONValidator exceeds length",
			validator: &JSONValidator{MaxLength: 5},
			output:    `{"key": "value"}`,
		},
		{
			name:      "TypedValidator bad schema",
			validator: &TypedValidator[testReviewResult]{},
			output:    "not json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validator.Validate(tt.output)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			msg := err.Error()
			if len(msg) < 20 {
				t.Errorf("error message too short to be actionable: %q", msg)
			}
			if strings.Contains(strings.ToLower(msg), "panic") {
				t.Errorf("error message should not mention panic: %q", msg)
			}
		})
	}
}
