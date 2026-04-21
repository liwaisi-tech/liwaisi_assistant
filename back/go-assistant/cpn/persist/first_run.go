package persist

import (
	"context"
	"time"
)

// FirstRunLedgerEntry captures a binary's approval status on this host
// (REQ-010). Binaries are keyed by a SHA-256 fingerprint computed over
// the binary's bytes so a move/rename of the same file does not force a
// re-approval.
type FirstRunLedgerEntry struct {
	ID              string
	HostID          string
	BinaryPath      string
	BinarySHA256    string
	FirstSeen       time.Time
	FirstApprovedBy string
	FirstApprovedAt *time.Time
	Revoked         bool
}

// FirstRunRepository is the port for the first-run ledger. Implementations
// live in store/postgres/first_run_store.go (production) and a tiny
// in-memory adapter lives in infra/host/gate for tests.
type FirstRunRepository interface {
	// Record idempotently inserts the "first-seen" row for a binary. When
	// the row already exists, Record returns it untouched so callers can
	// inspect Revoked/FirstApprovedAt.
	Record(ctx context.Context, hostID, binaryPath, sha256 string) (*FirstRunLedgerEntry, error)

	// GetBySHA retrieves the entry for a given host+sha. Returns nil, nil
	// when the binary has never been seen.
	GetBySHA(ctx context.Context, hostID, sha256 string) (*FirstRunLedgerEntry, error)

	// Approve stamps FirstApprovedBy/FirstApprovedAt on the row and flips
	// Revoked to false.
	Approve(ctx context.Context, hostID, sha256, userID string) error

	// Revoke flips Revoked to true on the row.
	Revoke(ctx context.Context, hostID, sha256 string) error

	// List returns up to limit entries ordered by FirstSeen DESC.
	List(ctx context.Context, limit int) ([]*FirstRunLedgerEntry, error)
}

// GateDecisionRecord is one audit row per gate decision (REQ-003).
type GateDecisionRecord struct {
	ID             string
	SessionID      string
	OpKind         string
	CommandHash    string
	Path           string
	Sandbox        string
	Decision       string
	Reason         string
	RiskBand       string
	DecidedAt      time.Time
	HITLResponseID *string
}

// GateDecisionRepository is the port for the audit log.
type GateDecisionRepository interface {
	Insert(ctx context.Context, rec *GateDecisionRecord) error
	List(ctx context.Context, limit int) ([]*GateDecisionRecord, error)
}
