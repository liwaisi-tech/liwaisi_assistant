package cpn

// shell_parse.go implements ParseShellInvocation — a pure helper that
// extracts a human-readable shell command (or operation summary) from a
// tool invocation. Used by the HOST·HITL approval surface so the user
// sees a clean `$ uname -a && whoami` line instead of the raw tool JSON.
//
// See spec-process-bugfix-tool-hitl-single-gate.md §4.2 for the authoritative
// input→output table.
//
// Lives in the cpn package (not cpn/tools) because fire_llm.go needs it and
// cpn/tools already imports cpn — a reciprocal import would cycle. The
// helper is pure (no cpn types in its signature) so the placement is a
// compile-time concession only.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseShellInvocation converts a (tool, args) pair into a clean,
// human-readable command string and a confidence flag.
//
//   - For `bash_exec`: when args carry a `bash -c "<script>"` /
//     `bash -lc "<script>"` shape, the wrapper is stripped and the inner
//     script is returned verbatim. For plain executables it joins
//     command + args with single spaces.
//   - For `file_read` / `file_write`: produces a short
//     "read ← /path" or "write → /path" summary.
//   - Anything else falls back to the JSON stringification of args and
//     returns ok=false so callers can surface a heightened-risk badge.
//
// args may be a `json.RawMessage`, a `[]byte`, a `string` (serialised JSON),
// or any `map`/struct that json.Marshal accepts.
func ParseShellInvocation(tool string, args any) (command string, ok bool) {
	raw, rawOK := rawArgsJSON(args)
	switch tool {
	case "bash_exec":
		if !rawOK {
			return stringifyFallback(args), false
		}
		var parsed struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return string(raw), false
		}
		if parsed.Command == "" {
			return string(raw), false
		}
		if isShellWrapper(parsed.Command) && len(parsed.Args) >= 2 && isDashC(parsed.Args[0]) {
			return parsed.Args[1], true
		}
		if len(parsed.Args) == 0 {
			return parsed.Command, true
		}
		return parsed.Command + " " + strings.Join(parsed.Args, " "), true

	case "file_write":
		if !rawOK {
			return stringifyFallback(args), false
		}
		var parsed struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Path == "" {
			return stringifyFallback(args), false
		}
		return "write → " + parsed.Path, true

	case "file_read":
		if !rawOK {
			return stringifyFallback(args), false
		}
		var parsed struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Path == "" {
			return stringifyFallback(args), false
		}
		return "read ← " + parsed.Path, true
	}

	return stringifyFallback(args), false
}

func rawArgsJSON(args any) ([]byte, bool) {
	if args == nil {
		return nil, false
	}
	switch v := args.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			return nil, false
		}
		return []byte(v), true
	case []byte:
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	case string:
		if v == "" {
			return nil, false
		}
		return []byte(v), true
	}
	b, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return b, true
}

func stringifyFallback(args any) string {
	if b, ok := rawArgsJSON(args); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", args)
}

func isShellWrapper(command string) bool {
	switch command {
	case "bash", "sh", "/bin/bash", "/bin/sh", "zsh", "/bin/zsh":
		return true
	}
	return false
}

func isDashC(flag string) bool {
	return flag == "-c" || flag == "-lc"
}
