// Package skill provides a skill registry with progressive disclosure for the
// go-assistant agent. Skills are packaged procedural knowledge (SKILL.md files)
// that teach the agent how to accomplish multi-step workflows.
package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Metadata holds the Level-1 metadata extracted from SKILL.md frontmatter.
// This is what appears in the system prompt (~100 tokens per skill).
type Metadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Meta        map[string]string `yaml:"metadata,omitempty"`
}

// Skill represents a fully parsed SKILL.md with metadata and body.
type Skill struct {
	Metadata
	Body    string // markdown body after frontmatter (Level 2)
	Dir     string // absolute path to skill directory
	Builtin bool
}

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	reservedNames = map[string]bool{
		"activate_skill":      true,
		"list_skills":         true,
		"read_skill_resource": true,
	}
)

const (
	nameMinLen        = 3
	nameMaxLen        = 50
	descriptionMaxLen = 500
	frontmatterDelim  = "---"
)

// ParseFile reads and parses a SKILL.md from the OS filesystem at the given path.
func ParseFile(path string) (*Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening skill file: %w", err)
	}
	defer f.Close()
	return parse(f)
}

// ParseFS reads and parses a SKILL.md from an fs.FS at the given path.
func ParseFS(fsys fs.FS, path string) (*Skill, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening skill file from fs: %w", err)
	}
	defer f.Close()
	return parse(f)
}

func parse(r io.Reader) (*Skill, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading skill file: %w", err)
	}

	content := string(data)
	meta, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, err
	}

	var sm Metadata
	if err := yaml.Unmarshal([]byte(meta), &sm); err != nil {
		return nil, fmt.Errorf("parsing YAML frontmatter: %w", err)
	}

	if err := validate(&sm); err != nil {
		return nil, err
	}

	return &Skill{
		Metadata: sm,
		Body:     body,
	}, nil
}

// splitFrontmatter extracts YAML frontmatter (between --- delimiters) and
// the remaining markdown body.
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, frontmatterDelim) {
		return "", "", errors.New("SKILL.md must start with '---' frontmatter delimiter")
	}

	rest := content[len(frontmatterDelim):]
	idx := strings.Index(rest, "\n"+frontmatterDelim)
	if idx < 0 {
		return "", "", errors.New("missing closing '---' frontmatter delimiter")
	}

	// +1 to skip the leading newline; the closing delimiter line is "---"
	frontmatter = rest[:idx]
	afterClose := rest[idx+1+len(frontmatterDelim):]

	// Strip the optional newline right after closing ---
	body = strings.TrimPrefix(afterClose, "\n")

	return strings.TrimSpace(frontmatter), body, nil
}

func validate(sm *Metadata) error {
	sm.Name = strings.TrimSpace(sm.Name)
	sm.Description = strings.TrimSpace(sm.Description)

	if sm.Name == "" {
		return errors.New("skill name is required")
	}
	if len(sm.Name) < nameMinLen || len(sm.Name) > nameMaxLen {
		return fmt.Errorf("skill name must be %d-%d characters, got %d", nameMinLen, nameMaxLen, len(sm.Name))
	}
	if !namePattern.MatchString(sm.Name) {
		return fmt.Errorf("skill name %q must match [a-z0-9-] and not start/end with hyphen", sm.Name)
	}
	if reservedNames[sm.Name] {
		return fmt.Errorf("skill name %q is reserved", sm.Name)
	}

	if sm.Description == "" {
		return errors.New("skill description is required")
	}
	if len(sm.Description) > descriptionMaxLen {
		return fmt.Errorf("skill description must be at most %d characters, got %d", descriptionMaxLen, len(sm.Description))
	}
	if strings.ContainsAny(sm.Description, "<>") {
		return errors.New("skill description must not contain '<' or '>' characters")
	}

	return nil
}
