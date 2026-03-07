package skill

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseFS_ValidSkill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
		wantBody string
		wantMeta map[string]string
	}{
		{
			name: "minimal frontmatter",
			content: `---
name: my-skill
description: A short description.
---
# Instructions

Do something.
`,
			wantName: "my-skill",
			wantDesc: "A short description.",
			wantBody: "# Instructions\n\nDo something.\n",
		},
		{
			name: "full frontmatter with metadata",
			content: `---
name: go-project-scaffold
description: Scaffolds a new Go project with idiomatic structure.
metadata:
  author: liwaisi
  version: "1.0.0"
  domain: golang
---
# Scaffold

Step 1: create directories.
`,
			wantName: "go-project-scaffold",
			wantDesc: "Scaffolds a new Go project with idiomatic structure.",
			wantBody: "# Scaffold\n\nStep 1: create directories.\n",
			wantMeta: map[string]string{
				"author":  "liwaisi",
				"version": "1.0.0",
				"domain":  "golang",
			},
		},
		{
			name: "empty body",
			content: `---
name: empty-body
description: Skill with no body.
---
`,
			wantName: "empty-body",
			wantDesc: "Skill with no body.",
			wantBody: "",
		},
		{
			name:     "windows line endings",
			content:  "---\r\nname: win-skill\r\ndescription: Has CRLF line endings.\r\n---\r\n# Body\r\n",
			wantName: "win-skill",
			wantDesc: "Has CRLF line endings.",
			wantBody: "# Body\n",
		},
		{
			name: "three-character name",
			content: `---
name: abc
description: Minimum length name.
---
`,
			wantName: "abc",
			wantDesc: "Minimum length name.",
		},
		{
			name: "multiline description",
			content: `---
name: multi-desc
description: |
  This is a multiline description
  that spans several lines.
---
Body here.
`,
			wantName: "multi-desc",
			wantDesc: "This is a multiline description\nthat spans several lines.",
			wantBody: "Body here.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fsys := fstest.MapFS{
				"SKILL.md": &fstest.MapFile{Data: []byte(tt.content)},
			}
			skill, err := ParseFS(fsys, "SKILL.md")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", skill.Description, tt.wantDesc)
			}
			if skill.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", skill.Body, tt.wantBody)
			}
			if tt.wantMeta != nil {
				for k, v := range tt.wantMeta {
					if got := skill.Meta[k]; got != v {
						t.Errorf("Tags[%q] = %q, want %q", k, got, v)
					}
				}
			}
		})
	}
}

func TestParseFS_InvalidSkill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "no frontmatter delimiter",
			content: "name: bad\ndescription: missing delimiters\n",
			wantErr: "must start with '---'",
		},
		{
			name:    "missing closing delimiter",
			content: "---\nname: bad\ndescription: no close\n",
			wantErr: "missing closing '---'",
		},
		{
			name:    "invalid yaml",
			content: "---\nname: [invalid\n---\n",
			wantErr: "parsing YAML",
		},
		{
			name:    "missing name",
			content: "---\ndescription: No name field.\n---\n",
			wantErr: "skill name is required",
		},
		{
			name:    "name too short",
			content: "---\nname: ab\ndescription: Too short.\n---\n",
			wantErr: "must be 3-50 characters",
		},
		{
			name:    "name too long",
			content: "---\nname: abcdefghij-abcdefghij-abcdefghij-abcdefghij-extrass\ndescription: Too long.\n---\n",
			wantErr: "must be 3-50 characters",
		},
		{
			name:    "name with uppercase",
			content: "---\nname: MySkill\ndescription: Bad chars.\n---\n",
			wantErr: "must match [a-z0-9-]",
		},
		{
			name:    "name with underscore",
			content: "---\nname: my_skill\ndescription: Bad chars.\n---\n",
			wantErr: "must match [a-z0-9-]",
		},
		{
			name:    "name starts with hyphen",
			content: "---\nname: -bad-name\ndescription: Starts with hyphen.\n---\n",
			wantErr: "must match [a-z0-9-]",
		},
		{
			name:    "name ends with hyphen",
			content: "---\nname: bad-name-\ndescription: Ends with hyphen.\n---\n",
			wantErr: "must match [a-z0-9-]",
		},
		{
			name:    "reserved name activate_skill",
			content: "---\nname: activate-skill\ndescription: Not actually reserved.\n---\n",
			wantErr: "", // activate-skill != activate_skill, so this should pass
		},
		{
			name:    "missing description",
			content: "---\nname: no-desc\n---\n",
			wantErr: "description is required",
		},
		{
			name:    "description with angle brackets",
			content: "---\nname: bad-desc\ndescription: Has <script> tags.\n---\n",
			wantErr: "must not contain '<' or '>'",
		},
		{
			name:    "description too long",
			content: "---\nname: long-desc\ndescription: " + strings.Repeat("a", 501) + "\n---\n",
			wantErr: "at most 500 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fsys := fstest.MapFS{
				"SKILL.md": &fstest.MapFile{Data: []byte(tt.content)},
			}
			skill, err := ParseFS(fsys, "SKILL.md")

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if skill == nil {
					t.Fatal("expected non-nil skill")
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want substring %q", got, tt.wantErr)
			}
		})
	}
}

func TestParseFS_FileNotFound(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	_, err := ParseFS(fsys, "SKILL.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !isNotExist(err) {
		t.Errorf("expected fs.ErrNotExist, got: %v", err)
	}
}

func isNotExist(err error) bool {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err == fs.ErrNotExist
	}
	return false
}
