package persist

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// PurgeService hard-deletes quarantined artefacts older than a grace
// window. It is intended to run as an hourly goroutine and is safe to
// invoke concurrently with Rollback/Restore — the ledger itself serialises
// the state transitions and the filesystem removals are idempotent.
type PurgeService struct {
	Ledger AuthoredArtefactLedger
	FS     FSMover
	Logger *slog.Logger

	// GraceDays configures the default grace window used when Run is
	// called with the zero time. ARTEFACT_GRACE_DAYS overrides this at
	// composition-root level.
	GraceDays int
}

// NewPurgeService constructs a purge service with sane defaults.
func NewPurgeService(ledger AuthoredArtefactLedger, graceDays int, logger *slog.Logger) *PurgeService {
	if logger == nil {
		logger = slog.Default()
	}
	if graceDays <= 0 {
		graceDays = 7
	}
	return &PurgeService{
		Ledger:    ledger,
		FS:        OSFSMover{},
		Logger:    logger,
		GraceDays: graceDays,
	}
}

// Run purges every artefact whose quarantined_at is older than cutoff. Pass
// time.Time{} to default to now − GraceDays. Returns the number of rows
// purged and the first filesystem error, if any — the ledger is always
// updated so subsequent runs do not retry forever.
func (p *PurgeService) Run(ctx context.Context, cutoff time.Time) (int, error) {
	if cutoff.IsZero() {
		cutoff = time.Now().UTC().Add(-time.Duration(p.GraceDays) * 24 * time.Hour)
	}

	rows, err := p.Ledger.PurgeExpired(ctx, cutoff, "purge-service")
	if err != nil {
		return 0, fmt.Errorf("purge: ledger: %w", err)
	}

	// Group by set_id so we can remove the enclosing quarantine directory
	// once every row in the set is gone.
	setsTouched := make(map[string]string)
	var firstErr error
	for _, art := range rows {
		if art.QuarantinePath != "" {
			setsTouched[art.SetID] = art.QuarantinePath
		}
		// Path on disk: inside the quarantine dir, same relative structure
		// as the original path.
		if art.QuarantinePath == "" {
			continue
		}
		// We cannot compute a precise on-disk path without AllowedRoot —
		// but QuarantinePath is the set directory, and every file lives
		// inside it. RemoveAll on the set dir (below) handles the file
		// removals in one shot.
	}

	for setID, qdir := range setsTouched {
		if err := p.FS.RemoveAll(qdir); err != nil && !os.IsNotExist(err) {
			p.Logger.Error("purge: remove quarantine dir failed",
				slog.String("set_id", setID),
				slog.String("dir", qdir),
				slog.Any("error", err),
			)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Fire-and-forget audit hook. The ledger also records a per-set
		// event during PurgeExpired; this is defensive for callers that
		// wire the ledger loosely.
		_ = p.Ledger.RecordEvent(ctx, ArtefactEvent{
			SetID: setID,
			Kind:  "purge_fs_complete",
			Actor: "purge-service",
			At:    time.Now().UTC(),
		})
	}

	p.Logger.Info("purge: completed",
		slog.Int("rows", len(rows)),
		slog.Int("sets", len(setsTouched)),
		slog.Time("cutoff", cutoff),
	)
	return len(rows), firstErr
}

// Loop runs Run on every tick, stopping when ctx is cancelled. Intended
// to be launched as a goroutine from the composition root.
func (p *PurgeService) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	// Fire once on startup to clear any backlog, then every interval.
	if _, err := p.Run(ctx, time.Time{}); err != nil {
		p.Logger.Warn("purge: initial run failed", slog.Any("error", err))
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := p.Run(ctx, time.Time{}); err != nil {
				p.Logger.Warn("purge: scheduled run failed", slog.Any("error", err))
			}
		}
	}
}
