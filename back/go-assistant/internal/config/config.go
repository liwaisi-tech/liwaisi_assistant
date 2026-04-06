package config

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a config key does not exist in the store.
var ErrNotFound = errors.New("config: not found")

// Entry represents a stored config value.
type Entry struct {
	Key       string
	Value     string // decrypted plaintext
	IsSecret  bool
	UpdatedAt time.Time
	UpdatedBy string
}

// Store is the port interface for encrypted config persistence.
type Store interface {
	Get(ctx context.Context, key string) (*Entry, error)
	Set(ctx context.Context, entry *Entry) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]*Entry, error)
}

// Def defines metadata for a known platform config key.
type Def struct {
	Key      string
	EnvVar   string
	IsSecret bool
	Required bool
	Default  string
	Label    string
	Category string // "llm", "auth", "server", "models"
}

// PlatformConfigs is the list of all known platform config keys.
var PlatformConfigs = []Def{
	// LLM
	{Key: "openrouter_api_key", EnvVar: "OPENROUTER_API_KEY", IsSecret: true, Required: true, Label: "OpenRouter API Key", Category: "llm"},
	{Key: "default_model", EnvVar: "DEFAULT_MODEL", IsSecret: false, Default: "anthropic/claude-sonnet-4-6", Label: "Default Model", Category: "llm"},
	{Key: "openrouter_app_url", EnvVar: "OPENROUTER_APP_URL", IsSecret: false, Label: "OpenRouter App URL", Category: "llm"},
	{Key: "openrouter_app_title", EnvVar: "OPENROUTER_APP_TITLE", IsSecret: false, Label: "OpenRouter App Title", Category: "llm"},

	// Auth
	{Key: "google_client_id", EnvVar: "GOOGLE_CLIENT_ID", IsSecret: false, Label: "Google Client ID", Category: "auth"},
	{Key: "allowed_emails", EnvVar: "ALLOWED_EMAILS", IsSecret: false, Label: "Allowed Emails (comma-separated, empty = allow all)", Category: "auth"},

	// Server
	{Key: "cors_origins", EnvVar: "CORS_ORIGINS", IsSecret: false, Default: "*", Label: "CORS Origins", Category: "server"},

	// Model role overrides
	{Key: "model_classifier", EnvVar: "MODEL_CLASSIFIER", IsSecret: false, Label: "Classifier Model", Category: "models"},
	{Key: "model_structured", EnvVar: "MODEL_STRUCTURED", IsSecret: false, Label: "Structured Model", Category: "models"},
	{Key: "model_reasoning", EnvVar: "MODEL_REASONING", IsSecret: false, Label: "Reasoning Model", Category: "models"},
	{Key: "model_long_context", EnvVar: "MODEL_LONG_CONTEXT", IsSecret: false, Label: "Long Context Model", Category: "models"},
	{Key: "model_summarize", EnvVar: "MODEL_SUMMARIZE", IsSecret: false, Label: "Summarize Model", Category: "models"},
	{Key: "model_thinking", EnvVar: "MODEL_THINKING", IsSecret: false, Label: "Thinking Model", Category: "models"},
}

// DefByKey returns the definition for a given key, or nil if not found.
func DefByKey(key string) *Def {
	for i := range PlatformConfigs {
		if PlatformConfigs[i].Key == key {
			return &PlatformConfigs[i]
		}
	}
	return nil
}
