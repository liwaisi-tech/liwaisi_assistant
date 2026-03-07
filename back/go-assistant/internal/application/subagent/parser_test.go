package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const validSubAgentMD = `---
name: code-reviewer
description: Reviews code for quality, security, and best practices.
model_tier: fast
allowed_tools:
  - read_file
  - grep
max_turns: 5
timeout: 3m
metadata:
  author: test
---
You are a senior code reviewer. Analyze the provided code for:
1. Security vulnerabilities
2. Performance issues
3. Code style violations

Provide actionable feedback.
`

func TestParseSubAgentFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantName  string
		wantDesc  string
		wantTier  string
		wantTools []string
		wantTurns int
		wantErr   bool
	}{
		{
			name:      "valid subagent",
			content:   validSubAgentMD,
			wantName:  "code-reviewer",
			wantDesc:  "Reviews code for quality, security, and best practices.",
			wantTier:  "fast",
			wantTools: []string{"read_file", "grep"},
			wantTurns: 5,
		},
		{
			name: "minimal valid subagent",
			content: `---
name: test-agent
description: A test agent.
---
You are a test agent.
`,
			wantName:  "test-agent",
			wantDesc:  "A test agent.",
			wantTurns: 0,
		},
		{
			name: "missing opening delimiter",
			content: `name: bad
description: no delimiters
`,
			wantErr: true,
		},
		{
			name: "missing closing delimiter",
			content: `---
name: bad
description: no closing
`,
			wantErr: true,
		},
		{
			name: "missing name",
			content: `---
description: no name field
---
Body here.
`,
			wantErr: true,
		},
		{
			name: "name too short",
			content: `---
name: ab
description: name too short
---
Body here.
`,
			wantErr: true,
		},
		{
			name: "invalid name pattern",
			content: `---
name: Invalid_Name
description: bad pattern
---
Body here.
`,
			wantErr: true,
		},
		{
			name: "missing description",
			content: `---
name: valid-name
---
Body here.
`,
			wantErr: true,
		},
		{
			name: "invalid model tier",
			content: `---
name: bad-tier
description: invalid tier
model_tier: ultra
---
Body here.
`,
			wantErr: true,
		},
		{
			name: "invalid timeout",
			content: `---
name: bad-timeout
description: invalid timeout
timeout: not-a-duration
---
Body here.
`,
			wantErr: true,
		},
		{
			name:     "windows line endings",
			content:  "---\r\nname: win-agent\r\ndescription: Windows line endings.\r\n---\r\nInstruction body.\r\n",
			wantName: "win-agent",
			wantDesc: "Windows line endings.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, subagentFileName)
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("writing test file: %v", err)
			}

			parsed, err := ParseSubAgentFile(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if parsed.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", parsed.Name, tt.wantName)
			}
			if parsed.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", parsed.Description, tt.wantDesc)
			}
			if tt.wantTier != "" && parsed.ModelTier != tt.wantTier {
				t.Errorf("ModelTier = %q, want %q", parsed.ModelTier, tt.wantTier)
			}
			if tt.wantTools != nil {
				if len(parsed.AllowedTools) != len(tt.wantTools) {
					t.Errorf("AllowedTools = %v, want %v", parsed.AllowedTools, tt.wantTools)
				}
			}
			if parsed.MaxTurns != tt.wantTurns {
				t.Errorf("MaxTurns = %d, want %d", parsed.MaxTurns, tt.wantTurns)
			}
		})
	}
}

func TestParseSubAgentFS(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"test-agent/SUBAGENT.md": &fstest.MapFile{
			Data: []byte(`---
name: test-agent
description: FS-based test agent.
---
You are a test agent loaded from fs.FS.
`),
		},
	}

	parsed, err := ParseSubAgentFS(fsys, "test-agent/SUBAGENT.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", parsed.Name, "test-agent")
	}
	if parsed.Body == "" {
		t.Error("Body should not be empty")
	}
}

