package persist

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ---- Sentinel Errors ----

func TestSentinelErrors(t *testing.T) {
	t.Run("Distinct", func(t *testing.T) {
		errs := []error{
			ErrSessionNotFound, ErrSessionExists, ErrSessionClosed,
			ErrFlowNotFound, ErrEventAppendFailed, ErrHITLNotFound,
			ErrHITLExpired, ErrLedgerNotFound, ErrInvalidInput,
		}
		for i := 0; i < len(errs); i++ {
			for j := i + 1; j < len(errs); j++ {
				if errors.Is(errs[i], errs[j]) {
					t.Errorf("sentinel %d and %d are equal", i, j)
				}
			}
		}
	})

	t.Run("WrapCorrectly", func(t *testing.T) {
		for _, sentinel := range []error{ErrSessionNotFound, ErrFlowNotFound} {
			if !errors.Is(sentinel, sentinel) {
				t.Errorf("errors.Is(%v, %v) should be true", sentinel, sentinel)
			}
		}
	})

	t.Run("NotEqualToCPNErrors", func(t *testing.T) {
		if errors.Is(ErrSessionClosed, cpn.ErrSessionClosed) {
			t.Error("persist.ErrSessionClosed should NOT equal cpn.ErrSessionClosed")
		}
	})
}

// ---- SessionRepository ----

func TestMemorySessionRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	makeSession := func(id, userID string) *SessionRecord {
		return &SessionRecord{
			ID:             id,
			UserID:         userID,
			Channel:        "web",
			State:          SessionActive,
			CreatedAt:      now,
			LastActivityAt: now,
		}
	}

	t.Run("Create", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.Create(ctx, makeSession("s1", "u1"))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("Create_duplicate", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		err := r.Create(ctx, makeSession("s1", "u1"))
		if !errors.Is(err, ErrSessionExists) {
			t.Fatalf("want ErrSessionExists, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		s, err := r.Get(ctx, "s1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if s.ID != "s1" || s.UserID != "u1" {
			t.Fatalf("unexpected session: %+v", s)
		}
	})

	t.Run("Get_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_, err := r.Get(ctx, "missing")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("GetByUserID", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		_ = r.Create(ctx, makeSession("s2", "u1"))
		_ = r.Create(ctx, makeSession("s3", "u2"))
		sessions, err := r.GetByUserID(ctx, "u1")
		if err != nil {
			t.Fatalf("GetByUserID: %v", err)
		}
		if len(sessions) != 2 {
			t.Fatalf("want 2 sessions, got %d", len(sessions))
		}
	})

	t.Run("AppendMessage", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		msg := &MessageRecord{ID: "m1", SessionID: "s1", Role: "user", Content: "hello"}
		err := r.AppendMessage(ctx, "s1", msg)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		s, _ := r.Get(ctx, "s1")
		if len(s.Messages) != 1 {
			t.Fatalf("want 1 message, got %d", len(s.Messages))
		}
	})

	t.Run("AppendMessage_closed", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		_ = r.Close(ctx, "s1")
		msg := &MessageRecord{ID: "m1", Role: "user", Content: "hello"}
		err := r.AppendMessage(ctx, "s1", msg)
		if !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("want ErrSessionClosed, got %v", err)
		}
	})

	t.Run("UpdateState", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		err := r.UpdateState(ctx, "s1", SessionExpired)
		if err != nil {
			t.Fatalf("UpdateState: %v", err)
		}
		s, _ := r.Get(ctx, "s1")
		if s.State != SessionExpired {
			t.Fatalf("want expired, got %s", s.State)
		}
	})

	t.Run("Touch", func(t *testing.T) {
		r := NewMemorySessionRepository()
		s := makeSession("s1", "u1")
		s.LastActivityAt = now.Add(-time.Hour)
		_ = r.Create(ctx, s)
		err := r.Touch(ctx, "s1")
		if err != nil {
			t.Fatalf("Touch: %v", err)
		}
		got, _ := r.Get(ctx, "s1")
		if got.LastActivityAt.Before(now) {
			t.Fatal("Touch did not update LastActivityAt")
		}
	})

	t.Run("Close", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		err := r.Close(ctx, "s1")
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		s, _ := r.Get(ctx, "s1")
		if s.State != SessionClosed {
			t.Fatalf("want closed, got %s", s.State)
		}
		if s.ClosedAt == nil {
			t.Fatal("ClosedAt should be set")
		}
	})

	t.Run("ListExpired", func(t *testing.T) {
		r := NewMemorySessionRepository()
		old := makeSession("s1", "u1")
		old.LastActivityAt = now.Add(-2 * time.Hour)
		_ = r.Create(ctx, old)
		fresh := makeSession("s2", "u2")
		fresh.LastActivityAt = now
		_ = r.Create(ctx, fresh)

		ids, err := r.ListExpired(ctx, now.Add(-time.Hour))
		if err != nil {
			t.Fatalf("ListExpired: %v", err)
		}
		if len(ids) != 1 || ids[0] != "s1" {
			t.Fatalf("want [s1], got %v", ids)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, makeSession("s1", "u1"))
		err := r.Delete(ctx, "s1")
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("want 0, got %d", r.Len())
		}
	})
}

