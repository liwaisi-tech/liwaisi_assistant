package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func newAdminArtefactsHandlers(ledger persist.AuthoredArtefactLedger, roll ArtefactRollbackService, purge ArtefactPurgeRunner) *Handlers {
	return &Handlers{
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		ArtefactLedger:   ledger,
		ArtefactRollback: roll,
		ArtefactPurge:    purge,
	}
}

type fakeRollback struct {
	rollbackErr error
	restoreErr  error
	rolledBack  []string
	restored    []string
}

func (f *fakeRollback) Rollback(_ context.Context, setID, _ string) error {
	if f.rollbackErr != nil {
		return f.rollbackErr
	}
	f.rolledBack = append(f.rolledBack, setID)
	return nil
}

func (f *fakeRollback) Restore(_ context.Context, setID, _ string) error {
	if f.restoreErr != nil {
		return f.restoreErr
	}
	f.restored = append(f.restored, setID)
	return nil
}

type fakePurge struct {
	err error
	n   int
}

func (f *fakePurge) Run(_ context.Context, _ time.Time) (int, error) {
	return f.n, f.err
}

func TestAdminListArtefacts_NoLedger_503(t *testing.T) {
	h := newAdminArtefactsHandlers(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artefacts", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListArtefacts(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503", rr.Code)
	}
}

func TestAdminListArtefacts_OK(t *testing.T) {
	ledger := persist.NewMemoryArtefactLedger()
	ctx := context.Background()
	id, _ := ledger.PreWrite(ctx, persist.WriteIntent{
		SetID: "s1", HostID: "host-a", Classification: persist.ClassSource,
	}, "/p/a.go", 0o644)
	_ = ledger.PostWrite(ctx, id, "h", 13, "text/plain")

	h := newAdminArtefactsHandlers(ledger, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artefacts?host_id=host-a", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListArtefacts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body)
	}
	var body adminArtefactListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Total != 1 {
		t.Errorf("total = %d; want 1", body.Total)
	}
}

func TestAdminGetArtefact_NotFound(t *testing.T) {
	ledger := persist.NewMemoryArtefactLedger()
	h := newAdminArtefactsHandlers(ledger, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artefacts/nope", nil)
	req.SetPathValue("id", "nope")
	rr := httptest.NewRecorder()
	h.HandleAdminGetArtefact(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", rr.Code)
	}
}

func TestAdminRollbackArtefactSet_NoService_503(t *testing.T) {
	h := newAdminArtefactsHandlers(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artefacts/sets/set-1/rollback", nil)
	req.SetPathValue("set_id", "set-1")
	rr := httptest.NewRecorder()
	h.HandleAdminRollbackArtefactSet(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503", rr.Code)
	}
}

func TestAdminRollbackArtefactSet_OK(t *testing.T) {
	svc := &fakeRollback{}
	h := newAdminArtefactsHandlers(nil, svc, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artefacts/sets/set-1/rollback", nil)
	req.SetPathValue("set_id", "set-1")
	rr := httptest.NewRecorder()
	h.HandleAdminRollbackArtefactSet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body)
	}
	if len(svc.rolledBack) != 1 || svc.rolledBack[0] != "set-1" {
		t.Errorf("rolledBack = %v; want [set-1]", svc.rolledBack)
	}
}

func TestAdminRestoreArtefactSet_NotQuarantined_409(t *testing.T) {
	svc := &fakeRollback{restoreErr: persist.ErrArtefactNotQuarantined}
	h := newAdminArtefactsHandlers(nil, svc, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artefacts/sets/set-1/restore", nil)
	req.SetPathValue("set_id", "set-1")
	rr := httptest.NewRecorder()
	h.HandleAdminRestoreArtefactSet(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409", rr.Code)
	}
}

func TestAdminPurgeArtefacts_DefaultCutoff(t *testing.T) {
	p := &fakePurge{n: 3}
	h := newAdminArtefactsHandlers(nil, nil, p)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artefacts/purge", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	h.HandleAdminPurgeArtefacts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body)
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if n, _ := body["purged"].(float64); int(n) != 3 {
		t.Errorf("purged = %v; want 3", body["purged"])
	}
}

func TestAdminPurgeArtefacts_WithOlderThanDays(t *testing.T) {
	p := &fakePurge{n: 1}
	h := newAdminArtefactsHandlers(nil, nil, p)
	body := bytes.NewReader([]byte(`{"older_than_days": 14}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/artefacts/purge", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleAdminPurgeArtefacts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body)
	}
}

func TestAdminArtefacts_RespectsAuth(t *testing.T) {
	// Build a full mini-router with the admin middleware to confirm
	// unauthenticated callers are rejected with 403.
	ledger := persist.NewMemoryArtefactLedger()
	h := newAdminArtefactsHandlers(ledger, &fakeRollback{}, &fakePurge{})
	h.AdminEmails = []string{"admin@example.com"}

	mux := http.NewServeMux()
	RegisterRoutes(mux, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/artefacts", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403 (no auth context)", rr.Code)
	}
}
