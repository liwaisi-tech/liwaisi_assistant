package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// TestResolveHITL_OrphanTransition_LiveSession validates spec §4.3 / REQ-006:
// when the session is live (Running/Waiting) but the specific transition has
// no registered HITL channel (token already consumed / flow moved past the
// gate), ResolveHITL returns ErrTransitionOrphaned — NOT ErrSessionInactive.
func TestResolveHITL_OrphanTransition_LiveSession(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	const sessionID = "sess-orphan"
	repo.seedSession(t, &persist.SessionRecord{
		ID: sessionID, UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	// Rehydrate the session into memory.
	if _, err := svc.GetSession(sessionID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	// Force the state to Running so the predicate treats this as a live
	// session. No HITL channel is registered for the transition ID we try
	// to resolve, so session.ResolveHITL returns ErrNoHITLWaiting which
	// ResolveHITL must surface as ErrTransitionOrphaned.
	svc.mu.RLock()
	st := svc.states[sessionID]
	svc.mu.RUnlock()
	if st == nil {
		t.Fatalf("state missing after rehydrate")
	}
	st.set(cpn.StateRunning)

	err := svc.ResolveHITL(context.Background(), sessionID, "t-stale",
		cpn.HITLResponse{Action: cpn.HITLApprove})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrTransitionOrphaned) {
		t.Fatalf("err = %v, want ErrTransitionOrphaned", err)
	}
	if errors.Is(err, ErrSessionInactive) {
		t.Fatalf("err must not also be ErrSessionInactive: %v", err)
	}
	// The wrapped cause must remain errors.Is-checkable so callers that
	// still care about the CPN-level sentinel can branch on it.
	if !errors.Is(err, cpn.ErrNoHITLWaiting) {
		t.Fatalf("err should wrap cpn.ErrNoHITLWaiting, got %v", err)
	}
}

// TestResolveHITL_Inactive_DeadOrIdleSession validates that a session whose
// CPN is not live (rehydrated-idle / completed / failed) returns
// ErrSessionInactive, NOT ErrTransitionOrphaned. This preserves the existing
// REQ-003 ghost-session contract (spec §4.2 row 2).
func TestResolveHITL_Inactive_DeadOrIdleSession(t *testing.T) {
	t.Parallel()

	repo := newCountingSessionRepo()
	svc := newRehydrateService(repo)

	const sessionID = "sess-idle"
	repo.seedSession(t, &persist.SessionRecord{
		ID: sessionID, UserID: "user-1", Channel: "web",
		State: persist.SessionActive, CreatedAt: time.Now().UTC(),
	})

	err := svc.ResolveHITL(context.Background(), sessionID, "t-any",
		cpn.HITLResponse{Action: cpn.HITLApprove})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("err = %v, want ErrSessionInactive", err)
	}
	if errors.Is(err, ErrTransitionOrphaned) {
		t.Fatalf("err must not be ErrTransitionOrphaned for dead session: %v", err)
	}
}