// ---- EventRepository ----

func TestMemoryEventRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	makeEvent := func(id, sessionID, cpnID, typ string) *EventRecord {
		return &EventRecord{
			ID:        id,
			Type:      typ,
			SessionID: sessionID,
			CPNID:     cpnID,
			Timestamp: now,
		}
	}

	t.Run("Append_single", func(t *testing.T) {
		r := NewMemoryEventRepository()
		err := r.Append(ctx, makeEvent("e1", "s1", "c1", "transition_fired"))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("Append_batch", func(t *testing.T) {
		r := NewMemoryEventRepository()
		err := r.Append(ctx,
			makeEvent("e1", "s1", "c1", "t1"),
			makeEvent("e2", "s1", "c1", "t2"),
		)
		if err != nil {
			t.Fatalf("Append batch: %v", err)
		}
		if r.Len() != 2 {
			t.Fatalf("Len = %d, want 2", r.Len())
		}
	})

	t.Run("Append_nil", func(t *testing.T) {
		r := NewMemoryEventRepository()
		err := r.Append(ctx, nil)
		if !errors.Is(err, ErrEventAppendFailed) {
			t.Fatalf("want ErrEventAppendFailed, got %v", err)
		}
	})

	t.Run("QueryBySession", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, makeEvent("e1", "s1", "c1", "t1"))
		_ = r.Append(ctx, makeEvent("e2", "s2", "c1", "t1"))
		page, err := r.QueryBySession(ctx, "s1", nil)
		if err != nil {
			t.Fatalf("QueryBySession: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("want 1, got %d", len(page.Items))
		}
	})

	t.Run("QueryBySession_pagination", func(t *testing.T) {
		r := NewMemoryEventRepository()
		for i := 0; i < 5; i++ {
			_ = r.Append(ctx, makeEvent("e"+string(rune('0'+i)), "s1", "c1", "t1"))
		}
		opts := &EventQueryOpts{Limit: 2}
		page, err := r.QueryBySession(ctx, "s1", opts)
		if err != nil {
			t.Fatalf("QueryBySession: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("want 2 items, got %d", len(page.Items))
		}
		if !page.HasMore {
			t.Fatal("want HasMore=true")
		}
		if page.NextCursor == "" {
			t.Fatal("want non-empty NextCursor")
		}

		opts2 := &EventQueryOpts{Limit: 2, Cursor: page.NextCursor}
		page2, _ := r.QueryBySession(ctx, "s1", opts2)
		if len(page2.Items) != 2 {
			t.Fatalf("page2: want 2 items, got %d", len(page2.Items))
		}
	})

	t.Run("QueryByCPN", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, makeEvent("e1", "s1", "c1", "t1"))
		_ = r.Append(ctx, makeEvent("e2", "s1", "c2", "t1"))
		page, err := r.QueryByCPN(ctx, "c1", nil)
		if err != nil {
			t.Fatalf("QueryByCPN: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("want 1, got %d", len(page.Items))
		}
	})

	t.Run("QueryByType", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, makeEvent("e1", "s1", "c1", "transition_fired"))
		_ = r.Append(ctx, makeEvent("e2", "s1", "c1", "subnet_started"))
		from := now.Add(-time.Hour)
		to := now.Add(time.Hour)
		page, err := r.QueryByType(ctx, "transition_fired", from, to, nil)
		if err != nil {
			t.Fatalf("QueryByType: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("want 1, got %d", len(page.Items))
		}
	})

	t.Run("Count", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, makeEvent("e1", "s1", "c1", "t1"))
		_ = r.Append(ctx, makeEvent("e2", "s1", "c1", "t2"))
		count, err := r.Count(ctx, nil)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count != 2 {
			t.Fatalf("want 2, got %d", count)
		}
	})

	t.Run("Count_filtered", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, makeEvent("e1", "s1", "c1", "t1"))
		_ = r.Append(ctx, makeEvent("e2", "s2", "c1", "t1"))
		count, err := r.Count(ctx, &EventQueryOpts{SessionID: "s1"})
		if err != nil {
			t.Fatalf("Count filtered: %v", err)
		}
		if count != 1 {
			t.Fatalf("want 1, got %d", count)
		}
	})
}

