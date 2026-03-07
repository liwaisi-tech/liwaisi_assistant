package shellexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/filemanagement"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type executeCommandArgs struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
}

type executeCommandResult struct {
	Command          string `json:"command"`
	ExitCode         int    `json:"exit_code"`
	Stdout           string `json:"stdout"`
	Stderr           string `json:"stderr"`
	WorkingDirectory string `json:"working_directory"`
	TimedOut         bool   `json:"timed_out"`
	DurationMs       int64  `json:"duration_ms"`
}

func registerExecuteCommand(registry *tool.Registry, sandbox *filemanagement.Sandbox, policy *CommandPolicy, executor *CommandExecutor, redactor OutputRedactor) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "execute_command",
			Description: "Execute an OS command within the agent workspace. The command runs in a sandboxed " +
				"environment with the working directory restricted to the workspace. Use this to run builds " +
				"(go build, make, npm install), execute scripts (python3 script.py), query APIs (curl), " +
				"run tests (go test ./...), operate developer toolchains (git, docker, kubectl), or inspect " +
				"the environment (which, uname, env). Commands run via the system shell (sh -c) so pipes (|), " +
				"redirects (>), environment variables (KEY=val cmd), and chaining (&&) are supported. Commands " +
				"are subject to a configurable timeout (default 30s, max 300s) and a blocklist of dangerous " +
				"operations (e.g., rm -rf /, sudo, mkfs). Output (stdout + stderr) is capped at 1MB to prevent " +
				"memory exhaustion. Only non-interactive commands are supported.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "The shell command to execute. Supports full shell syntax: pipes (|), redirects (>), environment variables (KEY=val cmd), chaining (&&), and globbing (*). Example: 'go test ./... 2>&1' or 'curl -s https://api.example.com/status | jq .'"
					},
					"working_directory": {
						"type": "string",
						"description": "Relative path within the workspace to use as the working directory for the command. Defaults to the workspace root if not specified. Example: 'src/backend' or 'packages/core'"
					},
					"timeout_seconds": {
						"type": "integer",
						"description": "Maximum number of seconds the command may run before being killed (1-300). Default: 30. Set higher for long-running commands like test suites (120) or builds (60).",
						"minimum": 1,
						"maximum": 300,
						"default": 30
					}
				},
				"required": ["command"]
			}`),
		},
	}

	registry.Register(def, executeCommandHandler(sandbox, policy, executor, redactor))
}

func executeCommandHandler(sandbox *filemanagement.Sandbox, policy *CommandPolicy, executor *CommandExecutor, redactor OutputRedactor) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args executeCommandArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing execute_command arguments: %w", err)
		}

		if err := policy.Validate(args.Command); err != nil {
			return "", err
		}

		workDir := args.WorkingDirectory
		if workDir == "" {
			workDir = "."
		}

		absWorkDir, err := sandbox.Resolve(workDir)
		if err != nil {
			return "", err
		}

		timeout := time.Duration(args.TimeoutSeconds) * time.Second
		if args.TimeoutSeconds <= 0 {
			timeout = executor.DefaultTimeout()
		}
		maxTimeout := executor.MaxTimeout()
		if timeout > maxTimeout {
			timeout = maxTimeout
		}

		result, err := executor.Execute(ctx, args.Command, absWorkDir, timeout)
		if err != nil {
			return "", fmt.Errorf("executing command: %w", err)
		}

		stdout := result.Stdout
		stderr := result.Stderr
		if redactor != nil {
			stdout = redactor.Redact(stdout)
			stderr = redactor.Redact(stderr)
		}

		res := executeCommandResult{
			Command:          args.Command,
			ExitCode:         result.ExitCode,
			Stdout:           stdout,
			Stderr:           stderr,
			WorkingDirectory: workDir,
			TimedOut:         result.TimedOut,
			DurationMs:       result.Duration.Milliseconds(),
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
