// Package env provides agent tools for environment variable introspection.
package env

import (
	"context"
	"encoding/json"
	"fmt"

	appenv "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ToolSet groups environment introspection tools for batch registration.
type ToolSet struct {
	store *appenv.Store
}

// NewToolSet creates an env ToolSet backed by the given store.
func NewToolSet(store *appenv.Store) *ToolSet {
	return &ToolSet{store: store}
}

// CatalogEntry returns the catalog metadata and factory for the env tool
// category, enabling JIT registration into an ActiveRegistry.
func (ts *ToolSet) CatalogEntry() *tool.CatalogEntry {
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:    valueobject.ToolCategoryEnv,
			Description: "Environment variable management: check availability and reload credentials from the env file without restarting.",
			Tools: []valueobject.ToolSummary{
				{Name: "check_env_variable", Description: "Check whether an environment variable is set (status only, never the value)"},
				{Name: "reload_env", Description: "Reload environment variables from env file into the process"},
			},
			EstTokenCost: 400,
		},
		Factory: func(r *tool.Registry) { ts.Register(r) },
	}
}

// Register adds all env tools to the registry.
func (ts *ToolSet) Register(registry *tool.Registry) {
	registerCheckEnvVariable(registry, ts.store)
	registerReloadEnv(registry, ts.store)
}

// Compile-time check that ToolSet implements tool.Set.
var _ tool.Set = (*ToolSet)(nil)

// --- check_env_variable ---

type checkEnvArgs struct {
	Name string `json:"name"`
}

type checkEnvResult struct {
	Variable string `json:"variable"`
	Status   string `json:"status"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

func registerCheckEnvVariable(registry *tool.Registry, store *appenv.Store) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "check_env_variable",
			Description: "Check whether an environment variable is currently set in the agent's process " +
				"environment. Returns ONLY the variable's status (set or not set) — NEVER its value. " +
				"Use this tool to verify that required credentials, API keys, or configuration variables " +
				"are available before attempting operations that depend on them. For example, check if " +
				"CLICKUP_API_KEY is set before running a curl command to the ClickUp API. If a variable " +
				"is not set, advise the user to run 'liwaisi env set <KEY>' to configure it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "The environment variable name to check (e.g., 'OPENROUTER_API_KEY', 'CLICKUP_API_KEY', 'GITHUB_TOKEN'). Must be a valid environment variable name (uppercase letters, digits, underscores)."
					}
				},
				"required": ["name"]
			}`),
		},
	}

	registry.Register(def, checkEnvHandler(store))
}

func checkEnvHandler(store *appenv.Store) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args checkEnvArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing check_env_variable arguments: %w", err)
		}

		if args.Name == "" {
			return "", fmt.Errorf("check_env_variable: 'name' parameter is required")
		}

		found, source, err := store.Has(ctx, args.Name)
		if err != nil {
			return "", fmt.Errorf("checking env variable: %w", err)
		}

		res := checkEnvResult{Variable: args.Name}
		if found {
			res.Status = "set"
			res.Source = source
			res.Message = "Variable is available in the process environment."
		} else {
			res.Status = "not_set"
			res.Message = fmt.Sprintf(
				"Variable is not set. The user can configure it with: liwaisi env set %s", args.Name,
			)
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

// --- reload_env ---

type reloadEnvResult struct {
	Status          string `json:"status"`
	VariablesLoaded int    `json:"variables_loaded"`
	Message         string `json:"message"`
}

func registerReloadEnv(registry *tool.Registry, store *appenv.Store) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "reload_env",
			Description: "Reload environment variables from the agent's env file (~/.liwaisi/config/env.yaml) " +
				"into the current process. Use this when the user has added, modified, or removed credentials " +
				"in the env file and wants them to take effect without restarting the agent. Returns the " +
				"count of variables loaded — NEVER their values.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"required": []
			}`),
		},
	}

	registry.Register(def, reloadEnvHandler(store))
}

func reloadEnvHandler(store *appenv.Store) tool.Handler {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		n, err := store.Load(ctx)
		if err != nil {
			return "", fmt.Errorf("reloading env: %w", err)
		}

		res := reloadEnvResult{
			Status:          "ok",
			VariablesLoaded: n,
			Message:         fmt.Sprintf("Environment reloaded successfully. %d variables are now available.", n),
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
