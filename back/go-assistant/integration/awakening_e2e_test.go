//go:build e2e

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/integration/llmmock"
	pgstore "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres"
)

// e2eDSN is the DSN pointed at by docker-compose.e2e.yml's Postgres service.
const e2eDSN = "postgres://liwaisi_e2e:liwaisi_e2e@127.0.0.1:55432/liwaisi_e2e?sslmode=disable"

// TestAwakeningE2E (SC-17 / REQ-1701) exercises the full brae-awakens flow
// against a docker-compose Postgres + an in-process OpenAI-compatible mock
// LLM server. Validates that:
//   - runAwakening completes end-to-end,
//   - a HostCapabilitySnapshot row is written to Postgres,
//   - an A2UI first-turn envelope is emitted with non-zero components.
//
// The test skips with a helpful message when docker / the compose stack is
// unavailable so dev laptops without Docker still have a green `go test -tags=e2e`.
func TestAwakeningE2E(t *testing.T) {
	ensureDockerOrSkip(t)

	composePath := findComposeFile(t)
	runCompose(t, composePath, "up", "-d", "--wait")
	t.Cleanup(func() {
		_ = runComposeSilent(composePath, "down", "-v")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := waitForPostgres(t, ctx, e2eDSN)
	t.Cleanup(pool.Close)

	migrationsDir := filepath.Join(repoRoot(t), "store", "postgres", "migrations")
	if err := pgstore.RunMigrations(e2eDSN, migrationsDir); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	repo := pgstore.NewHostCapabilityRepository(pool)

	reportJSON := `{"os":{"name":"linux","version":"22.04","kernel":"6.1","arch":"amd64"},` +
		`"shell":{"path":"/bin/bash","implementation":"bash"},` +
		`"identity":{"user":"brae","uid":1000,"gid":1000,"home":"/home/brae"},` +
		`"present_tools":[{"name":"git","version":"2.42.0"}],` +
		`"absent_tools":["docker"],` +
		`"capabilities":[],"tools_to_register":[],` +
		`"narrative_md":"e2e","probe_trace":[]}`

	mock := llmmock.New([]string{reportJSON})
	t.Cleanup(mock.Close)

	llm := openrouter.NewClient("e2e-mock-key", "google/gemini-2.5-flash")
	llm.BaseURL = mock.URL() + "/v1"

	t.Setenv("BRAE_AWAKENING_MODE", "legacy")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	snap, envelope, source, err := runAwakeningE2E(ctx, logger, llm, repo, "sess-e2e", "user-e2e")
	if err != nil {
		t.Fatalf("runAwakening: %v", err)
	}
	if source != awakens.SourceAwakening {
		t.Errorf("source = %q; want %q", source, awakens.SourceAwakening)
	}
	if snap.HostID == "" {
		t.Fatal("empty host_id on snapshot")
	}
	if len(envelope.Components) == 0 {
		t.Fatal("envelope has no components")
	}
	if mock.Calls() == 0 {
		t.Fatal("LLM mock never called")
	}

	persisted, err := repo.LatestForHost(ctx, snap.HostID)
	if err != nil {
		t.Fatalf("LatestForHost: %v", err)
	}
	if persisted.Source != awakens.SourceAwakening {
		t.Errorf("persisted.Source = %q", persisted.Source)
	}
	if persisted.HostID != snap.HostID {
		t.Errorf("persisted.HostID = %q; want %q", persisted.HostID, snap.HostID)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func ensureDockerOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("SKIP: docker not on PATH — install Docker to run the E2E harness")
	}
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("SKIP: `docker info` failed — is the Docker daemon running?")
	}
}

func findComposeFile(t *testing.T) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(self), "docker-compose.e2e.yml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("missing compose file at %s: %v", p, err)
	}
	return p
}

func runCompose(t *testing.T, composeFile string, args ...string) {
	t.Helper()
	full := append([]string{"compose", "-f", composeFile}, args...)
	cmd := exec.Command("docker", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(full, " "), err, out)
	}
}

func runComposeSilent(composeFile string, args ...string) error {
	full := append([]string{"compose", "-f", composeFile}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func waitForPostgres(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			pingErr := pool.Ping(pctx)
			cancel()
			if pingErr == nil {
				return pool
			}
			pool.Close()
			lastErr = pingErr
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres not reachable at %s within 30s: %v", dsn, lastErr)
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(self)) // go-assistant/
}

// runAwakeningE2E is a thin wrapper that mirrors what SessionService does
// internally so the E2E test exercises the awakening flow without pulling in
// the full CreateSession path (which requires sessions tables, redis, etc).
//
// It intentionally reuses the same LegacyTopologyFactory path the production
// SessionService uses.
func runAwakeningE2E(
	ctx context.Context,
	logger *slog.Logger,
	llm cpn.LLMClient,
	repo persist.HostCapabilityRepository,
	sessionID, userID string,
) (persist.HostCapabilitySnapshot, awakens.A2UIMessage, string, error) {
	_ = userID
	emitter := awakens.NewEmitter(logger)
	deps := awakens.Deps{
		Repository: repo,
		HostID:     "e2e-host",
		Source:     awakens.SourceAwakening,
		Clock:      awakens.SystemClock,
		Emitter:    emitter,
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	root := awakens.LegacyTopologyFactory(sessionID, deps)
	if root == nil {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", errors.New("nil legacy topology")
	}
	root.LLMClient = llm
	for _, tr := range root.Transitions {
		if tr == nil || tr.Kind != cpn.NodeKindLLM {
			continue
		}
		if tr.LLMConfig == nil {
			tr.LLMConfig = &cpn.LLMConfig{}
		}
		tr.LLMConfig.Model = "google/gemini-2.5-flash"
	}
	if err := root.Run(runCtx); err != nil {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", fmt.Errorf("cpn.run: %w", err)
	}

	envPlace, ok := root.Places[awakens.PlaceAwakeningMessage+"-emitted"]
	if !ok {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", errors.New("missing emitted place")
	}
	envTokens, ok := envPlace.Peek()
	if !ok || len(envTokens) == 0 {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", errors.New("no emitted envelope")
	}
	envelope, _ := envTokens[0].Payload.(awakens.A2UIMessage)

	snapPlace, ok := root.Places[cpn.WellKnownHostCapabilitiesPlace]
	if !ok {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", errors.New("missing snapshot place")
	}
	snapTokens, ok := snapPlace.Peek()
	if !ok || len(snapTokens) == 0 {
		return persist.HostCapabilitySnapshot{}, awakens.A2UIMessage{}, "", errors.New("no snapshot token")
	}
	snap, _ := snapTokens[0].Payload.(persist.HostCapabilitySnapshot)

	return snap, envelope, awakens.SourceAwakening, nil
}
