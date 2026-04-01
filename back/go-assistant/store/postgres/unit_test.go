package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ---------------------------------------------------------------------------
// 1. pool.go — PoolConfig
// ---------------------------------------------------------------------------

func TestPoolConfig_String_RedactsCredentials(t *testing.T) {
	cfg := PoolConfig{DSN: "postgres://secretuser:secretpass@db.example.com:5432/mydb?sslmode=disable"}
	s := cfg.String()
	if strings.Contains(s, "secretuser") || strings.Contains(s, "secretpass") {
		t.Fatalf("credentials leaked: %s", s)
	}
	// url.UserPassword encodes * as %2A in the URL string
	if !strings.Contains(s, "***") && !strings.Contains(s, "%2A%2A%2A") {
		t.Fatalf("expected redacted marker: %s", s)
	}
}

func TestPoolConfig_String_PreservesHostPortDB(t *testing.T) {
	cfg := PoolConfig{DSN: "postgres://u:p@myhost:9999/testdb"}
	s := cfg.String()
	for _, want := range []string{"myhost", "9999", "testdb"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestPoolConfig_String_InvalidDSN(t *testing.T) {
	cfg := PoolConfig{DSN: "://bad"}
	s := cfg.String()
	if !strings.Contains(s, "<invalid>") {
		t.Fatalf("expected <invalid> for bad DSN, got: %s", s)
	}
}

func TestPoolConfig_String_NoUser(t *testing.T) {
	cfg := PoolConfig{DSN: "postgres://localhost:5432/mydb"}
	s := cfg.String()
	if strings.Contains(s, "***") {
		t.Fatalf("should not redact when no user info present: %s", s)
	}
	if !strings.Contains(s, "localhost") {
		t.Fatalf("host missing: %s", s)
	}
}

func TestPoolConfig_Defaults(t *testing.T) {
	cfg := PoolConfig{} // all zero values

	if got := cfg.maxConns(); got != 10 {
		t.Fatalf("maxConns default: want 10, got %d", got)
	}
	if got := cfg.minConns(); got != 2 {
		t.Fatalf("minConns default: want 2, got %d", got)
	}
	if got := cfg.maxConnLifetime(); got != 30*time.Minute {
		t.Fatalf("maxConnLifetime default: want 30m, got %v", got)
	}
	if got := cfg.healthCheckInterval(); got != 15*time.Second {
		t.Fatalf("healthCheckInterval default: want 15s, got %v", got)
	}
}

func TestPoolConfig_CustomOverrides(t *testing.T) {
	cfg := PoolConfig{
		MaxConns:            20,
		MinConns:            5,
		MaxConnLifetime:     1 * time.Hour,
		HealthCheckInterval: 30 * time.Second,
	}

	if got := cfg.maxConns(); got != 20 {
		t.Fatalf("maxConns: want 20, got %d", got)
	}
	if got := cfg.minConns(); got != 5 {
		t.Fatalf("minConns: want 5, got %d", got)
	}
	if got := cfg.maxConnLifetime(); got != 1*time.Hour {
		t.Fatalf("maxConnLifetime: want 1h, got %v", got)
	}
	if got := cfg.healthCheckInterval(); got != 30*time.Second {
		t.Fatalf("healthCheckInterval: want 30s, got %v", got)
	}
}

func TestPoolConfig_NegativeValuesUseDefaults(t *testing.T) {
	cfg := PoolConfig{
		MaxConns:            -1,
		MinConns:            -1,
		MaxConnLifetime:     -1,
		HealthCheckInterval: -1,
	}
	if got := cfg.maxConns(); got != 10 {
		t.Fatalf("maxConns negative: want 10, got %d", got)
	}
	if got := cfg.minConns(); got != 2 {
		t.Fatalf("minConns negative: want 2, got %d", got)
	}
	if got := cfg.maxConnLifetime(); got != 30*time.Minute {
		t.Fatalf("maxConnLifetime negative: want 30m, got %v", got)
	}
	if got := cfg.healthCheckInterval(); got != 15*time.Second {
		t.Fatalf("healthCheckInterval negative: want 15s, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// 2. event.go — Pure functions
// ---------------------------------------------------------------------------

func TestParseOpts_NilReturnsDefaults(t *testing.T) {
	limit, cursor := parseOpts(nil)
	if limit != 100 {
		t.Fatalf("default limit: want 100, got %d", limit)
	}
	if cursor != "" {
		t.Fatalf("default cursor: want empty, got %q", cursor)
	}
}

func TestParseOpts_CustomValues(t *testing.T) {
	opts := &persist.EventQueryOpts{Limit: 50, Cursor: "abc123"}
	limit, cursor := parseOpts(opts)
	if limit != 50 {
		t.Fatalf("limit: want 50, got %d", limit)
	}
	if cursor != "abc123" {
		t.Fatalf("cursor: want abc123, got %q", cursor)
	}
}

func TestParseOpts_ZeroLimitUsesDefault(t *testing.T) {
	opts := &persist.EventQueryOpts{Limit: 0, Cursor: "cur"}
	limit, cursor := parseOpts(opts)
	if limit != 100 {
		t.Fatalf("zero limit should default to 100, got %d", limit)
	}
	if cursor != "cur" {
		t.Fatalf("cursor: want cur, got %q", cursor)
	}
}

func TestParseCursor_Valid(t *testing.T) {
	now := time.Now()
	nanos := now.UnixNano()
	cursorStr := fmt.Sprintf("%d|event-42", nanos)

	ts, eid := parseCursor(cursorStr)
	if eid != "event-42" {
		t.Fatalf("event id: want event-42, got %q", eid)
	}
	if ts.UnixNano() != nanos {
		t.Fatalf("timestamp nanos: want %d, got %d", nanos, ts.UnixNano())
	}
}

func TestParseCursor_NoPipe(t *testing.T) {
	ts, eid := parseCursor("nopipehere")
	if !ts.IsZero() {
		t.Fatalf("timestamp should be zero for invalid cursor, got %v", ts)
	}
	// When no pipe, eid gets the full cursor string
	if eid != "nopipehere" {
		t.Fatalf("eid: want nopipehere, got %q", eid)
	}
}

func TestParseCursor_EmptyString(t *testing.T) {
	ts, eid := parseCursor("")
	if !ts.IsZero() {
		t.Fatalf("timestamp should be zero, got %v", ts)
	}
	if eid != "" {
		t.Fatalf("eid should be empty, got %q", eid)
	}
}

func TestParseCursor_InvalidTimestamp(t *testing.T) {
	ts, eid := parseCursor("notanumber|event-1")
	if !ts.IsZero() {
		t.Fatalf("timestamp should be zero for non-numeric prefix, got %v", ts)
	}
	if eid != "event-1" {
		t.Fatalf("eid: want event-1, got %q", eid)
	}
}

func TestBuildCursor(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC)
	e := &persist.EventRecord{ID: "ev-abc", Timestamp: ts}

	cursor := buildCursor(e)
	wantPrefix := strconv.FormatInt(ts.UnixNano(), 10)
	want := wantPrefix + "|ev-abc"
	if cursor != want {
		t.Fatalf("buildCursor: want %q, got %q", want, cursor)
	}
}

func TestBuildCursor_Roundtrip(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	e := &persist.EventRecord{ID: "roundtrip-id", Timestamp: ts}

	cursor := buildCursor(e)
	parsedTS, parsedID := parseCursor(cursor)

	if parsedID != e.ID {
		t.Fatalf("roundtrip ID: want %q, got %q", e.ID, parsedID)
	}
	if !parsedTS.Equal(ts) {
		t.Fatalf("roundtrip timestamp: want %v, got %v", ts, parsedTS)
	}
}

// ---------------------------------------------------------------------------
// 3. event_batcher.go — Batcher logic with mock appender
// ---------------------------------------------------------------------------

// mockAppender records calls and can be configured to return errors.
type mockAppender struct {
	mu         sync.Mutex
	calls      []int // number of events per call
	total      int
	err        error // if non-nil, all Append calls return this
	failCount  int   // fail this many times then succeed (0 = always use err)
	failsSoFar int
}

func (m *mockAppender) Append(_ context.Context, events ...*persist.EventRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, len(events))
	if m.err != nil {
		if m.failCount == 0 || m.failsSoFar < m.failCount {
			m.failsSoFar++
			return m.err
		}
	}
	m.total += len(events)
	return nil
}

func (m *mockAppender) totalEvents() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.total
}

func (m *mockAppender) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func makeEvent(id string) *persist.EventRecord {
	return &persist.EventRecord{
		ID:        id,
		Type:      "test",
		SessionID: "sess-1",
		CPNID:     "cpn-1",
		Timestamp: time.Now(),
	}
}

func TestBatcher_FlushOnBatchSize(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 5, 10*time.Second) // long interval so only size triggers
	ctx := context.Background()

	for i := range 5 {
		if err := batcher.Submit(ctx, makeEvent(fmt.Sprintf("e%d", i))); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}

	// Give the goroutine time to flush
	time.Sleep(100 * time.Millisecond)

	if got := mock.totalEvents(); got != 5 {
		t.Fatalf("want 5 flushed on size, got %d", got)
	}

	if err := batcher.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBatcher_FlushOnInterval(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 100, 100*time.Millisecond) // small interval
	ctx := context.Background()

	for i := range 3 {
		if err := batcher.Submit(ctx, makeEvent(fmt.Sprintf("e%d", i))); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}

	// Wait for interval flush
	time.Sleep(300 * time.Millisecond)

	if got := mock.totalEvents(); got != 3 {
		t.Fatalf("want 3 flushed on interval, got %d", got)
	}

	if err := batcher.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBatcher_CloseFlushesRemaining(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 100, 10*time.Second) // won't flush by size or interval
	ctx := context.Background()

	for i := range 7 {
		if err := batcher.Submit(ctx, makeEvent(fmt.Sprintf("e%d", i))); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}

	if err := batcher.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := mock.totalEvents(); got != 7 {
		t.Fatalf("want 7 after Close, got %d", got)
	}
}

func TestBatcher_CloseReturnsFlushError(t *testing.T) {
	flushErr := errors.New("db down")
	mock := &mockAppender{err: flushErr}
	batcher := NewEventBatcher(mock, 100, 10*time.Second)
	ctx := context.Background()

	_ = batcher.Submit(ctx, makeEvent("e0"))

	err := batcher.Close(ctx)
	if err == nil {
		t.Fatal("Close should return flush error")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatcher_SubmitCancelledContext(t *testing.T) {
	mock := &mockAppender{}
	// Use batchSize=1 so channel capacity is 2. Fill it up so Submit blocks.
	batcher := NewEventBatcher(mock, 1, 10*time.Second)

	// The run() goroutine will consume some events; we need to fill the channel
	// faster than it drains. First, pause: submit enough to fill ch (cap=2).
	// The goroutine may consume one, so submit 3 to guarantee the channel is full.
	bgCtx := context.Background()
	for i := 0; i < 3; i++ {
		// These may or may not block depending on goroutine timing
		go func(i int) {
			_ = batcher.Submit(bgCtx, makeEvent(fmt.Sprintf("fill%d", i)))
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // let channel fill up

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Now the channel is full, so Submit must choose between ctx.Done and ch
	err := batcher.Submit(ctx, makeEvent("blocked"))
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("want nil or context.Canceled, got %v", err)
	}

	_ = batcher.Close(bgCtx)
}

func TestBatcher_SubmitAfterClose(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 100, 10*time.Second)
	ctx := context.Background()

	_ = batcher.Close(ctx)

	// After Close, the done channel is closed. Submit may succeed (if ch has capacity)
	// or return ErrEventAppendFailed (if the done case wins). Both are valid Go select
	// behavior. We verify it does NOT hang and that if an error is returned, it's the
	// expected one.
	err := batcher.Submit(ctx, makeEvent("e0"))
	if err != nil && !errors.Is(err, persist.ErrEventAppendFailed) {
		t.Fatalf("want nil or ErrEventAppendFailed, got %v", err)
	}
}

func TestBatcher_ConcurrentSubmit(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 10, 50*time.Millisecond)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = batcher.Submit(ctx, makeEvent(fmt.Sprintf("c%d", i)))
		}(i)
	}
	wg.Wait()

	if err := batcher.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := mock.totalEvents(); got != 50 {
		t.Fatalf("want 50 total, got %d", got)
	}
}

