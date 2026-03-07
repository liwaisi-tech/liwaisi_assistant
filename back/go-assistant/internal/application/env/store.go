package env

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	envFilePermissions = 0o600
	envDirPermissions  = 0o750
)

var validKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// envFile is the on-disk YAML structure for stored environment variables.
type envFile struct {
	Variables map[string]string `yaml:"variables"`
}

const defaultFileHeader = "# Managed by liwaisi. Do not commit this file to version control.\n" +
	"# Set variables: liwaisi env set <KEY>\n" +
	"# List variables: liwaisi env list\n" +
	"# Delete variables: liwaisi env delete <KEY>\n"

// Store manages a YAML-backed environment variable file and injects its
// contents into the process environment via os.Setenv. It is safe for
// concurrent use.
type Store struct {
	mu       sync.RWMutex
	filePath string
	redactor *Redactor
}

// NewStore creates a Store that reads from and writes to filePath.
// If redactor is nil an internal no-op redactor is used.
func NewStore(filePath string, redactor *Redactor) *Store {
	if redactor == nil {
		redactor = NewRedactor()
	}
	return &Store{
		filePath: filePath,
		redactor: redactor,
	}
}

// Redactor returns the Redactor associated with this Store.
func (s *Store) Redactor() *Redactor {
	return s.redactor
}

// FilePath returns the absolute path to the env file.
func (s *Store) FilePath() string {
	return s.filePath
}

// Load reads the YAML env file and calls os.Setenv for each variable.
// If the file does not exist it returns (0, nil) — first-boot is not an error.
func (s *Store) Load(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ef, err := s.readFile()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("loading env file: %w", err)
	}

	s.checkPermissions()

	for k, v := range ef.Variables {
		if err := os.Setenv(k, v); err != nil {
			return 0, fmt.Errorf("setting env var %s: %w", k, err)
		}
		s.redactor.Register(k, v)
		slog.Debug("env variable loaded", "key", k)
	}

	return len(ef.Variables), nil
}

// Set writes a key-value pair to the env file and calls os.Setenv.
// The key must match ^[A-Z][A-Z0-9_]*$. The file is created with 0600
// permissions if absent.
func (s *Store) Set(_ context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("env key is empty")
	}
	if !validKeyRE.MatchString(key) {
		return fmt.Errorf("invalid env key %q: must match %s", key, validKeyRE.String())
	}
	if value == "" {
		return fmt.Errorf("env value for %s is empty", key)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ef, err := s.readFile()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading env file: %w", err)
	}
	if ef == nil {
		ef = &envFile{Variables: make(map[string]string)}
	}

	ef.Variables[key] = value

	if err := s.writeFile(ef); err != nil {
		return err
	}

	if err := os.Setenv(key, value); err != nil {
		return fmt.Errorf("setting env var %s: %w", key, err)
	}
	s.redactor.Register(key, value)
	slog.Info("env variable set", "key", key)
	return nil
}

// Delete removes a variable from the env file and calls os.Unsetenv.
func (s *Store) Delete(_ context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("env key is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ef, err := s.readFile()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("env file does not exist")
		}
		return fmt.Errorf("reading env file: %w", err)
	}

	if _, ok := ef.Variables[key]; !ok {
		return fmt.Errorf("env variable %s not found", key)
	}

	delete(ef.Variables, key)

	if err := s.writeFile(ef); err != nil {
		return err
	}

	if err := os.Unsetenv(key); err != nil {
		return fmt.Errorf("unsetting env var %s: %w", key, err)
	}
	s.redactor.Unregister(key)
	slog.Info("env variable deleted", "key", key)
	return nil
}

// List returns the sorted list of variable names stored in the env file.
// It never returns values.
func (s *Store) List(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ef, err := s.readFile()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading env file: %w", err)
	}

	keys := make([]string, 0, len(ef.Variables))
	for k := range ef.Variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// Has reports whether a variable is set. It checks the process environment
// first, then the env file. It returns (found, source, error) — source is
// "process_env" or "env_file". It never returns the variable's value.
func (s *Store) Has(_ context.Context, key string) (found bool, source string, err error) {
	if key == "" {
		return false, "", fmt.Errorf("env key is empty")
	}

	if _, ok := os.LookupEnv(key); ok {
		return true, "process_env", nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ef, err := s.readFile()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("reading env file: %w", err)
	}

	if _, ok := ef.Variables[key]; ok {
		return true, "env_file", nil
	}
	return false, "", nil
}

// readFile reads and unmarshals the YAML env file. Caller must hold at least
// s.mu.RLock.
func (s *Store) readFile() (*envFile, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, err
	}

	var ef envFile
	if err := yaml.Unmarshal(data, &ef); err != nil {
		return nil, fmt.Errorf("parsing env file: %w", err)
	}
	if ef.Variables == nil {
		ef.Variables = make(map[string]string)
	}
	return &ef, nil
}

// writeFile marshals the envFile to YAML and writes it with the standard
// header comment. Caller must hold s.mu.Lock.
func (s *Store) writeFile(ef *envFile) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), envDirPermissions); err != nil {
		return fmt.Errorf("creating env file directory: %w", err)
	}

	data, err := yaml.Marshal(ef)
	if err != nil {
		return fmt.Errorf("marshaling env file: %w", err)
	}

	content := defaultFileHeader + string(data)
	if err := os.WriteFile(s.filePath, []byte(content), envFilePermissions); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}
	return nil
}

// checkPermissions logs a warning if the env file has overly permissive
// permissions. Caller must hold at least s.mu.RLock.
func (s *Store) checkPermissions() {
	info, err := os.Stat(s.filePath)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		slog.Warn("env file has insecure permissions",
			"path", s.filePath,
			"mode", fmt.Sprintf("%04o", info.Mode().Perm()),
			"recommended", "0600",
		)
	}
}
