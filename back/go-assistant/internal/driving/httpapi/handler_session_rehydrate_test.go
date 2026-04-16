package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// ── Ghost-session handler integration tests ────────────────────────────────
//
// These tests verify the HTTP status + body table defined in
// spec/spec-process-bugfix-ghost-session-rehydration.md §4.2. The fixture wires
// a Handlers struct against a SessionService that has a real persistence
// repository but an empty in-memory sessions map — exactly the post-restart
// state the bug describes.

// erroringSessionRepo wraps MemorySessionRepository to inject errors on Get.
type erroringSessionRepo struct {
	*persist.MemorySessionRepository
	err error
}

func (r *erroringSessionRepo) Get(ctx context.Context, sessionID string) (*persist.SessionRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.MemorySessionRepository.Get(ctx, sessionID)
}

// testHandlersWithPersist returns a Handlers + SessionRepository pair where
// the service has persistence wired but no live in-memory sessions. This is
// the ghost-session condition from the bug report.
func testHandlersWithPersist(repo persist.SessionRepository) *Handlers {
	logger := testLogger()
	svc := app.NewSessionService(
		&mockLLMClient{},
		&mockCostProvider{cost: 0.0},
		logger,
		testTopologyFactory,
		app.WithPersistence(&app.PersistDeps{Sessions: repo}),
	)
	broker := NewSSEBroker(logger)
	return &Handlers{App: svc, Broker: broker, Logger: logger}
}

func seedPersistedSession(t *testing.T, repo *persist.MemorySessionRepository, id, userID string, softDeleted bool) {
	t.Helper()
	rec := &persist.SessionRecord{
		ID:             id,
		UserID:         userID,
		Channel:        "web",
		State:          persist.SessionActive,
		CreatedAt:      time.Now().UTC().Truncate(time.Millisecond),
		LastActivityAt: time.Now().UTC(),
	}
	if softDeleted {
		now := time.Now().UTC()
		rec.DeletedAt = &now
	}
	if err := repo.Create(context.Background(), rec); err != nil {
		t.Fatalf("seed persisted session: %v", err)
	}
}

// Spec §4.2 row 1: session missing in persistence → 404 "session not found".
func TestHandleGetSession_GhostNotInPersistence(t *testing.T) {
	t.Parallel()
	repo := persist.NewMemorySessionRepository()
	h := testHandlersWithPersist(repo)
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/ghost-id", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "session not found" {
		t.Errorf("body = %+v, want error=session not found", body)
	}
}

// Spec AC-001: session in persistence, not in memory → 200 with session payload
// after rehydration.
func TestHandleGetSession_RehydratesFromPersistence(t *testing.T) {
	t.Parallel()
	repo := persist.NewMemorySessionRepository()
	seedPersistedSession(t, repo, "sess-reborn", "user-1", false)

	h := testHandlersWithPersist(repo)
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-reborn", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp SessionDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "sess-reborn" {
		t.Errorf("id = %q, want sess-reborn", resp.ID)
	}
	if resp.UserID != "user-1" {
		t.Errorf("user_id = %q, want user-1", resp.UserID)
	}
}

// Spec AC-003: soft-deleted session returns 404 and stays absent from memory.
func TestHandleGetSession_SoftDeletedRemainsGhost(t *testing.T) {
	t.Parallel()
	repo := persist.NewMemorySessionRepository()
	seedPersistedSession(t, repo, "sess-gone", "user-1", true)

	h := testHandlersWithPersist(repo)
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-gone", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// Verify via GetSession again that the soft-deleted session was NOT inserted.
	_, err := h.App.GetSession("sess-gone")
	if !errors.Is(err, app.ErrSessionNotFound) {
		t.Errorf("follow-up err = %v, want ErrSessionNotFound", err)
	}
}

// Spec §4.2 row 3: persistence unavailable → 503 "persistence unavailable".
func TestHandleGetSession_PersistenceUnavailable(t *testing.T) {
	t.Parallel()
	repo := &erroringSessionRepo{
		MemorySessionRepository: persist.NewMemorySessionRepository(),
		err:                     fmt.Errorf("simulated driver failure"),
	}
	h := testHandlersWithPersist(repo)
	mux := testMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/any-id", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "persistence unavailable" {
		t.Errorf("body = %+v, want error=persistence unavailable", body)
	}
}

// Spec §4.2 row 2: rehydrated session is not executing → HITL returns 409
// "session inactive".
func TestHandleResolveHITL_InactiveAfterRehydrate(t *testing.T) {
	t.Parallel()
	repo := persist.NewMemorySessionRepository()
	seedPersistedSession(t, repo, "sess-hitl", "user-1", false)

	h := testHandlersWithPersist(repo)
	mux := testMux(h)

	reqBody := jsonBody(t, ResolveHITLRequest{Action: string(cpn.HITLApprove)})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sessions/sess-hitl/hitl/t-some-transition",
		reqBody,
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "session inactive" {
		t.Errorf("body = %+v, want error=session inactive", body)
	}
}
