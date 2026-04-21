package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// stubToolboxLister is a minimal ToolboxLister for handler tests. Engineer B
// owns the real Registry.Toolboxes implementation; we exercise the HTTP
// surface here, not the aggregation.
type stubToolboxLister struct {
	boxes []tools.ToolboxManifest
}

func (s *stubToolboxLister) Toolboxes(_ context.Context) []tools.ToolboxManifest {
	return s.boxes
}

// stubVerifier rejects every token so AuthMiddleware returns 401. Used to
// simulate an anonymous (unauthenticated) client for AC-009.
type stubVerifier struct{}

func (stubVerifier) Verify(_ context.Context, _ string) (*auth.AuthenticatedUser, error) {
	return nil, http.ErrAbortHandler
}

func newToolboxFixture() []tools.ToolboxManifest {
	return []tools.ToolboxManifest{
		{
			Namespace:          "brae",
			Title:              "Brae",
			Summary:            "seeded",
			Hashtags:           []string{"kind-fetch"},
			ToolCount:          2,
			LatestRegisteredAt: time.Now().UTC(),
		},
	}
}

// TestAdminListToolboxes_Authenticated_OK (AC-009, REQ-010): admin request
// succeeds and returns a JSON array.
func TestAdminListToolboxes_Authenticated_OK(t *testing.T) {
	h := &Handlers{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ToolboxLister: &stubToolboxLister{boxes: newToolboxFixture()},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/toolboxes", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListToolboxes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d want 200; body=%s", rr.Code, rr.Body)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q want application/json", ct)
	}
	var body []tools.ToolboxManifest
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body) != 1 || body[0].Namespace != "brae" {
		t.Fatalf("body = %+v want 1 brae entry", body)
	}
}

// TestAdminListToolboxes_Unconfigured: nil lister degrades to 503 instead of
// panicking — matches the pattern used by other admin endpoints.
func TestAdminListToolboxes_Unconfigured(t *testing.T) {
	h := &Handlers{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/toolboxes", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListToolboxes(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rr.Code)
	}
}

// TestAdminListToolboxes_AnonymousReturns401 (AC-009): when the full auth +
// admin middleware chain is applied, an anonymous client (no Authorization
// header, verifier configured) receives 401 from AuthMiddleware. The
// downstream handler MUST NOT run, so no toolbox payload leaks.
func TestAdminListToolboxes_AnonymousReturns401(t *testing.T) {
	lister := &stubToolboxLister{boxes: newToolboxFixture()}
	h := &Handlers{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ToolboxLister: lister,
	}

	mux := http.NewServeMux()
	adminAuth := AdminMiddleware([]string{"admin@liwaisi.tech"})
	mux.Handle("GET /api/v1/admin/toolboxes", adminAuth(http.HandlerFunc(h.HandleAdminListToolboxes)))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	chain := AuthMiddleware(stubVerifier{}, logger, nil)(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/toolboxes", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d want 401; body=%s", rr.Code, rr.Body.String())
	}
	// Body must not carry any toolbox data.
	if strings.Contains(rr.Body.String(), "brae") {
		t.Fatalf("response leaked toolbox data: %s", rr.Body.String())
	}
}
