package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Fakes ──────────────────────────────────────────────────────────────────
//
// countingSessionRepo embeds a real in-memory repository but counts calls to
// Get and optionally injects delay/errors. AC-004 needs an exact call counter;
// embedding keeps the rest of the interface satisfied with zero boilerplate.
type countingSessionRepo struct {
	*persist.MemorySessionRepository
	getCalls atomic.Int64
	getDelay time.Duration
	getErr   error
}

func newCountingSessionRepo() *countingSessionRepo {
	return &countingSessionRepo{MemorySessionRepository: persist.NewMemorySessionRepository()}
}

func (r *countingSessionRepo) Get(ctx context.Context, sessionID string) (*persist.SessionRecord, error) {
	r.getCalls.Add(1)
	if r.getDelay > 0 {
		select {
		case <-time.After(r.getDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.MemorySessionRepository.Get(ctx, sessionID)
}

// seedSession writes a session record directly, bypassing the service, to
// simulate "session exists in Postgres but not in memory" (i.e. post-restart).
func (r *countingSessionRepo) seedSession(t *testing.T, rec *persist.SessionRecord) {
	t.Helper()
	if err := r.MemorySessionRepository.Create(context.Background(), rec); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// newRehydrateService wires a SessionService with the given (optional) repo.
// Passing nil yields a service with no persistence (REQ-007 back-compat path).
func newRehydrateService(repo persist.SessionRepository) *SessionService {
	svc := NewSessionService(
		&mockLLMClient{},
		&mockCostProvider{cost: 0.0},
		testLogger(),
		testTopologyFactory,
	)
	if repo != nil {
		svc.persist = &PersistDeps{Sessions: repo}
	}
	return svc
}

// ── Tests ──────────────────────────────────────────────────────────────────

// AC-001: a session that exists in persistence but not in memory must be
// rehydrated on GetSession and returned with a matching SessionInfo.
func TestRehydrate_GetSession_HitFromPersistence(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	created := time.Now().UTC().Truncate(time.Millisecond)
	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-hot", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: created,
		Messages: []*persist.MessageRecord{
			{ID: "m1", SessionID: "sess-hot", Role: "user", Content: "hola", Timestamp: created},
		},
	})

	info, err := svc.GetSession("sess-hot")
	if err != nil {
		t.Fatalf("GetSession after rehydrate: %v", err)
	}
	if info.ID != "sess-hot" || info.UserID != "user-1" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.Channel != cpn.ChannelWeb {
		t.Errorf("channel = %s, want web", info.Channel)
	}
	if !info.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v (should be preserved from persistence)", info.CreatedAt, created)
	}
	if len(info.Messages) != 1 || info.Messages[0].Content != "hola" {
		t.Errorf("messages not replayed from persistence: %+v", info.Messages)
	}
	if got := repo.getCalls.Load(); got != 1 {
		t.Errorf("expected exactly 1 persistence Get call, got %d", got)
	}

	// Second call must be served from memory — no additional persistence hit.
	if _, err := svc.GetSession("sess-hot"); err != nil {
		t.Fatalf("second GetSession: %v", err)
	}
	if got := repo.getCalls.Load(); got != 1 {
		t.Errorf("expected no additional persistence Get, got total %d", got)
	}
}

// AC-002: unknown id produces ErrSessionNotFound and records a miss.
func TestRehydrate_GetSession_Miss(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	_, err := svc.GetSession("does-not-exist")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if got := repo.getCalls.Load(); got != 1 {
		t.Errorf("expected 1 persistence attempt on miss, got %d", got)
	}

	// Must NOT have inserted a ghost into memory.
	svc.mu.RLock()
	_, present := svc.sessions["does-not-exist"]
	svc.mu.RUnlock()
	if present {
		t.Error("miss must not insert a ghost entry into the in-memory map")
	}
}

// AC-003: soft-deleted session is treated as missing and NOT inserted.
func TestRehydrate_GetSession_SoftDeleted(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	now := time.Now()
	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-del", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: now, DeletedAt: &now,
	})

	_, err := svc.GetSession("sess-del")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound for soft-deleted session", err)
	}

	svc.mu.RLock()
	_, present := svc.sessions["sess-del"]
	svc.mu.RUnlock()
	if present {
		t.Error("soft-deleted session must not be inserted into in-memory map")
	}
}

