package redis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestRedis starts a miniredis and returns a connected go-redis client.
// The caller should defer mr.Close().
func newTestRedis(t *testing.T) (*miniredis.Miniredis, *goredis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return mr, client
}

// ---------------------------------------------------------------------------
// mockSessionRepo — implements persist.SessionRepository
// ---------------------------------------------------------------------------

type mockSessionRepo struct {
	createFn      func(ctx context.Context, s *persist.SessionRecord) error
	getFn         func(ctx context.Context, id string) (*persist.SessionRecord, error)
	getByUserIDFn func(ctx context.Context, uid string) ([]*persist.SessionRecord, error)
	appendMsgFn   func(ctx context.Context, sid string, msg *persist.MessageRecord) error
	updateStateFn func(ctx context.Context, sid string, st persist.SessionState) error
	touchFn       func(ctx context.Context, sid string) error
	closeFn       func(ctx context.Context, sid string) error
	listExpiredFn func(ctx context.Context, before time.Time) ([]string, error)
	deleteFn      func(ctx context.Context, sid string) error

	createCalls      int
	getCalls         int
	getByUserIDCalls int
	appendMsgCalls   int
	updateStateCalls int
	touchCalls       int
	closeCalls       int
	listExpiredCalls int
	deleteCalls      int
}

func (m *mockSessionRepo) Create(ctx context.Context, s *persist.SessionRecord) error {
	m.createCalls++
	if m.createFn != nil {
		return m.createFn(ctx, s)
	}
	return nil
}

func (m *mockSessionRepo) Get(ctx context.Context, id string) (*persist.SessionRecord, error) {
	m.getCalls++
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return &persist.SessionRecord{ID: id}, nil
}

func (m *mockSessionRepo) GetByUserID(ctx context.Context, uid string) ([]*persist.SessionRecord, error) {
	m.getByUserIDCalls++
	if m.getByUserIDFn != nil {
		return m.getByUserIDFn(ctx, uid)
	}
	return nil, nil
}

func (m *mockSessionRepo) AppendMessage(ctx context.Context, sid string, msg *persist.MessageRecord) error {
	m.appendMsgCalls++
	if m.appendMsgFn != nil {
		return m.appendMsgFn(ctx, sid, msg)
	}
	return nil
}

func (m *mockSessionRepo) UpdateState(ctx context.Context, sid string, st persist.SessionState) error {
	m.updateStateCalls++
	if m.updateStateFn != nil {
		return m.updateStateFn(ctx, sid, st)
	}
	return nil
}

func (m *mockSessionRepo) Touch(ctx context.Context, sid string) error {
	m.touchCalls++
	if m.touchFn != nil {
		return m.touchFn(ctx, sid)
	}
	return nil
}

func (m *mockSessionRepo) Close(ctx context.Context, sid string) error {
	m.closeCalls++
	if m.closeFn != nil {
		return m.closeFn(ctx, sid)
	}
	return nil
}

func (m *mockSessionRepo) ListExpired(ctx context.Context, before time.Time) ([]string, error) {
	m.listExpiredCalls++
	if m.listExpiredFn != nil {
		return m.listExpiredFn(ctx, before)
	}
	return nil, nil
}

func (m *mockSessionRepo) Delete(ctx context.Context, sid string) error {
	m.deleteCalls++
	if m.deleteFn != nil {
		return m.deleteFn(ctx, sid)
	}
	return nil
}

// ---------------------------------------------------------------------------
// pool.go tests
// ---------------------------------------------------------------------------

func TestNewRedisPool_ValidURL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client, err := NewRedisPool("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	defer client.Close()
}