func TestBatcher_RetainsBufferOnFlushFailure(t *testing.T) {
	// Fail the first flush, succeed on the second (interval retry)
	mock := &mockAppender{err: errors.New("transient"), failCount: 1}
	batcher := NewEventBatcher(mock, 3, 100*time.Millisecond)
	ctx := context.Background()

	// Submit exactly batchSize to trigger a size-based flush (which will fail)
	for i := range 3 {
		if err := batcher.Submit(ctx, makeEvent(fmt.Sprintf("r%d", i))); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}

	// Wait for initial failed flush + interval retry
	time.Sleep(350 * time.Millisecond)

	// The retry on interval should have succeeded
	if got := mock.totalEvents(); got != 3 {
		t.Fatalf("want 3 events after retry, got %d (calls: %d)", got, mock.callCount())
	}

	// Close may return the lastErr from the first failed flush — that's expected.
	// The key assertion is that events were NOT lost (totalEvents == 3).
	_ = batcher.Close(ctx)
}

func TestBatcher_DefaultsWhenZeroValues(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 0, 0) // zero values -> defaults

	if batcher.batchSize != 100 {
		t.Fatalf("default batchSize: want 100, got %d", batcher.batchSize)
	}
	if batcher.flushInterval != 500*time.Millisecond {
		t.Fatalf("default flushInterval: want 500ms, got %v", batcher.flushInterval)
	}

	_ = batcher.Close(context.Background())
}

