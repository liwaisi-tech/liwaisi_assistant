// Package config provides encrypted platform configuration storage with
// hot-reload support. It implements a resolution chain: env var > DB > default.
package config

import (
	"context"
	"time"
)

// ConfigEntry is a single configuration value stored in the database.
type ConfigEntry struct {
	Key       string
	Value     string
	IsSecret  bool
	UpdatedBy string
	UpdatedAt time.Time
}

// ConfigDef describes a known platform configuration key.
type ConfigDef struct {
	Key      string
	Label    string
	Category string
	EnvVar   string
	IsSecret bool
	Required bool
	Default  string
}

// ConfigItem is a merged view of a config key shown to the admin UI.
type ConfigItem struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret"`
	Source    string `json:"source"` // "env", "db", "default"
	Label     string `json:"label"`
	Category  string `json:"category"`
	Required  bool   `json:"required"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// ConfigStore is the persistence interface for config entries.
type ConfigStore interface {
	Get(ctx context.Context, key string) (*ConfigEntry, error)
	Set(ctx context.Context, entry *ConfigEntry) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]*ConfigEntry, error)
}

// PlatformConfigs is the static list of all known configuration keys.
var PlatformConfigs = []ConfigDef{
	{Key: "openrouter_api_key", Label: "OpenRouter API Key", Category: "llm", EnvVar: "OPENROUTER_API_KEY", IsSecret: true, Required: true},
	{Key: "google_client_id", Label: "Google Client ID", Category: "auth", EnvVar: "GOOGLE_CLIENT_ID", IsSecret: false, Required: false},
	{Key: "default_model", Label: "Default Model", Category: "llm", EnvVar: "DEFAULT_MODEL", IsSecret: false, Required: false, Default: "anthropic/claude-sonnet-4-6"},
	{Key: "cors_origins", Label: "CORS Origins", Category: "server", EnvVar: "CORS_ORIGINS", IsSecret: false, Required: false, Default: "*"},
	{Key: "model_classifier", Label: "Classifier Model", Category: "models", EnvVar: "MODEL_CLASSIFIER", IsSecret: false, Required: false, Default: "google/gemini-2.0-flash-001"},
	{Key: "model_structured", Label: "Structured Model", Category: "models", EnvVar: "MODEL_STRUCTURED", IsSecret: false, Required: false, Default: "anthropic/claude-haiku-4-5-20251001"},
	{Key: "model_reasoning", Label: "Reasoning Model", Category: "models", EnvVar: "MODEL_REASONING", IsSecret: false, Required: false, Default: "anthropic/claude-sonnet-4-6"},
	{Key: "model_long_context", Label: "Long Context Model", Category: "models", EnvVar: "MODEL_LONG_CONTEXT", IsSecret: false, Required: false, Default: "google/gemini-2.0-pro-001"},
	{Key: "model_summarize", Label: "Summarize Model", Category: "models", EnvVar: "MODEL_SUMMARIZE", IsSecret: false, Required: false, Default: "meta-llama/llama-3.3-8b-instruct"},
	{Key: "model_thinking", Label: "Thinking Model", Category: "models", EnvVar: "MODEL_THINKING", IsSecret: false, Required: false, Default: "anthropic/claude-opus-4-6"},
	{Key: "openrouter_app_url", Label: "OpenRouter App URL", Category: "llm", EnvVar: "OPENROUTER_APP_URL", IsSecret: false, Required: false},
	{Key: "openrouter_app_title", Label: "OpenRouter App Title", Category: "llm", EnvVar: "OPENROUTER_APP_TITLE", IsSecret: false, Required: false},
}

// platformConfigIndex is a lookup map built at init time.
var platformConfigIndex map[string]*ConfigDef

func init() {
	platformConfigIndex = make(map[string]*ConfigDef, len(PlatformConfigs))
	for i := range PlatformConfigs {
		platformConfigIndex[PlatformConfigs[i].Key] = &PlatformConfigs[i]
	}
}

// LookupConfigDef returns the ConfigDef for a key, or nil if unknown.
func LookupConfigDef(key string) *ConfigDef {
	return platformConfigIndex[key]
}
