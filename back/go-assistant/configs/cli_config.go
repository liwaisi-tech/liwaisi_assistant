package configs

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CLIConfig holds CLI-specific configuration.
type CLIConfig struct {
	// Theme is the color theme name (e.g. "dark", "light").
	Theme string `yaml:"theme"`
	// HistorySize is the number of input history entries to retain.
	HistorySize int `yaml:"history_size"`
}

// DefaultCLIConfig returns the default CLI configuration.
func DefaultCLIConfig() *CLIConfig {
	return &CLIConfig{
		Theme:       "dark",
		HistorySize: 100,
	}
}

// LoadCLIConfig reads CLI configuration from a YAML file, then applies
// environment variable overrides. Missing file is not an error; defaults
// are returned instead.
func LoadCLIConfig(path string) (*CLIConfig, error) {
	cfg := DefaultCLIConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading CLI config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing CLI config: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// SaveCLIConfig writes the CLI configuration to a YAML file.
// It creates the parent directory if it does not exist.
func SaveCLIConfig(path string, cfg *CLIConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling CLI config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing CLI config: %w", err)
	}
	return nil
}

func applyEnvOverrides(cfg *CLIConfig) {
	if v := os.Getenv("GO_ASSISTANT_CLI_THEME"); v != "" {
		cfg.Theme = v
	}
	if v := os.Getenv("GO_ASSISTANT_CLI_HISTORY_SIZE"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			cfg.HistorySize = n
		} else {
			slog.Warn("ignoring invalid GO_ASSISTANT_CLI_HISTORY_SIZE", "value", v)
		}
	}
}
