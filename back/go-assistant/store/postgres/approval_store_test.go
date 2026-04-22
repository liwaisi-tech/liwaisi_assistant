package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolapproval"
)

var approvalCols = []string{"session_id", "tool_name", "provenance_sha256", "approved_at"}

func TestApprovalRepo_Record_Success(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectExec(`INSERT INTO tool_approvals`).
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Record(context.Background(), toolapproval.Approval{
		SessionID: "s1", ToolName: "gcc", ProvenanceSHA256: "prov-1", ApprovedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Record_InvalidKey(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	cases := []toolapproval.Approval{
		{SessionID: "", ToolName: "gcc", ProvenanceSHA256: "p"},
		{SessionID: "s1", ToolName: "", ProvenanceSHA256: "p"},
		{SessionID: "s1", ToolName: "gcc", ProvenanceSHA256: ""},
	}
	for _, c := range cases {
		if err := repo.Record(context.Background(), c); !errors.Is(err, toolapproval.ErrInvalidKey) {
			t.Fatalf("want ErrInvalidKey for %+v, got %v", c, err)
		}
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Record_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectExec(`INSERT INTO tool_approvals`).
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg()).
		WillReturnError(errors.New("db down"))

	err := repo.Record(context.Background(), toolapproval.Approval{
		SessionID: "s1", ToolName: "gcc", ProvenanceSHA256: "prov-1", ApprovedAt: now,
	})
	if err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Record_Upsert(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	// Two Record calls for the same key — both produce an INSERT...ON CONFLICT;
	// verify both succeed and the repo doesn't gate on a prior existence check.
	mock.ExpectExec(`INSERT INTO tool_approvals`).
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`INSERT INTO tool_approvals`).
		WithArgs("s1", "gcc", "prov-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	a := toolapproval.Approval{SessionID: "s1", ToolName: "gcc", ProvenanceSHA256: "prov-1", ApprovedAt: now}
	if err := repo.Record(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Lookup_Approved(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectQuery(`SELECT 1\s+FROM tool_approvals`).
		WithArgs("s1", "gcc", "prov-1").
		WillReturnRows(pgxmock.NewRows([]string{"?column?"}).AddRow(1))

	d, err := repo.Lookup(context.Background(), "s1", "gcc", "prov-1")
	if err != nil {
		t.Fatal(err)
	}
	if d != toolapproval.DecisionApproved {
		t.Fatalf("want Approved, got %v", d)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Lookup_Unknown(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectQuery(`SELECT 1\s+FROM tool_approvals`).
		WithArgs("s1", "gcc", "prov-drift").
		WillReturnError(pgx.ErrNoRows)

	d, err := repo.Lookup(context.Background(), "s1", "gcc", "prov-drift")
	if err != nil {
		t.Fatal(err)
	}
	if d != toolapproval.DecisionUnknown {
		t.Fatalf("want Unknown, got %v", d)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Lookup_InvalidKey(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	if _, err := repo.Lookup(context.Background(), "", "gcc", "p"); !errors.Is(err, toolapproval.ErrInvalidKey) {
		t.Fatalf("want ErrInvalidKey, got %v", err)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_Lookup_DBError(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectQuery(`SELECT 1\s+FROM tool_approvals`).
		WithArgs("s1", "gcc", "prov-1").
		WillReturnError(errors.New("boom"))

	if _, err := repo.Lookup(context.Background(), "s1", "gcc", "prov-1"); err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestApprovalRepo_ListForSession(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM tool_approvals\s+WHERE session_id`).
		WithArgs("s1").
		WillReturnRows(pgxmock.NewRows(approvalCols).
			AddRow("s1", "gcc", "prov-1", now).
			AddRow("s1", "ls", "prov-2", now))

	list, err := repo.ListForSession(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2, got %d", len(list))
	}
	if list[0].ToolName != "gcc" || list[1].ToolName != "ls" {
		t.Fatalf("ordering wrong: %+v", list)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_ListForSession_EmptyKey(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	if _, err := repo.ListForSession(context.Background(), ""); !errors.Is(err, toolapproval.ErrInvalidKey) {
		t.Fatalf("want ErrInvalidKey, got %v", err)
	}
	expectMet(t, mock)
}

func TestApprovalRepo_ListForSession_QueryError(t *testing.T) {
	mock := newMock(t)
	repo := NewApprovalRepository(mock)

	mock.ExpectQuery(`SELECT .+ FROM tool_approvals\s+WHERE session_id`).
		WithArgs("s1").
		WillReturnError(errors.New("boom"))

	if _, err := repo.ListForSession(context.Background(), "s1"); err == nil {
		t.Fatal("want error")
	}
	expectMet(t, mock)
}

func TestApprovalRepo_ConcurrentRecord(t *testing.T) {
	pool := newCountingPool()
	repo := NewApprovalRepository(pool)

	var wg sync.WaitGroup
	ctx := context.Background()
	const N = 25
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.Record(ctx, toolapproval.Approval{
				SessionID: "s1", ToolName: "gcc", ProvenanceSHA256: "prov-1", ApprovedAt: now,
			}); err != nil {
				t.Errorf("record: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := pool.execCount(); got != N {
		t.Fatalf("want %d Exec calls, got %d", N, got)
	}
}
