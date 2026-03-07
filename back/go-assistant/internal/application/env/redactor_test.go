package env_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
)

func TestRedactor_Redact(t *testing.T) {
	tests := []struct {
		name     string
		secrets  map[string]string // key -> value
		input    string
		expected string
	}{
		{
			name:     "no secrets registered",
			secrets:  nil,
			input:    "some output with no secrets",
			expected: "some output with no secrets",
		},
		{
			name:     "single secret redacted",
			secrets:  map[string]string{"API_KEY": "sk-secret-123"},
			input:    "Authorization: Bearer sk-secret-123",
			expected: "Authorization: Bearer [REDACTED:API_KEY]",
		},
		{
			name: "multiple secrets redacted",
			secrets: map[string]string{
				"OPENROUTER_API_KEY": "sk-or-v1-abc",
				"CLICKUP_API_KEY":    "pk_12345",
			},
			input:    "key1=sk-or-v1-abc key2=pk_12345",
			expected: "key1=[REDACTED:OPENROUTER_API_KEY] key2=[REDACTED:CLICKUP_API_KEY]",
		},
		{
			name: "longest match first prevents partial replacement",
			secrets: map[string]string{
				"SHORT":  "abc",
				"LONGER": "abcdef",
			},
			input:    "value=abcdef",
			expected: "value=[REDACTED:LONGER]",
		},
		{
			name:     "no match returns original",
			secrets:  map[string]string{"KEY": "needle"},
			input:    "haystack without the target",
			expected: "haystack without the target",
		},
		{
			name:     "empty input",
			secrets:  map[string]string{"KEY": "val"},
			input:    "",
			expected: "",
		},
		{
			name:     "secret appears multiple times",
			secrets:  map[string]string{"TOKEN": "xyz"},
			input:    "first xyz then xyz again",
			expected: "first [REDACTED:TOKEN] then [REDACTED:TOKEN] again",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := env.NewRedactor()
			for k, v := range tt.secrets {
				r.Register(k, v)
			}

			got := r.Redact(tt.input)
			if got != tt.expected {
				t.Errorf("Redact() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRedactor_RegisterOverwrite(t *testing.T) {
	r := env.NewRedactor()
	r.Register("KEY", "old-value")
	r.Register("KEY", "new-value")

	got := r.Redact("old-value and new-value")
	if strings.Contains(got, "new-value") {
		t.Errorf("expected new-value to be redacted, got %q", got)
	}
	if !strings.Contains(got, "old-value") {
		t.Errorf("expected old-value to NOT be redacted after overwrite, got %q", got)
	}
}

func TestRedactor_Unregister(t *testing.T) {
	r := env.NewRedactor()
	r.Register("KEY", "secret")

	got := r.Redact("has secret here")
	if !strings.Contains(got, "[REDACTED:KEY]") {
		t.Fatal("expected redaction before unregister")
	}

	r.Unregister("KEY")

	got = r.Redact("has secret here")
	if strings.Contains(got, "[REDACTED") {
		t.Errorf("expected no redaction after unregister, got %q", got)
	}
}

func TestRedactor_UnregisterNonExistent(t *testing.T) {
	r := env.NewRedactor()
	r.Unregister("DOES_NOT_EXIST")
}

func TestRedactor_RegisterEmptyValue(t *testing.T) {
	r := env.NewRedactor()
	r.Register("KEY", "")

	got := r.Redact("some text")
	if got != "some text" {
		t.Errorf("registering empty value should be a no-op, got %q", got)
	}
}

func TestRedactor_ConcurrentAccess(t *testing.T) {
	r := env.NewRedactor()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		key := "KEY"
		val := "secret-value"

		go func() {
			defer wg.Done()
			r.Register(key, val)
		}()
		go func() {
			defer wg.Done()
			_ = r.Redact("text with secret-value inside")
		}()
		go func() {
			defer wg.Done()
			r.Unregister(key)
		}()
	}

	wg.Wait()
}
