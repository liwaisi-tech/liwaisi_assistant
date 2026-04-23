// Package tools — file-write result shapes.
//
// These types define the canonical receipts returned by state-changing
// file tools. They intentionally OMIT a `content` field: the LLM has no
// reason to receive bytes back from an operation it just authored, and
// echoing the bytes encourages the model to paste them into chat (the
// failure mode catalogued in spec REQ-040..043).
//
// If the LLM later needs to inspect the file it just wrote, it MUST
// call read_file explicitly. This costs an extra round-trip, which is
// the point — it ensures the read is intentional, not reflexive.
package tools

import "fmt"

// WriteFileResult is the receipt returned by the write_file tool. It
// carries enough metadata for the LLM to confirm the write succeeded
// and emit a useful narration ("wrote main.go, 58 lines"), but never
// the file body itself.
type WriteFileResult struct {
	// Path is the destination path as it was passed to the tool.
	Path string `json:"path"`

	// Bytes is the size of the written file in bytes (post-write,
	// from os.Stat). Not the bytes the tool was asked to write —
	// these can differ when the tool normalises line endings.
	Bytes int64 `json:"bytes"`

	// Lines is the number of '\n'-terminated lines in the written
	// content. Empty files report 0; binary files report 0 if no
	// newlines are present.
	Lines int `json:"lines"`

	// SHA256 is the lowercase hex sha256 of the post-write bytes.
	// Used by callers to detect identical-rewrite no-ops (REQ-9.5).
	SHA256 string `json:"sha256"`

	// Created is true when the path did not exist before this call.
	// False on overwrite. Lets the assistant narrate "Wrote new file"
	// vs. "Updated file" without an extra Stat round-trip.
	Created bool `json:"created"`

	// NoOp is true when the post-write SHA256 matches the pre-write
	// SHA256 (file existed AND its content was identical to the
	// requested content). When NoOp is true, callers SHOULD emit the
	// ledger line "[ok] write <path> (no-op, identical)" rather than
	// a fresh write entry, so the ledger does not accumulate
	// duplicate write events for the same path+content pair.
	NoOp bool `json:"noop,omitempty"`
}

// SizeLabel renders Bytes as a compact human label ("1.2KB", "843B")
// suitable for use as the size argument to cpn.LedgerSuccess.
func (r WriteFileResult) SizeLabel() string { return humanBytes(r.Bytes) }

// EditFileResult is the receipt returned by the edit_file tool. It
// returns a unified diff (capped) rather than the post-edit body, for
// the same reasons as WriteFileResult.
type EditFileResult struct {
	// Path is the edited file's path.
	Path string `json:"path"`

	// BytesAfter is the post-edit file size in bytes.
	BytesAfter int64 `json:"bytes_after"`

	// LinesAfter is the post-edit line count.
	LinesAfter int `json:"lines_after"`

	// Diff is a unified diff (`---`/`+++`/`@@` headers) capped at
	// MaxEditDiffLines lines. Long diffs are truncated middle-elided.
	// MUST NOT contain the full post-edit body.
	Diff string `json:"diff"`

	// DiffTruncated is true when the original diff exceeded
	// MaxEditDiffLines and was elided. Lets the assistant narrate
	// "diff truncated, see file for full change" honestly.
	DiffTruncated bool `json:"diff_truncated,omitempty"`
}

// MaxEditDiffLines caps the diff field so a refactor that touches
// thousands of lines doesn't single-handedly blow the context window.
const MaxEditDiffLines = 200

// humanBytes renders n as "843B", "1.2KB", "3.4MB" with one decimal
// for KB+ and no decimals for sub-KB. Cheap and dependency-free —
// keeps tools/ from picking up a humanize lib for one helper.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(1024*1024*1024))
	}
}
