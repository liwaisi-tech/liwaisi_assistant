package shellexec

import (
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/filemanagement"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// OutputRedactor redacts sensitive values from command output before returning
// it to the agent. Implementations must be safe for concurrent use.
type OutputRedactor interface {
	Redact(output string) string
}

// ToolSet groups shell execution tools for the agent workspace.
type ToolSet struct {
	sandbox  *filemanagement.Sandbox
	policy   *CommandPolicy
	executor *CommandExecutor
	redactor OutputRedactor
}

// ToolSetOption configures a ToolSet.
type ToolSetOption func(*ToolSet)

// WithPolicy overrides the default command security policy.
func WithPolicy(policy *CommandPolicy) ToolSetOption {
	return func(ts *ToolSet) {
		if policy != nil {
			ts.policy = policy
		}
	}
}

// WithExecutorConfig overrides the default executor configuration.
func WithExecutorConfig(cfg ExecutorConfig) ToolSetOption {
	return func(ts *ToolSet) {
		ts.executor = NewCommandExecutor(cfg)
	}
}

// WithRedactor sets the output redactor that masks secret values in command
// output before returning it to the agent.
func WithRedactor(r OutputRedactor) ToolSetOption {
	return func(ts *ToolSet) {
		if r != nil {
			ts.redactor = r
		}
	}
}

// NewToolSet creates a shell execution ToolSet sandboxed to workspaceRoot.
// Returns an error if workspaceRoot is not an absolute path.
func NewToolSet(workspaceRoot string, opts ...ToolSetOption) (*ToolSet, error) {
	sb, err := filemanagement.NewSandbox(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("creating shell execution sandbox: %w", err)
	}

	ts := &ToolSet{
		sandbox:  sb,
		policy:   NewDefaultPolicy(),
		executor: NewCommandExecutor(ExecutorConfig{}),
	}
	for _, opt := range opts {
		opt(ts)
	}
	return ts, nil
}

// CatalogEntry returns the catalog metadata and factory for the shell
// execution tool category, enabling JIT registration into an ActiveRegistry.
func (ts *ToolSet) CatalogEntry() *tool.CatalogEntry {
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:    valueobject.ToolCategoryShellExec,
			Description: "OS command execution within the workspace: run builds, tests, git, scripts with full shell features (pipes, redirects, env vars).",
			Tools: []valueobject.ToolSummary{
				{Name: "execute_command", Description: "Run a shell command with configurable timeout"},
			},
			EstTokenCost: 300,
		},
		Factory: func(r *tool.Registry) { ts.Register(r) },
	}
}

// Register adds all shell execution tools to the registry.
func (ts *ToolSet) Register(registry *tool.Registry) {
	registerExecuteCommand(registry, ts.sandbox, ts.policy, ts.executor, ts.redactor)
}

// Compile-time check that ToolSet implements tool.Set.
var _ tool.Set = (*ToolSet)(nil)
