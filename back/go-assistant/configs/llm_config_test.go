package configs

import "testing"

func TestLoadLLMConfig_MissingAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")

	_, err := LoadLLMConfig()
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestLoadLLMConfig_Defaults(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key-123")

	for _, key := range []string{
		"GO_ASSISTANT_LLM_MODEL",
		"GO_ASSISTANT_LLM_BASE_URL",
		"GO_ASSISTANT_LLM_TEMPERATURE",
		"GO_ASSISTANT_LLM_MAX_TOKENS",
	} {
		t.Setenv(key, "")
	}

	cfg, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig error: %v", err)
	}

	if cfg.APIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "test-key-123")
	}
	if cfg.Model != "google/gemini-3.1-flash-lite-preview" {
		t.Errorf("Model = %q, want %q", cfg.Model, "google/gemini-3.1-flash-lite-preview")
	}
	if cfg.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://openrouter.ai/api/v1")
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", cfg.Temperature)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", cfg.MaxTokens)
	}
}

func TestLoadLLMConfig_EnvOverrides(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "my-key")
	t.Setenv("GO_ASSISTANT_LLM_MODEL", "openai/gpt-4o")
	t.Setenv("GO_ASSISTANT_LLM_BASE_URL", "https://custom.api/v1")
	t.Setenv("GO_ASSISTANT_LLM_TEMPERATURE", "0.3")
	t.Setenv("GO_ASSISTANT_LLM_MAX_TOKENS", "2048")

	cfg, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig error: %v", err)
	}

	if cfg.Model != "openai/gpt-4o" {
		t.Errorf("Model = %q, want %q", cfg.Model, "openai/gpt-4o")
	}
	if cfg.BaseURL != "https://custom.api/v1" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://custom.api/v1")
	}
	if cfg.Temperature != 0.3 {
		t.Errorf("Temperature = %f, want 0.3", cfg.Temperature)
	}
	if cfg.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", cfg.MaxTokens)
	}
}

func TestLoadLLMConfig_InvalidTemperature(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"negative", "-1"},
		{"too high", "3.0"},
		{"not a number", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "key")
			t.Setenv("GO_ASSISTANT_LLM_TEMPERATURE", tt.value)

			cfg, err := LoadLLMConfig()
			if err != nil {
				t.Fatalf("LoadLLMConfig error: %v", err)
			}
			if cfg.Temperature != 0.7 {
				t.Errorf("Temperature = %f, want default 0.7 (invalid value ignored)", cfg.Temperature)
			}
		})
	}
}

func TestLoadLLMConfig_InvalidMaxTokens(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-100"},
		{"not a number", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "key")
			t.Setenv("GO_ASSISTANT_LLM_MAX_TOKENS", tt.value)

			cfg, err := LoadLLMConfig()
			if err != nil {
				t.Fatalf("LoadLLMConfig error: %v", err)
			}
			if cfg.MaxTokens != 4096 {
				t.Errorf("MaxTokens = %d, want default 4096 (invalid value ignored)", cfg.MaxTokens)
			}
		})
	}
}

func TestLoadLLMConfig_ToolLoadingStrategy(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{name: "default_is_jit", envValue: "", want: ToolLoadingJIT},
		{name: "explicit_jit", envValue: "jit", want: ToolLoadingJIT},
		{name: "explicit_eager", envValue: "eager", want: ToolLoadingEager},
		{name: "unknown_defaults_to_jit", envValue: "bogus", want: ToolLoadingJIT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_API_KEY", "key")
			t.Setenv("GO_ASSISTANT_TOOL_LOADING", tt.envValue)

			cfg, err := LoadLLMConfig()
			if err != nil {
				t.Fatalf("LoadLLMConfig error: %v", err)
			}
			if cfg.ToolLoadingStrategy != tt.want {
				t.Errorf("ToolLoadingStrategy = %q, want %q", cfg.ToolLoadingStrategy, tt.want)
			}
		})
	}
}
