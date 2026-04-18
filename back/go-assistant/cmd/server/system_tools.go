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
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// JSON schema constants passed to ToolSchema.Parameters so buildToolSchema
// includes them in the LLM tool description (REQ-005).
const (
	bashExecParamsSchema = `{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "The executable to run (e.g., 'ls', 'git', 'python3')."
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Arguments passed to the command. Do not inline args into command string."
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
      "description": "Absolute path to the file to read. Must be inside $HOME/.local/brae/ or user home."
    }
  }
}`

	fileWriteParamsSchema = `{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute path to write. Must be inside $HOME/.local/brae/."
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
)

// buildHostContextPreamble formats a host capability preamble from the CPN's
// p-host-capabilities place for injection into LLM system prompts (REQ-012).
// Returns "" when no snapshot is present or the type assertion fails.
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
	var sb strings.Builder
	sb.WriteString("SYSTEM CONTEXT — HOST ENVIRONMENT (read-only facts, do not expose raw to user unless asked):\n")
	fmt.Fprintf(&sb, "- OS: %s %s (%s)\n", s.Kernel.OS, s.Kernel.Kernel, s.Kernel.Arch)
	fmt.Fprintf(&sb, "- Hostname: %s\n", s.Identity.Hostname)
	fmt.Fprintf(&sb, "- User: %s (uid=%d home=%s)\n", s.Identity.User, s.Identity.UID, s.Identity.Home)
	fmt.Fprintf(&sb, "- CPU: %d cores | RAM: %d MB\n", s.Kernel.CPUCount, s.Kernel.MemMB)
	var presentBins []string
	for _, b := range s.Binaries {
		if b.Present {
			presentBins = append(presentBins, b.Name+":"+b.Path)
		}
	}
	if len(presentBins) > 0 {
		fmt.Fprintf(&sb, "- Present binaries: %s\n", strings.Join(presentBins, ", "))
	}
	return sb.String()
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
				Name:        "bash_exec",
				Namespace:   "system",
				Description: "Execute a shell command on the host OS. Returns stdout, stderr, and exit code.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorShellResult,
				Parameters:  json.RawMessage(bashExecParamsSchema),
				Version:     "1.0",
			},
			exec: makeBashExecExecutor(adapter, gate),
		},
		{
			schema: &tools.ToolSchema{
				Name:        "file_read",
				Namespace:   "system",
				Description: "Read a file from the host filesystem. Path must be inside $HOME/.local/brae/.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorArtifact,
				Parameters:  json.RawMessage(fileReadParamsSchema),
				Version:     "1.0",
			},
			exec: makeFileReadExecutor(adapter),
		},
		{
			schema: &tools.ToolSchema{
				Name:        "file_write",
				Namespace:   "system",
				Description: "Write content to a file on the host filesystem. Path must be inside $HOME/.local/brae/.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorArtifact,
				Parameters:  json.RawMessage(fileWriteParamsSchema),
				Version:     "1.0",
			},
			exec: makeFileWriteExecutor(adapter),
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

		// Gate check (REQ-002): bash_exec is NodeKindTool, not NodeKindBash,
		// so fireBash's built-in gate is not involved. We enforce here.
		if gate != nil {
			op := cpn.GateOp{Kind: "exec", Command: args.Command}
			if err := gate.Check(ctx, op); err != nil {
				return cpn.Token{}, err
			}
		}

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
		if args.Path == "" {
			return cpn.Token{}, fmt.Errorf("file_read: 'path' is required")
		}

		data, err := adapter.ReadFile(ctx, args.Path)
		if err != nil {
			if isPathDenied(err) {
				return cpn.Token{}, fmt.Errorf("error: path %s is outside the allowed root. Use paths under $HOME/.local/brae/", args.Path)
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
		if args.Path == "" {
			return cpn.Token{}, fmt.Errorf("file_write: 'path' is required")
		}
		if args.Mode == 0 {
			args.Mode = 0o644
		}

		if err := adapter.WriteFile(ctx, args.Path, []byte(args.Content), fs.FileMode(args.Mode)); err != nil {
			if isPathDenied(err) {
				return cpn.Token{}, fmt.Errorf("error: path %s is outside the allowed root. Use paths under $HOME/.local/brae/", args.Path)
			}
			return cpn.Token{}, fmt.Errorf("file_write: %w", err)
		}

		type writeResult struct {
			Written bool   `json:"written"`
			Path    string `json:"path"`
		}
		return cpn.Token{
			Color:   cpn.ColorArtifact,
			Payload: writeResult{Written: true, Path: args.Path},
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
