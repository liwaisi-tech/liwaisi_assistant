package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mock.Close() })
	return mock
}

func expectMet(t *testing.T, mock pgxmock.PgxPoolIface) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var now = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// SESSION
// ---------------------------------------------------------------------------

func TestSessionRepo_Create_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO sessions").
		WithArgs("s1", "u1", "web", "active", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(context.Background(), &persist.SessionRecord{
		ID: "s1", UserID: "u1", Channel: "web", State: persist.SessionActive,
		CreatedAt: now, LastActivityAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Create_Duplicate(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO sessions").
		WithArgs("s1", "u1", "web", "active", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(&pgconn.PgError{Code: "23505"})

	err := repo.Create(context.Background(), &persist.SessionRecord{
		ID: "s1", UserID: "u1", Channel: "web", State: persist.SessionActive,
		CreatedAt: now, LastActivityAt: now,
	})
	if !errors.Is(err, persist.ErrSessionExists) {
		t.Fatalf("want ErrSessionExists, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Create_EmptyID(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	err := repo.Create(context.Background(), &persist.SessionRecord{ID: ""})
	if !errors.Is(err, persist.ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Get_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	// session row
	mock.ExpectQuery("SELECT .+ FROM sessions WHERE id").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "channel", "state",
			"created_at", "last_activity_at", "closed_at", "metadata",
		}).AddRow("s1", "u1", "web", "active", now, now, nil, nil))

	// messages
	mock.ExpectQuery("SELECT .+ FROM messages WHERE session_id").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "session_id", "role", "content",
			"cpn_id", "cpn_role", "cpn_depth", "timestamp",
		}).AddRow("m1", "s1", "user", "hello", "cpn1", "coordinator", 0, now))

	s, err := repo.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "s1" || s.UserID != "u1" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", s.Messages)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Get_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE id").
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.Get(context.Background(), "missing")
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_GetByUserID_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE user_id").
		WithArgs("u1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "channel", "state",
			"created_at", "last_activity_at", "closed_at", "metadata",
		}).
			AddRow("s1", "u1", "web", "active", now, now, nil, nil).
			AddRow("s2", "u1", "api", "closed", now, now, &now, nil))

	result, err := repo.GetByUserID(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestSessionRepo_GetByUserID_Empty(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE user_id").
		WithArgs("nobody").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "channel", "state",
			"created_at", "last_activity_at", "closed_at", "metadata",
		}))

	result, err := repo.GetByUserID(context.Background(), "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("want empty, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestSessionRepo_AppendMessage_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO messages").
		WithArgs("m1", "s1", "user", "hi", "", "", 0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.AppendMessage(context.Background(), "s1", &persist.MessageRecord{
		ID: "m1", Role: "user", Content: "hi", Timestamp: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_AppendMessage_SessionNotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	// 0 rows affected => check state
	mock.ExpectExec("INSERT INTO messages").
		WithArgs("m1", "missing", "user", "hi", "", "", 0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	// state check fails → not found
	mock.ExpectQuery("SELECT state FROM sessions WHERE id").
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	err := repo.AppendMessage(context.Background(), "missing", &persist.MessageRecord{
		ID: "m1", Role: "user", Content: "hi", Timestamp: now,
	})
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_AppendMessage_SessionClosed(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO messages").
		WithArgs("m1", "s-closed", "user", "hi", "", "", 0, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))

	mock.ExpectQuery("SELECT state FROM sessions WHERE id").
		WithArgs("s-closed").
		WillReturnRows(pgxmock.NewRows([]string{"state"}).AddRow("closed"))

	err := repo.AppendMessage(context.Background(), "s-closed", &persist.MessageRecord{
		ID: "m1", Role: "user", Content: "hi", Timestamp: now,
	})
	if !errors.Is(err, persist.ErrSessionClosed) {
		t.Fatalf("want ErrSessionClosed, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_AppendMessage_FKViolation(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO messages").
		WithArgs("m1", "bad", "user", "hi", "", "", 0, pgxmock.AnyArg()).
		WillReturnError(&pgconn.PgError{Code: "23503"})

	err := repo.AppendMessage(context.Background(), "bad", &persist.MessageRecord{
		ID: "m1", Role: "user", Content: "hi", Timestamp: now,
	})
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_UpdateState_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state").
		WithArgs("s1", "active", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateState(context.Background(), "s1", persist.SessionActive)
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_UpdateState_Closed(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state").
		WithArgs("s1", "closed", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateState(context.Background(), "s1", persist.SessionClosed)
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_UpdateState_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state").
		WithArgs("missing", "active", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateState(context.Background(), "missing", persist.SessionActive)
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Touch_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET last_activity_at").
		WithArgs("s1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Touch(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Touch_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET last_activity_at").
		WithArgs("missing").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Touch(context.Background(), "missing")
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Close_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state = 'closed'").
		WithArgs("s1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Close(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Close_AlreadyClosed(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state = 'closed'").
		WithArgs("s1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	err := repo.Close(context.Background(), "s1")
	if !errors.Is(err, persist.ErrSessionClosed) {
		t.Fatalf("want ErrSessionClosed, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Close_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state = 'closed'").
		WithArgs("s1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	err := repo.Close(context.Background(), "s1")
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_ListExpired(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)
	before := now.Add(-time.Hour)

	mock.ExpectQuery("SELECT id FROM sessions WHERE state = 'active'").
		WithArgs(before).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("s1").AddRow("s2"))

	ids, err := repo.ListExpired(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2, got %d", len(ids))
	}
	expectMet(t, mock)
}

func TestSessionRepo_Delete_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("DELETE FROM sessions WHERE id").
		WithArgs("s1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.Delete(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestSessionRepo_Delete_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("DELETE FROM sessions WHERE id").
		WithArgs("missing").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.Delete(context.Background(), "missing")
	if !errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	expectMet(t, mock)
}

// ---------------------------------------------------------------------------
// EVENT
// ---------------------------------------------------------------------------

var eventCols = []string{
	"id", "type", "session_id", "cpn_id", "cpn_role", "cpn_depth",
	"transition_id", "transition_kind", "token_snapshot", "payload", "timestamp",
}

func TestEventRepo_Append_Single(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectCopyFrom(pgx.Identifier{"events"}, []string{
		"id", "type", "session_id", "cpn_id", "cpn_role", "cpn_depth",
		"transition_id", "transition_kind", "token_snapshot", "payload", "timestamp",
	}).WillReturnResult(1)

	err := repo.Append(context.Background(), &persist.EventRecord{
		ID: "e1", Type: "transition_fired", SessionID: "s1", CPNID: "c1",
		CPNRole: "coordinator", Timestamp: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestEventRepo_Append_Empty(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	err := repo.Append(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestEventRepo_Append_Nil(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	err := repo.Append(context.Background(), nil)
	if !errors.Is(err, persist.ErrEventAppendFailed) {
		t.Fatalf("want ErrEventAppendFailed, got %v", err)
	}
	expectMet(t, mock)
}

func TestEventRepo_Append_CopyError(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectCopyFrom(pgx.Identifier{"events"}, []string{
		"id", "type", "session_id", "cpn_id", "cpn_role", "cpn_depth",
		"transition_id", "transition_kind", "token_snapshot", "payload", "timestamp",
	}).WillReturnError(errors.New("copy failed"))

	err := repo.Append(context.Background(), &persist.EventRecord{
		ID: "e1", Type: "t", SessionID: "s1", CPNID: "c1", Timestamp: now,
	})
	if !errors.Is(err, persist.ErrEventAppendFailed) {
		t.Fatalf("want ErrEventAppendFailed, got %v", err)
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryBySession_NoCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM events WHERE session_id").
		WithArgs("s1", 101).
		WillReturnRows(pgxmock.NewRows(eventCols).
			AddRow("e1", "transition_fired", "s1", "c1", "coord", 0, "t1", "llm", json.RawMessage("{}"), json.RawMessage("{}"), now).
			AddRow("e2", "subnet_started", "s1", "c1", "coord", 0, "t2", "tool", json.RawMessage("{}"), json.RawMessage("{}"), now))

	page, err := repo.QueryBySession(context.Background(), "s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("want 2, got %d", len(page.Items))
	}
	if page.HasMore {
		t.Fatal("should not have more")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryBySession_WithCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 2, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE session_id .+ AND \\(timestamp, id\\)").
		WithArgs("s1", pgxmock.AnyArg(), "e0", 3).
		WillReturnRows(pgxmock.NewRows(eventCols).
			AddRow("e1", "t", "s1", "c1", "r", 0, "", "", nil, nil, now).
			AddRow("e2", "t", "s1", "c1", "r", 0, "", "", nil, nil, now).
			AddRow("e3", "t", "s1", "c1", "r", 0, "", "", nil, nil, now))

	page, err := repo.QueryBySession(context.Background(), "s1", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("want 2, got %d", len(page.Items))
	}
	if !page.HasMore {
		t.Fatal("should have more")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByCPN_NoCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM events WHERE cpn_id").
		WithArgs("c1", 101).
		WillReturnRows(pgxmock.NewRows(eventCols).
			AddRow("e1", "t", "s1", "c1", "r", 0, "", "", nil, nil, now))

	page, err := repo.QueryByCPN(context.Background(), "c1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1, got %d", len(page.Items))
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByCPN_WithCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 1, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE cpn_id .+ AND \\(timestamp, id\\)").
		WithArgs("c1", pgxmock.AnyArg(), "e0", 2).
		WillReturnRows(pgxmock.NewRows(eventCols).
			AddRow("e1", "t", "s1", "c1", "r", 0, "", "", nil, nil, now))

	page, err := repo.QueryByCPN(context.Background(), "c1", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1, got %d", len(page.Items))
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByType_NoCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)
	from := now.Add(-time.Hour)
	to := now

	mock.ExpectQuery("SELECT .+ FROM events WHERE type").
		WithArgs("transition_fired", from, to, 101).
		WillReturnRows(pgxmock.NewRows(eventCols))

	page, err := repo.QueryByType(context.Background(), "transition_fired", from, to, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("want 0, got %d", len(page.Items))
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByType_WithCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)
	from := now.Add(-time.Hour)
	to := now
	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 5, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE type .+ AND \\(timestamp, id\\)").
		WithArgs("transition_fired", from, to, pgxmock.AnyArg(), "e0", 6).
		WillReturnRows(pgxmock.NewRows(eventCols).
			AddRow("e1", "transition_fired", "s1", "c1", "r", 0, "", "", nil, nil, now))

	page, err := repo.QueryByType(context.Background(), "transition_fired", from, to, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1, got %d", len(page.Items))
	}
	expectMet(t, mock)
}

func TestEventRepo_Count_NilOpts(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM events WHERE 1=1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(42)))

	count, err := repo.Count(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if count != 42 {
		t.Fatalf("want 42, got %d", count)
	}
	expectMet(t, mock)
}

func TestEventRepo_Count_WithFilters(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	opts := &persist.EventQueryOpts{
		SessionID: "s1",
		CPNID:     "c1",
		EventType: "transition_fired",
		From:      now.Add(-time.Hour),
		To:        now,
	}

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM events WHERE 1=1").
		WithArgs("s1", "c1", "transition_fired", opts.From, opts.To).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(10)))

	count, err := repo.Count(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("want 10, got %d", count)
	}
	expectMet(t, mock)
}

func TestEventRepo_Count_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM events").
		WillReturnError(errors.New("db down"))

	_, err := repo.Count(context.Background(), nil)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

// ---------------------------------------------------------------------------
// FLOW
// ---------------------------------------------------------------------------

var flowCols = []string{
	"hash", "role", "topology_json", "function_mapping",
	"execution_count", "success_rate", "avg_cost_usd", "avg_duration_ms",
	"created_at", "updated_at", "deleted_at",
}

func TestFlowRepo_Save_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("INSERT INTO flows").
		WithArgs("h1", "coordinator", json.RawMessage(`{}`), json.RawMessage(`{}`),
			int64(0), float64(0), float64(0), int64(0), now, now).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Save(context.Background(), &persist.FlowRecord{
		Hash: "h1", Role: "coordinator",
		TopologyJSON: json.RawMessage(`{}`), FunctionMapping: json.RawMessage(`{}`),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_GetByHash_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE hash").
		WithArgs("h1").
		WillReturnRows(pgxmock.NewRows(flowCols).AddRow(
			"h1", "coordinator", json.RawMessage(`{}`), json.RawMessage(`{}`),
			int64(5), 0.95, 0.01, int64(200), now, now, nil))

	f, err := repo.GetByHash(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	if f.Hash != "h1" || f.Stats.ExecutionCount != 5 {
		t.Fatalf("unexpected: %+v", f)
	}
	expectMet(t, mock)
}

func TestFlowRepo_GetByHash_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE hash").
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetByHash(context.Background(), "missing")
	if !errors.Is(err, persist.ErrFlowNotFound) {
		t.Fatalf("want ErrFlowNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_List_NoRole(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE deleted_at IS NULL\\s+ORDER BY created_at").
		WithArgs(101, 0).
		WillReturnRows(pgxmock.NewRows(flowCols).AddRow(
			"h1", "coordinator", json.RawMessage(`{}`), json.RawMessage(`{}`),
			int64(1), 0.9, 0.01, int64(100), now, now, nil))

	page, err := repo.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1, got %d", len(page.Items))
	}
	if page.HasMore {
		t.Fatal("should not have more")
	}
	expectMet(t, mock)
}

func TestFlowRepo_List_WithRole(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE deleted_at IS NULL AND role").
		WithArgs("coordinator", 11, 0).
		WillReturnRows(pgxmock.NewRows(flowCols))

	page, err := repo.List(context.Background(), &persist.FlowListOpts{
		Role: "coordinator", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("want 0, got %d", len(page.Items))
	}
	expectMet(t, mock)
}

func TestFlowRepo_List_WithCursor(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE deleted_at IS NULL\\s+ORDER BY created_at").
		WithArgs(101, 50).
		WillReturnRows(pgxmock.NewRows(flowCols))

	_, err := repo.List(context.Background(), &persist.FlowListOpts{Cursor: "50"})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_UpdateStats_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET execution_count").
		WithArgs("h1", int64(10), 0.98, 0.05, int64(300)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateStats(context.Background(), "h1", &persist.FlowStats{
		ExecutionCount: 10, SuccessRate: 0.98, AvgCostUSD: 0.05, AvgDurationMs: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_UpdateStats_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET execution_count").
		WithArgs("missing", int64(1), 1.0, 0.01, int64(100)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateStats(context.Background(), "missing", &persist.FlowStats{
		ExecutionCount: 1, SuccessRate: 1.0, AvgCostUSD: 0.01, AvgDurationMs: 100,
	})
	if !errors.Is(err, persist.ErrFlowNotFound) {
		t.Fatalf("want ErrFlowNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_Delete_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET deleted_at").
		WithArgs("h1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Delete(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestFlowRepo_Delete_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET deleted_at").
		WithArgs("missing", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Delete(context.Background(), "missing")
	if !errors.Is(err, persist.ErrFlowNotFound) {
		t.Fatalf("want ErrFlowNotFound, got %v", err)
	}
	expectMet(t, mock)
}

// ---------------------------------------------------------------------------
// LEDGER
// ---------------------------------------------------------------------------

var ledgerCols = []string{
	"session_id", "input_tokens", "output_tokens", "calls",
	"total_cost_usd", "daily_total_usd", "last_updated",
}

func TestLedgerRepo_Record_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectExec("INSERT INTO token_ledger").
		WithArgs("s1", int64(100), int64(200), int64(1), 0.003).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Record(context.Background(), &persist.LedgerRecord{
		SessionID: "s1", InputTokens: 100, OutputTokens: 200, Calls: 1, TotalCostUSD: 0.003,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_Record_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectExec("INSERT INTO token_ledger").
		WithArgs("s1", int64(100), int64(200), int64(1), 0.003).
		WillReturnError(errors.New("db down"))

	err := repo.Record(context.Background(), &persist.LedgerRecord{
		SessionID: "s1", InputTokens: 100, OutputTokens: 200, Calls: 1, TotalCostUSD: 0.003,
	})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestLedgerRepo_GetBySession_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE session_id").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows(ledgerCols).AddRow("s1", int64(100), int64(200), int64(1), 0.003, 0.05, now))

	l, err := repo.GetBySession(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if l.SessionID != "s1" || l.InputTokens != 100 {
		t.Fatalf("unexpected: %+v", l)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_GetBySession_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE session_id").
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetBySession(context.Background(), "missing")
	if !errors.Is(err, persist.ErrLedgerNotFound) {
		t.Fatalf("want ErrLedgerNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_QueryByDate(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE").
		WithArgs(now).
		WillReturnRows(pgxmock.NewRows(ledgerCols).
			AddRow("s1", int64(100), int64(200), int64(1), 0.003, 0.05, now).
			AddRow("s2", int64(50), int64(100), int64(2), 0.001, 0.02, now))

	result, err := repo.QueryByDate(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestLedgerRepo_SetDailyTotal_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectExec("UPDATE token_ledger SET daily_total_usd").
		WithArgs("s1", 1.23).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.SetDailyTotal(context.Background(), "s1", now, 1.23)
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_SetDailyTotal_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectExec("UPDATE token_ledger SET daily_total_usd").
		WithArgs("missing", 1.23).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.SetDailyTotal(context.Background(), "missing", now, 1.23)
	if !errors.Is(err, persist.ErrLedgerNotFound) {
		t.Fatalf("want ErrLedgerNotFound, got %v", err)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_AggregateByDateRange(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)
	from := now.Add(-24 * time.Hour)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE").
		WithArgs(from, now).
		WillReturnRows(pgxmock.NewRows([]string{
			"total_input", "total_output", "total_calls", "total_cost",
		}).AddRow(int64(1000), int64(2000), int64(50), 0.5))

	agg, err := repo.AggregateByDateRange(context.Background(), from, now)
	if err != nil {
		t.Fatal(err)
	}
	if agg.TotalInputTokens != 1000 || agg.TotalCalls != 50 {
		t.Fatalf("unexpected: %+v", agg)
	}
	expectMet(t, mock)
}

func TestLedgerRepo_AggregateByDateRange_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db fail"))

	_, err := repo.AggregateByDateRange(context.Background(), now, now)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

// ---------------------------------------------------------------------------
// INTELLIGENCE
// ---------------------------------------------------------------------------

var execCols = []string{
	"id", "cpn_id", "cpn_role", "cpn_depth", "session_id",
	"transitions_fired", "llm_calls", "tool_calls", "tokens_produced",
	"total_cost_usd", "duration_ms", "success", "started_at", "completed_at",
}

func TestIntelRepo_RecordExecution(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectExec("INSERT INTO execution_records").
		WithArgs("ex1", "c1", "coordinator", 0, "s1",
			5, 3, 2, 100, 0.01, int64(500), true, now, now).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.RecordExecution(context.Background(), &persist.ExecutionRecord{
		ID: "ex1", CPNID: "c1", CPNRole: "coordinator", CPNDepth: 0, SessionID: "s1",
		TransitionsFired: 5, LLMCalls: 3, ToolCalls: 2, TokensProduced: 100,
		TotalCostUSD: 0.01, DurationMs: 500, Success: true, StartedAt: now, CompletedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestIntelRepo_RecordExecution_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectExec("INSERT INTO execution_records").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db down"))

	err := repo.RecordExecution(context.Background(), &persist.ExecutionRecord{
		ID: "ex1", StartedAt: now, CompletedAt: now,
	})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestIntelRepo_QueryByRole(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)
	from := now.Add(-time.Hour)

	mock.ExpectQuery("SELECT .+ FROM execution_records WHERE cpn_role").
		WithArgs("coordinator", from, now).
		WillReturnRows(pgxmock.NewRows(execCols).
			AddRow("ex1", "c1", "coordinator", 0, "s1", 5, 3, 2, 100, 0.01, int64(500), true, now, now).
			AddRow("ex2", "c2", "coordinator", 0, "s2", 3, 1, 1, 50, 0.005, int64(200), false, now, now))

	result, err := repo.QueryByRole(context.Background(), "coordinator", from, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestIntelRepo_QueryByRole_Empty(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM execution_records WHERE cpn_role").
		WithArgs("nobody", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(execCols))

	result, err := repo.QueryByRole(context.Background(), "nobody", now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("want 0, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestIntelRepo_Aggregate(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)
	from := now.Add(-time.Hour)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs("coordinator", from, now).
		WillReturnRows(pgxmock.NewRows([]string{
			"count", "avg_llm", "avg_tool", "avg_cost", "avg_dur", "success_rate",
		}).AddRow(int64(10), 2.5, 1.5, 0.01, 300.0, 0.9))

	m, err := repo.Aggregate(context.Background(), "coordinator", from, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExecutionCount != 10 || m.Role != "coordinator" {
		t.Fatalf("unexpected: %+v", m)
	}
	expectMet(t, mock)
}

func TestIntelRepo_Aggregate_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT COUNT").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	_, err := repo.Aggregate(context.Background(), "r", now, now)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestIntelRepo_TopFlows(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM execution_records\\s+GROUP BY").
		WithArgs(5).
		WillReturnRows(pgxmock.NewRows([]string{
			"cpn_id", "cpn_role", "exec_count", "success_rate", "avg_cost", "avg_dur",
		}).
			AddRow("c1", "coordinator", int64(10), 0.95, 0.01, int64(200)).
			AddRow("c2", "planner", int64(5), 0.80, 0.02, int64(300)))

	result, err := repo.TopFlows(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("want 2, got %d", len(result))
	}
	if result[0].Hash != "c1" {
		t.Fatalf("unexpected first: %+v", result[0])
	}
	// Verify score computation
	expectedScore := 0.95*100 - 0.01*10
	if result[0].Score != expectedScore {
		t.Fatalf("want score %f, got %f", expectedScore, result[0].Score)
	}
	expectMet(t, mock)
}

func TestIntelRepo_TopFlows_Empty(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM execution_records\\s+GROUP BY").
		WithArgs(10).
		WillReturnRows(pgxmock.NewRows([]string{
			"cpn_id", "cpn_role", "exec_count", "success_rate", "avg_cost", "avg_dur",
		}))

	result, err := repo.TopFlows(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("want 0, got %d", len(result))
	}
	expectMet(t, mock)
}

func TestIntelRepo_TopFlows_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM execution_records").
		WithArgs(5).
		WillReturnError(errors.New("fail"))

	_, err := repo.TopFlows(context.Background(), 5)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

// ---------------------------------------------------------------------------
// POOL — NewPool empty DSN (no mock needed, pure validation)
// ---------------------------------------------------------------------------

func TestNewPool_EmptyDSN(t *testing.T) {
	_, err := NewPool(context.Background(), PoolConfig{DSN: ""})
	if err == nil {
		t.Fatal("want error for empty DSN")
	}
}

func TestHealth_Success(t *testing.T) {
	mock := newMock(t)

	mock.ExpectQuery("SELECT 1").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))

	err := Health(context.Background(), mock)
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestHealth_Error(t *testing.T) {
	mock := newMock(t)

	mock.ExpectQuery("SELECT 1").
		WillReturnError(errors.New("connection refused"))

	err := Health(context.Background(), mock)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestClose_NoPanic(t *testing.T) {
	mock := newMock(t)
	// pgxmock.Close() is already called by t.Cleanup, just verify Close() doesn't panic
	Close(mock)
}

// ---------------------------------------------------------------------------
// Additional error-path tests to increase coverage
// ---------------------------------------------------------------------------

func TestSessionRepo_Create_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO sessions").
		WithArgs("s1", "u1", "web", "active", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("connection reset"))

	err := repo.Create(context.Background(), &persist.SessionRecord{
		ID: "s1", UserID: "u1", Channel: "web", State: persist.SessionActive,
		CreatedAt: now, LastActivityAt: now,
	})
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, persist.ErrSessionExists) {
		t.Fatal("should not be ErrSessionExists")
	}
	expectMet(t, mock)
}

func TestSessionRepo_Get_MessageQueryError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE id").
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "channel", "state",
			"created_at", "last_activity_at", "closed_at", "metadata",
		}).AddRow("s1", "u1", "web", "active", now, now, nil, nil))

	mock.ExpectQuery("SELECT .+ FROM messages WHERE session_id").
		WithArgs("s1").
		WillReturnError(errors.New("msg query fail"))

	_, err := repo.Get(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_GetByUserID_QueryError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE user_id").
		WithArgs("u1").
		WillReturnError(errors.New("fail"))

	_, err := repo.GetByUserID(context.Background(), "u1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_Touch_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET last_activity_at").
		WithArgs("s1").
		WillReturnError(errors.New("fail"))

	err := repo.Touch(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_Close_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state = 'closed'").
		WithArgs("s1").
		WillReturnError(errors.New("fail"))

	err := repo.Close(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_ListExpired_QueryError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT id FROM sessions WHERE state = 'active'").
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	_, err := repo.ListExpired(context.Background(), now)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_Delete_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("DELETE FROM sessions WHERE id").
		WithArgs("s1").
		WillReturnError(errors.New("fail"))

	err := repo.Delete(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_UpdateState_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("UPDATE sessions SET state").
		WithArgs("s1", "active", pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	err := repo.UpdateState(context.Background(), "s1", persist.SessionActive)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_AppendMessage_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectExec("INSERT INTO messages").
		WithArgs("m1", "s1", "user", "hi", "", "", 0, pgxmock.AnyArg()).
		WillReturnError(errors.New("generic db error"))

	err := repo.AppendMessage(context.Background(), "s1", &persist.MessageRecord{
		ID: "m1", Role: "user", Content: "hi", Timestamp: now,
	})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryBySession_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM events WHERE session_id").
		WithArgs("s1", 101).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryBySession(context.Background(), "s1", nil)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByCPN_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM events WHERE cpn_id").
		WithArgs("c1", 101).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByCPN(context.Background(), "c1", nil)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByType_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM events WHERE type").
		WithArgs("t", pgxmock.AnyArg(), pgxmock.AnyArg(), 101).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByType(context.Background(), "t", now, now, nil)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryBySession_CursorError(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 2, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE session_id .+ AND \\(timestamp, id\\)").
		WithArgs("s1", pgxmock.AnyArg(), "e0", 3).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryBySession(context.Background(), "s1", opts)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByCPN_CursorError(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)

	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 1, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE cpn_id .+ AND \\(timestamp, id\\)").
		WithArgs("c1", pgxmock.AnyArg(), "e0", 2).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByCPN(context.Background(), "c1", opts)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestEventRepo_QueryByType_CursorError(t *testing.T) {
	mock := newMock(t)
	repo := NewEventRepository(mock)
	from := now.Add(-time.Hour)
	cursor := buildCursor(&persist.EventRecord{ID: "e0", Timestamp: now})
	opts := &persist.EventQueryOpts{Limit: 5, Cursor: cursor}

	mock.ExpectQuery("SELECT .+ FROM events WHERE type .+ AND \\(timestamp, id\\)").
		WithArgs("t", from, now, pgxmock.AnyArg(), "e0", 6).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByType(context.Background(), "t", from, now, opts)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestFlowRepo_Save_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("INSERT INTO flows").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	err := repo.Save(context.Background(), &persist.FlowRecord{
		Hash: "h1", TopologyJSON: json.RawMessage(`{}`), FunctionMapping: json.RawMessage(`{}`),
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestFlowRepo_List_QueryError(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE deleted_at IS NULL\\s+ORDER BY created_at").
		WithArgs(101, 0).
		WillReturnError(errors.New("fail"))

	_, err := repo.List(context.Background(), nil)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestFlowRepo_UpdateStats_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET execution_count").
		WithArgs("h1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	err := repo.UpdateStats(context.Background(), "h1", &persist.FlowStats{})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestFlowRepo_Delete_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectExec("UPDATE flows SET deleted_at").
		WithArgs("h1", pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	err := repo.Delete(context.Background(), "h1")
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestLedgerRepo_QueryByDate_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE").
		WithArgs(now).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByDate(context.Background(), now)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestLedgerRepo_SetDailyTotal_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectExec("UPDATE token_ledger SET daily_total_usd").
		WithArgs("s1", 1.23).
		WillReturnError(errors.New("fail"))

	err := repo.SetDailyTotal(context.Background(), "s1", now, 1.23)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestIntelRepo_QueryByRole_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewIntelligenceRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM execution_records WHERE cpn_role").
		WithArgs("r", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("fail"))

	_, err := repo.QueryByRole(context.Background(), "r", now, now)
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestFlowRepo_GetByHash_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewFlowRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM flows WHERE hash").
		WithArgs("h1").
		WillReturnError(errors.New("connection reset"))

	_, err := repo.GetByHash(context.Background(), "h1")
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, persist.ErrFlowNotFound) {
		t.Fatal("should not be ErrFlowNotFound for generic DB error")
	}
	expectMet(t, mock)
}

func TestLedgerRepo_GetBySession_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewLedgerRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM token_ledger WHERE session_id").
		WithArgs("s1").
		WillReturnError(errors.New("connection reset"))

	_, err := repo.GetBySession(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, persist.ErrLedgerNotFound) {
		t.Fatal("should not be ErrLedgerNotFound for generic DB error")
	}
	expectMet(t, mock)
}

func TestSessionRepo_Get_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewSessionRepository(mock)

	mock.ExpectQuery("SELECT .+ FROM sessions WHERE id").
		WithArgs("s1").
		WillReturnError(errors.New("connection reset"))

	_, err := repo.Get(context.Background(), "s1")
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, persist.ErrSessionNotFound) {
		t.Fatal("should not be ErrSessionNotFound for generic DB error")
	}
	expectMet(t, mock)
}