func TestBatcher_NegativeValuesUseDefaults(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, -5, -1*time.Second)

	if batcher.batchSize != 100 {
		t.Fatalf("negative batchSize: want 100, got %d", batcher.batchSize)
	}
	if batcher.flushInterval != 500*time.Millisecond {
		t.Fatalf("negative flushInterval: want 500ms, got %v", batcher.flushInterval)
	}

	_ = batcher.Close(context.Background())
}

// ---------------------------------------------------------------------------
// 4. migrate.go — URL scheme conversion (error paths, no real DB)
// ---------------------------------------------------------------------------

func TestRunMigrations_EmptyDSN(t *testing.T) {
	err := RunMigrations("", "/nonexistent/path")
	if err == nil {
		t.Fatal("RunMigrations with empty DSN should error")
	}
}

func TestRunMigrations_InvalidPath(t *testing.T) {
	err := RunMigrations("postgres://user:pass@localhost:5432/db", "/nonexistent/migrations")
	if err == nil {
		t.Fatal("RunMigrations with invalid path should error")
	}
}

func TestRunMigrations_PostgresSchemeConverted(t *testing.T) {
	// This will fail at connect time, but it exercises the scheme conversion path.
	err := RunMigrations("postgres://user:pass@localhost:5432/db", "/nonexistent")
	if err == nil {
		t.Fatal("expected error (can't connect)")
	}
	// The error should come from migrate.New (source or db), not from a scheme issue.
	// If the conversion didn't happen, the error message would mention "unknown driver".
	errMsg := err.Error()
	if strings.Contains(errMsg, "unknown driver") || strings.Contains(errMsg, "unknown database") {
		t.Fatalf("scheme conversion may have failed: %v", err)
	}
}

