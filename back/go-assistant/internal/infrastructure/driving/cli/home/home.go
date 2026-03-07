// Package home manages the liwaisi CLI runtime directory ($HOME/.liwaisi/).
//
// The directory layout follows Linux Filesystem Hierarchy Standard conventions:
//
//	$HOME/.liwaisi/
//	├── boot/       — startup config, profile
//	├── config/     — user-editable settings
//	├── workspace/  — per-project contexts
//	├── data/       — persistent storage (SQLite, migrations)
//	├── bin/        — agent tools, plugins
//	└── tmp/        — ephemeral scratch space
package home

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Dir identifies a well-known subdirectory inside the liwaisi home.
type Dir int

const (
	// Boot holds startup and initialization files.
	Boot Dir = iota
	// Config holds user-editable configuration.
	Config
	// Workspace holds per-project contexts.
	Workspace
	// Data holds persistent storage (databases, migrations).
	Data
	// Bin holds agent tools, plugins, and executables.
	Bin
	// Tmp holds ephemeral scratch files cleaned on startup.
	Tmp
)

const (
	defaultDirName = ".liwaisi"
	envLiwaisiHome = "BRAE_HOME"
	dirPerm        = 0o750
	filePerm       = 0o640
)

var dirNames = map[Dir]string{
	Boot:      "boot",
	Config:    "config",
	Workspace: "workspace",
	Data:      "data",
	Bin:       "bin",
	Tmp:       "tmp",
}

// Home manages the liwaisi runtime directory tree.
type Home struct {
	root string
}

// New creates a Home rooted at path. If path is empty, it resolves
// $BRAE_HOME first, then falls back to $HOME/.liwaisi.
func New(path string) (*Home, error) {
	root, err := resolveRoot(path)
	if err != nil {
		return nil, fmt.Errorf("resolving liwaisi home: %w", err)
	}
	return &Home{root: root}, nil
}

// Root returns the absolute path of the liwaisi home directory.
func (h *Home) Root() string {
	return h.root
}

// Init creates all well-known subdirectories and writes default config files
// if they do not already exist. The operation is idempotent.
func (h *Home) Init() error {
	for _, name := range dirNames {
		dir := filepath.Join(h.root, name)
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	defaults := []struct {
		rel     string
		content any
	}{
		{"boot/init.yaml", defaultInitConfig()},
		{"boot/profile.yaml", defaultProfileConfig()},
		{"config/liwaisi.yaml", defaultLiwaisiConfig()},
	}

	for _, d := range defaults {
		path := filepath.Join(h.root, d.rel)
		if err := writeYAMLIfAbsent(path, d.content); err != nil {
			return fmt.Errorf("writing default %s: %w", d.rel, err)
		}
	}

	return nil
}

// Path returns the absolute path for a well-known directory.
func (h *Home) Path(d Dir) string {
	name, ok := dirNames[d]
	if !ok {
		return h.root
	}
	return filepath.Join(h.root, name)
}

// CleanTmp removes files inside the tmp directory that are older than maxAge.
// Subdirectories are not traversed — only top-level entries are considered.
func (h *Home) CleanTmp(maxAge time.Duration) error {
	tmpDir := h.Path(Tmp)
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading tmp directory: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	var errs []error

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(tmpDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, fmt.Errorf("removing %s: %w", path, err))
			}
		}
	}

	return errors.Join(errs...)
}

// WorkspacePath returns the absolute path for a named project workspace.
func (h *Home) WorkspacePath(project string) string {
	return filepath.Join(h.Path(Workspace), project)
}

func resolveRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}

	if env := os.Getenv(envLiwaisiHome); env != "" {
		return filepath.Abs(env)
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining user home directory: %w", err)
	}
	return filepath.Join(userHome, defaultDirName), nil
}

func writeYAMLIfAbsent(path string, content any) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	data, err := yaml.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}
	return os.WriteFile(path, data, filePerm)
}

type initConfig struct {
	Version   string `yaml:"version"`
	CreatedAt string `yaml:"created_at"`
}

type profileConfig struct {
	DefaultModel string            `yaml:"default_model"`
	Aliases      map[string]string `yaml:"aliases"`
}

type liwaisiConfig struct {
	Theme       string `yaml:"theme"`
	HistorySize int    `yaml:"history_size"`
}

func defaultInitConfig() *initConfig {
	return &initConfig{
		Version:   "1",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func defaultProfileConfig() *profileConfig {
	return &profileConfig{
		DefaultModel: "",
		Aliases:      map[string]string{},
	}
}

func defaultLiwaisiConfig() *liwaisiConfig {
	return &liwaisiConfig{
		Theme:       "dark",
		HistorySize: 100,
	}
}
