package persist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ── Ports consumed by RollbackService ───────────────────────────────────────

// FSMover is the filesystem port used by RollbackService. A nil FSMover
// falls back to the default OSFSMover, which uses os.Rename + os.MkdirAll.
//
// Kept tiny so tests can inject an in-memory fake and exercise partial
// failure + reversal paths.
type FSMover interface {
	// Move renames src → dst, creating intermediate directories as needed.
	Move(src, dst string) error

	// Remove deletes a path (file or empty directory). Used by the purge
	// service; rollback reverses on failure via Move in the opposite
	// direction, not Remove.
	Remove(path string) error

	// MkdirAll mirrors os.MkdirAll for the staging directory.
	MkdirAll(path string, perm os.FileMode) error

	// RemoveAll removes a directory tree (for cleaning up the staging
	// directory and for purge).
	RemoveAll(path string) error

	// Exists reports whether a path exists.
	Exists(path string) bool
}

// OSFSMover is the production FSMover backed by os.Rename / os.MkdirAll.
type OSFSMover struct{}

// Move renames src → dst, creating intermediate directories as needed.
func (OSFSMover) Move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Remove deletes a single path.
func (OSFSMover) Remove(path string) error { return os.Remove(path) }

// MkdirAll mirrors os.MkdirAll.
func (OSFSMover) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

// RemoveAll removes a tree.
func (OSFSMover) RemoveAll(path string) error { return os.RemoveAll(path) }

// Exists reports whether path exists.
func (OSFSMover) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ToolDeprecator is the callback interface RollbackService uses to mark a
// registered tool as deprecated when its backing binary is rolled back. The
// production binding lives in the integration layer so cpn/persist stays
// storage-agnostic (no import of cpn/tools).
type ToolDeprecator interface {
	// Deprecate flags the qualified tool name as deprecated with the given
	// reason. A no-op implementation is safe when no tool registry is
	// wired.
	Deprecate(ctx context.Context, qualifiedName, reason string) error

	// LookupByBinaryPath returns the qualified name of the tool whose
	// binary_path matches the given path, or "" when no tool is registered
	// for that path.
	LookupByBinaryPath(ctx context.Context, binaryPath string) (qualifiedName string, found bool)
}

// NoopToolDeprecator is the safe fallback when no tool registry is wired.
type NoopToolDeprecator struct{}

// Deprecate is a no-op.
func (NoopToolDeprecator) Deprecate(context.Context, string, string) error { return nil }

// LookupByBinaryPath always returns (qn="", found=false).
func (NoopToolDeprecator) LookupByBinaryPath(context.Context, string) (string, bool) { return "", false }

// ── RollbackService ────────────────────────────────────────────────────────

// RollbackService orchestrates the two-phase delete described in the GAP-10
// spec: move every file in a set into a staging directory under
// $HOME/.local/brae/quarantine/.staging-<set_id>/ (preserving the relative
// structure), then atomically rename the staging dir to
// $HOME/.local/brae/quarantine/<set_id>/ on success, or reverse every move
// on the first error.
type RollbackService struct {
	Ledger     AuthoredArtefactLedger
	FS         FSMover
	AllowedRoot string // typically $HOME/.local/brae
	Tools      ToolDeprecator
	Logger     *slog.Logger
}

