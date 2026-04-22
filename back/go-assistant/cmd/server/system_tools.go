package main

// system_tools.go wires the three system-level tool executors (bash_exec,
// file_read, file_write) into the tool registry (GAP-11 REQ-001–REQ-006) and
// provides buildHostContextPreamble (REQ-012) for LLM system prompt injection.
// Lives in cmd/server/ so it can import both cpn and cpn/persist without
// circular-import issues.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// resolveUserPath expands ~, $HOME, and ${HOME} in a user-supplied path and
// requires the result to be absolute. The LLM routinely emits literal "$HOME"
// or "~/foo" expecting shell-style expansion; file_read/file_write do not
// shell out, so we expand here before handing the path to the adapter.
func resolveUserPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("'path' is required")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		switch {
		case p == "~":
			p = home
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		}
		p = strings.ReplaceAll(p, "${HOME}", home)
		p = strings.ReplaceAll(p, "$HOME", home)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path %q must be absolute (after ~ / $HOME expansion)", p)
	}
	return filepath.Clean(p), nil
}

// JSON schema constants passed to ToolSchema.Parameters so buildToolSchema
// includes them in the LLM tool description (REQ-005).
const (
	bashExecParamsSchema = `{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "REQUIRED. The executable to run. Always invoke a concrete binary as command=<name> with its flags in args=[...]. Examples: 'ls', 'uname', 'git', 'python3', 'go'. For multi-command pipelines or shell builtins use command='bash' with args=['-c','<pipeline>']. The sandbox ships GNU bash (4.x+), coreutils, git, make, jq, curl, and the Go toolchain. MUST NOT be empty."
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Arguments passed to the command as a list. Example: args=['-la', '/tmp']. Do not inline args into the command string. Issue one bash_exec per command. DO NOT chain commands with shell operators (&&, ||, ;, |, >, <, command substitution) — the host gate treats each compound chain as a novel command and will request HITL approval every time, even after the user clicked 'remember'. For a multi-step plan, emit sequential tool_calls; the runtime batches their approvals."
    },
    "cwd": {
      "type": "string",
      "description": "Working directory for the command. Defaults to the user home directory."
    },
    "timeout_seconds": {
      "type": "integer",
      "minimum": 1,
      "maximum": 300,
      "description": "Maximum execution time in seconds. Defaults to 30."
    }
  }
}`

	fileReadParamsSchema = `{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute path to the file to read. Must resolve inside the brae user's $HOME. You may use ~, ~/, $HOME, or ${HOME} — they are expanded to the home directory before resolution."
    }
  }
}`

	fileWriteParamsSchema = `{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute path to write. Must resolve inside the brae user's $HOME. You may use ~, ~/, $HOME, or ${HOME} — they are expanded to the home directory before resolution. Parent directories are NOT created automatically; run mkdir -p first via bash_exec if needed."
    },
    "content": {
      "type": "string",
      "description": "UTF-8 content to write to the file."
    },
    "mode": {
      "type": "integer",
      "description": "Unix file permission mode as decimal integer (e.g., 420 = 0644). Defaults to 420."
    }
  }
}`

	registerToolParamsSchema = `{
  "type": "object",
  "required": ["name", "description", "binary_path"],
  "properties": {
    "name": {
      "type": "string",
      "description": "Short identifier (e.g., 'check-health'). Must match [A-Za-z][A-Za-z0-9_-]{0,63}."
    },
    "description": {
      "type": "string",
      "description": "One-line human-readable summary of what the tool does."
    },
    "binary_path": {
      "type": "string",
      "description": "Absolute path to the executable script or binary on the brae user's host. Must resolve inside $HOME. You may use ~, ~/, $HOME, or ${HOME}."
    },
    "toolbox": {
      "type": "string",
      "description": "Optional toolbox grouping. One of: system, developer, web, image, pdf, data, general. Defaults to 'general'.",
      "enum": ["system", "developer", "web", "image", "pdf", "data", "general"]
    },
    "hashtags": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Optional 2–5 short capability tags (e.g. ['system','health','read']). Unknown tags are dropped silently."
    },
    "help_text": {
      "type": "string",
      "description": "Optional usage hint — typically the first line of the tool's --help output."
    },
    "version": {
      "type": "string",
      "description": "Optional semver string. Defaults to '0.1.0'."
    }
  }
}`
)

