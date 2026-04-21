package toolapproval

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryApprovalStore_RecordAndLookup(t *testing.T) {
	s := NewMemoryApprovalStore()
	ctx := context.Background()
	a := Approval{
		SessionID:        "sess-1",
		ToolName:         "rg",
		ProvenanceSHA256: "sha-A",
		ApprovedAt:       time.Now(),
	}
	if err := s.Record(ctx, a); err != nil {
		t.Fatalf("Record: %v", err)
	}
	d, err := s.Lookup(ctx, "sess-1", "rg", "sha-A")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if d != DecisionApproved {
		t.Fatalf("want approved, got %v", d)
	}
}

func TestMemoryApprovalStore_UnknownReturnsUnknown(t *testing.T) {
	s := NewMemoryApprovalStore()
	d, err := s.Lookup(context.Background(), "sess-1", "rg", "sha-A")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if d != DecisionUnknown {
		t.Fatalf("want unknown, got %v", d)
	}
}

func TestMemoryApprovalStore_InvalidKey(t *testing.T) {
	s := NewMemoryApprovalStore()
	if err := s.Record(context.Background(), Approval{ToolName: "rg", ProvenanceSHA256: "sha"}); err == nil {
		t.Fatal("want ErrInvalidKey on empty session")
	}
	if _, err := s.Lookup(context.Background(), "", "rg", "sha"); err == nil {
		t.Fatal("want ErrInvalidKey on empty session lookup")
	}
	if _, err := s.ListForSession(context.Background(), ""); err == nil {
		t.Fatal("want ErrInvalidKey on empty session list")
	}
}

func TestMemoryApprovalStore_ConcurrentSafe(t *testing.T) {
	s := NewMemoryApprovalStore()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = s.Record(ctx, Approval{SessionID: "sess", ToolName: "rg", ProvenanceSHA256: "sha", ApprovedAt: time.Now()})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = s.Lookup(ctx, "sess", "rg", "sha")
		}()
	}
	wg.Wait()
}

func TestMemoryApprovalStore_ProvenanceDrift_DoesNotCarryApproval(t *testing.T) {
	s := NewMemoryApprovalStore()
	ctx := context.Background()
	_ = s.Record(ctx, Approval{SessionID: "sess", ToolName: "rg", ProvenanceSHA256: "sha-A", ApprovedAt: time.Now()})
	d, _ := s.Lookup(ctx, "sess", "rg", "sha-B")
	if d != DecisionUnknown {
		t.Fatalf("drift MUST miss lookup; got %v", d)
	}
}

func TestMemoryApprovalStore_ListForSession(t *testing.T) {
	s := NewMemoryApprovalStore()
	ctx := context.Background()
	_ = s.Record(ctx, Approval{SessionID: "s1", ToolName: "rg", ProvenanceSHA256: "a", ApprovedAt: time.Now()})
	_ = s.Record(ctx, Approval{SessionID: "s1", ToolName: "fd", ProvenanceSHA256: "b", ApprovedAt: time.Now()})
	_ = s.Record(ctx, Approval{SessionID: "s2", ToolName: "rg", ProvenanceSHA256: "c", ApprovedAt: time.Now()})
	got, err := s.ListForSession(ctx, "s1")
	if err != nil {
		t.Fatalf("ListForSession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if got[0].ToolName != "fd" || got[1].ToolName != "rg" {
		t.Fatalf("want sorted by tool name, got %v", got)
	}
}