// ---- LedgerRepository ----

func TestMemoryLedgerRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("Record_create", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		err := r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100, OutputTokens: 50, Calls: 1, TotalCostUSD: 0.01})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("Record_upsert", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100, Calls: 1, TotalCostUSD: 0.01})
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 200, Calls: 2, TotalCostUSD: 0.02})
		l, _ := r.GetBySession(ctx, "s1")
		if l.InputTokens != 300 {
			t.Fatalf("want 300 input tokens, got %d", l.InputTokens)
		}
		if l.Calls != 3 {
			t.Fatalf("want 3 calls, got %d", l.Calls)
		}
		if l.TotalCostUSD < 0.029 || l.TotalCostUSD > 0.031 {
			t.Fatalf("want ~0.03 cost, got %f", l.TotalCostUSD)
		}
	})

	t.Run("GetBySession", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100})
		l, err := r.GetBySession(ctx, "s1")
		if err != nil {
			t.Fatalf("GetBySession: %v", err)
		}
		if l.SessionID != "s1" {
			t.Fatalf("want s1, got %s", l.SessionID)
		}
	})

	t.Run("GetBySession_not_found", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_, err := r.GetBySession(ctx, "missing")
		if !errors.Is(err, ErrLedgerNotFound) {
			t.Fatalf("want ErrLedgerNotFound, got %v", err)
		}
	})

	t.Run("QueryByDate", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100})
		results, err := r.QueryByDate(ctx, now)
		if err != nil {
			t.Fatalf("QueryByDate: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("want 1, got %d", len(results))
		}
	})

	t.Run("SetDailyTotal", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100})
		err := r.SetDailyTotal(ctx, "s1", now, 1.50)
		if err != nil {
			t.Fatalf("SetDailyTotal: %v", err)
		}
		l, _ := r.GetBySession(ctx, "s1")
		if l.DailyTotalUSD != 1.50 {
			t.Fatalf("want 1.50, got %f", l.DailyTotalUSD)
		}
	})

	t.Run("Aggregate", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 100, OutputTokens: 50, Calls: 1, TotalCostUSD: 0.01})
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s2", InputTokens: 200, OutputTokens: 100, Calls: 2, TotalCostUSD: 0.02})
		from := now.Add(-time.Hour)
		to := now.Add(time.Hour)
		agg, err := r.AggregateByDateRange(ctx, from, to)
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if agg.TotalInputTokens != 300 {
			t.Fatalf("want 300, got %d", agg.TotalInputTokens)
		}
		if agg.TotalCalls != 3 {
			t.Fatalf("want 3, got %d", agg.TotalCalls)
		}
	})
}

// ---- FlowRepository ----

func TestMemoryFlowRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	makeFlow := func(hash, role string) *FlowRecord {
		return &FlowRecord{
			Hash:         hash,
			Role:         role,
			TopologyJSON: json.RawMessage(`{"places":[]}`),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
	}

	t.Run("Save", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		err := r.Save(ctx, makeFlow("h1", "coordinator"))
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("GetByHash", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		f, err := r.GetByHash(ctx, "h1")
		if err != nil {
			t.Fatalf("GetByHash: %v", err)
		}
		if f.Hash != "h1" {
			t.Fatalf("want h1, got %s", f.Hash)
		}
	})

	t.Run("GetByHash_not_found", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_, err := r.GetByHash(ctx, "missing")
		if !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("want ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("GetByHash_soft_deleted", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		_ = r.Delete(ctx, "h1")
		_, err := r.GetByHash(ctx, "h1")
		if !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("want ErrFlowNotFound for soft-deleted, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		_ = r.Save(ctx, makeFlow("h2", "worker"))
		page, err := r.List(ctx, nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("want 2, got %d", len(page.Items))
		}
	})

	t.Run("List_pagination", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		for i := 0; i < 5; i++ {
			f := makeFlow("h"+string(rune('a'+i)), "r")
			f.CreatedAt = now.Add(time.Duration(i) * time.Minute)
			_ = r.Save(ctx, f)
		}
		opts := &FlowListOpts{Limit: 2}
		page, err := r.List(ctx, opts)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("want 2, got %d", len(page.Items))
		}
		if !page.HasMore {
			t.Fatal("want HasMore=true")
		}
	})

	t.Run("List_by_role", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		_ = r.Save(ctx, makeFlow("h2", "worker"))
		page, err := r.List(ctx, &FlowListOpts{Role: "coordinator"})
		if err != nil {
			t.Fatalf("List by role: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("want 1, got %d", len(page.Items))
		}
	})

	t.Run("UpdateStats", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		stats := &FlowStats{ExecutionCount: 10, SuccessRate: 0.9}
		err := r.UpdateStats(ctx, "h1", stats)
		if err != nil {
			t.Fatalf("UpdateStats: %v", err)
		}
		f, _ := r.GetByHash(ctx, "h1")
		if f.Stats.ExecutionCount != 10 {
			t.Fatalf("want 10, got %d", f.Stats.ExecutionCount)
		}
	})

	t.Run("Delete_soft", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, makeFlow("h1", "coordinator"))
		err := r.Delete(ctx, "h1")
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		// Still in storage (soft delete)
		if r.Len() != 1 {
			t.Fatalf("want 1 (soft deleted), got %d", r.Len())
		}
		// But not findable
		_, err = r.GetByHash(ctx, "h1")
		if !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("want ErrFlowNotFound, got %v", err)
		}
	})
}

// ---- IntelligenceRepository ----

func TestMemoryIntelligenceRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	makeExec := func(id, role string, success bool) *ExecutionRecord {
		return &ExecutionRecord{
			ID:           id,
			CPNID:        "cpn-" + id,
			CPNRole:      role,
			SessionID:    "s1",
			LLMCalls:     5,
			ToolCalls:    3,
			DurationMs:   1000,
			Success:      success,
			StartedAt:    now,
			CompletedAt:  now.Add(time.Second),
			TotalCostUSD: 0.05,
		}
	}

	t.Run("RecordExecution", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		err := r.RecordExecution(ctx, makeExec("e1", "coordinator", true))
		if err != nil {
			t.Fatalf("RecordExecution: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("RecordExecution_with_depth", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		rec := makeExec("e1", "worker", true)
		rec.CPNDepth = 2
		rec.TokensProduced = 150
		err := r.RecordExecution(ctx, rec)
		if err != nil {
			t.Fatalf("RecordExecution: %v", err)
		}
		results, _ := r.QueryByRole(ctx, "worker", now.Add(-time.Hour), now.Add(time.Hour))
		if len(results) != 1 {
			t.Fatalf("want 1, got %d", len(results))
		}
		if results[0].CPNDepth != 2 {
			t.Fatalf("want depth 2, got %d", results[0].CPNDepth)
		}
		if results[0].TokensProduced != 150 {
			t.Fatalf("want 150 tokens, got %d", results[0].TokensProduced)
		}
	})

	t.Run("QueryByRole", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		_ = r.RecordExecution(ctx, makeExec("e1", "coordinator", true))
		_ = r.RecordExecution(ctx, makeExec("e2", "worker", true))
		from := now.Add(-time.Hour)
		to := now.Add(time.Hour)
		results, err := r.QueryByRole(ctx, "coordinator", from, to)
		if err != nil {
			t.Fatalf("QueryByRole: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("want 1, got %d", len(results))
		}
	})

	t.Run("Aggregate", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		_ = r.RecordExecution(ctx, makeExec("e1", "coordinator", true))
		rec2 := makeExec("e2", "coordinator", false)
		rec2.LLMCalls = 3
		rec2.ToolCalls = 1
		_ = r.RecordExecution(ctx, rec2)
		from := now.Add(-time.Hour)
		to := now.Add(time.Hour)
		m, err := r.Aggregate(ctx, "coordinator", from, to)
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if m.ExecutionCount != 2 {
			t.Fatalf("want 2, got %d", m.ExecutionCount)
		}
		if m.SuccessRate != 0.5 {
			t.Fatalf("want 0.5 success rate, got %f", m.SuccessRate)
		}
		if m.AvgLLMCalls != 4.0 {
			t.Fatalf("want 4.0 avg LLM calls, got %f", m.AvgLLMCalls)
		}
	})

	t.Run("Aggregate_empty", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		from := now.Add(-time.Hour)
		to := now.Add(time.Hour)
		m, err := r.Aggregate(ctx, "nonexistent", from, to)
		if err != nil {
			t.Fatalf("Aggregate empty: %v", err)
		}
		if m.ExecutionCount != 0 {
			t.Fatalf("want 0, got %d", m.ExecutionCount)
		}
	})

	t.Run("TopFlows", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		for i := 0; i < 3; i++ {
			rec := makeExec("e"+string(rune('1'+i)), "coordinator", true)
			rec.CPNID = "flow-a"
			_ = r.RecordExecution(ctx, rec)
		}
		rec := makeExec("e4", "worker", false)
		rec.CPNID = "flow-b"
		_ = r.RecordExecution(ctx, rec)

		flows, err := r.TopFlows(ctx, 2)
		if err != nil {
			t.Fatalf("TopFlows: %v", err)
		}
		if len(flows) != 2 {
			t.Fatalf("want 2, got %d", len(flows))
		}
		if flows[0].Hash != "flow-a" {
			t.Fatalf("want flow-a as top, got %s", flows[0].Hash)
		}
	})
}

// ---- HITLRepository ----

func TestMemoryHITLRepository(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	makeReq := func(sid, tid string) *HITLPendingRequest {
		return &HITLPendingRequest{
			SessionID:    sid,
			TransitionID: tid,
			CPNID:        "cpn1",
			CPNRole:      "coordinator",
			Proposal:     json.RawMessage(`{"action":"approve"}`),
			CreatedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
		}
	}

	t.Run("Enqueue", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		err := r.Enqueue(ctx, makeReq("s1", "t1"))
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("Len = %d, want 1", r.Len())
		}
	})

	t.Run("Dequeue", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		_ = r.Enqueue(ctx, makeReq("s1", "t1"))
		req, err := r.Dequeue(ctx, "s1", "t1")
		if err != nil {
			t.Fatalf("Dequeue: %v", err)
		}
		if req.SessionID != "s1" || req.TransitionID != "t1" {
			t.Fatalf("unexpected: %+v", req)
		}
		if r.Len() != 0 {
			t.Fatalf("Len = %d, want 0 after dequeue", r.Len())
		}
	})

	t.Run("Dequeue_not_found", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		_, err := r.Dequeue(ctx, "missing", "missing")
		if !errors.Is(err, ErrHITLNotFound) {
			t.Fatalf("want ErrHITLNotFound, got %v", err)
		}
	})

	t.Run("ListPending", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		_ = r.Enqueue(ctx, makeReq("s1", "t1"))
		_ = r.Enqueue(ctx, makeReq("s1", "t2"))
		_ = r.Enqueue(ctx, makeReq("s2", "t3"))
		pending, err := r.ListPending(ctx, "s1")
		if err != nil {
			t.Fatalf("ListPending: %v", err)
		}
		if len(pending) != 2 {
			t.Fatalf("want 2, got %d", len(pending))
		}
	})

	t.Run("Expire", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		old := makeReq("s1", "t1")
		old.ExpiresAt = now.Add(-2 * time.Hour)
		_ = r.Enqueue(ctx, old)
		fresh := makeReq("s1", "t2")
		fresh.ExpiresAt = now.Add(time.Hour)
		_ = r.Enqueue(ctx, fresh)

		removed, err := r.Expire(ctx, time.Hour)
		if err != nil {
			t.Fatalf("Expire: %v", err)
		}
		if removed != 1 {
			t.Fatalf("want 1 removed, got %d", removed)
		}
		if r.Len() != 1 {
			t.Fatalf("want 1 remaining, got %d", r.Len())
		}
	})
}

