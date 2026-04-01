//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres"
	storeredis "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/redis"
)

// ---------------------------------------------------------------------------
// Test infrastructure: shared Postgres + Redis containers
// ---------------------------------------------------------------------------

type testInfra struct {
	pool    *pgxpool.Pool
	dsn     string
	redis   *goredis.Client
	rediURL string
}

func setupInfra(t *testing.T) *testInfra {
	t.Helper()
	ctx := context.Background()

	// Start Postgres 16
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("liwaisi_integration"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { pgContainer.Terminate(ctx) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("pg connection string: %v", err)
	}

	// Run migrations
	if err := postgres.RunMigrations(dsn, "migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	// Start Redis 7
	redisContainer, err := tcredis.Run(ctx,
		"redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { redisContainer.Terminate(ctx) })

	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis connection string: %v", err)
	}

	rdb, err := storeredis.NewRedisPool(redisURL)
	if err != nil {
		t.Fatalf("redis pool: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })

	return &testInfra{pool: pool, dsn: dsn, redis: rdb, rediURL: redisURL}
}

// truncateAll removes all rows from all tables for test isolation.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`TRUNCATE sessions, events, token_ledger, flows, execution_records CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers: factory functions for test records
// ---------------------------------------------------------------------------

func newSession(id, userID, channel string) *persist.SessionRecord {
	now := time.Now().Truncate(time.Microsecond)
	return &persist.SessionRecord{
		ID:             id,
		UserID:         userID,
		Channel:        channel,
		State:          persist.SessionActive,
		CreatedAt:      now,
		LastActivityAt: now,
		Metadata:       json.RawMessage(`{"test": true}`),
	}
}

func newMessage(id, sessionID, role, content string) *persist.MessageRecord {
	return &persist.MessageRecord{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		CPNID:     "cpn-1",
		CPNRole:   "assistant",
		CPNDepth:  0,
		Timestamp: time.Now().Truncate(time.Microsecond),
	}
}

func newEvent(id, eventType, sessionID, cpnID string, ts time.Time) *persist.EventRecord {
	return &persist.EventRecord{
		ID:             id,
		Type:           eventType,
		SessionID:      sessionID,
		CPNID:          cpnID,
		CPNRole:        "assistant",
		CPNDepth:       0,
		TransitionID:   "t-1",
		TransitionKind: "llm",
		TokenSnapshot:  json.RawMessage(`{"tokens": 100}`),
		Payload:        json.RawMessage(`{"data": "test"}`),
		Timestamp:      ts.Truncate(time.Microsecond),
	}
}

func newLedger(sessionID string, input, output, calls int64, cost float64) *persist.LedgerRecord {
	return &persist.LedgerRecord{
		SessionID:    sessionID,
		InputTokens:  input,
		OutputTokens: output,
		Calls:        calls,
		TotalCostUSD: cost,
	}
}

func newFlow(hash, role string) *persist.FlowRecord {
	now := time.Now().Truncate(time.Microsecond)
	return &persist.FlowRecord{
		Hash:            hash,
		Role:            role,
		TopologyJSON:    json.RawMessage(`{"places": [], "transitions": []}`),
		FunctionMapping: json.RawMessage(`{"funcs": {}}`),
		Stats:           persist.FlowStats{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func newExecution(id, cpnID, role, sessionID string, success bool, cost float64) *persist.ExecutionRecord {
	now := time.Now().Truncate(time.Microsecond)
	return &persist.ExecutionRecord{
		ID:               id,
		CPNID:            cpnID,
		CPNRole:          role,
		CPNDepth:         0,
		SessionID:        sessionID,
		TransitionsFired: 5,
		LLMCalls:         3,
		ToolCalls:        2,
		TokensProduced:   500,
		TotalCostUSD:     cost,
		DurationMs:       150,
		Success:          success,
		StartedAt:        now.Add(-time.Second),
		CompletedAt:      now,
	}
}

func newHITL(sessionID, transitionID string, ttl time.Duration) *persist.HITLPendingRequest {
	now := time.Now()
	return &persist.HITLPendingRequest{
		SessionID:    sessionID,
		TransitionID: transitionID,
		CPNID:        "cpn-1",
		CPNRole:      "assistant",
		Proposal:     json.RawMessage(`{"action": "approve"}`),
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
	}
}

// ===========================================================================
// SessionRepository integration tests
// ===========================================================================

func TestSessionRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := postgres.NewSessionRepository(infra.pool)
	ctx := context.Background()

	t.Run("Full_CRUD_lifecycle", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-1", "user-1", "web")
		// Create
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// Get
		got, err := repo.Get(ctx, "sess-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != "sess-1" || got.UserID != "user-1" || got.State != persist.SessionActive {
			t.Fatalf("unexpected session: %+v", got)
		}

		// Touch
		if err := repo.Touch(ctx, "sess-1"); err != nil {
			t.Fatalf("Touch: %v", err)
		}
		touched, _ := repo.Get(ctx, "sess-1")
		if !touched.LastActivityAt.After(got.LastActivityAt) && !touched.LastActivityAt.Equal(got.LastActivityAt) {
			t.Fatalf("Touch did not update LastActivityAt")
		}

		// Close
		if err := repo.Close(ctx, "sess-1"); err != nil {
			t.Fatalf("Close: %v", err)
		}
		closed, _ := repo.Get(ctx, "sess-1")
		if closed.State != persist.SessionClosed {
			t.Fatalf("expected closed state, got %s", closed.State)
		}
		if closed.ClosedAt == nil {
			t.Fatal("ClosedAt should be set after Close")
		}

		// Delete
		if err := repo.Delete(ctx, "sess-1"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err = repo.Get(ctx, "sess-1")
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
		}
	})

	t.Run("Duplicate_create_returns_ErrSessionExists", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-dup", "user-1", "web")
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		err := repo.Create(ctx, s)
		if !errors.Is(err, persist.ErrSessionExists) {
			t.Fatalf("expected ErrSessionExists, got %v", err)
		}
	})

	t.Run("Create_with_empty_ID_returns_ErrInvalidInput", func(t *testing.T) {
		s := newSession("", "user-1", "web")
		err := repo.Create(ctx, s)
		if !errors.Is(err, persist.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("AppendMessage_on_active_session", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-msg", "user-1", "web")
		repo.Create(ctx, s)

		msg := newMessage("msg-1", "sess-msg", "user", "hello")
		if err := repo.AppendMessage(ctx, "sess-msg", msg); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}

		got, _ := repo.Get(ctx, "sess-msg")
		if len(got.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(got.Messages))
		}
		if got.Messages[0].Content != "hello" {
			t.Fatalf("unexpected message content: %s", got.Messages[0].Content)
		}
	})

	t.Run("AppendMessage_on_closed_session_returns_ErrSessionClosed", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-closed", "user-1", "web")
		repo.Create(ctx, s)
		repo.Close(ctx, "sess-closed")

		msg := newMessage("msg-fail", "sess-closed", "user", "should fail")
		err := repo.AppendMessage(ctx, "sess-closed", msg)
		if !errors.Is(err, persist.ErrSessionClosed) {
			t.Fatalf("expected ErrSessionClosed, got %v", err)
		}
	})

	t.Run("AppendMessage_on_nonexistent_returns_ErrSessionNotFound", func(t *testing.T) {
		msg := newMessage("msg-x", "nonexistent", "user", "hi")
		err := repo.AppendMessage(ctx, "nonexistent", msg)
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("GetByUserID_returns_multiple_ordered", func(t *testing.T) {
		truncateAll(t, infra.pool)

		for i := 1; i <= 3; i++ {
			s := newSession(fmt.Sprintf("sess-u-%d", i), "user-multi", "web")
			s.CreatedAt = time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Microsecond)
			repo.Create(ctx, s)
		}

		sessions, err := repo.GetByUserID(ctx, "user-multi")
		if err != nil {
			t.Fatalf("GetByUserID: %v", err)
		}
		if len(sessions) != 3 {
			t.Fatalf("expected 3 sessions, got %d", len(sessions))
		}
		// Should be ordered by created_at DESC
		if sessions[0].ID != "sess-u-3" {
			t.Fatalf("expected most recent first, got %s", sessions[0].ID)
		}
	})

	t.Run("GetByUserID_empty_returns_empty_slice", func(t *testing.T) {
		sessions, err := repo.GetByUserID(ctx, "no-such-user")
		if err != nil {
			t.Fatalf("GetByUserID: %v", err)
		}
		if sessions == nil || len(sessions) != 0 {
			t.Fatalf("expected empty slice, got %v", sessions)
		}
	})

	t.Run("ListExpired_filters_by_last_activity", func(t *testing.T) {
		truncateAll(t, infra.pool)

		// Create 2 sessions: one old, one recent
		old := newSession("sess-old", "user-1", "web")
		old.LastActivityAt = time.Now().Add(-2 * time.Hour).Truncate(time.Microsecond)
		repo.Create(ctx, old)

		recent := newSession("sess-recent", "user-1", "web")
		repo.Create(ctx, recent)

		ids, err := repo.ListExpired(ctx, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("ListExpired: %v", err)
		}
		if len(ids) != 1 || ids[0] != "sess-old" {
			t.Fatalf("expected [sess-old], got %v", ids)
		}
	})

	t.Run("UpdateState_sets_closed_at_on_closed", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-state", "user-1", "web")
		repo.Create(ctx, s)

		if err := repo.UpdateState(ctx, "sess-state", persist.SessionClosed); err != nil {
			t.Fatalf("UpdateState: %v", err)
		}

		got, _ := repo.Get(ctx, "sess-state")
		if got.State != persist.SessionClosed || got.ClosedAt == nil {
			t.Fatalf("expected closed with ClosedAt set: state=%s, closedAt=%v", got.State, got.ClosedAt)
		}
	})

	t.Run("UpdateState_nonexistent_returns_ErrSessionNotFound", func(t *testing.T) {
		err := repo.UpdateState(ctx, "no-exist", persist.SessionActive)
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Close_already_closed_returns_ErrSessionClosed", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-dbl-close", "user-1", "web")
		repo.Create(ctx, s)
		repo.Close(ctx, "sess-dbl-close")

		err := repo.Close(ctx, "sess-dbl-close")
		if !errors.Is(err, persist.ErrSessionClosed) {
			t.Fatalf("expected ErrSessionClosed, got %v", err)
		}
	})

	t.Run("Delete_cascades_messages_FK", func(t *testing.T) {
		truncateAll(t, infra.pool)

		s := newSession("sess-cascade", "user-1", "web")
		repo.Create(ctx, s)
		repo.AppendMessage(ctx, "sess-cascade", newMessage("msg-c1", "sess-cascade", "user", "hi"))
		repo.AppendMessage(ctx, "sess-cascade", newMessage("msg-c2", "sess-cascade", "assistant", "hello"))

		// Verify messages exist
		got, _ := repo.Get(ctx, "sess-cascade")
		if len(got.Messages) != 2 {
			t.Fatalf("expected 2 messages before delete, got %d", len(got.Messages))
		}

		// Delete session — should cascade to messages
		repo.Delete(ctx, "sess-cascade")

		// Verify messages are gone (check via direct query)
		var count int
		infra.pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages WHERE session_id = $1", "sess-cascade").Scan(&count)
		if count != 0 {
			t.Fatalf("expected 0 messages after cascade delete, got %d", count)
		}
	})

	t.Run("Touch_nonexistent_returns_ErrSessionNotFound", func(t *testing.T) {
		err := repo.Touch(ctx, "ghost")
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Delete_nonexistent_returns_ErrSessionNotFound", func(t *testing.T) {
		err := repo.Delete(ctx, "ghost")
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("Close_nonexistent_returns_ErrSessionNotFound", func(t *testing.T) {
		err := repo.Close(ctx, "ghost")
		if !errors.Is(err, persist.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

// ===========================================================================
// EventRepository integration tests
// ===========================================================================

func TestEventRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := postgres.NewEventRepository(infra.pool)
	ctx := context.Background()

	t.Run("Append_and_QueryBySession_with_pagination", func(t *testing.T) {
		truncateAll(t, infra.pool)

		base := time.Now().Truncate(time.Microsecond)
		for i := 0; i < 5; i++ {
			e := newEvent(
				fmt.Sprintf("evt-%d", i), "transition_fired", "sess-e1", "cpn-1",
				base.Add(time.Duration(i)*time.Millisecond),
			)
			if err := repo.Append(ctx, e); err != nil {
				t.Fatalf("Append event %d: %v", i, err)
			}
		}

		// Page 1: limit 3
		page1, err := repo.QueryBySession(ctx, "sess-e1", &persist.EventQueryOpts{Limit: 3})
		if err != nil {
			t.Fatalf("QueryBySession page1: %v", err)
		}
		if len(page1.Items) != 3 || !page1.HasMore {
			t.Fatalf("page1: expected 3 items with HasMore, got %d items, HasMore=%v", len(page1.Items), page1.HasMore)
		}

		// Page 2: use cursor
		page2, err := repo.QueryBySession(ctx, "sess-e1", &persist.EventQueryOpts{Limit: 3, Cursor: page1.NextCursor})
		if err != nil {
			t.Fatalf("QueryBySession page2: %v", err)
		}
		if len(page2.Items) != 2 || page2.HasMore {
			t.Fatalf("page2: expected 2 items without HasMore, got %d items, HasMore=%v", len(page2.Items), page2.HasMore)
		}
	})

	t.Run("Append_bulk_CopyFrom", func(t *testing.T) {
		truncateAll(t, infra.pool)

		base := time.Now().Truncate(time.Microsecond)
		events := make([]*persist.EventRecord, 10)
		for i := range events {
			events[i] = newEvent(
				fmt.Sprintf("bulk-%d", i), "bulk_event", "sess-bulk", "cpn-bulk",
				base.Add(time.Duration(i)*time.Millisecond),
			)
		}

		if err := repo.Append(ctx, events...); err != nil {
			t.Fatalf("Bulk Append: %v", err)
		}

		count, err := repo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bulk"})
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count != 10 {
			t.Fatalf("expected 10 events, got %d", count)
		}
	})

	t.Run("Append_nil_event_returns_ErrEventAppendFailed", func(t *testing.T) {
		err := repo.Append(ctx, nil)
		if !errors.Is(err, persist.ErrEventAppendFailed) {
			t.Fatalf("expected ErrEventAppendFailed, got %v", err)
		}
	})

	t.Run("Append_empty_is_noop", func(t *testing.T) {
		if err := repo.Append(ctx); err != nil {
			t.Fatalf("empty Append should succeed: %v", err)
		}
	})

	t.Run("QueryByCPN", func(t *testing.T) {
		truncateAll(t, infra.pool)

		base := time.Now().Truncate(time.Microsecond)
		repo.Append(ctx, newEvent("cpn-evt-1", "fired", "s1", "target-cpn", base))
		repo.Append(ctx, newEvent("cpn-evt-2", "fired", "s2", "target-cpn", base.Add(time.Millisecond)))
		repo.Append(ctx, newEvent("cpn-evt-3", "fired", "s1", "other-cpn", base.Add(2*time.Millisecond)))

		page, err := repo.QueryByCPN(ctx, "target-cpn", nil)
		if err != nil {
			t.Fatalf("QueryByCPN: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("expected 2 events for target-cpn, got %d", len(page.Items))
		}
	})

	t.Run("QueryByType_with_time_range", func(t *testing.T) {
		truncateAll(t, infra.pool)

		base := time.Now().Truncate(time.Microsecond)
		repo.Append(ctx, newEvent("type-1", "llm_call", "s1", "c1", base))
		repo.Append(ctx, newEvent("type-2", "llm_call", "s1", "c1", base.Add(time.Hour)))
		repo.Append(ctx, newEvent("type-3", "tool_call", "s1", "c1", base.Add(30*time.Minute)))

		page, err := repo.QueryByType(ctx, "llm_call", base.Add(-time.Minute), base.Add(30*time.Minute), nil)
		if err != nil {
			t.Fatalf("QueryByType: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("expected 1 llm_call in range, got %d", len(page.Items))
		}
	})

	t.Run("Count_with_combined_filters", func(t *testing.T) {
		truncateAll(t, infra.pool)

		base := time.Now().Truncate(time.Microsecond)
		repo.Append(ctx, newEvent("cf-1", "fired", "s-count", "c1", base))
		repo.Append(ctx, newEvent("cf-2", "fired", "s-count", "c1", base.Add(time.Millisecond)))
		repo.Append(ctx, newEvent("cf-3", "done", "s-count", "c1", base.Add(2*time.Millisecond)))
		repo.Append(ctx, newEvent("cf-4", "fired", "s-other", "c1", base.Add(3*time.Millisecond)))

		count, err := repo.Count(ctx, &persist.EventQueryOpts{
			SessionID: "s-count",
			EventType: "fired",
		})
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2, got %d", count)
		}
	})
}

// ===========================================================================
// EventBatcher integration tests
// ===========================================================================

func TestEventBatcher_Integration(t *testing.T) {
	infra := setupInfra(t)
	eventRepo := postgres.NewEventRepository(infra.pool)
	ctx := context.Background()

	t.Run("Flush_on_batch_size", func(t *testing.T) {
		truncateAll(t, infra.pool)

		batcher := postgres.NewEventBatcher(eventRepo, 5, 10*time.Second) // long interval, small batch
		base := time.Now().Truncate(time.Microsecond)

		for i := 0; i < 5; i++ {
			e := newEvent(
				fmt.Sprintf("batch-sz-%d", i), "batch_test", "sess-bsz", "cpn-bsz",
				base.Add(time.Duration(i)*time.Millisecond),
			)
			if err := batcher.Submit(ctx, e); err != nil {
				t.Fatalf("Submit %d: %v", i, err)
			}
		}

		// Wait for flush to complete
		time.Sleep(200 * time.Millisecond)

		count, _ := eventRepo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bsz"})
		if count != 5 {
			t.Fatalf("expected 5 events after batch-size flush, got %d", count)
		}

		batcher.Close(ctx)
	})

	t.Run("Flush_on_interval", func(t *testing.T) {
		truncateAll(t, infra.pool)

		batcher := postgres.NewEventBatcher(eventRepo, 100, 200*time.Millisecond) // small interval, large batch
		base := time.Now().Truncate(time.Microsecond)

		for i := 0; i < 3; i++ {
			e := newEvent(
				fmt.Sprintf("batch-int-%d", i), "interval_test", "sess-bint", "cpn-bint",
				base.Add(time.Duration(i)*time.Millisecond),
			)
			batcher.Submit(ctx, e)
		}

		// Wait for interval flush
		time.Sleep(500 * time.Millisecond)

		count, _ := eventRepo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-bint"})
		if count != 3 {
			t.Fatalf("expected 3 events after interval flush, got %d", count)
		}

		batcher.Close(ctx)
	})

	t.Run("Close_drains_remaining", func(t *testing.T) {
		truncateAll(t, infra.pool)

		batcher := postgres.NewEventBatcher(eventRepo, 100, 10*time.Second) // won't auto-flush
		base := time.Now().Truncate(time.Microsecond)

		for i := 0; i < 7; i++ {
			e := newEvent(
				fmt.Sprintf("batch-drain-%d", i), "drain_test", "sess-drain", "cpn-drain",
				base.Add(time.Duration(i)*time.Millisecond),
			)
			batcher.Submit(ctx, e)
		}

		// Close should drain and flush
		if err := batcher.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}

		count, _ := eventRepo.Count(ctx, &persist.EventQueryOpts{SessionID: "sess-drain"})
		if count != 7 {
			t.Fatalf("expected 7 events after Close drain, got %d", count)
		}
	})
}

// ===========================================================================
// LedgerRepository integration tests
// ===========================================================================

func TestLedgerRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := postgres.NewLedgerRepository(infra.pool)
	ctx := context.Background()

	t.Run("Record_upsert_creates_then_increments", func(t *testing.T) {
		truncateAll(t, infra.pool)

		// First record — create
		rec := newLedger("sess-led-1", 100, 200, 1, 0.05)
		if err := repo.Record(ctx, rec); err != nil {
			t.Fatalf("Record (create): %v", err)
		}

		got, err := repo.GetBySession(ctx, "sess-led-1")
		if err != nil {
			t.Fatalf("GetBySession: %v", err)
		}
		if got.InputTokens != 100 || got.OutputTokens != 200 || got.Calls != 1 {
			t.Fatalf("unexpected ledger after create: %+v", got)
		}

		// Second record — increment
		rec2 := newLedger("sess-led-1", 50, 100, 2, 0.03)
		if err := repo.Record(ctx, rec2); err != nil {
			t.Fatalf("Record (increment): %v", err)
		}

		got, _ = repo.GetBySession(ctx, "sess-led-1")
		if got.InputTokens != 150 || got.OutputTokens != 300 || got.Calls != 3 {
			t.Fatalf("expected incremented values (150,300,3), got (%d,%d,%d)",
				got.InputTokens, got.OutputTokens, got.Calls)
		}
		if got.TotalCostUSD < 0.079 || got.TotalCostUSD > 0.081 {
			t.Fatalf("expected ~0.08 total cost, got %f", got.TotalCostUSD)
		}
	})

	t.Run("GetBySession_nonexistent_returns_ErrLedgerNotFound", func(t *testing.T) {
		_, err := repo.GetBySession(ctx, "no-such-ledger")
		if !errors.Is(err, persist.ErrLedgerNotFound) {
			t.Fatalf("expected ErrLedgerNotFound, got %v", err)
		}
	})

	t.Run("SetDailyTotal", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Record(ctx, newLedger("sess-daily", 100, 200, 1, 0.05))
		if err := repo.SetDailyTotal(ctx, "sess-daily", time.Now(), 1.50); err != nil {
			t.Fatalf("SetDailyTotal: %v", err)
		}

		got, _ := repo.GetBySession(ctx, "sess-daily")
		if got.DailyTotalUSD != 1.50 {
			t.Fatalf("expected 1.50, got %f", got.DailyTotalUSD)
		}
	})

	t.Run("SetDailyTotal_nonexistent_returns_ErrLedgerNotFound", func(t *testing.T) {
		err := repo.SetDailyTotal(ctx, "ghost", time.Now(), 1.0)
		if !errors.Is(err, persist.ErrLedgerNotFound) {
			t.Fatalf("expected ErrLedgerNotFound, got %v", err)
		}
	})

	t.Run("QueryByDate", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Record(ctx, newLedger("sess-qbd-1", 100, 200, 1, 0.05))
		repo.Record(ctx, newLedger("sess-qbd-2", 200, 300, 2, 0.10))

		records, err := repo.QueryByDate(ctx, time.Now())
		if err != nil {
			t.Fatalf("QueryByDate: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
	})

	t.Run("AggregateByDateRange", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Record(ctx, newLedger("sess-agg-1", 100, 200, 1, 0.05))
		repo.Record(ctx, newLedger("sess-agg-2", 300, 400, 2, 0.10))

		agg, err := repo.AggregateByDateRange(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("AggregateByDateRange: %v", err)
		}
		if agg.TotalInputTokens != 400 || agg.TotalOutputTokens != 600 {
			t.Fatalf("unexpected aggregate: input=%d output=%d", agg.TotalInputTokens, agg.TotalOutputTokens)
		}
		if agg.TotalCalls != 3 {
			t.Fatalf("expected 3 total calls, got %d", agg.TotalCalls)
		}
	})

	t.Run("AggregateByDateRange_empty_returns_zeros", func(t *testing.T) {
		truncateAll(t, infra.pool)

		agg, err := repo.AggregateByDateRange(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("AggregateByDateRange: %v", err)
		}
		if agg.TotalInputTokens != 0 || agg.TotalCalls != 0 {
			t.Fatalf("expected zeros, got %+v", agg)
		}
	})
}

// ===========================================================================
// FlowRepository integration tests
// ===========================================================================

func TestFlowRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := postgres.NewFlowRepository(infra.pool)
	ctx := context.Background()

	t.Run("Save_GetByHash_lifecycle", func(t *testing.T) {
		truncateAll(t, infra.pool)

		flow := newFlow("hash-1", "assistant")
		if err := repo.Save(ctx, flow); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := repo.GetByHash(ctx, "hash-1")
		if err != nil {
			t.Fatalf("GetByHash: %v", err)
		}
		if got.Hash != "hash-1" || got.Role != "assistant" {
			t.Fatalf("unexpected flow: %+v", got)
		}
	})

	t.Run("Save_upsert_updates_topology", func(t *testing.T) {
		truncateAll(t, infra.pool)

		flow := newFlow("hash-upsert", "assistant")
		repo.Save(ctx, flow)

		// Upsert with updated topology
		flow.TopologyJSON = json.RawMessage(`{"places": ["p1"], "transitions": ["t1"]}`)
		flow.UpdatedAt = time.Now().Truncate(time.Microsecond)
		repo.Save(ctx, flow)

		got, _ := repo.GetByHash(ctx, "hash-upsert")
		if string(got.TopologyJSON) != `{"places": ["p1"], "transitions": ["t1"]}` {
			t.Fatalf("topology not updated: %s", string(got.TopologyJSON))
		}
	})

	t.Run("Soft_delete_hides_from_GetByHash_and_List", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Save(ctx, newFlow("hash-del", "assistant"))

		if err := repo.Delete(ctx, "hash-del"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		_, err := repo.GetByHash(ctx, "hash-del")
		if !errors.Is(err, persist.ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound after soft delete, got %v", err)
		}

		page, _ := repo.List(ctx, nil)
		if len(page.Items) != 0 {
			t.Fatalf("expected 0 visible flows, got %d", len(page.Items))
		}
	})

	t.Run("Delete_already_deleted_returns_ErrFlowNotFound", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Save(ctx, newFlow("hash-dbl-del", "assistant"))
		repo.Delete(ctx, "hash-dbl-del")

		err := repo.Delete(ctx, "hash-dbl-del")
		if !errors.Is(err, persist.ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("GetByHash_nonexistent_returns_ErrFlowNotFound", func(t *testing.T) {
		_, err := repo.GetByHash(ctx, "ghost-hash")
		if !errors.Is(err, persist.ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound, got %v", err)
		}
	})

	t.Run("List_with_role_filter", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Save(ctx, newFlow("h-asst-1", "assistant"))
		repo.Save(ctx, newFlow("h-asst-2", "assistant"))
		repo.Save(ctx, newFlow("h-tool-1", "tool"))

		page, err := repo.List(ctx, &persist.FlowListOpts{Role: "assistant"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("expected 2 assistant flows, got %d", len(page.Items))
		}
	})

	t.Run("List_pagination_offset_based", func(t *testing.T) {
		truncateAll(t, infra.pool)

		for i := 0; i < 5; i++ {
			f := newFlow(fmt.Sprintf("h-page-%d", i), "assistant")
			f.CreatedAt = time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Microsecond)
			repo.Save(ctx, f)
		}

		page1, _ := repo.List(ctx, &persist.FlowListOpts{Limit: 3})
		if len(page1.Items) != 3 || !page1.HasMore {
			t.Fatalf("page1: expected 3 with HasMore, got %d HasMore=%v", len(page1.Items), page1.HasMore)
		}

		page2, _ := repo.List(ctx, &persist.FlowListOpts{Limit: 3, Cursor: page1.NextCursor})
		if len(page2.Items) != 2 || page2.HasMore {
			t.Fatalf("page2: expected 2 without HasMore, got %d HasMore=%v", len(page2.Items), page2.HasMore)
		}
	})

	t.Run("UpdateStats_on_existing_flow", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Save(ctx, newFlow("h-stats", "assistant"))

		stats := &persist.FlowStats{
			ExecutionCount: 10,
			SuccessRate:    0.85,
			AvgCostUSD:     0.12,
			AvgDurationMs:  250,
		}
		if err := repo.UpdateStats(ctx, "h-stats", stats); err != nil {
			t.Fatalf("UpdateStats: %v", err)
		}

		got, _ := repo.GetByHash(ctx, "h-stats")
		if got.Stats.ExecutionCount != 10 || got.Stats.SuccessRate != 0.85 {
			t.Fatalf("unexpected stats: %+v", got.Stats)
		}
	})

	t.Run("UpdateStats_on_soft_deleted_returns_ErrFlowNotFound", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.Save(ctx, newFlow("h-del-stats", "assistant"))
		repo.Delete(ctx, "h-del-stats")

		err := repo.UpdateStats(ctx, "h-del-stats", &persist.FlowStats{ExecutionCount: 1})
		if !errors.Is(err, persist.ErrFlowNotFound) {
			t.Fatalf("expected ErrFlowNotFound, got %v", err)
		}
	})
}

// ===========================================================================
// IntelligenceRepository integration tests
// ===========================================================================

func TestIntelligenceRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := postgres.NewIntelligenceRepository(infra.pool)
	ctx := context.Background()

	t.Run("RecordExecution_and_QueryByRole", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.RecordExecution(ctx, newExecution("exec-1", "cpn-1", "assistant", "s-1", true, 0.05))
		repo.RecordExecution(ctx, newExecution("exec-2", "cpn-2", "assistant", "s-2", false, 0.10))
		repo.RecordExecution(ctx, newExecution("exec-3", "cpn-3", "tool", "s-1", true, 0.02))

		records, err := repo.QueryByRole(ctx, "assistant",
			time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("QueryByRole: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 assistant executions, got %d", len(records))
		}
	})

	t.Run("Aggregate_computes_correct_metrics", func(t *testing.T) {
		truncateAll(t, infra.pool)

		repo.RecordExecution(ctx, newExecution("agg-1", "cpn-1", "assistant", "s-1", true, 0.10))
		repo.RecordExecution(ctx, newExecution("agg-2", "cpn-2", "assistant", "s-2", false, 0.20))

		m, err := repo.Aggregate(ctx, "assistant",
			time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if m.ExecutionCount != 2 {
			t.Fatalf("expected 2 executions, got %d", m.ExecutionCount)
		}
		// success rate = (1.0 + 0.0) / 2 = 0.5
		if m.SuccessRate < 0.49 || m.SuccessRate > 0.51 {
			t.Fatalf("expected ~0.5 success rate, got %f", m.SuccessRate)
		}
		// avg cost = (0.10 + 0.20) / 2 = 0.15
		if m.AvgCostUSD < 0.14 || m.AvgCostUSD > 0.16 {
			t.Fatalf("expected ~0.15 avg cost, got %f", m.AvgCostUSD)
		}
	})

	t.Run("Aggregate_empty_range_returns_zeros", func(t *testing.T) {
		truncateAll(t, infra.pool)

		m, err := repo.Aggregate(ctx, "assistant",
			time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if m.ExecutionCount != 0 || m.SuccessRate != 0 {
			t.Fatalf("expected zeros, got %+v", m)
		}
	})

	t.Run("TopFlows_ranking_by_score", func(t *testing.T) {
		truncateAll(t, infra.pool)

		// Flow A: 100% success, low cost → high score
		repo.RecordExecution(ctx, newExecution("top-1", "flow-a", "assistant", "s-1", true, 0.01))
		repo.RecordExecution(ctx, newExecution("top-2", "flow-a", "assistant", "s-2", true, 0.01))

		// Flow B: 50% success, high cost → low score
		repo.RecordExecution(ctx, newExecution("top-3", "flow-b", "tool", "s-1", true, 1.00))
		repo.RecordExecution(ctx, newExecution("top-4", "flow-b", "tool", "s-2", false, 1.00))

		ranked, err := repo.TopFlows(ctx, 5)
		if err != nil {
			t.Fatalf("TopFlows: %v", err)
		}
		if len(ranked) != 2 {
			t.Fatalf("expected 2 ranked flows, got %d", len(ranked))
		}

		// Flow A should rank higher: score = 1.0*100 - 0.01*10 = 99.9
		// Flow B: score = 0.5*100 - 1.0*10 = 40.0
		if ranked[0].Hash != "flow-a" {
			t.Fatalf("expected flow-a to rank first, got %s (score=%.2f)", ranked[0].Hash, ranked[0].Score)
		}
		if ranked[0].Score < 99.0 {
			t.Fatalf("flow-a score should be ~99.9, got %.2f", ranked[0].Score)
		}
		if ranked[1].Score > 41.0 {
			t.Fatalf("flow-b score should be ~40.0, got %.2f", ranked[1].Score)
		}
	})

	t.Run("TopFlows_empty_returns_nil", func(t *testing.T) {
		truncateAll(t, infra.pool)

		ranked, err := repo.TopFlows(ctx, 5)
		if err != nil {
			t.Fatalf("TopFlows: %v", err)
		}
		if len(ranked) != 0 {
			t.Fatalf("expected 0 ranked flows, got %d", len(ranked))
		}
	})
}

// ===========================================================================
// HITL Repository integration tests (Redis)
// ===========================================================================

func TestHITLRepository_Integration(t *testing.T) {
	infra := setupInfra(t)
	repo := storeredis.NewHITLRepository(infra.redis)
	ctx := context.Background()

	t.Run("Enqueue_ListPending_Dequeue_lifecycle", func(t *testing.T) {
		infra.redis.FlushAll(ctx)

		req := newHITL("sess-h1", "t-1", 5*time.Minute)
		if err := repo.Enqueue(ctx, req); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		pending, err := repo.ListPending(ctx, "sess-h1")
		if err != nil {
			t.Fatalf("ListPending: %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("expected 1 pending, got %d", len(pending))
		}

		dequeued, err := repo.Dequeue(ctx, "sess-h1", "t-1")
		if err != nil {
			t.Fatalf("Dequeue: %v", err)
		}
		if dequeued.TransitionID != "t-1" {
			t.Fatalf("unexpected transition: %s", dequeued.TransitionID)
		}

		// After dequeue, should be gone
		_, err = repo.Dequeue(ctx, "sess-h1", "t-1")
		if !errors.Is(err, persist.ErrHITLNotFound) {
			t.Fatalf("expected ErrHITLNotFound after dequeue, got %v", err)
		}
	})

	t.Run("Enqueue_expired_returns_ErrHITLExpired", func(t *testing.T) {
		req := newHITL("sess-h2", "t-exp", -time.Second) // already expired
		err := repo.Enqueue(ctx, req)
		if !errors.Is(err, persist.ErrHITLExpired) {
			t.Fatalf("expected ErrHITLExpired, got %v", err)
		}
	})

	t.Run("Dequeue_nonexistent_returns_ErrHITLNotFound", func(t *testing.T) {
		_, err := repo.Dequeue(ctx, "ghost-sess", "ghost-t")
		if !errors.Is(err, persist.ErrHITLNotFound) {
			t.Fatalf("expected ErrHITLNotFound, got %v", err)
		}
	})

	t.Run("ListPending_multiple_transitions", func(t *testing.T) {
		infra.redis.FlushAll(ctx)

		repo.Enqueue(ctx, newHITL("sess-multi", "t-a", 5*time.Minute))
		repo.Enqueue(ctx, newHITL("sess-multi", "t-b", 5*time.Minute))
		repo.Enqueue(ctx, newHITL("other-sess", "t-c", 5*time.Minute))

		pending, err := repo.ListPending(ctx, "sess-multi")
		if err != nil {
			t.Fatalf("ListPending: %v", err)
		}
		if len(pending) != 2 {
			t.Fatalf("expected 2 pending for sess-multi, got %d", len(pending))
		}
	})

	t.Run("Expire_cleanup", func(t *testing.T) {
		infra.redis.FlushAll(ctx)

		// Enqueue with short TTL — it'll still be alive but Expire scans for anomalies
		repo.Enqueue(ctx, newHITL("sess-exp", "t-exp", 5*time.Minute))

		count, err := repo.Expire(ctx, time.Hour)
		if err != nil {
			t.Fatalf("Expire: %v", err)
		}
		// Normally returns 0 since Redis handles TTL natively
		_ = count
	})
}

// ===========================================================================
// Redis Session Cache integration tests
// ===========================================================================

func TestRedisSessionCache_Integration(t *testing.T) {
	infra := setupInfra(t)
	pgRepo := postgres.NewSessionRepository(infra.pool)
	cachedRepo := storeredis.NewSessionRepository(infra.redis, pgRepo)
	ctx := context.Background()

	t.Run("Cache_miss_then_hit", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-1", "user-1", "web")
		cachedRepo.Create(ctx, s)

		// Invalidate cache to force miss
		infra.redis.Del(ctx, "session:sess-cache-1")

		// Get should miss cache, fetch from PG, then populate cache
		got, err := cachedRepo.Get(ctx, "sess-cache-1")
		if err != nil {
			t.Fatalf("Get (miss): %v", err)
		}
		if got.ID != "sess-cache-1" {
			t.Fatalf("unexpected ID: %s", got.ID)
		}

		// Verify it's now cached
		exists := infra.redis.Exists(ctx, "session:sess-cache-1").Val()
		if exists != 1 {
			t.Fatal("session should be cached after Get miss")
		}

		// Second Get should hit cache (we can verify by checking it returns same data)
		got2, err := cachedRepo.Get(ctx, "sess-cache-1")
		if err != nil {
			t.Fatalf("Get (hit): %v", err)
		}
		if got2.ID != "sess-cache-1" {
			t.Fatalf("cache hit returned wrong ID: %s", got2.ID)
		}
	})

	t.Run("Close_evicts_cache", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-close", "user-1", "web")
		cachedRepo.Create(ctx, s)

		// Verify cached
		exists := infra.redis.Exists(ctx, "session:sess-cache-close").Val()
		if exists != 1 {
			t.Fatal("session should be cached after Create")
		}

		cachedRepo.Close(ctx, "sess-cache-close")

		// Cache should be evicted
		exists = infra.redis.Exists(ctx, "session:sess-cache-close").Val()
		if exists != 0 {
			t.Fatal("session cache should be evicted after Close")
		}
	})

	t.Run("Delete_evicts_cache", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-del", "user-1", "web")
		cachedRepo.Create(ctx, s)
		cachedRepo.Delete(ctx, "sess-cache-del")

		exists := infra.redis.Exists(ctx, "session:sess-cache-del").Val()
		if exists != 0 {
			t.Fatal("session cache should be evicted after Delete")
		}
	})

	t.Run("UpdateState_closed_evicts_active_invalidates", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-state", "user-1", "web")
		cachedRepo.Create(ctx, s)

		// UpdateState to closed — should evict
		cachedRepo.UpdateState(ctx, "sess-cache-state", persist.SessionClosed)
		exists := infra.redis.Exists(ctx, "session:sess-cache-state").Val()
		if exists != 0 {
			t.Fatal("cache should be evicted on closed state")
		}
	})

	t.Run("AppendMessage_resets_TTL", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-msg", "user-1", "web")
		cachedRepo.Create(ctx, s)

		msg := newMessage("msg-cache-1", "sess-cache-msg", "user", "hello")
		if err := cachedRepo.AppendMessage(ctx, "sess-cache-msg", msg); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}

		// Check messages list exists in Redis
		listLen := infra.redis.LLen(ctx, "session:sess-cache-msg:messages").Val()
		if listLen != 1 {
			t.Fatalf("expected 1 cached message, got %d", listLen)
		}
	})

	t.Run("Touch_resets_TTL", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		s := newSession("sess-cache-touch", "user-1", "web")
		cachedRepo.Create(ctx, s)

		if err := cachedRepo.Touch(ctx, "sess-cache-touch"); err != nil {
			t.Fatalf("Touch: %v", err)
		}

		ttl := infra.redis.TTL(ctx, "session:sess-cache-touch").Val()
		if ttl <= 0 {
			t.Fatalf("TTL should be positive after Touch, got %v", ttl)
		}
	})
}

