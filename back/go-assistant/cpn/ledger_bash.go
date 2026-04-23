package cpn

import (
	"fmt"
	"path/filepath"
	"strings"
)

// classifyBashCommand inspects a bash invocation and returns the
// state-changing verb + target if the command is known to mutate the
// workspace. Returns ok=false for read-only commands (ls, cat, find,
// grep, ...) and unknown commands — those should not produce ledger
// entries.
//
// Heuristics are intentionally narrow: better to miss a stateful
// command (no ledger entry, soft failure) than to mislabel a read-only
// one (false ledger entry, misleading workspace preamble). Authors who
// want richer detection can wrap their write tool in a registered
// NodeKindTool that calls LedgerSuccess directly.
func classifyBashCommand(cmd string, args []string) (verb LedgerVerb, target string, ok bool) {
	base := filepath.Base(strings.TrimSpace(cmd))

	switch base {
	case "mkdir":
		return LedgerVerbMkdir, lastNonFlag(args), lastNonFlag(args) != ""
	case "chmod":
		// chmod <mode> <path>... — target is the last positional.
		return LedgerVerbChmod, lastNonFlag(args), lastNonFlag(args) != ""
	case "cp":
		// cp [flags] <src...> <dst>. Target is the destination = last positional.
		return LedgerVerbCp, lastNonFlag(args), lastNonFlag(args) != ""
	case "mv":
		return LedgerVerbMv, lastNonFlag(args), lastNonFlag(args) != ""
	case "go":
		// go build [-o <out>] <pkg> — surface the -o target if present,
		// otherwise the package path. Skip non-build go subcommands.
		if len(args) == 0 || args[0] != "build" {
			return "", "", false
		}
		if out := flagValue(args, "-o"); out != "" {
			return LedgerVerbBuild, out, true
		}
		// Fall back to last positional (the package selector).
		if last := lastNonFlag(args[1:]); last != "" {
			return LedgerVerbBuild, last, true
		}
		return LedgerVerbBuild, ".", true
	case "make":
		// make <target> — surface the first positional or "default".
		if last := lastNonFlag(args); last != "" {
			return LedgerVerbBuild, last, true
		}
		return LedgerVerbBuild, "default", true
	case "tee":
		// tee writes to its file argument. -a appends, both count as edits.
		if last := lastNonFlag(args); last != "" {
			return LedgerVerbWrite, last, true
		}
	}
	return "", "", false
}

// lastNonFlag returns the last argument that does not start with "-".
// Empty string when args is empty or contains only flags.
func lastNonFlag(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		a := args[i]
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// flagValue returns the value following the named flag (e.g. "-o out.bin"
// → "out.bin"). Returns "" when the flag is absent or terminal.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// emitBashLedger appends a ledger entry to c.History for a bash
// invocation whose result is now known. It is a no-op for commands the
// classifier does not recognise — keeping the call site free of
// per-command branching.
//
// The caller MUST hold no lock on c when calling this; emitBashLedger
// takes c.mu under write to append.
func emitBashLedger(c *CPN, cmd string, args []string, exitCode int, stderr string) {
	if c == nil {
		return
	}
	verb, target, ok := classifyBashCommand(cmd, args)
	if !ok {
		return
	}

	var msg *Message
	var err error
	if exitCode == 0 {
		// Successful state change. Size is best-effort: for build/cp/mv
		// we don't stat the target here (would race with concurrent
		// modifications and add I/O on the hot path). Size left empty.
		msg, err = LedgerSuccess(verb, target, "")
	} else {
		cause := fmt.Sprintf("exit %d: %s", exitCode, stderr)
		msg, err = LedgerFailure(verb, target, cause)
	}
	if err != nil || msg == nil {
		return // ledger emission is best-effort; never fail the caller.
	}

	c.mu.Lock()
	c.History = append(c.History, msg)
	c.mu.Unlock()
}
