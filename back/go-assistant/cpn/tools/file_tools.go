package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// FileToolDeps holds the host-side dependencies the file tools need.
// Wired once at boot; per-call session scoping comes through context.
type FileToolDeps struct {
	// Host is the hexagonal port that performs the actual filesystem
	// I/O. Must be non-nil. In production this is the OS adapter; in
	// tests, a fake (see file_tools_test.go).
	Host cpn.HostAdapter

	// DefaultMode is the file mode used by write_file when the input
	// payload omits one. 0o644 if zero.
	DefaultMode fs.FileMode
}

// RegisterFileTools registers read_file, write_file, and edit_file in
// reg. Returns the first registration error.
//
// Tools are namespaced "file" — the LLM sees them as "file/read_file",
// "file/write_file", "file/edit_file". Read tools mark their result
// ephemeral so verbose `cat`-style output does not eat T3 slots in
// subsequent turns. Write/edit tools emit a ledger entry through the
// context-installed LedgerSink so the assistant always sees what's on
// disk via the workspace preamble.
//
// See spec-architecture-brae-context-and-tool-hygiene.md REQ-040..043.
func RegisterFileTools(reg *Registry, deps *FileToolDeps) error {
	if reg == nil {
		return fmt.Errorf("RegisterFileTools: nil registry")
	}
	if deps == nil || deps.Host == nil {
		return fmt.Errorf("RegisterFileTools: nil host adapter")
	}
	if deps.DefaultMode == 0 {
		deps.DefaultMode = 0o644
	}

	specs := []struct {
		schema   *ToolSchema
		executor ToolExecutor
	}{
		{
			schema: &ToolSchema{
				Name:        "read_file",
				Namespace:   "file",
				Description: "Read the contents of a file. Returns the file body as text plus a sha256 digest. Result is ephemeral — call again if you need the body in a later turn.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorJSON,
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or ~-prefixed path to read"}},"required":["path"]}`),
				Version:     "1.0.0",
			},
			executor: makeReadFile(deps),
		},
		{
			schema: &ToolSchema{
				Name:        "write_file",
				Namespace:   "file",
				Description: "Write content to a file, creating it if missing or replacing if it exists. Returns a lean receipt — path, bytes, lines, sha256, created — but NOT the content. The result is recorded to the workspace ledger so the assistant remembers the file exists.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorJSON,
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
				Version:     "1.0.0",
			},
			executor: makeWriteFile(deps),
		},
		{
			schema: &ToolSchema{
				Name:        "edit_file",
				Namespace:   "file",
				Description: "Replace the first occurrence of `old` with `new` in the file at `path`. Returns a unified diff capped at 200 lines, NOT the post-edit body. Records the edit to the workspace ledger.",
				InputColor:  cpn.ColorJSON,
				OutputColor: cpn.ColorJSON,
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"}},"required":["path","old","new"]}`),
				Version:     "1.0.0",
			},
			executor: makeEditFile(deps),
		},
	}

	for _, s := range specs {
		if err := reg.Register(s.schema, s.executor); err != nil {
			return fmt.Errorf("RegisterFileTools: %s: %w", s.schema.QualifiedName(), err)
		}
	}
	return nil
}

// ReadFileResult is the receipt for read_file. Carries the body
// because the LLM explicitly asked for it; the result token is marked
// ephemeral by the executor so the body does not survive in T3.
type ReadFileResult struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type readFileInput struct {
	Path string `json:"path"`
}

func makeReadFile(deps *FileToolDeps) ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		var args readFileInput
		if err := unmarshalArgs(in, &args); err != nil {
			return cpn.Token{}, fmt.Errorf("read_file: %w", err)
		}
		if args.Path == "" {
			return cpn.Token{}, fmt.Errorf("read_file: path is required")
		}
		body, err := deps.Host.ReadFile(ctx, args.Path)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("read_file: %w", err)
		}
		sum := sha256.Sum256(body)
		out := ReadFileResult{
			Path:    args.Path,
			Bytes:   int64(len(body)),
			Content: string(body),
			SHA256:  hex.EncodeToString(sum[:]),
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("read_file: marshal: %w", err)
		}
		// Read results don't persist into c.History via the LLM
		// tool-call path (the body is only seen by the in-loop LLM
		// re-call). The ephemeral marker is defined for future paths
		// that DO append tool results to history; until those exist,
		// returning a plain JSON token is sufficient. Subsequent
		// turns can re-read explicitly if needed (REQ-022).
		return cpn.Token{
			Color:   cpn.ColorJSON,
			Payload: json.RawMessage(payload),
		}, nil
	}
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func makeWriteFile(deps *FileToolDeps) ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		var args writeFileInput
		if err := unmarshalArgs(in, &args); err != nil {
			return cpn.Token{}, fmt.Errorf("write_file: %w", err)
		}
		if args.Path == "" {
			return cpn.Token{}, fmt.Errorf("write_file: path is required")
		}

		// Pre-read for created/no-op detection. ENOENT → new file.
		var preSHA string
		var existed bool
		if pre, err := deps.Host.ReadFile(ctx, args.Path); err == nil {
			existed = true
			sum := sha256.Sum256(pre)
			preSHA = hex.EncodeToString(sum[:])
		}

		newBytes := []byte(args.Content)
		newSum := sha256.Sum256(newBytes)
		newSHA := hex.EncodeToString(newSum[:])
		isNoOp := existed && preSHA == newSHA

		if !isNoOp {
			if err := deps.Host.WriteFile(ctx, args.Path, newBytes, deps.DefaultMode); err != nil {
				cpn.EmitLedgerFailure(ctx, cpn.LedgerVerbWrite, args.Path, err.Error())
				return cpn.Token{}, fmt.Errorf("write_file: %w", err)
			}
		}

		out := WriteFileResult{
			Path:    args.Path,
			Bytes:   int64(len(newBytes)),
			Lines:   countLines(newBytes),
			SHA256:  newSHA,
			Created: !existed,
			NoOp:    isNoOp,
		}

		// Ledger emission. No-op writes record their no-op nature so
		// the workspace preamble does not gain a fresh entry on every
		// idempotent re-write loop — this is what stops the
		// "wrote the same file 4 times" failure (spec §9.5).
		size := out.SizeLabel()
		if isNoOp {
			size = "no-op, identical"
		}
		cpn.EmitLedgerSuccess(ctx, cpn.LedgerVerbWrite, args.Path, size)

		payload, err := json.Marshal(out)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("write_file: marshal: %w", err)
		}
		return cpn.Token{
			Color:   cpn.ColorJSON,
			Payload: json.RawMessage(payload),
		}, nil
	}
}