// AC-004: N concurrent misses for the same id collapse to exactly one
// persistence Get via singleflight.
func TestRehydrate_SingleFlight_ConcurrentMisses(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	// Delay the Get to widen the race window so all goroutines pile up.
	repo.getDelay = 20 * time.Millisecond
	svc := newRehydrateService(repo)

	created := time.Now().UTC()
	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-hot", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: created,
	})

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make(chan error, N)
	for range N {
		go func() {
			defer wg.Done()
			_, err := svc.GetSession("sess-hot")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("unexpected error on concurrent rehydrate: %v", err)
		}
	}

	// This is the AC-004 assertion.
	if got := repo.getCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 persist.Sessions.Get call, got %d", got)
	}
}

// REQ-007 / AC-005: no persistence wired → rehydration is a no-op and
// GetSession on a miss still returns ErrSessionNotFound (backwards compat).
func TestRehydrate_NoPersistence_PreservesLegacyBehavior(t *testing.T) {
	t.Parallel()

	svc := newRehydrateService(nil)
	_, err := svc.GetSession("anything")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

// §9.6: persistence I/O failure surfaces as ErrPersistenceUnavailable so the
// handler can map it to HTTP 503.
func TestRehydrate_PersistenceError_MapsToUnavailable(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	repo.getErr = fmt.Errorf("simulated driver failure")
	svc := newRehydrateService(repo)

	_, err := svc.GetSession("sess-x")
	if !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("err = %v, want ErrPersistenceUnavailable", err)
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Error("persistence error must not be conflated with not-found")
	}
}

// StreamChannel must rehydrate on miss and return a live stream channel.
func TestRehydrate_StreamChannel_OnMiss(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-stream", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	ch, err := svc.StreamChannel("sess-stream")
	if err != nil {
		t.Fatalf("StreamChannel: %v", err)
	}
	if ch == nil {
		t.Fatal("StreamChannel returned nil channel")
	}

	// Subsequent call must be a pure memory hit.
	if _, err := svc.StreamChannel("sess-stream"); err != nil {
		t.Fatalf("second StreamChannel: %v", err)
	}
	if got := repo.getCalls.Load(); got != 1 {
		t.Errorf("expected exactly 1 persistence Get across two StreamChannel calls, got %d", got)
	}
}

// SendStreamChunk on a rehydrated (but idle) session returns ErrSessionInactive
// because there is no live producer attached to the stream buffer.
func TestRehydrate_SendStreamChunk_InactiveAfterRehydrate(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-inactive", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	err := svc.SendStreamChunk("sess-inactive", cpn.StreamChunk{SessionID: "sess-inactive", Content: "x"})
	if !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("err = %v, want ErrSessionInactive", err)
	}
}

// ResolveHITL on a rehydrated session returns ErrSessionInactive because no
// HITL request is in-flight on a newly reconstructed topology.
func TestRehydrate_ResolveHITL_InactiveAfterRehydrate(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-hitl", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	err := svc.ResolveHITL(context.Background(), "sess-hitl", "t-any", cpn.HITLResponse{Action: cpn.HITLApprove})
	if !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("err = %v, want ErrSessionInactive", err)
	}
}

// Ownership/state integrity: after rehydration the session is usable as if it
// had been created normally — GetSession returns the same payload twice.
func TestRehydrate_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	repo.seedSession(t, &persist.SessionRecord{
		ID: "sess-i", UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	first, err := svc.GetSession("sess-i")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.GetSession("sess-i")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID || first.UserID != second.UserID || first.Channel != second.Channel {
		t.Errorf("snapshots diverged:\nfirst=%+v\nsecond=%+v", first, second)
	}
}