// buildHostContextPreamble formats the canonical "## Environment awareness"
// block (REQ-007 / PAT-003) from the CPN's p-host-capabilities place for
// injection into every non-awakening LLM turn's system prompt. Returns "" when
// no snapshot is present or the type assertion fails.
//
// Lives here (cmd/server/) rather than cpn/ to avoid cpn importing cpn/persist
// (circular dependency — persist already imports cpn).
func buildHostContextPreamble(c *cpn.CPN) string {
	snap, ok := cpn.PeekHostSnapshot(c)
	if !ok || snap == nil {
		return ""
	}
	s, ok := snap.(persist.HostCapabilitySnapshot)
	if !ok {
		return ""
	}
	return awakens.EnvironmentAwarenessBlock(s)
}

// registerSystemTools registers bash_exec, file_read, and file_write into
// toolReg using the supplied HostAdapter and HostGate. Must be called BEFORE
// toolReg.Seal() so the system namespace accepts the entries (REQ-001/REQ-006).
func registerSystemTools(toolReg *tools.Registry, adapter cpn.HostAdapter, gate cpn.HostGate) error {
	entries := []struct {
		schema *tools.ToolSchema
		exec   tools.ToolExecutor
	}{
		{
			schema: &tools.ToolSchema{
				Name:         "bash_exec",
				Namespace:    "system",
				Description:  "Execute a shell command on the host OS. Returns stdout, stderr, and exit code.",
				InputColor:   cpn.ColorJSON,
				OutputColor:  cpn.ColorShellResult,
				Parameters:   json.RawMessage(bashExecParamsSchema),
				RequiresHITL: true,
				Version:      "1.0",
			},
			exec: makeBashExecExecutor(adapter, gate),
		},
		{
			schema: &tools.ToolSchema{
				Name:        "file_read",
				Namespace:   "system",
				Description: "Read a file from the host filesystem. Path must resolve inside the brae user's $HOME.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorArtifact,
				Parameters:  json.RawMessage(fileReadParamsSchema),
				Version:     "1.0",
			},
			exec: makeFileReadExecutor(adapter),
		},
		{
			schema: &tools.ToolSchema{
				Name:         "file_write",
				Namespace:    "system",
				Description:  "Write content to a file on the host filesystem. Path must resolve inside the brae user's $HOME.",
				InputColor:   cpn.ColorJSON,
				OutputColor:  cpn.ColorArtifact,
				Parameters:   json.RawMessage(fileWriteParamsSchema),
				RequiresHITL: true,
				Version:      "1.0",
			},
			exec: makeFileWriteExecutor(adapter),
		},
		{
			schema: &tools.ToolSchema{
				Name:         "register_tool",
				Namespace:    "system",
				Description:  "Register a new tool in brae's catalog so future sessions can discover it. Use this after you have created a working executable script on disk (e.g., a health-check written to ~/.local/bin/). The registration persists name, description, binary_path, toolbox, and hashtags in the catalog; it does NOT copy or compile anything. Requires human approval.",
				InputColor:   cpn.ColorJSON,
				OutputColor:  cpn.ColorArtifact,
				Parameters:   json.RawMessage(registerToolParamsSchema),
				RequiresHITL: true,
				Version:      "1.0",
			},
			exec: makeRegisterToolExecutor(toolReg, adapter),
		},
	}

	for _, e := range entries {
		if err := toolReg.Register(e.schema, e.exec); err != nil {
			return fmt.Errorf("system_tools: register %s: %w", e.schema.Name, err)
		}
	}
	return nil
}

