// Package persist — artefacts.go defines the authored-artefact provenance
// ledger port (GAP-10).
//
// Spec: spec-architecture-authored-artifact-provenance.md.
//
// Every file that brae writes under the path jail ($HOME/.local/brae/) flows
// through the HostAdapter.WriteFile hook, which consults this ledger both
// before the write (PreWrite — reserve an ID and a row so policy can reject
// the write) and after the write (PostWrite — capture SHA-256, size and
// MIME). Rollback moves the file to quarantine; Restore reverses the move;
// Purge hard-deletes rows whose quarantined_at is older than the grace
// window (default 7 days).
//
// The interface lives in cpn/persist so both the in-memory fake and the
// Postgres adapter can satisfy it; the integration-layer glue (rollback +
// purge + HTTP admin) imports it without tying the domain to a DB driver.
package persist

import (
	"context"
	"errors"
	"time"
)

// WriteIntentContextKey is a marker type used to extract a WriteIntent from
// a context.Context WITHOUT importing cpn from cpn/persist (which would
// create a cycle). The value of this key must match cpn.WriteIntentKey so
// both packages interop on the same context slot; see doc comment on
// LoadWriteIntent below.
type WriteIntentContextKey struct{}

// LoadWriteIntent extracts a WriteIntent attached via cpn.WithWriteIntent.
// The second return is false when no intent is attached or the stored
// value is of another type. The lookup uses a typed key that MUST match
// cpn.WriteIntentKey — we assert this with a compile-time check in the
// infra/host adapter (see host.AssertWriteIntentKey).
//
// Callers who can depend on cpn should prefer cpn.WithWriteIntent for the
// producer side; this helper exists so the in-process host adapter can
// read the intent without gaining a dependency on the cpn package (which
// would create an import cycle through fire_synthesize.go).
func LoadWriteIntent(ctx context.Context, key any) (WriteIntent, bool) {
	if ctx == nil || key == nil {
		return WriteIntent{}, false
	}
	v := ctx.Value(key)
	if v == nil {
		return WriteIntent{}, false
	}
	intent, ok := v.(WriteIntent)
	return intent, ok
}

// ── Value objects ───────────────────────────────────────────────────────────

// ArtefactClassification is the coarse bucket the host maps a written file
// into (spec §3 REQ-005). Callers may pass a value explicitly in
// WriteIntent.Classification; when absent, Classify() derives one from the
// path + mode on the auto-classification fallback.
type ArtefactClassification string

const (
	// ClassSource is human-authored source code (.go, .py, .rs, .ts, …).
	ClassSource ArtefactClassification = "source"
	// ClassBinary is an executable or object file.
	ClassBinary ArtefactClassification = "binary"
	// ClassConfig is a configuration file (.yaml, .toml, .json, …).
	ClassConfig ArtefactClassification = "config"
	// ClassLog is a log file (.log, …).
	ClassLog ArtefactClassification = "log"
	// ClassMan is a man page or help/documentation file.
	ClassMan ArtefactClassification = "man"
	// ClassOther is anything that does not match the known classes.
	ClassOther ArtefactClassification = "other"
)

// ArtefactState enumerates the lifecycle of an authored artefact row.
type ArtefactState string

const (
	// ArtefactStatePending means PreWrite succeeded but PostWrite has not
	// recorded the hash yet. Rows in this state belong to in-flight writes.
	ArtefactStatePending ArtefactState = "pending"
	// ArtefactStateActive means the write completed and the file is on disk.
	ArtefactStateActive ArtefactState = "active"
	// ArtefactStateQuarantined means the file was moved to the quarantine
	// staging area by a rollback — it still exists on disk under
	// $HOME/.local/brae/quarantine/<set_id>/.
	ArtefactStateQuarantined ArtefactState = "quarantined"
	// ArtefactStatePurged means the quarantined file was hard-deleted by
	// the purge job. The row is retained for audit.
	ArtefactStatePurged ArtefactState = "purged"
	// ArtefactStateRestored means a previously-quarantined file was moved
	// back to its original path.
	ArtefactStateRestored ArtefactState = "restored"
)

// WriteIntent is the caller-supplied provenance envelope for a write. The
// forge (GAP-5), synth (GAP-8) and manifest plumbers set these fields before
// calling HostAdapter.WriteFile; other callers may pass an empty value and
// the host will auto-classify (see cpn/persist.Classify).
type WriteIntent struct {
	// ForgeRunID identifies the forge run that authored this write. May
	// be empty for writes outside a forge run.
	ForgeRunID string

	// SetID groups artefacts that must be rolled back atomically. Typically
	// the ID of a forge step or a group of related writes. When empty, the
	// adapter synthesises a per-write singleton set (one row, one set).
	SetID string

	// HostID identifies the host machine. Filled from the host-capability
	// registry; allowed to be empty for single-host deployments.
	HostID string

	// FlowHash mirrors the CPN flow identity.
	FlowHash string

	// AuthoringCPNID is the ID of the CPN that emitted the write.
	AuthoringCPNID string

	// TransitionID is the transition that fired the write.
	TransitionID string

	// SessionID is the user session that drove the write.
	SessionID string

	// Classification, when non-empty, overrides auto-classification.
	Classification ArtefactClassification

	// Actor is the email / identifier of the acting user. Empty for
	// non-interactive writes.
	Actor string
}