func TestParseSubAgentFile_NotFound(t *testing.T) {
	t.Parallel()
	_, err := ParseSubAgentFile("/nonexistent/path/SUBAGENT.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestToSubAgentSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parsed      ParsedSubAgent
		wantName    string
		wantTier    valueobject.ModelTier
		wantTimeout time.Duration
		wantErr     bool
	}{
		{
			name: "full conversion",
			parsed: ParsedSubAgent{
				Metadata: Metadata{
					Name:         "code-reviewer",
					Description:  "Reviews code.",
					ModelTier:    "fast",
					AllowedTools: []string{"read_file"},
					MaxTurns:     5,
					Timeout:      "3m",
				},
				Body: "You are a code reviewer.",
			},
			wantName:    "code-reviewer",
			wantTier:    valueobject.ModelTierFast,
			wantTimeout: 3 * time.Minute,
		},
		{
			name: "minimal conversion",
			parsed: ParsedSubAgent{
				Metadata: Metadata{
					Name:        "basic-agent",
					Description: "Basic.",
				},
				Body: "Do something.",
			},
			wantName: "basic-agent",
			wantTier: "",
		},
		{
			name: "empty body",
			parsed: ParsedSubAgent{
				Metadata: Metadata{
					Name:        "empty-body",
					Description: "No instruction.",
				},
				Body: "   ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := tt.parsed.ToSubAgentSpec()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if spec.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", spec.Name, tt.wantName)
			}
			if spec.ModelTier != tt.wantTier {
				t.Errorf("ModelTier = %q, want %q", spec.ModelTier, tt.wantTier)
			}
			if tt.wantTimeout > 0 && spec.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", spec.Timeout, tt.wantTimeout)
			}
		})
	}
}

func TestFormatSubAgentMD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    *entity.SubAgentSpec
		wantErr bool
	}{
		{
			name: "full roundtrip",
			spec: &entity.SubAgentSpec{
				Name:         "code-reviewer",
				Description:  "Reviews code.",
				Instruction:  "You are a code reviewer.",
				ModelTier:    valueobject.ModelTierFast,
				AllowedTools: []string{"read_file"},
				MaxTurns:     5,
				Timeout:      3 * time.Minute,
			},
		},
		{
			name: "minimal spec",
			spec: &entity.SubAgentSpec{
				Name:        "basic",
				Instruction: "Do the thing.",
			},
		},
		{
			name: "missing name",
			spec: &entity.SubAgentSpec{
				Instruction: "No name.",
			},
			wantErr: true,
		},
		{
			name: "missing instruction",
			spec: &entity.SubAgentSpec{
				Name: "no-instruction",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := FormatSubAgentMD(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if content == "" {
				t.Fatal("content should not be empty")
			}
			if !strings.HasPrefix(content, frontmatterDelim) {
				t.Error("content should start with frontmatter delimiter")
			}
		})
	}
}

func TestFormatSubAgentMD_Roundtrip(t *testing.T) {
	t.Parallel()

	original := &entity.SubAgentSpec{
		Name:         "roundtrip-agent",
		Description:  "Tests roundtrip serialization.",
		Instruction:  "You are a roundtrip test agent.\nDo great things.",
		ModelTier:    valueobject.ModelTierBalanced,
		AllowedTools: []string{"read_file", "grep"},
		MaxTurns:     8,
		Timeout:      5 * time.Minute,
	}

	content, err := FormatSubAgentMD(original)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, subagentFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	parsed, err := ParseSubAgentFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	spec, err := parsed.ToSubAgentSpec()
	if err != nil {
		t.Fatalf("to spec: %v", err)
	}

	if spec.Name != original.Name {
		t.Errorf("Name = %q, want %q", spec.Name, original.Name)
	}
	if spec.Description != original.Description {
		t.Errorf("Description = %q, want %q", spec.Description, original.Description)
	}
	if spec.ModelTier != original.ModelTier {
		t.Errorf("ModelTier = %q, want %q", spec.ModelTier, original.ModelTier)
	}
	if len(spec.AllowedTools) != len(original.AllowedTools) {
		t.Errorf("AllowedTools = %v, want %v", spec.AllowedTools, original.AllowedTools)
	}
	if spec.MaxTurns != original.MaxTurns {
		t.Errorf("MaxTurns = %d, want %d", spec.MaxTurns, original.MaxTurns)
	}
	if spec.Timeout != original.Timeout {
		t.Errorf("Timeout = %v, want %v", spec.Timeout, original.Timeout)
	}
}
