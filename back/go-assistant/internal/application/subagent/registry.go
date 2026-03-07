package subagent

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

const registryConcurrencyLimit = 10

// Registry discovers, stores, and persists subagent definitions from
// SUBAGENT.md files on disk. All exported methods are safe for
// concurrent use.
type Registry struct {
	mu    sync.RWMutex
	specs map[string]*ParsedSubAgent
	dirs  []string
}

// NewRegistry creates an empty subagent Registry.
func NewRegistry() *Registry {
	return &Registry{
		specs: make(map[string]*ParsedSubAgent),
	}
}

// LoadFromDir discovers subagents in dir, parsing each <name>/SUBAGENT.md
// concurrently. Invalid subagents are logged and skipped. The directory is
// remembered for future Refresh calls.
func (r *Registry) LoadFromDir(dir string) error {
	r.mu.Lock()
	r.dirs = append(r.dirs, dir)
	r.mu.Unlock()

	return r.loadDir(dir)
}

// Lookup returns the SubAgentSpec for the given name. The boolean reports
// whether the name was found.
func (r *Registry) Lookup(name string) (entity.SubAgentSpec, bool) {
	r.mu.RLock()
	parsed, ok := r.specs[name]
	r.mu.RUnlock()

	if !ok {
		return entity.SubAgentSpec{}, false
	}

	spec, err := parsed.ToSubAgentSpec()
	if err != nil {
		slog.Warn("invalid persisted subagent", "name", name, "error", err)
		return entity.SubAgentSpec{}, false
	}
	return *spec, true
}

// List returns all registered subagent specs sorted alphabetically by name.
func (r *Registry) List() []entity.SubAgentSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]entity.SubAgentSpec, 0, len(r.specs))
	for _, parsed := range r.specs {
		spec, err := parsed.ToSubAgentSpec()
		if err != nil {
			continue
		}
		result = append(result, *spec)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Save persists a SubAgentSpec as a SUBAGENT.md file in the given directory.
// The file is written to dir/{spec.Name}/SUBAGENT.md.
func (r *Registry) Save(spec *entity.SubAgentSpec, dir string) error {
	if spec == nil {
		return errors.New("subagent spec is nil")
	}

	content, err := FormatSubAgentMD(spec)
	if err != nil {
		return fmt.Errorf("formatting subagent: %w", err)
	}

	agentDir := filepath.Join(dir, spec.Name)
	if err := os.MkdirAll(agentDir, 0o750); err != nil {
		return fmt.Errorf("creating subagent directory %q: %w", agentDir, err)
	}

	filePath := filepath.Join(agentDir, subagentFileName)
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing subagent file: %w", err)
	}

	parsed, err := ParseSubAgentFile(filePath)
	if err != nil {
		return fmt.Errorf("verifying saved subagent: %w", err)
	}
	parsed.Dir = agentDir

	r.mu.Lock()
	r.specs[spec.Name] = parsed
	r.mu.Unlock()

	return nil
}

// Refresh re-scans all registered directories. New subagents are added,
// deleted subagents are removed.
func (r *Registry) Refresh() error {
	fresh := make(map[string]*ParsedSubAgent)

	r.mu.RLock()
	dirs := make([]string, len(r.dirs))
	copy(dirs, r.dirs)
	r.mu.RUnlock()

	for _, dir := range dirs {
		agents, err := discoverSubAgentDir(dir)
		if err != nil {
			slog.Warn("refresh: scanning subagent directory", "dir", dir, "error", err)
			continue
		}
		for _, a := range agents {
			fresh[a.Name] = a
		}
	}

	r.mu.Lock()
	r.specs = fresh
	r.mu.Unlock()

	return nil
}

// Count returns the number of registered subagents.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.specs)
}

// Has reports whether the named subagent exists in the registry.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.specs[name]
	return ok
}

// SystemPromptFragment generates an XML block listing all persisted
// subagents for inclusion in the system prompt.
func (r *Registry) SystemPromptFragment() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.specs) == 0 {
		return ""
	}

	names := make([]string, 0, len(r.specs))
	for n := range r.specs {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("\n<available_subagents>\n")
	for _, n := range names {
		s := r.specs[n]
		fmt.Fprintf(&b, "<subagent name=%q>%s</subagent>\n", s.Name, html.EscapeString(s.Description))
	}
	b.WriteString("</available_subagents>")
	return b.String()
}

func (r *Registry) loadDir(dir string) error {
	agents, err := discoverSubAgentDir(dir)
	if err != nil {
		return err
	}
	r.mu.Lock()
	for _, a := range agents {
		r.specs[a.Name] = a
	}
	r.mu.Unlock()
	return nil
}

// discoverSubAgentDir scans dir for subdirectories containing SUBAGENT.md
// and parses them concurrently with bounded parallelism.
func discoverSubAgentDir(dir string) ([]*ParsedSubAgent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading subagents directory %q: %w", dir, err)
	}

	var mu sync.Mutex
	var agents []*ParsedSubAgent

	g := new(errgroup.Group)
	g.SetLimit(registryConcurrencyLimit)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(dir, entry.Name())

		g.Go(func() error {
			filePath := filepath.Join(agentDir, subagentFileName)
			a, err := ParseSubAgentFile(filePath)
			if err != nil {
				slog.Warn("skipping subagent", "dir", agentDir, "error", err)
				return nil
			}
			a.Dir = agentDir

			mu.Lock()
			agents = append(agents, a)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return agents, nil
}
