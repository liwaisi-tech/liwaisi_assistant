package app

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ── Fixtures ───────────────────────────────────────────────────────────────

func stubHostIDResolver(t *testing.T, id string) func() {
	t.Helper()
	origRead := osReadFileHostID
	origHost := osHostname
	osReadFileHostID = func(_ string) ([]byte, error) {
		return []byte(id + "\n"), nil
	}
	osHostname = func() (string, error) { return "test-host", nil }
	return func() {
		osReadFileHostID = origRead
		osHostname = origHost
	}
}

// fakeHostDiscoveryFactory returns a CPN that produces a canned snapshot in
// a terminal p-snapshot place — no bash transitions, no executor work.
func fakeHostDiscoveryFactory(snap persist.HostCapabilitySnapshot, repo persist.HostCapabilityRepository) func(string) *cpn.CPN {
	return func(sessionID string) *cpn.CPN {
		places := map[string]*cpn.Place{
			"p-trigger":  cpn.NewPlace("p-trigger", cpn.ColorString, cpn.SpaceComputation),
			"p-snapshot": cpn.NewPlace("p-snapshot", cpn.ColorHostFact, cpn.SpaceComputation),
		}
		_ = places["p-trigger"].Deposit(&cpn.Token{
			Color: cpn.ColorString, Space: cpn.SpaceComputation, Payload: "go",
		})
		tFinish := cpn.NewTransition("t-persist", cpn.NodeKindTool,
			[]string{"p-trigger"}, []string{"p-snapshot"})
		tFinish.ToolHandler = func(ctx context.Context, _ []cpn.Token) (map[string]cpn.Token, error) {
			if repo != nil {
				_ = repo.Save(ctx, snap)
			}
			return map[string]cpn.Token{
				"p-snapshot": {Color: cpn.ColorHostFact, Space: cpn.SpaceComputation, Payload: snap},
			}, nil
		}
		transitions := map[string]*cpn.Transition{"t-persist": tFinish}
		c := cpn.NewCPN("cpn-"+sessionID+"-discover", "host-discovery", 0, cpn.ModeMAS, sessionID, places, transitions)
		return c
	}
}

// ── Tests ──────────────────────────────────────────────────────────────────

// Fresh session, no prior snapshot → discovery runs + seeds p-host-capabilities.
func TestEnsureHostCapabilitiesSeed_ColdRun(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-1")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	expected := persist.HostCapabilitySnapshot{
		ID:         "snap-cold",
		HostID:     "machine-1",
		CapturedAt: time.Now(),
		Source:     persist.HostSnapshotSourceSession,
	}
	svc := &SessionService{
		logger:               testLogger(),
		hostCapabilityRepo:   repo,
		hostDiscoveryFactory: fakeHostDiscoveryFactory(expected, repo),
	}

	root := emptyRootCPN()
	svc.ensureHostCapabilitiesSeed(context.Background(), root)

	got, ok := cpn.PeekHostSnapshot(root)
	if !ok {
		t.Fatal("expected snapshot seeded")
	}
	snap, _ := got.(persist.HostCapabilitySnapshot)
	if snap.ID != "snap-cold" {
		t.Fatalf("got snapshot id %q, want snap-cold", snap.ID)
	}
	// And it should have landed in the repo.
	latest, err := repo.LatestForHost(context.Background(), "machine-1")
	if err != nil || latest.ID != "snap-cold" {
		t.Fatalf("latest not saved: %+v err=%v", latest, err)
	}
}

// Fresh snapshot in repo → no discovery run, seed from cache.
func TestEnsureHostCapabilitiesSeed_FreshCache(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-1")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	cached := persist.HostCapabilitySnapshot{
		ID:         "snap-cached",
		HostID:     "machine-1",
		CapturedAt: time.Now().Add(-1 * time.Hour),
		Source:     persist.HostSnapshotSourceBootstrap,
	}
	_ = repo.Save(context.Background(), cached)

	calls := 0
	svc := &SessionService{
		logger:             testLogger(),
		hostCapabilityRepo: repo,
		hostDiscoveryFactory: func(string) *cpn.CPN {
			calls++
			return nil
		},
	}

	root := emptyRootCPN()
	svc.ensureHostCapabilitiesSeed(context.Background(), root)

	if calls != 0 {
		t.Errorf("expected discovery NOT to run on fresh cache (calls=%d)", calls)
	}
	got, ok := cpn.PeekHostSnapshot(root)
	if !ok {
		t.Fatal("expected cached snapshot seeded")
	}
	snap, _ := got.(persist.HostCapabilitySnapshot)
	if snap.ID != "snap-cached" {
		t.Fatalf("got id %q, want snap-cached", snap.ID)
	}
}

// Stale snapshot → discovery re-runs.
func TestEnsureHostCapabilitiesSeed_StaleCache(t *testing.T) {
	restore := stubHostIDResolver(t, "machine-1")
	defer restore()

	repo := persist.NewMemoryHostCapabilityRepository()
	// 25h old → older than the 24h TTL.
	old := persist.HostCapabilitySnapshot{
		ID:         "snap-old",
		HostID:     "machine-1",
		CapturedAt: time.Now().Add(-25 * time.Hour),
	}
	_ = repo.Save(context.Background(), old)

	fresh := persist.HostCapabilitySnapshot{
		ID:         "snap-fresh",
		HostID:     "machine-1",
		CapturedAt: time.Now(),
	}
	svc := &SessionService{
		logger:               testLogger(),
		hostCapabilityRepo:   repo,
		hostDiscoveryFactory: fakeHostDiscoveryFactory(fresh, repo),
	}

	root := emptyRootCPN()
	svc.ensureHostCapabilitiesSeed(context.Background(), root)

	got, ok := cpn.PeekHostSnapshot(root)
	if !ok {
		t.Fatal("expected fresh snapshot seeded")
	}
	snap, _ := got.(persist.HostCapabilitySnapshot)
	if snap.ID != "snap-fresh" {
		t.Fatalf("got id %q, want snap-fresh", snap.ID)
	}
}

// When no repo is wired, the bootstrap helper is a no-op and does not panic.
func TestEnsureHostCapabilitiesSeed_NoRepo(t *testing.T) {
	svc := &SessionService{logger: testLogger()}
	root := emptyRootCPN()
	svc.ensureHostCapabilitiesSeed(context.Background(), root)
	if _, ok := cpn.PeekHostSnapshot(root); ok {
		t.Error("expected no snapshot seeded when repo is nil")
	}
}

// emptyRootCPN returns a trivial CPN with one input/output place so the
// session service helpers have something to attach the well-known place to.
func emptyRootCPN() *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorString, cpn.SpaceSurface),
	}
	return cpn.NewCPN("cpn-root-test", "test", 0, cpn.ModeMAS, "sess-test", places, nil)
}