// makeBashExecExecutor returns a ToolExecutor that parses bash_exec arguments
// from the LLM tool call, enforces the gate, and runs the command via the
// HostAdapter. Non-zero exits are returned as a ShellResultPayload (not
// errors) so the LLM can inspect stdout/stderr and retry (REQ-017).
func makeBashExecExecutor(adapter cpn.HostAdapter, gate cpn.HostGate) tools.ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		payload, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("bash_exec: expected string payload, got %T", in.Payload)
		}
		var args struct {
			Command        string   `json:"command"`
			Args           []string `json:"args"`
			Cwd            string   `json:"cwd"`
			TimeoutSeconds int      `json:"timeout_seconds"`
		}
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			return cpn.Token{}, fmt.Errorf("bash_exec: parse arguments: %w", err)
		}
		if args.Command == "" {
			return cpn.Token{}, fmt.Errorf("bash_exec: 'command' is required")
		}
		if args.TimeoutSeconds <= 0 {
			args.TimeoutSeconds = 30
		}

		// Gate enforcement is owned end-to-end by fire_llm.go's pre-check,
		// which calls HostRuntime.Gate.Check + HITLHandler.HandleHITL before
		// dispatching to this executor. Re-checking here is NOT defense in
		// depth: it re-evaluates the policy from scratch, so approve-once
		// resolutions (and first-run approvals of RiskUnknown commands) get
		// re-denied with the same ErrRequiresHITL the user just cleared —
		// the yellow "host gate requires HITL" error documented in
		// spec-process-bugfix-hitl-remember-and-bash-runtime.md. Trust the
		// single gate.
		_ = gate

		req := cpn.ExecRequest{
			Command:      args.Command,
			Args:         args.Args,
			Cwd:          args.Cwd,
			Timeout:      time.Duration(args.TimeoutSeconds) * time.Second,
			AllowNonZero: true, // non-zero exits become ShellResultPayload, not errors
		}
		result, err := adapter.Exec(ctx, req)
		if err != nil {
			// Gate denial, command not found, timeout — return as error so
			// fireLLM sends a tool error message to the LLM.
			return cpn.Token{}, err
		}

		return cpn.Token{
			Color: cpn.ColorShellResult,
			Payload: cpn.ShellResultPayload{
				ExitCode:   result.ExitCode,
				Stdout:     string(result.Stdout),
				Stderr:     string(result.Stderr),
				DurationMs: result.DurationMs,
				Truncated:  result.Truncated,
			},
		}, nil
	}
}

// makeFileReadExecutor returns a ToolExecutor that reads a file from the host.
// Path denial errors are returned as descriptive strings (REQ-018).
func makeFileReadExecutor(adapter cpn.HostAdapter) tools.ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		payload, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("file_read: expected string payload, got %T", in.Payload)
		}
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			return cpn.Token{}, fmt.Errorf("file_read: parse arguments: %w", err)
		}
		resolved, err := resolveUserPath(args.Path)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("file_read: %w", err)
		}

		data, err := adapter.ReadFile(ctx, resolved)
		if err != nil {
			if isPathDenied(err) {
				return cpn.Token{}, fmt.Errorf("error: path %s is outside the allowed root (brae user's $HOME)", resolved)
			}
			return cpn.Token{}, fmt.Errorf("file_read: %w", err)
		}

		return cpn.Token{
			Color:   cpn.ColorArtifact,
			Payload: string(data),
		}, nil
	}
}

// makeFileWriteExecutor returns a ToolExecutor that writes content to a file.
// mode defaults to 0644 (decimal 420) when absent or zero (REQ-004).
func makeFileWriteExecutor(adapter cpn.HostAdapter) tools.ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		payload, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("file_write: expected string payload, got %T", in.Payload)
		}
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Mode    int    `json:"mode"`
		}
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			return cpn.Token{}, fmt.Errorf("file_write: parse arguments: %w", err)
		}
		resolved, err := resolveUserPath(args.Path)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("file_write: %w", err)
		}
		if args.Mode == 0 {
			args.Mode = 0o644
		}

		if err := adapter.WriteFile(ctx, resolved, []byte(args.Content), fs.FileMode(args.Mode)); err != nil { //nolint:gosec // G115: Mode is a POSIX permission bitmask (<=0o777) that fits in uint32
			if isPathDenied(err) {
				return cpn.Token{}, fmt.Errorf("error: path %s is outside the allowed root (brae user's $HOME)", resolved)
			}
			return cpn.Token{}, fmt.Errorf("file_write: %w", err)
		}

		type writeResult struct {
			Written bool   `json:"written"`
			Path    string `json:"path"`
		}
		return cpn.Token{
			Color:   cpn.ColorArtifact,
			Payload: writeResult{Written: true, Path: resolved},
		}, nil
	}
}

