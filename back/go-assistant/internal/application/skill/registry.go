package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

const (
	skillFileName    = "SKILL.md"
	maxResourceSize  = 1 << 20 // 1 MB
	concurrencyLimit = 10
)

type embedSource struct {
	fsys fs.FS
	root string
}

// Registry discovers, stores, and activates skills with progressive disclosure.
// All exported methods are safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	skills   map[string]*Skill
	dirs     []string
	embedFSs []embedSource
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*Skill),
	}
}

// LoadFromDir discovers skills in dir, parsing each <name>/SKILL.md concurrently.
// Invalid skills are logged and skipped, not fatal. The directory is remembered
// for future Refresh calls.
func (r *Registry) LoadFromDir(dir string) error {
	r.mu.Lock()
	r.dirs = append(r.dirs, dir)
	r.mu.Unlock()

	return r.loadDir(dir)
}

// LoadEmbedded loads skills from an embed.FS. Each subdirectory under root
// containing a SKILL.md is treated as a skill.
func (r *Registry) LoadEmbedded(fsys fs.FS, root string) error {
	r.mu.Lock()
	r.embedFSs = append(r.embedFSs, embedSource{fsys: fsys, root: root})
	r.mu.Unlock()

	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return fmt.Errorf("reading embedded skills root %q: %w", root, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := entry.Name() + "/" + skillFileName
		if root != "." && root != "" {
			skillPath = root + "/" + skillPath
		}

		skill, err := ParseFS(fsys, skillPath)
		if err != nil {
			slog.Warn("skipping embedded skill", "dir", entry.Name(), "error", err)
			continue
		}

		skill.Builtin = true
		r.register(skill)
	}

	return nil
}

// List returns Level-1 metadata for all registered skills, sorted by name.
func (r *Registry) List() []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metas := make([]Metadata, 0, len(r.skills))
	for _, s := range r.skills {
		metas = append(metas, s.Metadata)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}

// Activate returns the Level-2 body (full SKILL.md markdown) for the named skill.
func (r *Registry) Activate(name string) (string, error) {
	r.mu.RLock()
	s, ok := r.skills[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}

	return s.Body, nil
}

// ReadResource reads a Level-3 resource file from within a skill's directory.
// Path traversal outside the skill directory is rejected.
func (r *Registry) ReadResource(name, path string) (string, error) {
	r.mu.RLock()
	s, ok := r.skills[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}

	if s.Builtin {
		return r.readEmbeddedResource(s, path)
	}

	return r.readFSResource(s, path)
}

// Refresh re-scans all registered directories and embedded sources.
// New skills are added, deleted skills are removed.
func (r *Registry) Refresh() error {
	fresh := make(map[string]*Skill)

	// Reload embedded skills.
	r.mu.RLock()
	embedSources := make([]embedSource, len(r.embedFSs))
	copy(embedSources, r.embedFSs)
	dirs := make([]string, len(r.dirs))
	copy(dirs, r.dirs)
	r.mu.RUnlock()

	for _, src := range embedSources {
		entries, err := fs.ReadDir(src.fsys, src.root)
		if err != nil {
			slog.Warn("refresh: reading embedded root", "root", src.root, "error", err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillPath := entry.Name() + "/" + skillFileName
			if src.root != "." && src.root != "" {
				skillPath = src.root + "/" + skillPath
			}
			skill, err := ParseFS(src.fsys, skillPath)
			if err != nil {
				slog.Warn("refresh: skipping embedded skill", "dir", entry.Name(), "error", err)
				continue
			}
			skill.Builtin = true
			fresh[skill.Name] = skill
		}
	}

	// Reload directory skills.
	for _, dir := range dirs {
		skills, err := discoverDir(dir)
		if err != nil {
			slog.Warn("refresh: scanning directory", "dir", dir, "error", err)
			continue
		}
		for _, s := range skills {
			fresh[s.Name] = s
		}
	}

	r.mu.Lock()
	r.skills = fresh
	r.mu.Unlock()

	return nil
}

// Has reports whether the named skill exists in the registry.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.skills[name]
	return ok
}

// Count returns the number of registered skills.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}

// SystemPromptFragment generates an XML block listing all registered skills
// for inclusion in the system prompt. Each skill entry uses ~100 tokens.
func (r *Registry) SystemPromptFragment() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.skills) == 0 {
		return ""
	}

	names := make([]string, 0, len(r.skills))
	for n := range r.skills {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("\n<available_skills>\n")
	for _, n := range names {
		s := r.skills[n]
		fmt.Fprintf(&b, "<skill name=%q>%s</skill>\n", s.Name, s.Description)
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func (r *Registry) register(s *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name] = s
}

func (r *Registry) loadDir(dir string) error {
	skills, err := discoverDir(dir)
	if err != nil {
		return err
	}
	for _, s := range skills {
		r.register(s)
	}
	return nil
}

// discoverDir scans dir for subdirectories containing SKILL.md and parses
// them concurrently with bounded parallelism.
func discoverDir(dir string) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading skills directory %q: %w", dir, err)
	}

	var mu sync.Mutex
	var skills []*Skill

	g := new(errgroup.Group)
	g.SetLimit(concurrencyLimit)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())

		g.Go(func() error {
			skillPath := filepath.Join(skillDir, skillFileName)
			s, err := ParseFile(skillPath)
			if err != nil {
				slog.Warn("skipping skill", "dir", skillDir, "error", err)
				return nil // don't fail the whole batch
			}
			s.Dir = skillDir

			mu.Lock()
			skills = append(skills, s)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return skills, nil
}

func (r *Registry) readFSResource(s *Skill, path string) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("skill %q has no directory", s.Name)
	}

	cleaned := filepath.Clean(path)
	abs := filepath.Join(s.Dir, cleaned)
	abs = filepath.Clean(abs)

	if !strings.HasPrefix(abs, s.Dir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resource path %q escapes skill directory boundary", path)
	}

	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("resource not found: %s", path)
		}
		return "", fmt.Errorf("resolving resource path: %w", err)
	}
	realDir, err := filepath.EvalSymlinks(s.Dir)
	if err != nil {
		return "", fmt.Errorf("resolving skill directory: %w", err)
	}
	if !strings.HasPrefix(realAbs, realDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resource path %q escapes skill directory boundary", path)
	}

	info, err := os.Stat(realAbs)
	if err != nil {
		return "", fmt.Errorf("accessing resource: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("resource path %q is a directory", path)
	}
	if info.Size() > maxResourceSize {
		return "", fmt.Errorf("resource too large (%d bytes, max %d)", info.Size(), maxResourceSize)
	}

	data, err := os.ReadFile(realAbs)
	if err != nil {
		return "", fmt.Errorf("reading resource: %w", err)
	}
	return string(data), nil
}

func (r *Registry) readEmbeddedResource(s *Skill, path string) (string, error) {
	r.mu.RLock()
	sources := make([]embedSource, len(r.embedFSs))
	copy(sources, r.embedFSs)
	r.mu.RUnlock()

	cleaned := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("resource path %q escapes skill directory boundary", path)
	}

	for _, src := range sources {
		resourcePath := s.Name + "/" + cleaned
		if src.root != "." && src.root != "" {
			resourcePath = src.root + "/" + resourcePath
		}

		data, err := fs.ReadFile(src.fsys, resourcePath)
		if err != nil {
			continue
		}
		if len(data) > maxResourceSize {
			return "", fmt.Errorf("resource too large (%d bytes, max %d)", len(data), maxResourceSize)
		}
		return string(data), nil
	}

	return "", fmt.Errorf("resource %q not found in skill %q", path, s.Name)
}
