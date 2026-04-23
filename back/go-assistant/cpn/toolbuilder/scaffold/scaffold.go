// Package scaffold materialises a hexagonal Go project skeleton into a
// target directory. Used by the tool-atelier CPN's t-scaffold-workspace
// transition before it hands off to git init + go mod init.
//
// All templates are embedded at build time; rendering is deterministic given
// a Params value. No network, no filesystem reads beyond the target tree.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

//go:embed all:templates
var templatesFS embed.FS

// templatesRoot is the on-disk root inside the embed.FS.
const templatesRoot = "templates"

// cmdAppPlaceholder is the static directory name under cmd/ in the embedded
// templates; at render time it is renamed to cmd/<Name>/ so the binary
// lives at cmd/{{.Name}} per Go convention.
const cmdAppPlaceholder = "cmd/app"

// Params drives template expansion. All fields are validated by Validate().
type Params struct {
	// Name is the tool's short identifier (becomes cmd/<Name>/main.go
	// and the Makefile BINARY variable). Must match nameRE.
	Name string

	// Module is the Go module path (becomes the go.mod `module` line).
	// Must be non-empty and contain no spaces.
	Module string

	// Description is a short one-line human summary, embedded in --help text.
	// Must not contain newlines or backticks.
	Description string

	// Author is optional; rendered into README provenance.
	Author string

	// Date overrides "now" for deterministic tests. Zero value means time.Now().
	Date time.Time
}

// nameRE bounds tool names to a safe subset: lowercase letters, digits, dashes.
// Must start with a letter.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Validate checks Params for obvious errors before rendering.
func (p Params) Validate() error {
	if !nameRE.MatchString(p.Name) {
		return fmt.Errorf("scaffold: invalid Name %q (must match %s)", p.Name, nameRE)
	}
	if strings.TrimSpace(p.Module) == "" {
		return errors.New("scaffold: Module is required")
	}
	if strings.ContainsAny(p.Module, " \t\n") {
		return fmt.Errorf("scaffold: Module contains whitespace: %q", p.Module)
	}
	if strings.ContainsAny(p.Description, "\n\r`") {
		return errors.New("scaffold: Description must be a single plain line")
	}
	return nil
}

// templateParams is the type passed to text/template execution. It exposes
// derived fields (notably Date) alongside the user-provided Params.
type templateParams struct {
	Name        string
	Module      string
	Description string
	Author      string
	Date        string
}

func (p Params) toTemplate() templateParams {
	t := p.Date
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return templateParams{
		Name:        p.Name,
		Module:      p.Module,
		Description: p.Description,
		Author:      p.Author,
		Date:        t.Format("2006-01-02"),
	}
}

// RenderInto materialises the scaffold into targetDir. If targetDir exists
// and is non-empty, RenderInto fails. On success the target tree is a ready
// project awaiting `git init` and `go mod init` (those run in the bash
// transition, not here — this function is hermetic).
func RenderInto(targetDir string, params Params) error {
	if err := params.Validate(); err != nil {
		return err
	}

	if err := ensureEmptyDir(targetDir); err != nil {
		return err
	}

	tp := params.toTemplate()

	return fs.WalkDir(templatesFS, templatesRoot, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == templatesRoot {
			return nil
		}

		rel, err := filepath.Rel(templatesRoot, srcPath)
		if err != nil {
			return fmt.Errorf("scaffold: compute rel path %q: %w", srcPath, err)
		}
		rel = remapCmdPath(rel, params.Name)
		dst := filepath.Join(targetDir, strings.TrimSuffix(rel, ".tmpl"))

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		data, err := templatesFS.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("scaffold: read %q: %w", srcPath, err)
		}

		var out []byte
		if strings.HasSuffix(srcPath, ".tmpl") {
			rendered, err := renderTemplate(srcPath, data, tp)
			if err != nil {
				return err
			}
			out = rendered
		} else {
			out = data
		}

		mode := os.FileMode(0o644)
		if strings.HasSuffix(dst, "install.sh") {
			mode = 0o755
		}
		return writeFile(dst, out, mode)
	})
}

// remapCmdPath rewrites "cmd/app/..." → "cmd/<Name>/..." so the binary
// lives under its canonical Go-convention path.
func remapCmdPath(rel, name string) string {
	normalised := filepath.ToSlash(rel)
	if normalised == cmdAppPlaceholder {
		return filepath.FromSlash("cmd/" + name)
	}
	if strings.HasPrefix(normalised, cmdAppPlaceholder+"/") {
		return filepath.FromSlash("cmd/" + name + strings.TrimPrefix(normalised, cmdAppPlaceholder))
	}
	return rel
}

// renderTemplate executes a text/template with tp as the dot value. The
// template name is set to srcPath for better error messages.
func renderTemplate(srcPath string, body []byte, tp templateParams) ([]byte, error) {
	tmpl, err := template.New(srcPath).Option("missingkey=error").Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("scaffold: parse template %q: %w", srcPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tp); err != nil {
		return nil, fmt.Errorf("scaffold: execute template %q: %w", srcPath, err)
	}
	return buf.Bytes(), nil
}

// writeFile is os.WriteFile with one difference: it returns a wrapped error
// including the target path so the caller can surface it in logs/UI.
func writeFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("scaffold: open %q: %w", path, err)
	}
	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		_ = f.Close()
		return fmt.Errorf("scaffold: write %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("scaffold: close %q: %w", path, err)
	}
	return nil
}

// ensureEmptyDir guarantees targetDir exists and contains no entries. If it
// does not exist, it is created.
func ensureEmptyDir(targetDir string) error {
	if targetDir == "" {
		return errors.New("scaffold: target directory is empty")
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(targetDir, 0o755)
		}
		return fmt.Errorf("scaffold: stat %q: %w", targetDir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("scaffold: target %q is not empty (%d entries)", targetDir, len(entries))
	}
	return nil
}