// ---- Concurrent Access ----

func TestConcurrent_AllRepositories(t *testing.T) {
	ctx := context.Background()
	const goroutines = 10

	t.Run("SessionRepository", func(t *testing.T) {
		r := NewMemorySessionRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id := "s" + string(rune('a'+i))
				_ = r.Create(ctx, &SessionRecord{ID: id, UserID: "u1", State: SessionActive, LastActivityAt: time.Now()})
				_, _ = r.Get(ctx, id)
				_ = r.Touch(ctx, id)
			}(i)
		}
		wg.Wait()
	})

	t.Run("EventRepository", func(t *testing.T) {
		r := NewMemoryEventRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = r.Append(ctx, &EventRecord{ID: "e" + string(rune('a'+i)), SessionID: "s1", Type: "t1", Timestamp: time.Now()})
				_, _ = r.QueryBySession(ctx, "s1", nil)
			}(i)
		}
		wg.Wait()
	})

	t.Run("LedgerRepository", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 10, Calls: 1})
				_, _ = r.GetBySession(ctx, "s1")
			}(i)
		}
		wg.Wait()
	})

	t.Run("FlowRepository", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				hash := "h" + string(rune('a'+i))
				_ = r.Save(ctx, &FlowRecord{Hash: hash, Role: "r", CreatedAt: time.Now(), UpdatedAt: time.Now()})
				_, _ = r.GetByHash(ctx, hash)
			}(i)
		}
		wg.Wait()
	})

	t.Run("IntelligenceRepository", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = r.RecordExecution(ctx, &ExecutionRecord{ID: "e" + string(rune('a'+i)), CPNRole: "r", StartedAt: time.Now()})
				_, _ = r.TopFlows(ctx, 5)
			}(i)
		}
		wg.Wait()
	})

	t.Run("HITLRepository", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = r.Enqueue(ctx, &HITLPendingRequest{SessionID: "s1", TransitionID: "t" + string(rune('a'+i)), ExpiresAt: time.Now().Add(time.Hour)})
				_, _ = r.ListPending(ctx, "s1")
			}(i)
		}
		wg.Wait()
	})
}

// ---- DTO Conversion ----

func TestEventToRecord(t *testing.T) {
	now := time.Now()

	t.Run("WithToken", func(t *testing.T) {
		e := &cpn.Event{
			ID:        "e1",
			Type:      cpn.EventTransitionFired,
			SessionID: "s1",
			CPNID:     "c1",
			CPNDepth:  1,
			Token: &cpn.Token{
				Color:   cpn.ColorString,
				Payload: "hello",
			},
			Timestamp: now,
		}
		rec, err := EventToRecord("s1", e)
		if err != nil {
			t.Fatalf("EventToRecord: %v", err)
		}
		if rec.TokenSnapshot == nil {
			t.Fatal("want non-nil TokenSnapshot")
		}
		if rec.Type != string(cpn.EventTransitionFired) {
			t.Fatalf("want %s, got %s", cpn.EventTransitionFired, rec.Type)
		}
	})

	t.Run("WithPayload", func(t *testing.T) {
		e := &cpn.Event{
			ID:      "e1",
			Payload: map[string]string{"key": "value"},
		}
		rec, err := EventToRecord("s1", e)
		if err != nil {
			t.Fatalf("EventToRecord: %v", err)
		}
		if rec.Payload == nil {
			t.Fatal("want non-nil Payload")
		}
	})

	t.Run("NilToken", func(t *testing.T) {
		e := &cpn.Event{ID: "e1", Token: nil}
		rec, err := EventToRecord("s1", e)
		if err != nil {
			t.Fatalf("EventToRecord: %v", err)
		}
		if rec.TokenSnapshot != nil {
			t.Fatal("want nil TokenSnapshot for nil Token")
		}
	})

	t.Run("NilPayload", func(t *testing.T) {
		e := &cpn.Event{ID: "e1", Payload: nil}
		rec, err := EventToRecord("s1", e)
		if err != nil {
			t.Fatalf("EventToRecord: %v", err)
		}
		if rec.Payload != nil {
			t.Fatal("want nil Payload for nil Payload")
		}
	})

	t.Run("SessionIDFallback", func(t *testing.T) {
		e := &cpn.Event{ID: "e1", SessionID: "from-event"}
		rec, err := EventToRecord("fallback", e)
		if err != nil {
			t.Fatalf("EventToRecord: %v", err)
		}
		if rec.SessionID != "from-event" {
			t.Fatalf("want from-event, got %s", rec.SessionID)
		}

		e2 := &cpn.Event{ID: "e2", SessionID: ""}
		rec2, _ := EventToRecord("fallback", e2)
		if rec2.SessionID != "fallback" {
			t.Fatalf("want fallback, got %s", rec2.SessionID)
		}
	})
}