func TestNewRedisPool_InvalidURL(t *testing.T) {
	_, err := NewRedisPool("not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestNewRedisPool_UnreachableURL(t *testing.T) {
	// Valid URL format but nothing listening on that port.
	_, err := NewRedisPool("redis://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

// ---------------------------------------------------------------------------
// session.go tests
// ---------------------------------------------------------------------------

func TestSessionRepository_Create(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	sess := &persist.SessionRecord{ID: "s1", UserID: "u1", State: persist.SessionActive}
	if err := repo.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if mock.createCalls != 1 {
		t.Fatalf("expected 1 fallback Create call, got %d", mock.createCalls)
	}
	// Verify cached in Redis.
	data, err := client.Get(ctx, sessionKey("s1")).Bytes()
	if err != nil {
		t.Fatalf("expected cache entry, got err: %v", err)
	}
	var cached persist.SessionRecord
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal cached: %v", err)
	}
	if cached.ID != "s1" {
		t.Fatalf("cached ID = %q, want s1", cached.ID)
	}
}

func TestSessionRepository_Get_CacheHit(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Pre-populate cache.
	sess := &persist.SessionRecord{ID: "s2", UserID: "u2"}
	data, _ := json.Marshal(sess)
	client.Set(ctx, sessionKey("s2"), data, defaultSessionTTL)

	got, err := repo.Get(ctx, "s2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "s2" {
		t.Fatalf("ID = %q, want s2", got.ID)
	}
	if mock.getCalls != 0 {
		t.Fatalf("fallback Get should not be called on cache hit, got %d calls", mock.getCalls)
	}
}

func TestSessionRepository_Get_CacheMiss(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{
		getFn: func(_ context.Context, id string) (*persist.SessionRecord, error) {
			return &persist.SessionRecord{ID: id, UserID: "from-pg"}, nil
		},
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	got, err := repo.Get(ctx, "s3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "from-pg" {
		t.Fatalf("UserID = %q, want from-pg", got.UserID)
	}
	if mock.getCalls != 1 {
		t.Fatalf("expected 1 fallback Get, got %d", mock.getCalls)
	}
	// Verify it was cached after miss.
	if _, err := client.Get(ctx, sessionKey("s3")).Bytes(); err != nil {
		t.Fatalf("expected cache populated after miss, got err: %v", err)
	}
}

func TestSessionRepository_GetByUserID(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	expected := []*persist.SessionRecord{{ID: "a"}, {ID: "b"}}
	mock := &mockSessionRepo{
		getByUserIDFn: func(_ context.Context, _ string) ([]*persist.SessionRecord, error) {
			return expected, nil
		},
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	got, err := repo.GetByUserID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByUserID: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if mock.getByUserIDCalls != 1 {
		t.Fatalf("expected 1 fallback call, got %d", mock.getByUserIDCalls)
	}
}

func TestSessionRepository_AppendMessage(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	msg := &persist.MessageRecord{ID: "m1", SessionID: "s1", Content: "hello"}
	if err := repo.AppendMessage(ctx, "s1", msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if mock.appendMsgCalls != 1 {
		t.Fatalf("expected 1 fallback AppendMessage, got %d", mock.appendMsgCalls)
	}
	// Verify message was appended to Redis list.
	llen := client.LLen(ctx, messagesKey("s1")).Val()
	if llen != 1 {
		t.Fatalf("messages list len = %d, want 1", llen)
	}
}

func TestSessionRepository_AppendMessage_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg down")
	mock := &mockSessionRepo{
		appendMsgFn: func(_ context.Context, _ string, _ *persist.MessageRecord) error {
			return wantErr
		},
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	msg := &persist.MessageRecord{ID: "m1", SessionID: "s1"}
	err := repo.AppendMessage(ctx, "s1", msg)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	// Redis should NOT have been written to.
	llen := client.LLen(ctx, messagesKey("s1")).Val()
	if llen != 0 {
		t.Fatalf("messages list should be empty, got %d", llen)
	}
}

func TestSessionRepository_UpdateState_Closed(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Pre-populate cache.
	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)
	client.RPush(ctx, messagesKey("s1"), "msg")

	if err := repo.UpdateState(ctx, "s1", persist.SessionClosed); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if mock.updateStateCalls != 1 {
		t.Fatalf("expected 1 fallback call, got %d", mock.updateStateCalls)
	}
	// Both keys should be evicted.
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("session key should be evicted")
	}
	if client.Exists(ctx, messagesKey("s1")).Val() != 0 {
		t.Fatal("messages key should be evicted")
	}
}

func TestSessionRepository_UpdateState_Active(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Pre-populate cache.
	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)

	if err := repo.UpdateState(ctx, "s1", persist.SessionActive); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	// Session key should be invalidated (deleted), but messages list untouched.
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("session key should be invalidated")
	}
}

func TestSessionRepository_Touch(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Pre-populate cache with short TTL.
	client.Set(ctx, sessionKey("s1"), "data", 1*time.Second)

	if err := repo.Touch(ctx, "s1"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if mock.touchCalls != 1 {
		t.Fatalf("expected 1 fallback Touch, got %d", mock.touchCalls)
	}
	// TTL should have been reset to defaultSessionTTL.
	ttl := client.TTL(ctx, sessionKey("s1")).Val()
	if ttl < 29*time.Minute {
		t.Fatalf("TTL = %v, expected ~30m", ttl)
	}
}

func TestSessionRepository_Close(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)
	client.RPush(ctx, messagesKey("s1"), "msg")

	if err := repo.Close(ctx, "s1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if mock.closeCalls != 1 {
		t.Fatalf("expected 1 fallback Close, got %d", mock.closeCalls)
	}
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("session key should be evicted")
	}
	if client.Exists(ctx, messagesKey("s1")).Val() != 0 {
		t.Fatal("messages key should be evicted")
	}
}