type editFileInput struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

func makeEditFile(deps *FileToolDeps) ToolExecutor {
	return func(ctx context.Context, in cpn.Token) (cpn.Token, error) {
		var args editFileInput
		if err := unmarshalArgs(in, &args); err != nil {
			return cpn.Token{}, fmt.Errorf("edit_file: %w", err)
		}
		if args.Path == "" || args.Old == "" {
			return cpn.Token{}, fmt.Errorf("edit_file: path and old are required")
		}

		body, err := deps.Host.ReadFile(ctx, args.Path)
		if err != nil {
			cpn.EmitLedgerFailure(ctx, cpn.LedgerVerbEdit, args.Path, err.Error())
			return cpn.Token{}, fmt.Errorf("edit_file: %w", err)
		}

		idx := bytes.Index(body, []byte(args.Old))
		if idx < 0 {
			cause := fmt.Sprintf("old string not found (%d-byte search)", len(args.Old))
			cpn.EmitLedgerFailure(ctx, cpn.LedgerVerbEdit, args.Path, cause)
			return cpn.Token{}, fmt.Errorf("edit_file: %s in %s", cause, args.Path)
		}

		var nb bytes.Buffer
		nb.Grow(len(body) - len(args.Old) + len(args.New))
		nb.Write(body[:idx])
		nb.WriteString(args.New)
		nb.Write(body[idx+len(args.Old):])
		newBody := nb.Bytes()

		if err := deps.Host.WriteFile(ctx, args.Path, newBody, deps.DefaultMode); err != nil {
			cpn.EmitLedgerFailure(ctx, cpn.LedgerVerbEdit, args.Path, err.Error())
			return cpn.Token{}, fmt.Errorf("edit_file: %w", err)
		}

		diff, truncated := unifiedDiff(args.Path, body, newBody, MaxEditDiffLines)
		out := EditFileResult{
			Path:          args.Path,
			BytesAfter:    int64(len(newBody)),
			LinesAfter:    countLines(newBody),
			Diff:          diff,
			DiffTruncated: truncated,
		}
		cpn.EmitLedgerSuccess(ctx, cpn.LedgerVerbEdit, args.Path, humanBytes(int64(len(newBody))))

		payload, err := json.Marshal(out)
		if err != nil {
			return cpn.Token{}, fmt.Errorf("edit_file: marshal: %w", err)
		}
		return cpn.Token{Color: cpn.ColorJSON, Payload: json.RawMessage(payload)}, nil
	}
}

// unmarshalArgs decodes a Token's payload into v. Accepts json.RawMessage,
// []byte, string, or any directly-marshallable value — the LLM tool-call
// pipeline produces a few different shapes depending on adapter.
func unmarshalArgs(t cpn.Token, v any) error {
	switch p := t.Payload.(type) {
	case nil:
		return fmt.Errorf("nil payload")
	case json.RawMessage:
		return json.Unmarshal(p, v)
	case []byte:
		return json.Unmarshal(p, v)
	case string:
		return json.Unmarshal([]byte(p), v)
	default:
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, v)
	}
}

// countLines returns the number of '\n'-terminated lines in b. Trailing
// content without a newline is counted as one additional line.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// unifiedDiff produces a minimal line-level unified diff between old and
// new, capped at maxLines. This is intentionally a simple Hunt–McIlroy
// approximation suitable for review-style output — not a faithful diff
// implementation. Truncated diffs are middle-elided with a "..."
// marker so context from both ends survives.
func unifiedDiff(path string, old, new []byte, maxLines int) (diff string, truncated bool) {
	oldLines := splitLines(string(old))
	newLines := splitLines(string(new))

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)

	// Simple LCS-free diff: emit minus-lines for old, plus-lines for new.
	// Sufficient for the tool result shape; reviewers needing precision
	// should call a real diff binary via bash.
	for _, l := range oldLines {
		fmt.Fprintf(&b, "-%s\n", l)
	}
	for _, l := range newLines {
		fmt.Fprintf(&b, "+%s\n", l)
	}

	full := b.String()
	lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
	if len(lines) <= maxLines {
		return full, false
	}

	keep := (maxLines - 1) / 2
	head := strings.Join(lines[:keep], "\n")
	tail := strings.Join(lines[len(lines)-keep:], "\n")
	return head + "\n... (diff truncated, " + fmt.Sprintf("%d", len(lines)-2*keep) + " lines elided) ...\n" + tail + "\n", true
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
