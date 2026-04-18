//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres"
)

// TestFirstRunStore_RecordAndApprove covers the idempotent insert +
// approve round-trip on the real schema.
func TestFirstRunStore_RecordAndApprove(t *testing.T) {
	infra := setupInfra(t)
	fr := postgres.NewFirstRunStore(infra.pool)
	ctx := context.Background()

	entry, err := fr.Record(ctx, "host-1", "/usr/bin/gcc", "sha-001")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if entry == nil || entry.BinarySHA256 != "sha-001" {
		t.Fatalf("expected entry with sha-001, got %+v", entry)
	}

	entry2, err := fr.Record(ctx, "host-1", "/usr/bin/gcc", "sha-001")
	if err != nil {
		t.Fatalf("re-record: %v", err)
	}
	if entry2.ID != entry.ID {
		t.Errorf("expected same ID, got %s != %s", entry2.ID, entry.ID)
	}

	if err := fr.Approve(ctx, "host-1", "sha-001", "admin@example.com"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, err := fr.GetBySHA(ctx, "host-1", "sha-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.FirstApprovedAt == nil || got.FirstApprovedBy != "admin@example.com" {
		t.Fatalf("approve did not stamp row: %+v", got)
	}

	if err := fr.Revoke(ctx, "host-1", "sha-001"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ = fr.GetBySHA(ctx, "host-1", "sha-001")
	if !got.Revoked {
		t.Errorf("expected revoked=true, got %+v", got)
	}

	rows, err := fr.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one row")
	}
}

// TestGateDecisionsStore covers insert + list on the audit table.
func TestGateDecisionsStore(t *testing.T) {
	infra := setupInfra(t)
	dec := postgres.NewGateDecisionsStore(infra.pool)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		rec := &persist.GateDecisionRecord{
			SessionID:   "s-1",
			OpKind:      "exec",
			CommandHash: "hash-xyz",
			Path:        "",
			Sandbox:     "readonly",
			Decision:    "allow",
			Reason:      "test",
			RiskBand:    "safe",
			DecidedAt:   time.Now().UTC(),
		}
		if err := dec.Insert(ctx, rec); err != nil {
			t.Fatalf("insert[%d]: %v", i, err)
		}
	}

	rows, err := dec.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("expected ≥3 rows, got %d", len(rows))
	}
}