// isPathDenied reports whether err is a HostError with code path_denied.
func isPathDenied(err error) bool {
	if err == nil {
		return false
	}
	var he *cpn.HostError
	if errors.As(err, &he) {
		return he.Code == cpn.HostErrCodePathDenied
	}
	return false
}

// registerToolNameRe mirrors awakens.toolNameRe — conservative identifier
// set that cannot embed shell metachars or namespace separators.
var registerToolNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// makeRegisterToolExecutor returns a ToolExecutor for system/register_tool.
// After HITL approval it persists a ToolManifest in the registry under the
// "user" namespace so catalogue reads (GET /api/v1/tools) surface it.
func makeRegisterToolExecutor(toolReg *tools.Registry, adapter cpn.HostAdapter) tools.ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		payload, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("register_tool: expected string payload, got %T", in.Payload)
		}
		var args struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			BinaryPath  string   `json:"binary_path"`
			Toolbox     string   `json:"toolbox"`
			Hashtags    []string `json:"hashtags"`
			HelpText    string   `json:"help_text"`
			Version     string   `json:"version"`
		}
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			return cpn.Token{}, fmt.Errorf("register_tool: parse arguments: %w", err)
		}

		name := strings.TrimSpace(args.Name)
		if !registerToolNameRe.MatchString(name) {
			return cpn.Token{}, fmt.Errorf("register_tool: 'name' must match [A-Za-z][A-Za-z0-9_-]{0,63} (got %q)", name)
		}
		if strings.TrimSpace(args.Description) == "" {
			return cpn.Token{}, fmt.Errorf("register_tool: 'description' is required")
		}
		resolvedBin, err := resolveUserPath(args.BinaryPath)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("register_tool: %w", err)
		}
		if _, err := adapter.Stat(ctx, resolvedBin); err != nil {
			if isPathDenied(err) {
				return cpn.Token{}, fmt.Errorf("error: binary_path %s is outside the allowed root (brae user's $HOME)", resolvedBin)
			}
			return cpn.Token{}, fmt.Errorf("register_tool: binary_path %s: %w", resolvedBin, err)
		}

		version := strings.TrimSpace(args.Version)
		if version == "" {
			version = "0.1.0"
		}
		toolbox := strings.TrimSpace(strings.ToLower(args.Toolbox))
		if toolbox == "" {
			toolbox = "general"
		}
		helpText := args.HelpText
		if strings.TrimSpace(helpText) == "" {
			helpText = args.Description
		}

		manifest := cpn.ToolManifest{
			Namespace:    "user",
			Name:         name,
			Version:      version,
			Origin:       "user-authored",
			HelpText:     helpText,
			BinaryPath:   resolvedBin,
			Toolbox:      toolbox,
			Hashtags:     args.Hashtags,
			RegisteredBy: "system/register_tool",
			Provenance: cpn.ProvenanceSnapshot{
				PromptDigest: "system/register_tool@v1",
			},
		}
		result, err := toolReg.RegisterManifest(ctx, manifest)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("register_tool: %w", err)
		}

		type registerResult struct {
			QualifiedName string    `json:"qualified_name"`
			ID            string    `json:"id"`
			RegisteredAt  time.Time `json:"registered_at"`
			Toolbox       string    `json:"toolbox"`
			BinaryPath    string    `json:"binary_path"`
		}
		return cpn.Token{
			Color: cpn.ColorArtifact,
			Payload: registerResult{
				QualifiedName: result.QualifiedName,
				ID:            result.ID,
				RegisteredAt:  result.RegisteredAt,
				Toolbox:       toolbox,
				BinaryPath:    resolvedBin,
			},
		}, nil
	}
}