// NewRollbackService constructs a rollback service with sane defaults.
//
// allowedRoot MUST be the same path the HostAdapter uses as its jail — the
// relative path from allowedRoot to each artefact is preserved under the
// quarantine dir.
func NewRollbackService(ledger AuthoredArtefactLedger, allowedRoot string, tools ToolDeprecator, logger *slog.Logger) *RollbackService {
	if tools == nil {
		tools = NoopToolDeprecator{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RollbackService{
		Ledger:     ledger,
		FS:         OSFSMover{},
		AllowedRoot: allowedRoot,
		Tools:      tools,
		Logger:     logger,
	}
}

// QuarantineDir returns the final quarantine directory for a set_id.
func (s *RollbackService) QuarantineDir(setID string) string {
	return filepath.Join(s.AllowedRoot, "quarantine", setID)
}

// stagingDir returns the staging directory for a set_id.
func (s *RollbackService) stagingDir(setID string) string {
	return filepath.Join(s.AllowedRoot, "quarantine", ".staging-"+setID)
}

// Rollback moves every artefact in set_id into the quarantine directory and
// updates the ledger. On any filesystem error, every move made so far is
// reversed before returning the error. Tool-registry deprecation fires for
// every artefact whose classification == binary AND whose BinaryPath is
// known to the ToolDeprecator.
func (s *RollbackService) Rollback(ctx context.Context, setID, actor string) error {
	if setID == "" {
		return errors.New("rollback: set_id is required")
	}
	if s.AllowedRoot == "" {
		return errors.New("rollback: allowed root is empty")
	}

	artefacts, err := s.Ledger.SetForSet(ctx, setID)
	if err != nil {
		return fmt.Errorf("rollback: set %q: %w", setID, err)
	}
	if len(artefacts) == 0 {
		return ErrArtefactSetEmpty
	}

	staging := s.stagingDir(setID)
	final := s.QuarantineDir(setID)
	if s.FS.Exists(final) {
		return ErrArtefactAlreadyRolledBack
	}
	if err := s.FS.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("rollback: mkdir staging: %w", err)
	}

	type move struct {
		from string
		to   string
	}
	var moves []move

	reverse := func() {
		for i := len(moves) - 1; i >= 0; i-- {
			if err := s.FS.Move(moves[i].to, moves[i].from); err != nil {
				s.Logger.Error("rollback: reverse move failed",
					slog.String("set_id", setID),
					slog.String("from", moves[i].to),
					slog.String("to", moves[i].from),
					slog.Any("error", err),
				)
			}
		}
		_ = s.FS.RemoveAll(staging)
	}

	for _, art := range artefacts {
		if art.State != ArtefactStateActive && art.State != ArtefactStateRestored {
			// Skip already-quarantined/purged rows; they shouldn't be
			// re-moved, but we still want the ledger update to happen.
			continue
		}
		rel, err := filepath.Rel(s.AllowedRoot, art.Path)
		if err != nil || strings.HasPrefix(rel, "..") {
			reverse()
			return fmt.Errorf("rollback: path %q outside allowed root", art.Path)
		}
		dst := filepath.Join(staging, rel)
		if err := s.FS.Move(art.Path, dst); err != nil {
			s.Logger.Error("rollback: move failed",
				slog.String("set_id", setID),
				slog.String("from", art.Path),
				slog.String("to", dst),
				slog.Any("error", err),
			)
			reverse()
			return fmt.Errorf("rollback: move %q: %w", art.Path, err)
		}
		moves = append(moves, move{from: art.Path, to: dst})
	}

	// Atomic commit: rename staging → final.
	if err := s.FS.Move(staging, final); err != nil {
		reverse()
		return fmt.Errorf("rollback: commit: %w", err)
	}

	if err := s.Ledger.Rollback(ctx, setID, final, actor); err != nil {
		// The files have been moved but the ledger update failed; attempt
		// to reverse the commit so the next rollback attempt sees a clean
		// slate.
		if rerr := s.FS.Move(final, staging); rerr != nil {
			s.Logger.Error("rollback: cannot reverse commit after ledger failure",
				slog.String("set_id", setID),
				slog.Any("ledger_error", err),
				slog.Any("reverse_error", rerr),
			)
			return fmt.Errorf("rollback: ledger: %w", err)
		}
		reverse()
		return fmt.Errorf("rollback: ledger: %w", err)
	}

	// Deprecate registered tools whose binary was rolled back.
	s.deprecateTools(ctx, artefacts)
	return nil
}

// Restore is the symmetric operation: move every file in set_id back from
// the quarantine directory to its original path, then update the ledger.
func (s *RollbackService) Restore(ctx context.Context, setID, actor string) error {
	if setID == "" {
		return errors.New("restore: set_id is required")
	}
	artefacts, err := s.Ledger.SetForSet(ctx, setID)
	if err != nil {
		return fmt.Errorf("restore: set %q: %w", setID, err)
	}
	final := s.QuarantineDir(setID)
	if !s.FS.Exists(final) {
		return ErrArtefactNotQuarantined
	}

	type move struct {
		from string
		to   string
	}
	var moves []move

	reverse := func() {
		for i := len(moves) - 1; i >= 0; i-- {
			if err := s.FS.Move(moves[i].to, moves[i].from); err != nil {
				s.Logger.Error("restore: reverse move failed",
					slog.String("set_id", setID),
					slog.String("from", moves[i].to),
					slog.String("to", moves[i].from),
					slog.Any("error", err),
				)
			}
		}
	}

	for _, art := range artefacts {
		if art.State != ArtefactStateQuarantined {
			continue
		}
		rel, err := filepath.Rel(s.AllowedRoot, art.Path)
		if err != nil || strings.HasPrefix(rel, "..") {
			reverse()
			return fmt.Errorf("restore: path %q outside allowed root", art.Path)
		}
		src := filepath.Join(final, rel)
		if err := s.FS.Move(src, art.Path); err != nil {
			reverse()
			return fmt.Errorf("restore: move %q: %w", src, err)
		}
		moves = append(moves, move{from: src, to: art.Path})
	}

	// Best-effort cleanup — the directory should be empty now.
	_ = s.FS.RemoveAll(final)

	if err := s.Ledger.Restore(ctx, setID, actor); err != nil {
		return fmt.Errorf("restore: ledger: %w", err)
	}
	return nil
}

// deprecateTools flags any registered tools whose binary_path matches an
// artefact in the rolled-back set.
func (s *RollbackService) deprecateTools(ctx context.Context, artefacts []Artefact) {
	if s.Tools == nil {
		return
	}
	for _, art := range artefacts {
		if art.Classification != ClassBinary {
			continue
		}
		qn, ok := s.Tools.LookupByBinaryPath(ctx, art.Path)
		if !ok || qn == "" {
			continue
		}
		if err := s.Tools.Deprecate(ctx, qn, "artefact_rolled_back"); err != nil {
			s.Logger.Warn("rollback: tool deprecate failed",
				slog.String("qualified_name", qn),
				slog.String("path", art.Path),
				slog.Any("error", err),
			)
		} else {
			s.Logger.Info("rollback: tool deprecated",
				slog.String("qualified_name", qn),
				slog.String("path", art.Path),
			)
		}
	}
}
