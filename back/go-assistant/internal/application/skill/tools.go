package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RegisterSkillTools adds activate_skill, read_skill_resource, and list_skills
// tools to the tool registry, wired to the given skill registry.
func RegisterSkillTools(registry *tool.Registry, skills *Registry) {
	registerActivateSkill(registry, skills)
	registerReadSkillResource(registry, skills)
	registerListSkills(registry, skills)
}

// --- activate_skill ---

type activateSkillArgs struct {
	Name string `json:"name"`
}

func registerActivateSkill(registry *tool.Registry, skills *Registry) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "activate_skill",
			Description: "Load and activate a skill by name, returning its full instructions. " +
				"Use this when a user's request matches a skill's description from the available_skills list. " +
				"Skills provide step-by-step workflows that guide you through multi-step tasks. " +
				"After activation, follow the returned instructions carefully. " +
				"This is different from regular tools — skills teach you HOW to accomplish goals, " +
				"while tools perform discrete actions.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "The skill name exactly as shown in the available_skills list. Example: 'skill-creator'"
					}
				},
				"required": ["name"]
			}`),
		},
	}
	registry.Register(def, activateSkillHandler(skills))
}

func activateSkillHandler(skills *Registry) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args activateSkillArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing activate_skill arguments: %w", err)
		}

		body, err := skills.Activate(args.Name)
		if err != nil {
			return "", err
		}

		result, err := json.Marshal(map[string]string{
			"skill":        args.Name,
			"status":       "activated",
			"instructions": body,
		})
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(result), nil
	}
}

// --- read_skill_resource ---

type readSkillResourceArgs struct {
	SkillName string `json:"skill_name"`
	Path      string `json:"path"`
}

func registerReadSkillResource(registry *tool.Registry, skills *Registry) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "read_skill_resource",
			Description: "Read a resource file from an activated skill's directory. " +
				"Use this when skill instructions reference additional files in references/, " +
				"assets/, or scripts/ directories. Only call this tool when the skill's " +
				"instructions explicitly tell you to load a resource. " +
				"The path is relative to the skill's root directory.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"skill_name": {
						"type": "string",
						"description": "The name of the skill whose resource to read. Example: 'go-project-scaffold'"
					},
					"path": {
						"type": "string",
						"description": "Relative path to the resource within the skill directory. Example: 'references/conventions.md'"
					}
				},
				"required": ["skill_name", "path"]
			}`),
		},
	}
	registry.Register(def, readSkillResourceHandler(skills))
}

func readSkillResourceHandler(skills *Registry) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args readSkillResourceArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing read_skill_resource arguments: %w", err)
		}

		content, err := skills.ReadResource(args.SkillName, args.Path)
		if err != nil {
			return "", err
		}

		result, err := json.Marshal(map[string]string{
			"skill":   args.SkillName,
			"path":    args.Path,
			"content": content,
		})
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(result), nil
	}
}

// --- list_skills ---

func registerListSkills(registry *tool.Registry, skills *Registry) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "list_skills",
			Description: "List all available skills with their metadata. " +
				"This refreshes the skill registry to discover newly created skills. " +
				"Use this after creating a new skill to verify it was registered correctly, " +
				"or when you want to see all available skills.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	}
	registry.Register(def, listSkillsHandler(skills))
}

type listSkillsResult struct {
	Skills []listSkillEntry `json:"skills"`
	Count  int              `json:"count"`
}

type listSkillEntry struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Meta        map[string]string `json:"metadata,omitempty"`
}

func listSkillsHandler(skills *Registry) tool.Handler {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		if err := skills.Refresh(); err != nil {
			return "", fmt.Errorf("refreshing skills: %w", err)
		}

		metas := skills.List()
		entries := make([]listSkillEntry, len(metas))
		for i, m := range metas {
			entries[i] = listSkillEntry(m)
		}

		result, err := json.Marshal(listSkillsResult{
			Skills: entries,
			Count:  len(entries),
		})
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(result), nil
	}
}