func TestSessionRepository_ListExpired(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	expected := []string{"s1", "s2"}
	mock := &mockSessionRepo{
		listExpiredFn: func(_ context.Context, _ time.Time) ([]string, error) {
			return expected, nil
		},
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	got, err := repo.ListExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if mock.listExpiredCalls != 1 {
		t.Fatalf("expected 1 fallback call, got %d", mock.listExpiredCalls)
	}
}

func TestSessionRepository_Delete(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)
	client.RPush(ctx, messagesKey("s1"), "msg")

	if err := repo.Delete(ctx, "s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mock.deleteCalls != 1 {
		t.Fatalf("expected 1 fallback Delete, got %d", mock.deleteCalls)
	}
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("session key should be evicted")
	}
}

func TestSessionRepository_Create_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg create fail")
	mock := &mockSessionRepo{
		createFn: func(_ context.Context, _ *persist.SessionRecord) error { return wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	err := repo.Create(ctx, &persist.SessionRecord{ID: "s1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	// Redis should NOT have been written.
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("cache should not be populated on fallback error")
	}
}

func TestSessionRepository_Get_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg get fail")
	mock := &mockSessionRepo{
		getFn: func(_ context.Context, _ string) (*persist.SessionRecord, error) { return nil, wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	_, err := repo.Get(ctx, "s1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
}

func TestSessionRepository_Close_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg close fail")
	mock := &mockSessionRepo{
		closeFn: func(_ context.Context, _ string) error { return wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Pre-populate cache to verify it is NOT evicted on fallback error.
	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)

	err := repo.Close(ctx, "s1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	// Cache should still exist.
	if client.Exists(ctx, sessionKey("s1")).Val() == 0 {
		t.Fatal("cache should not be evicted on fallback error")
	}
}

func TestSessionRepository_Delete_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg delete fail")
	mock := &mockSessionRepo{
		deleteFn: func(_ context.Context, _ string) error { return wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)

	err := repo.Delete(ctx, "s1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	if client.Exists(ctx, sessionKey("s1")).Val() == 0 {
		t.Fatal("cache should not be evicted on fallback error")
	}
}

func TestSessionRepository_Touch_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg touch fail")
	mock := &mockSessionRepo{
		touchFn: func(_ context.Context, _ string) error { return wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	err := repo.Touch(ctx, "s1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
}

func TestSessionRepository_UpdateState_FallbackError(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	wantErr := errors.New("pg update fail")
	mock := &mockSessionRepo{
		updateStateFn: func(_ context.Context, _ string, _ persist.SessionState) error { return wantErr },
	}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	err := repo.UpdateState(ctx, "s1", persist.SessionClosed)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
}

func TestSessionRepository_UpdateState_Expired(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	client.Set(ctx, sessionKey("s1"), "data", defaultSessionTTL)
	client.RPush(ctx, messagesKey("s1"), "msg")

	if err := repo.UpdateState(ctx, "s1", persist.SessionExpired); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	// Expired state should evict, same as closed.
	if client.Exists(ctx, sessionKey("s1")).Val() != 0 {
		t.Fatal("session key should be evicted for expired state")
	}
	if client.Exists(ctx, messagesKey("s1")).Val() != 0 {
		t.Fatal("messages key should be evicted for expired state")
	}
}

func TestSessionRepository_Create_GracefulDegradation(t *testing.T) {
	mr, client := newTestRedis(t)

	mock := &mockSessionRepo{}
	repo := NewSessionRepository(client, mock)
	ctx := context.Background()

	// Stop Redis AFTER constructing the repo.
	mr.Close()

	sess := &persist.SessionRecord{ID: "s1", UserID: "u1"}
	err := repo.Create(ctx, sess)
	if err != nil {
		t.Fatalf("Create should succeed even if Redis is down, got: %v", err)
	}
	if mock.createCalls != 1 {
		t.Fatalf("fallback Create should have been called, got %d", mock.createCalls)
	}
}

// ---------------------------------------------------------------------------
// hitl.go tests
// ---------------------------------------------------------------------------

func TestHITLRepository_Enqueue(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	req := &persist.HITLPendingRequest{
		SessionID:    "s1",
		TransitionID: "t1",
		CPNID:        "cpn1",
		Proposal:     json.RawMessage(`{"action":"approve"}`),
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
	if err := repo.Enqueue(ctx, req); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Verify key exists with TTL.
	key := hitlKey("s1", "t1")
	ttl := client.TTL(ctx, key).Val()
	if ttl <= 0 {
		t.Fatalf("expected positive TTL, got %v", ttl)
	}
}

func TestHITLRepository_Enqueue_Expired(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	req := &persist.HITLPendingRequest{
		SessionID:    "s1",
		TransitionID: "t1",
		ExpiresAt:    time.Now().Add(-1 * time.Minute), // already expired
	}
	err := repo.Enqueue(ctx, req)
	if !errors.Is(err, persist.ErrHITLExpired) {
		t.Fatalf("expected ErrHITLExpired, got %v", err)
	}
}

func TestHITLRepository_Dequeue(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	req := &persist.HITLPendingRequest{
		SessionID:    "s1",
		TransitionID: "t1",
		CPNID:        "cpn1",
		Proposal:     json.RawMessage(`{"x":1}`),
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
	if err := repo.Enqueue(ctx, req); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := repo.Dequeue(ctx, "s1", "t1")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got.CPNID != "cpn1" {
		t.Fatalf("CPNID = %q, want cpn1", got.CPNID)
	}
	// Key should be deleted after dequeue.
	if client.Exists(ctx, hitlKey("s1", "t1")).Val() != 0 {
		t.Fatal("key should be deleted after Dequeue")
	}
}

func TestHITLRepository_Dequeue_NotFound(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	_, err := repo.Dequeue(ctx, "none", "none")
	if !errors.Is(err, persist.ErrHITLNotFound) {
		t.Fatalf("expected ErrHITLNotFound, got %v", err)
	}
}

func TestHITLRepository_ListPending(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	for _, tid := range []string{"t1", "t2"} {
		req := &persist.HITLPendingRequest{
			SessionID:    "s1",
			TransitionID: tid,
			CPNID:        "cpn1",
			CreatedAt:    time.Now(),
			ExpiresAt:    time.Now().Add(5 * time.Minute),
		}
		if err := repo.Enqueue(ctx, req); err != nil {
			t.Fatalf("Enqueue %s: %v", tid, err)
		}
	}

	got, err := repo.ListPending(ctx, "s1")
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestHITLRepository_ListPending_Empty(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	got, err := repo.ListPending(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}

func TestHITLRepository_Expire(t *testing.T) {
	mr, client := newTestRedis(t)
	defer mr.Close()

	repo := NewHITLRepository(client)
	ctx := context.Background()

	// Enqueue a request.
	req := &persist.HITLPendingRequest{
		SessionID:    "s1",
		TransitionID: "t1",
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
	if err := repo.Enqueue(ctx, req); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Expire should run without error (Redis handles TTL natively).
	_, err := repo.Expire(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
}
