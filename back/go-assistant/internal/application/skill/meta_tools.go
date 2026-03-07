package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RegisterFindSkills registers the find_skills meta-tool that unifies skill
// discovery, activation, and resource reading into a single tool definition.
// It replaces the previous list_skills, activate_skill, and
// read_skill_resource tools.
func RegisterFindSkills(registry tool.Registrar, skills *Registry) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "find_skills",
			Description: "Discover and activate skills on demand. " +
				"Call with action='list' to see all available skills with descriptions. " +
				"Call with action='activate' and name='<skill-name>' to load a skill's full instructions. " +
				"Skills teach you HOW to accomplish multi-step goals — they are different from tools. " +
				"After activation, follow the returned instructions carefully. " +
				"Use action='read_resource' with name and path to read additional skill files.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["list", "activate", "read_resource"],
						"description": "Action: 'list' shows skills, 'activate' loads instructions, 'read_resource' reads a skill file"
					},
					"name": {
						"type": "string",
						"description": "Skill name (required for 'activate' and 'read_resource')"
					},
					"path": {
						"type": "string",
						"description": "Resource path relative to skill directory (required for 'read_resource')"
					}
				},
				"required": ["action"]
			}`),
		},
	}

	registry.Register(def, findSkillsHandler(skills))
}

type findSkillsArgs struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

type findSkillsListEntry struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Meta        map[string]string `json:"metadata,omitempty"`
}

type findSkillsListResult struct {
	Skills []findSkillsListEntry `json:"skills"`
	Count  int                   `json:"count"`
}

func findSkillsHandler(skills *Registry) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args findSkillsArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing find_skills arguments: %w", err)
		}

		switch args.Action {
		case "list":
			return handleFindSkillsList(skills)
		case "activate":
			return handleFindSkillsActivate(skills, args.Name)
		case "read_resource":
			return handleFindSkillsReadResource(skills, args.Name, args.Path)
		default:
			return "", fmt.Errorf("find_skills: unknown action %q, expected 'list', 'activate', or 'read_resource'", args.Action)
		}
	}
}

func handleFindSkillsList(skills *Registry) (string, error) {
	if err := skills.Refresh(); err != nil {
		return "", fmt.Errorf("refreshing skills: %w", err)
	}

	metas := skills.List()
	entries := make([]findSkillsListEntry, len(metas))
	for i, m := range metas {
		entries[i] = findSkillsListEntry(m)
	}

	result, err := json.Marshal(findSkillsListResult{Skills: entries, Count: len(entries)})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}

func handleFindSkillsActivate(skills *Registry, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("find_skills: 'name' parameter is required when action='activate'")
	}

	body, err := skills.Activate(name)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(map[string]string{
		"skill":        name,
		"status":       "activated",
		"instructions": body,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}

func handleFindSkillsReadResource(skills *Registry, name, path string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("find_skills: 'name' parameter is required when action='read_resource'")
	}
	if path == "" {
		return "", fmt.Errorf("find_skills: 'path' parameter is required when action='read_resource'")
	}

	content, err := skills.ReadResource(name, path)
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(map[string]string{
		"skill":   name,
		"path":    path,
		"content": content,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}
