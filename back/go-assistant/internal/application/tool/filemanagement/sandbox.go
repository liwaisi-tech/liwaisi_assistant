// Package filemanagement provides file management tools for the agent workspace.
package filemanagement

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var defaultSensitivePatterns = []string{
	"env.yaml",
	".env",
	".env.*",
	"*.env",
	"*.key",
	"*.pem",
	"*.p12",
	"*.pfx",
	"*secret*",
	"*credential*",
	"*.keystore",
	"*.jks",
	"id_rsa",
	"id_ed25519",
	"id_ecdsa",
}

// Sandbox restricts file operations to a root directory and blocks access
// to files matching sensitive patterns.
type Sandbox struct {
	root              string
	sensitivePatterns []string
}

// SandboxOption configures a Sandbox.
type SandboxOption func(*Sandbox)

// WithSensitivePatterns overrides the default sensitive file patterns.
func WithSensitivePatterns(patterns []string) SandboxOption {
	return func(s *Sandbox) {
		s.sensitivePatterns = patterns
	}
}

// NewSandbox creates a sandbox rooted at the given absolute path.
// Returns an error if root is not an absolute path.
func NewSandbox(root string, opts ...SandboxOption) (*Sandbox, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("sandbox root must be an absolute path, got %q", root)
	}
	s := &Sandbox{
		root:              filepath.Clean(root),
		sensitivePatterns: defaultSensitivePatterns,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Root returns the absolute root path of the sandbox.
func (s *Sandbox) Root() string {
	return s.root
}

// Resolve converts a relative path to an absolute path within the sandbox.
// Returns an error if the resolved path escapes the sandbox boundary.
func (s *Sandbox) Resolve(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed, use relative paths within the workspace (got %q)", path)
	}

	cleaned := filepath.Clean(path)
	abs := filepath.Join(s.root, cleaned)
	abs = filepath.Clean(abs)

	if abs != s.root && !strings.HasPrefix(abs, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes workspace boundary", path)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err == nil {
		realAbs := filepath.Join(resolved, filepath.Base(abs))
		if realAbs != s.root && !strings.HasPrefix(realAbs, s.root+string(os.PathSeparator)) {
			return "", fmt.Errorf("path %q resolves outside workspace boundary via symlink", path)
		}
	}

	if realFull, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		if realFull != s.root && !strings.HasPrefix(realFull, s.root+string(os.PathSeparator)) {
			return "", fmt.Errorf("path %q resolves outside workspace boundary via symlink", path)
		}
	}

	return abs, nil
}

// IsSensitiveFile reports whether path matches any of the sensitive file
// patterns. It checks the basename of path against each pattern using
// filepath.Match.
func (s *Sandbox) IsSensitiveFile(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	for _, pattern := range s.sensitivePatterns {
		if matched, _ := filepath.Match(strings.ToLower(pattern), lower); matched {
			return true
		}
	}
	return false
}
