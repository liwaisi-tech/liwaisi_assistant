package a2a

import (
	"os"
	"strings"
)

// Config holds the A2A adapter configuration.
type Config struct {
	// Enabled controls whether the A2A adapter registers its handlers.
	Enabled bool

	// BaseURL is the public base URL for the A2A endpoint (e.g., "https://api.liwaisi.com").
	BaseURL string
}

// ConfigFromEnv reads A2A configuration from environment variables.
// ENABLE_A2A controls the feature flag (default: false).
// A2A_BASE_URL sets the public base URL (default: "http://localhost:8080").
func ConfigFromEnv() Config {
	enabled := strings.EqualFold(os.Getenv("ENABLE_A2A"), "true")
	baseURL := os.Getenv("A2A_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return Config{
		Enabled: enabled,
		BaseURL: baseURL,
	}
}

// IsEnabled returns true if the A2A adapter should be active.
func (c Config) IsEnabled() bool {
	return c.Enabled
}
