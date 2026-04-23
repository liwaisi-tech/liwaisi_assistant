package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/architect"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

func flowTestPlanner() *architect.Planner {
	reg := tools.NewRegistry()
	return &architect.Planner{
		Library:   cpn.NewFlowLibrary(),
		Retriever: architect.NewHashtagRetriever(reg),
	}
}

func TestHandleCreateFlow_503WhenNoPlanner(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	body := bytes.NewBufferString(`{"intent":"triage mail"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleCreateFlow(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleCreateFlow_400WhenEmpty(t *testing.T) {
	t.Parallel()
	h := &Handlers{FlowPlanner: flowTestPlanner()}
	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleCreateFlow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreateFlow_RejectWithEmptyDeps(t *testing.T) {
	t.Parallel()
	// Empty library + empty retriever + an intent → Planner emits a
	// StrategyReject draft. Handler responds 200 with strategy="reject".
	h := &Handlers{FlowPlanner: flowTestPlanner()}
	body := bytes.NewBufferString(`{"intent":"triage mail","hashtags":["mail"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleCreateFlow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (draft carries strategy)", rec.Code)
	}
	var resp FlowDraftResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Strategy != string(architect.StrategyReject) {
		t.Fatalf("strategy = %q, want %q", resp.Strategy, architect.StrategyReject)
	}
	if resp.Reason == "" {
		t.Error("reason empty")
	}
}