// Artefact is the persistence DTO for one authored file (spec §4).
type Artefact struct {
	ID             string
	Path           string
	Classification ArtefactClassification
	SetID          string
	ForgeRunID     string
	HostID         string
	FlowHash       string
	AuthoringCPNID string
	TransitionID   string
	SessionID      string
	SHA256         string
	Size           int64
	MIME           string
	Mode           uint32 // fs.FileMode cast to uint32 for the persistence boundary.
	State          ArtefactState
	CreatedAt      time.Time
	QuarantinedAt  *time.Time
	RestoredAt     *time.Time
	PurgedAt       *time.Time
	QuarantinePath string // populated when State == quarantined.
}

// ArtefactFilter constrains ListByHost queries.
type ArtefactFilter struct {
	ForgeRunID string
	SetID      string
	// ClassificationIn, when non-empty, keeps only rows whose
	// Classification matches any of the listed values.
	ClassificationIn []ArtefactClassification
	// State, when non-empty, keeps only rows in that lifecycle state.
	State ArtefactState
	Limit int // zero means "no limit".
	Offset int
}

// ArtefactEvent is a row in the artefact_events audit table (spec REQ-008).
type ArtefactEvent struct {
	EventID string
	SetID   string
	Kind    string // "rollback_started", "rollback_completed", "restore", "purge"
	Actor   string
	At      time.Time
}

// ── Errors ──────────────────────────────────────────────────────────────────

// ErrArtefactNotFound is returned when an artefact id or set id has no rows.
var ErrArtefactNotFound = errors.New("persist: artefact not found")

// ErrArtefactSetEmpty is returned when a rollback/restore targets an empty set.
var ErrArtefactSetEmpty = errors.New("persist: artefact set is empty")

// ErrArtefactAlreadyRolledBack is returned when a set is rolled back twice.
var ErrArtefactAlreadyRolledBack = errors.New("persist: artefact set already rolled back")

// ErrArtefactNotQuarantined is returned when Restore targets a set that is
// not currently in the quarantined state.
var ErrArtefactNotQuarantined = errors.New("persist: artefact set not quarantined")

// ── Port ────────────────────────────────────────────────────────────────────

// AuthoredArtefactLedger is the hexagonal port recording every authored
// artefact and orchestrating rollback / restore / purge operations against
// the metadata rows. Filesystem side-effects (moving files into quarantine,
// hard-deleting them) are owned by RollbackService / PurgeService — the
// ledger is metadata-only.
type AuthoredArtefactLedger interface {
	// PreWrite reserves a row for the incoming write and returns the new
	// artefact ID. The row is stored in state "pending" until PostWrite
	// settles the hash.
	PreWrite(ctx context.Context, intent WriteIntent, path string, mode uint32) (artefactID string, err error)

	// PostWrite records the hash, size and MIME of a successful write and
	// transitions the row from pending to active.
	PostWrite(ctx context.Context, artefactID, sha256 string, size int64, mime string) error

	// SetForForgeRun returns every artefact authored by the forge run.
	SetForForgeRun(ctx context.Context, forgeRunID string) ([]Artefact, error)

	// SetForSet returns every artefact sharing a set_id.
	SetForSet(ctx context.Context, setID string) ([]Artefact, error)

	// Rollback transitions every row in set_id from "active" to
	// "quarantined" (or re-quarantines a prior "restored" row) and records
	// QuarantinedAt + QuarantinePath. The caller is responsible for moving
	// the files on disk BEFORE calling Rollback, and for restoring on
	// failure.
	Rollback(ctx context.Context, setID string, quarantinePath string, actor string) error

	// Restore transitions every row in set_id from "quarantined" back to
	// "active" (state becomes "restored" to preserve audit). The caller
	// moves the files back on disk before calling Restore.
	Restore(ctx context.Context, setID, actor string) error

	// PurgeExpired hard-deletes every row whose State == quarantined and
	// QuarantinedAt is older than cutoff, returning the number of rows
	// purged. Rows move to state "purged" rather than being deleted from
	// the audit table; only the file on disk is removed by the caller.
	PurgeExpired(ctx context.Context, cutoff time.Time, actor string) (purged []Artefact, err error)

	// GetByID fetches a single artefact.
	GetByID(ctx context.Context, artefactID string) (Artefact, error)

	// ListByHost returns artefacts on the host matching the filter. HostID
	// may be empty to list across every host.
	ListByHost(ctx context.Context, hostID string, filter ArtefactFilter) ([]Artefact, error)

	// RecordEvent appends a row to artefact_events. Implementations SHOULD
	// call this from Rollback / Restore / PurgeExpired so the audit trail
	// stays in sync automatically; external callers may also use it for
	// ad-hoc admin actions.
	RecordEvent(ctx context.Context, ev ArtefactEvent) error

	// ListEvents returns the audit trail for a set_id in chronological
	// order.
	ListEvents(ctx context.Context, setID string) ([]ArtefactEvent, error)
}
