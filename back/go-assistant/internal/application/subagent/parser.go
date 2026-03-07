package subagent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	subagentFileName      = "SUBAGENT.md"
	nameMinLen            = 3
	nameMaxLen            = 50
	descriptionMaxLen     = 500
	frontmatterDelim      = "---"
	defaultParserMaxTurns = 10
	defaultParserTimeout  = 2 * time.Minute
)

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
)

// Metadata holds the YAML frontmatter from a SUBAGENT.md file.
type Metadata struct {
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	ModelTier    string            `yaml:"model_tier,omitempty"`
	AllowedTools []string          `yaml:"allowed_tools,omitempty"`
	DeniedTools  []string          `yaml:"denied_tools,omitempty"`
	MaxTurns     int               `yaml:"max_turns,omitempty"`
	Timeout      string            `yaml:"timeout,omitempty"`
	Meta         map[string]string `yaml:"metadata,omitempty"`
}

// ParsedSubAgent represents a fully parsed SUBAGENT.md with metadata and body.
type ParsedSubAgent struct {
	Metadata
	Body string
	Dir  string
}

// ParseSubAgentFile reads and parses a SUBAGENT.md from the OS filesystem.
func ParseSubAgentFile(path string) (*ParsedSubAgent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening subagent file: %w", err)
	}
	defer f.Close()
	return parseSubAgent(f)
}

// ParseSubAgentFS reads and parses a SUBAGENT.md from an fs.FS.
func ParseSubAgentFS(fsys fs.FS, path string) (*ParsedSubAgent, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening subagent file from fs: %w", err)
	}
	defer f.Close()
	return parseSubAgent(f)
}

func parseSubAgent(r io.Reader) (*ParsedSubAgent, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading subagent file: %w", err)
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

	if err := validateSubAgentMetadata(&sm); err != nil {
		return nil, err
	}

	return &ParsedSubAgent{
		Metadata: sm,
		Body:     body,
	}, nil
}

// splitFrontmatter extracts YAML frontmatter (between --- delimiters) and
// the remaining markdown body.
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, frontmatterDelim) {
		return "", "", errors.New("SUBAGENT.md must start with '---' frontmatter delimiter")
	}

	rest := content[len(frontmatterDelim):]
	idx := strings.Index(rest, "\n"+frontmatterDelim)
	if idx < 0 {
		return "", "", errors.New("missing closing '---' frontmatter delimiter")
	}

	frontmatter = rest[:idx]
	afterClose := rest[idx+1+len(frontmatterDelim):]
	body = strings.TrimPrefix(afterClose, "\n")

	return strings.TrimSpace(frontmatter), body, nil
}

func validateSubAgentMetadata(sm *Metadata) error {
	sm.Name = strings.TrimSpace(sm.Name)
	sm.Description = strings.TrimSpace(sm.Description)

	if sm.Name == "" {
		return errors.New("subagent name is required")
	}
	if len(sm.Name) < nameMinLen || len(sm.Name) > nameMaxLen {
		return fmt.Errorf("subagent name must be %d-%d characters, got %d", nameMinLen, nameMaxLen, len(sm.Name))
	}
	if !namePattern.MatchString(sm.Name) {
		return fmt.Errorf("subagent name %q must match [a-z0-9-] and not start/end with hyphen", sm.Name)
	}
	if sm.Description == "" {
		return errors.New("subagent description is required")
	}
	if len(sm.Description) > descriptionMaxLen {
		return fmt.Errorf("subagent description must be at most %d characters, got %d", descriptionMaxLen, len(sm.Description))
	}
	if sm.ModelTier != "" {
		tier := valueobject.ModelTier(sm.ModelTier)
		if !tier.IsValid() {
			return fmt.Errorf("invalid model_tier %q", sm.ModelTier)
		}
	}
	if sm.Timeout != "" {
		if _, err := time.ParseDuration(sm.Timeout); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", sm.Timeout, err)
		}
	}
	return nil
}

// ToSubAgentSpec converts a ParsedSubAgent into a domain SubAgentSpec.
// The markdown body becomes the subagent instruction.
func (p *ParsedSubAgent) ToSubAgentSpec() (*entity.SubAgentSpec, error) {
	instruction := strings.TrimSpace(p.Body)
	if instruction == "" {
		return nil, errors.New("subagent instruction (body) is required")
	}

	maxTurns := p.MaxTurns
	if maxTurns == 0 {
		maxTurns = defaultParserMaxTurns
	}

	timeout := defaultParserTimeout
	if p.Timeout != "" {
		d, _ := time.ParseDuration(p.Timeout)
		timeout = d
	}

	spec := &entity.SubAgentSpec{
		Name:         p.Name,
		Description:  p.Description,
		Instruction:  instruction,
		AllowedTools: p.AllowedTools,
		DeniedTools:  p.DeniedTools,
		MaxTurns:     maxTurns,
		Timeout:      timeout,
	}

	if p.ModelTier != "" {
		spec.ModelTier = valueobject.ModelTier(p.ModelTier)
	}

	return spec, nil
}

// FormatSubAgentMD serializes a SubAgentSpec into SUBAGENT.md content with
// YAML frontmatter and a markdown body (instruction).
func FormatSubAgentMD(spec *entity.SubAgentSpec) (string, error) {
	if spec.Name == "" {
		return "", errors.New("subagent name is required for serialization")
	}
	if spec.Instruction == "" {
		return "", errors.New("subagent instruction is required for serialization")
	}

	meta := Metadata{
		Name:         spec.Name,
		Description:  spec.Description,
		AllowedTools: spec.AllowedTools,
		DeniedTools:  spec.DeniedTools,
		MaxTurns:     spec.MaxTurns,
	}
	if spec.ModelTier != "" {
		meta.ModelTier = string(spec.ModelTier)
	}
	if spec.Timeout > 0 {
		meta.Timeout = spec.Timeout.String()
	}

	yamlBytes, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshaling subagent metadata: %w", err)
	}

	var b strings.Builder
	b.WriteString(frontmatterDelim)
	b.WriteByte('\n')
	b.Write(yamlBytes)
	b.WriteString(frontmatterDelim)
	b.WriteByte('\n')
	b.WriteString(spec.Instruction)
	if !strings.HasSuffix(spec.Instruction, "\n") {
		b.WriteByte('\n')
	}

	return b.String(), nil
}
