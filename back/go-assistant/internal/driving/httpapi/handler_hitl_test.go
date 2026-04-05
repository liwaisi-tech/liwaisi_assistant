package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleResolveHITL_SessionNotFound(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	body := jsonBody(t, ResolveHITLRequest{Action: "approve"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent/hitl/t-hitl-1", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResolveHITL_InvalidAction(t *testing.T) {
	t.Parallel()
	h := testHandlers()
	mux := testMux(h)

	body := jsonBody(t, ResolveHITLRequest{Action: "invalid-action"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent/hitl/t-hitl-1", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Session ownership check runs before input validation,
	// so a nonexistent session returns 404 instead of 400.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (ownership check before validation), got %d: %s", rec.Code, rec.Body.String())
	}
}
