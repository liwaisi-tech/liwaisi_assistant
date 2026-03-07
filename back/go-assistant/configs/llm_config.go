package configs

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	// APIKey is the OpenRouter API key (required).
	APIKey string
	// Model is the LLM model identifier.
	Model string
	// BaseURL is the API base URL.
	BaseURL string
	// Temperature controls sampling randomness (0.0–2.0).
	Temperature float64
	// MaxTokens is the maximum number of completion tokens.
	MaxTokens int
	// Timeout is the per-request HTTP timeout for LLM calls.
	Timeout time.Duration
	// MaxToolIterations is the maximum number of tool loop iterations.
	MaxToolIterations int
	// ToolLoadingStrategy controls how tools are registered.
	// "jit" (default): progressive disclosure via find_tools meta-tool.
	// "eager": all tools registered at startup (backward compat).
	ToolLoadingStrategy string
}

// ToolLoadingStrategy constants.
const (
	ToolLoadingJIT   = "jit"
	ToolLoadingEager = "eager"
)

// DefaultLLMConfig returns the default LLM configuration.
func DefaultLLMConfig() *LLMConfig {
	return &LLMConfig{
		Model:               "google/gemini-3.1-flash-lite-preview",
		BaseURL:             "https://openrouter.ai/api/v1",
		Temperature:         0.7,
		MaxTokens:           4096,
		Timeout:             5 * time.Minute,
		MaxToolIterations:   50,
		ToolLoadingStrategy: ToolLoadingJIT,
	}
}

// LoadLLMConfig reads LLM configuration from environment variables.
// It returns an error if OPENROUTER_API_KEY is not set.
func LoadLLMConfig() (*LLMConfig, error) {
	cfg := DefaultLLMConfig()

	cfg.APIKey = os.Getenv("OPENROUTER_API_KEY")
	if cfg.APIKey == "" {
		return nil, fmt.Errorf(
			"OPENROUTER_API_KEY environment variable is required. Get your key at https://openrouter.ai/keys",
		)
	}

	if v := os.Getenv("GO_ASSISTANT_LLM_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("GO_ASSISTANT_LLM_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("GO_ASSISTANT_LLM_TEMPERATURE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 2 {
			slog.Warn("ignoring invalid config value",
				"key", "GO_ASSISTANT_LLM_TEMPERATURE",
				"value", v,
			)
		} else {
			cfg.Temperature = f
		}
	}
	if v := os.Getenv("GO_ASSISTANT_LLM_MAX_TOKENS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			slog.Warn("ignoring invalid config value",
				"key", "GO_ASSISTANT_LLM_MAX_TOKENS",
				"value", v,
			)
		} else {
			cfg.MaxTokens = n
		}
	}

	if v := os.Getenv("GO_ASSISTANT_LLM_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			slog.Warn("ignoring invalid config value",
				"key", "GO_ASSISTANT_LLM_TIMEOUT",
				"value", v,
			)
		} else {
			cfg.Timeout = d
		}
	}
	if v := os.Getenv("GO_ASSISTANT_MAX_TOOL_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			slog.Warn("ignoring invalid config value",
				"key", "GO_ASSISTANT_MAX_TOOL_ITERATIONS",
				"value", v,
			)
		} else {
			cfg.MaxToolIterations = n
		}
	}

	if v := os.Getenv("GO_ASSISTANT_TOOL_LOADING"); v != "" {
		switch v {
		case ToolLoadingJIT, ToolLoadingEager:
			cfg.ToolLoadingStrategy = v
		default:
			slog.Warn("unknown tool loading strategy, defaulting to jit",
				"key", "GO_ASSISTANT_TOOL_LOADING",
				"value", v,
			)
		}
	}

	return cfg, nil
}
