package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolsynth"
)

// countingPool is a minimal goroutine-safe pgxPool stub used to verify the
// repositories are stateless — every Exec call increments a counter and
// returns a successful 1-row result. pgxmock is not goroutine-safe so
// concurrency tests use this stub instead.
type countingPool struct {
	mu    sync.Mutex
	execs int
}

func newCountingPool() *countingPool { return &countingPool{} }

func (p *countingPool) execCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.execs
}

func (p *countingPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	p.mu.Lock()
	p.execs++
	p.mu.Unlock()
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (p *countingPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("countingPool: Query not supported")
}
func (p *countingPool) QueryRow(context.Context, string, ...any) pgx.Row {
	return errRow{err: errors.New("countingPool: QueryRow not supported")}
}
func (p *countingPool) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("countingPool: CopyFrom not supported")
}
func (p *countingPool) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("countingPool: Begin not supported")
}
func (p *countingPool) Close() {}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// ---------------------------------------------------------------------------
// PendingToolRepository
// ---------------------------------------------------------------------------

var pendingCols = []string{
	"session_id", "tool_name", "provenance_sha256", "manifest_json", "source_sha256", "created_at",
}

func samplePending(sessionID, name, provenance string) toolsynth.PendingTool {
	return toolsynth.PendingTool{
		Manifest: cpn.ToolManifest{
			Namespace: "synthesized",
			Name:      name,
			Version:   "0.0.1",
			Origin:    "brae-awakens/help-parser",
			Kind:      "synthesized",
		},
		SourceSHA256:     "src-sha",
		ProvenanceSHA256: provenance,
		SessionID:        sessionID,
		CreatedAt:        now,
	}
}

func TestPendingToolRepo_Stage_Upsert(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	p := samplePending("s1", "gcc", "prov-1")

	mock.ExpectExec("INSERT INTO pending_tools").
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg(), "src-sha", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := repo.Stage(context.Background(), p); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Stage_MissingName(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	p := toolsynth.PendingTool{
		Manifest:         cpn.ToolManifest{Name: ""},
		ProvenanceSHA256: "prov-1",
		SessionID:        "s1",
	}
	if err := repo.Stage(context.Background(), p); err == nil {
		t.Fatal("want error for empty manifest name")
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Stage_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	p := samplePending("s1", "gcc", "prov-1")

	mock.ExpectExec("INSERT INTO pending_tools").
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg(), "src-sha", pgxmock.AnyArg()).
		WillReturnError(errors.New("db down"))

	if err := repo.Stage(context.Background(), p); err == nil {
		t.Fatal("want error on db failure")
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_List_BySession(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	manifestJSON, _ := json.Marshal(cpn.ToolManifest{Name: "gcc", Namespace: "synthesized"})

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+WHERE session_id`).
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows(pendingCols).
			AddRow("s1", "gcc", "prov-1", manifestJSON, "src-1", now).
			AddRow("s1", "ls", "prov-2", manifestJSON, "src-2", now))

	list, err := repo.List(context.Background(), "s1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2, got %d", len(list))
	}
	if list[0].Manifest.Name == "" {
		t.Fatal("manifest did not round-trip")
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_List_AllSessions(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	manifestJSON, _ := json.Marshal(cpn.ToolManifest{Name: "gcc"})

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+ORDER BY tool_name`).
		WillReturnRows(pgxmock.NewRows(pendingCols).
			AddRow("s1", "gcc", "prov-1", manifestJSON, "src-1", now))

	list, err := repo.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1, got %d", len(list))
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_List_Error(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM pending_tools`).
		WithArgs("s1").
		WillReturnError(errors.New("boom"))

	if _, err := repo.List(context.Background(), "s1"); err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Get_Hit(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	manifestJSON, _ := json.Marshal(cpn.ToolManifest{Name: "gcc", Version: "0.0.1"})

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+WHERE tool_name`).
		WithArgs("gcc").
		WillReturnRows(pgxmock.NewRows(pendingCols).
			AddRow("s1", "gcc", "prov-1", manifestJSON, "src-1", now))

	got, ok, err := repo.Get(context.Background(), "gcc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Manifest.Name != "gcc" || got.ProvenanceSHA256 != "prov-1" {
		t.Fatalf("unexpected: %+v", got)
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Get_Miss(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+WHERE tool_name`).
		WithArgs("ghost").
		WillReturnError(pgx.ErrNoRows)

	got, ok, err := repo.Get(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("want ok=false, got %+v", got)
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Get_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+WHERE tool_name`).
		WithArgs("gcc").
		WillReturnError(errors.New("boom"))

	if _, _, err := repo.Get(context.Background(), "gcc"); err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Delete_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectExec(`DELETE FROM pending_tools`).
		WithArgs("gcc").
		WillReturnResult(pgxmock.NewResult("DELETE", 2))

	if err := repo.Delete(context.Background(), "gcc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	expectMet(t, mock)
}

func TestPendingToolRepo_Delete_NotFound(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectExec(`DELETE FROM pending_tools`).
		WithArgs("ghost").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.Delete(context.Background(), "ghost")
	if !errors.Is(err, toolsynth.ErrPendingNotFound) {
		t.Fatalf("want ErrPendingNotFound, got %v", err)
	}
	expectMet(t, mock)
}

// TestPendingToolRepo_DriftDetection confirms the composite key behaviour —
// two stage calls for the same tool_name but different provenance_sha256
// result in two distinct INSERTs (no ON CONFLICT collision). The repo itself
// does not attempt to merge — SC-13 reads Get() and treats the latest-by-
// created_at as the staged candidate.
func TestPendingToolRepo_DriftDetection(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectExec(`INSERT INTO pending_tools`).
		WithArgs("s1", "gcc", "prov-A", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`INSERT INTO pending_tools`).
		WithArgs("s1", "gcc", "prov-B", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	ctx := context.Background()
	if err := repo.Stage(ctx, samplePending("s1", "gcc", "prov-A")); err != nil {
		t.Fatalf("stage A: %v", err)
	}
	if err := repo.Stage(ctx, samplePending("s1", "gcc", "prov-B")); err != nil {
		t.Fatalf("stage B: %v", err)
	}
	expectMet(t, mock)
}

// TestPendingToolRepo_ConcurrentStage verifies the repo is safe under
// concurrent Stage calls — the repo itself is stateless; pgxmock is not
// goroutine-safe so we wrap it in a thread-safe counting pool and assert
// every Stage call reaches the pool exactly once. Running with -race
// surfaces any unsynchronised state the repo might introduce.
func TestPendingToolRepo_ConcurrentStage(t *testing.T) {
	pool := newCountingPool()
	repo := NewPendingToolRepository(pool)

	var wg sync.WaitGroup
	ctx := context.Background()
	const N = 25
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := samplePending("s1", "gcc", "prov-x")
			p.CreatedAt = now.Add(time.Duration(i) * time.Millisecond)
			if err := repo.Stage(ctx, p); err != nil {
				t.Errorf("stage %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := pool.execCount(); got != N {
		t.Fatalf("want %d Exec calls, got %d", N, got)
	}
}

func TestPendingToolRepo_Scan_BadManifestJSON(t *testing.T) {
	mock := newMock(t)
	repo := NewPendingToolRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM pending_tools\s+WHERE session_id`).
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows(pendingCols).
			AddRow("s1", "gcc", "prov-1", []byte("{not-json"), "src-1", now))

	if _, err := repo.List(context.Background(), "s1"); err == nil {
		t.Fatal("want unmarshal error")
	}
	expectMet(t, mock)
}