func TestMessageToRecord(t *testing.T) {
	now := time.Now()

	t.Run("Basic", func(t *testing.T) {
		m := &cpn.Message{
			ID:        "m1",
			Role:      cpn.RoleUser,
			Content:   "hello",
			CPNID:     "c1",
			CPNRole:   "coordinator",
			CPNDepth:  0,
			Timestamp: now,
		}
		rec := MessageToRecord("s1", m)
		if rec.ID != "m1" {
			t.Fatalf("want m1, got %s", rec.ID)
		}
		if rec.Role != string(cpn.RoleUser) {
			t.Fatalf("want %s, got %s", cpn.RoleUser, rec.Role)
		}
		if rec.Content != "hello" {
			t.Fatalf("want hello, got %s", rec.Content)
		}
	})

	t.Run("SessionIDInjection", func(t *testing.T) {
		m := &cpn.Message{ID: "m1", Role: cpn.RoleAssistant}
		rec := MessageToRecord("injected-session", m)
		if rec.SessionID != "injected-session" {
			t.Fatalf("want injected-session, got %s", rec.SessionID)
		}
	})
}

// ---- Context Cancellation ----

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("Session", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.Create(ctx, &SessionRecord{ID: "s1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("Event", func(t *testing.T) {
		r := NewMemoryEventRepository()
		err := r.Append(ctx, &EventRecord{ID: "e1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("Ledger", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		err := r.Record(ctx, &LedgerRecord{SessionID: "s1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("Flow", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		err := r.Save(ctx, &FlowRecord{Hash: "h1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("Intelligence", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		err := r.RecordExecution(ctx, &ExecutionRecord{ID: "e1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("HITL", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		err := r.Enqueue(ctx, &HITLPendingRequest{SessionID: "s1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})
}

// ---- Reset & Len helpers ----

func TestResetHelpers(t *testing.T) {
	ctx := context.Background()

	t.Run("SessionRepository_Reset", func(t *testing.T) {
		r := NewMemorySessionRepository()
		_ = r.Create(ctx, &SessionRecord{ID: "s1", UserID: "u1", State: SessionActive, LastActivityAt: time.Now()})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})

	t.Run("EventRepository_Reset", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, &EventRecord{ID: "e1", Type: "t1", Timestamp: time.Now()})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})

	t.Run("LedgerRepository_Reset", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		_ = r.Record(ctx, &LedgerRecord{SessionID: "s1", InputTokens: 10})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})

	t.Run("FlowRepository_Reset", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, &FlowRecord{Hash: "h1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})

	t.Run("IntelligenceRepository_Reset", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		_ = r.RecordExecution(ctx, &ExecutionRecord{ID: "e1", CPNRole: "r", StartedAt: time.Now()})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})

	t.Run("HITLRepository_Reset", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		_ = r.Enqueue(ctx, &HITLPendingRequest{SessionID: "s1", TransitionID: "t1", ExpiresAt: time.Now().Add(time.Hour)})
		r.Reset()
		if r.Len() != 0 {
			t.Fatalf("want 0 after reset, got %d", r.Len())
		}
	})
}

// ---- Edge Cases ----

func TestEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("Append_empty_variadic", func(t *testing.T) {
		r := NewMemoryEventRepository()
		err := r.Append(ctx)
		if err != nil {
			t.Fatalf("Append empty should not error, got %v", err)
		}
	})

	t.Run("QueryBySession_empty", func(t *testing.T) {
		r := NewMemoryEventRepository()
		page, err := r.QueryBySession(ctx, "nonexistent", nil)
		if err != nil {
			t.Fatalf("QueryBySession empty: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("want 0 items, got %d", len(page.Items))
		}
	})

	t.Run("Pagination_past_end", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, &EventRecord{ID: "e1", SessionID: "s1", Timestamp: time.Now()})
		opts := &EventQueryOpts{Cursor: "999"}
		page, err := r.QueryBySession(ctx, "s1", opts)
		if err != nil {
			t.Fatalf("pagination past end: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("want 0 items, got %d", len(page.Items))
		}
	})

	t.Run("Flow_List_empty", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		page, err := r.List(ctx, nil)
		if err != nil {
			t.Fatalf("List empty: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("want 0, got %d", len(page.Items))
		}
	})

	t.Run("Flow_UpdateStats_not_found", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		err := r.UpdateStats(ctx, "missing", &FlowStats{})
		if !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("want ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("Flow_Delete_not_found", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		err := r.Delete(ctx, "missing")
		if !errors.Is(err, ErrFlowNotFound) {
			t.Fatalf("want ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("Session_Delete_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.Delete(ctx, "missing")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Session_AppendMessage_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.AppendMessage(ctx, "missing", &MessageRecord{ID: "m1"})
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Session_UpdateState_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.UpdateState(ctx, "missing", SessionClosed)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Session_Touch_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.Touch(ctx, "missing")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Session_Close_not_found", func(t *testing.T) {
		r := NewMemorySessionRepository()
		err := r.Close(ctx, "missing")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Ledger_SetDailyTotal_not_found", func(t *testing.T) {
		r := NewMemoryLedgerRepository()
		err := r.SetDailyTotal(ctx, "missing", time.Now(), 1.0)
		if !errors.Is(err, ErrLedgerNotFound) {
			t.Fatalf("want ErrLedgerNotFound, got %v", err)
		}
	})

	t.Run("Session_GetByUserID_empty", func(t *testing.T) {
		r := NewMemorySessionRepository()
		sessions, err := r.GetByUserID(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetByUserID empty: %v", err)
		}
		if len(sessions) != 0 {
			t.Fatalf("want 0, got %d", len(sessions))
		}
	})

	t.Run("HITL_ListPending_empty", func(t *testing.T) {
		r := NewMemoryHITLRepository()
		pending, err := r.ListPending(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("ListPending empty: %v", err)
		}
		if len(pending) != 0 {
			t.Fatalf("want 0, got %d", len(pending))
		}
	})

	t.Run("TopFlows_n_zero", func(t *testing.T) {
		r := NewMemoryIntelligenceRepository()
		flows, err := r.TopFlows(ctx, 0)
		if err != nil {
			t.Fatalf("TopFlows n=0: %v", err)
		}
		if len(flows) != 0 {
			t.Fatalf("want 0, got %d", len(flows))
		}
	})

	t.Run("Session_ListExpired_empty", func(t *testing.T) {
		r := NewMemorySessionRepository()
		ids, err := r.ListExpired(ctx, time.Now())
		if err != nil {
			t.Fatalf("ListExpired empty: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("want 0, got %d", len(ids))
		}
	})

	t.Run("Flow_List_pagination_past_end", func(t *testing.T) {
		r := NewMemoryFlowRepository()
		_ = r.Save(ctx, &FlowRecord{Hash: "h1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
		page, err := r.List(ctx, &FlowListOpts{Cursor: "999"})
		if err != nil {
			t.Fatalf("List past end: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("want 0, got %d", len(page.Items))
		}
	})

	t.Run("Event_Count_nil_opts", func(t *testing.T) {
		r := NewMemoryEventRepository()
		_ = r.Append(ctx, &EventRecord{ID: "e1", Timestamp: time.Now()})
		count, _ := r.Count(ctx, nil)
		if count != 1 {
			t.Fatalf("want 1, got %d", count)
		}
	})
}