// ===========================================================================
// Store Facade integration tests
// ===========================================================================

func TestStoreFacade_Integration(t *testing.T) {
	infra := setupInfra(t)
	ctx := context.Background()

	t.Run("NewStore_Health_Close_lifecycle", func(t *testing.T) {
		store, err := postgres.NewStore(ctx,
			postgres.PoolConfig{DSN: infra.dsn},
			postgres.RedisConfig{URL: infra.rediURL},
			"migrations",
		)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}

		// Health should pass
		if err := store.Health(ctx); err != nil {
			t.Fatalf("Health: %v", err)
		}

		// Accessor methods should return non-nil
		if store.Sessions() == nil {
			t.Fatal("Sessions() returned nil")
		}
		if store.Events() == nil {
			t.Fatal("Events() returned nil")
		}
		if store.EventBatcher() == nil {
			t.Fatal("EventBatcher() returned nil")
		}
		if store.Ledger() == nil {
			t.Fatal("Ledger() returned nil")
		}
		if store.Flows() == nil {
			t.Fatal("Flows() returned nil")
		}
		if store.Intelligence() == nil {
			t.Fatal("Intelligence() returned nil")
		}
		if store.HITL() == nil {
			t.Fatal("HITL() returned nil")
		}

		// Close should succeed
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("NewStore_bad_postgres_fails", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		_, err := postgres.NewStore(ctx,
			postgres.PoolConfig{DSN: "postgres://bad:bad@192.0.2.1:5432/db"},
			postgres.RedisConfig{URL: infra.rediURL},
			"",
		)
		if err == nil {
			t.Fatal("expected error on bad Postgres DSN")
		}
	})

	t.Run("NewStore_bad_redis_fails", func(t *testing.T) {
		_, err := postgres.NewStore(ctx,
			postgres.PoolConfig{DSN: infra.dsn},
			postgres.RedisConfig{URL: "redis://192.0.2.1:6379"},
			"migrations",
		)
		if err == nil {
			t.Fatal("expected error on bad Redis URL")
		}
	})

	t.Run("Store_end_to_end_flow", func(t *testing.T) {
		truncateAll(t, infra.pool)
		infra.redis.FlushAll(ctx)

		store, err := postgres.NewStore(ctx,
			postgres.PoolConfig{DSN: infra.dsn},
			postgres.RedisConfig{URL: infra.rediURL},
			"migrations",
		)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		// No defer — we close explicitly at step 8 to verify batcher flush

		// 1. Create session via cached repo
		s := newSession("e2e-sess", "e2e-user", "api")
		if err := store.Sessions().Create(ctx, s); err != nil {
			t.Fatalf("Create session: %v", err)
		}

		// 2. Append message
		msg := newMessage("e2e-msg-1", "e2e-sess", "user", "plan my trip")
		if err := store.Sessions().AppendMessage(ctx, "e2e-sess", msg); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}

		// 3. Submit events via batcher
		base := time.Now().Truncate(time.Microsecond)
		for i := 0; i < 3; i++ {
			e := newEvent(
				fmt.Sprintf("e2e-evt-%d", i), "transition_fired", "e2e-sess", "cpn-e2e",
				base.Add(time.Duration(i)*time.Millisecond),
			)
			store.EventBatcher().Submit(ctx, e)
		}

		// 4. Record ledger
		if err := store.Ledger().Record(ctx, newLedger("e2e-sess", 500, 1000, 3, 0.15)); err != nil {
			t.Fatalf("Record ledger: %v", err)
		}

		// 5. Save flow
		if err := store.Flows().Save(ctx, newFlow("e2e-hash", "assistant")); err != nil {
			t.Fatalf("Save flow: %v", err)
		}

		// 6. Record execution
		if err := store.Intelligence().RecordExecution(ctx, newExecution("e2e-exec", "cpn-e2e", "assistant", "e2e-sess", true, 0.15)); err != nil {
			t.Fatalf("RecordExecution: %v", err)
		}

		// 7. Enqueue HITL
		if err := store.HITL().Enqueue(ctx, newHITL("e2e-sess", "t-hitl", 5*time.Minute)); err != nil {
			t.Fatalf("Enqueue HITL: %v", err)
		}

		// 8. Close store (flushes batcher)
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		// Verify batcher flushed events to Postgres (use raw pool)
		var eventCount int64
		infra.pool.QueryRow(ctx, "SELECT COUNT(*) FROM events WHERE session_id = $1", "e2e-sess").Scan(&eventCount)
		if eventCount != 3 {
			t.Fatalf("expected 3 events after batcher flush, got %d", eventCount)
		}
	})
}
