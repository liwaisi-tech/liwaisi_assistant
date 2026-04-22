package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// userToolNameRe mirrors the same charset system/register_tool enforces at
// authoring time. Re-validated here because the bare name is also used as the
// CPN transition ID and the LLM-visible tool name (OpenAI / OpenRouter accept
// [A-Za-z0-9_-]{1,64}).
var userToolNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// fallbackUserToolParams is the JSON Schema attached to a user-authored tool
// when the registry entry has no persisted schema. Gives the LLM a stable
// contract instead of a bare "execute this binary" signal.
var fallbackUserToolParams = json.RawMessage(`{
  "type": "object",
  "properties": {
    "args": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Positional arguments passed to the user-authored binary. Omit for a no-arg invocation."
    },
    "timeout_seconds": {
      "type": "integer",
      "minimum": 1,
      "maximum": 300,
      "description": "Maximum execution time in seconds. Defaults to 30."
    }
  }
}`)

// materialiseUserTools synthesises a NodeKindTool transition on root for every
// non-deprecated user-authored entry in reg, attaches a HostAdapter-bound
// executor, and appends the new transition IDs to every NodeKindLLM transition
// whose LLMTools already include bash_exec (the canonical marker for "this
// transition opts into the system-tool surface"). Safe to call with nil
// collaborators — it becomes a no-op.
//
// See spec/spec-architecture-user-tool-session-surface.md for the contract.
func materialiseUserTools(root *cpn.CPN, reg SessionToolRegistry, rt *cpn.HostRuntime, logger *slog.Logger) []string {
	if root == nil || reg == nil || rt == nil || rt.Adapter == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	entries := reg.ListUserAuthored()
	if len(entries) == 0 {
		return nil
	}

	attached := make([]string, 0, len(entries))
	for _, e := range entries {
		if e == nil || e.Deprecated {
			continue
		}
		name := e.Name
		if !userToolNameRe.MatchString(name) {
			logger.Warn("session.user_tools.skipped",
				"reason", "invalid_name",
				"qualified_name", e.QualifiedName())
			continue
		}
		if _, clash := root.Transitions[name]; clash {
			// A static transition (or an earlier user entry at the same
			// bare name) already owns this ID. Skip to avoid shadowing the
			// built-in surface.
			logger.Warn("session.user_tools.skipped",
				"reason", "transition_id_clash",
				"tool_name", name)
			continue
		}
		if e.BinaryPath == "" {
			logger.Warn("session.user_tools.skipped",
				"reason", "missing_binary_path",
				"tool_name", name)
			continue
		}

		params := e.JSONSchema
		if len(params) == 0 {
			params = fallbackUserToolParams
		}
		description := e.HelpText
		if description == "" {
			description = fmt.Sprintf("User-authored tool %q (binary: %s).", name, e.BinaryPath)
		}

		t := cpn.NewTransition(name, cpn.NodeKindTool, []string{}, []string{})
		t.ToolName = name
		t.ToolMeta = &cpn.ToolMeta{
			Description:  description,
			Parameters:   params,
			RequiresHITL: true,
			Namespace:    e.Namespace,
		}
		t.Executor = makeUserToolExecutor(name, e.BinaryPath, rt.Adapter)
		root.Transitions[name] = t

		attached = append(attached, name)
		logger.Info("session.user_tools.injected",
			"session_id", root.SessionID,
			"tool_name", name,
			"binary_path", e.BinaryPath,
			"version", e.Version,
			"toolbox", e.Toolbox,
		)
	}

	if len(attached) == 0 {
		return nil
	}

	// Extend every LLM transition that already lists bash_exec. That is the
	// canonical marker for "this transition opts into the system-tool
	// surface" — today t-direct and t-execute, any future addition is
	// picked up automatically.
	for _, t := range root.Transitions {
		if t.Kind != cpn.NodeKindLLM || len(t.LLMTools) == 0 {
			continue
		}
		if !slices.Contains(t.LLMTools, "bash_exec") {
			continue
		}
		t.LLMTools = append(t.LLMTools, attached...)
	}

	return attached
}

// makeUserToolExecutor returns a ToolExecutor that invokes binaryPath via the
// HostAdapter. Args and timeout_seconds are read from the LLM tool-call
// payload; everything else is fixed by the registry entry. Non-zero exits are
// returned as ShellResultPayload (not errors) so the LLM can inspect stderr
// and retry, matching the bash_exec contract.
func makeUserToolExecutor(toolName, binaryPath string, adapter cpn.HostAdapter) tools.ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		payload, ok := in.Payload.(string)
		if !ok {
			return cpn.Token{}, fmt.Errorf("%s: expected string payload, got %T", toolName, in.Payload)
		}
		var args struct {
			Args           []string `json:"args"`
			TimeoutSeconds int      `json:"timeout_seconds"`
			Cwd            string   `json:"cwd"`
		}
		if len(payload) > 0 {
			if err := json.Unmarshal([]byte(payload), &args); err != nil {
				return cpn.Token{}, fmt.Errorf("%s: parse arguments: %w", toolName, err)
			}
		}
		if args.TimeoutSeconds <= 0 {
			args.TimeoutSeconds = 30
		}

		req := cpn.ExecRequest{
			Command:      binaryPath,
			Args:         args.Args,
			Cwd:          args.Cwd,
			Timeout:      time.Duration(args.TimeoutSeconds) * time.Second,
			AllowNonZero: true,
		}
		result, err := adapter.Exec(ctx, req)
		if err != nil {
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
