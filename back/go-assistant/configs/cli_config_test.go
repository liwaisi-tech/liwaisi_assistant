package configs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCLIConfig(t *testing.T) {
	cfg := DefaultCLIConfig()
	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", cfg.Theme)
	}
	if cfg.HistorySize != 100 {
		t.Errorf("HistorySize = %d, want 100", cfg.HistorySize)
	}
}

func TestLoadCLIConfig_MissingFile(t *testing.T) {
	cfg, err := LoadCLIConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want dark (default)", cfg.Theme)
	}
}

func TestLoadCLIConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "liwaisi.yaml")
	content := []byte("theme: light\nhistory_size: 50\n")
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := LoadCLIConfig(path)
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light", cfg.Theme)
	}
	if cfg.HistorySize != 50 {
		t.Errorf("HistorySize = %d, want 50", cfg.HistorySize)
	}
}

func TestLoadCLIConfig_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "liwaisi.yaml")
	content := []byte("theme: dark\nhistory_size: 100\n")
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	t.Setenv("GO_ASSISTANT_CLI_THEME", "light")
	t.Setenv("GO_ASSISTANT_CLI_HISTORY_SIZE", "200")

	cfg, err := LoadCLIConfig(path)
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light (from env)", cfg.Theme)
	}
	if cfg.HistorySize != 200 {
		t.Errorf("HistorySize = %d, want 200 (from env)", cfg.HistorySize)
	}
}

func TestSaveCLIConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "liwaisi.yaml")

	cfg := &CLIConfig{Theme: "light", HistorySize: 50}
	if err := SaveCLIConfig(path, cfg); err != nil {
		t.Fatalf("SaveCLIConfig() error = %v", err)
	}

	loaded, err := LoadCLIConfig(path)
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if loaded.Theme != "light" {
		t.Errorf("Theme = %q, want light", loaded.Theme)
	}
	if loaded.HistorySize != 50 {
		t.Errorf("HistorySize = %d, want 50", loaded.HistorySize)
	}
}

func TestLoadCLIConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "liwaisi.yaml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o640); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, err := LoadCLIConfig(path)
	if err == nil {
		t.Error("LoadCLIConfig() should error on invalid YAML")
	}
}