func TestRunMigrations_PostgresqlSchemeConverted(t *testing.T) {
	err := RunMigrations("postgresql://user:pass@localhost:5432/db", "/nonexistent")
	if err == nil {
		t.Fatal("expected error (can't connect)")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "unknown driver") || strings.Contains(errMsg, "unknown database") {
		t.Fatalf("postgresql:// scheme conversion may have failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. store.go — Accessor methods
// ---------------------------------------------------------------------------

// Minimal stubs implementing persist interfaces for testing accessors.

type stubSessionRepo struct{ persist.SessionRepository }
type stubEventRepo struct{ persist.EventRepository }
type stubLedgerRepo struct{ persist.LedgerRepository }
type stubFlowRepo struct{ persist.FlowRepository }
type stubIntelligenceRepo struct{ persist.IntelligenceRepository }
type stubHITLRepo struct{ persist.HITLRepository }

func TestStore_Accessors(t *testing.T) {
	sessions := &stubSessionRepo{}
	events := &stubEventRepo{}
	ledger := &stubLedgerRepo{}
	flows := &stubFlowRepo{}
	intel := &stubIntelligenceRepo{}
	hitl := &stubHITLRepo{}

	// Manually construct a Store (white-box: unexported fields)
	s := &Store{
		sessions:     sessions,
		events:       events,
		ledger:       ledger,
		flows:        flows,
		intelligence: intel,
		hitl:         hitl,
	}

	if s.Sessions() != sessions {
		t.Fatal("Sessions() returned wrong instance")
	}
	if s.Events() != events {
		t.Fatal("Events() returned wrong instance")
	}
	if s.Ledger() != ledger {
		t.Fatal("Ledger() returned wrong instance")
	}
	if s.Flows() != flows {
		t.Fatal("Flows() returned wrong instance")
	}
	if s.Intelligence() != intel {
		t.Fatal("Intelligence() returned wrong instance")
	}
	if s.HITL() != hitl {
		t.Fatal("HITL() returned wrong instance")
	}
}

func TestStore_EventBatcherAccessor(t *testing.T) {
	mock := &mockAppender{}
	batcher := NewEventBatcher(mock, 10, time.Second)
	defer batcher.Close(context.Background())

	s := &Store{batcher: batcher}
	if s.EventBatcher() != batcher {
		t.Fatal("EventBatcher() returned wrong instance")
	}
}
