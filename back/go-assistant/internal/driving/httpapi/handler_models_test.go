package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
)

// TestHandleGetModels_IncludesDefault asserts REQ-OBS-002 / AC-010:
// GET /api/v1/models returns a top-level "default" key equal to
// openrouter.PRODUCT_DEFAULT_MODEL so the frontend can seed the onboarding
// wizard without re-stating the product default.
func TestHandleGetModels_IncludesDefault(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	rr := httptest.NewRecorder()

	h.HandleGetModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, ok := body["default"].(string)
	if !ok {
		t.Fatalf("response missing top-level string field %q; body=%s", "default", rr.Body.String())
	}
	if got != openrouter.PRODUCT_DEFAULT_MODEL {
		t.Errorf("default = %q, want %q", got, openrouter.PRODUCT_DEFAULT_MODEL)
	}
	// Legacy alias retained for in-flight frontends.
	if body["default_model"] != openrouter.PRODUCT_DEFAULT_MODEL {
		t.Errorf("default_model = %v, want %q", body["default_model"], openrouter.PRODUCT_DEFAULT_MODEL)
	}
}

// TestHandleGetModels_RolesAllDefault asserts every role in the response
// resolves to PRODUCT_DEFAULT_MODEL (REQ-CFG-001).
func TestHandleGetModels_RolesAllDefault(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	rr := httptest.NewRecorder()

	h.HandleGetModels(rr, req)

	var body ModelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Roles) == 0 {
		t.Fatal("roles array is empty")
	}
	for _, r := range body.Roles {
		if r.DefaultModel != openrouter.PRODUCT_DEFAULT_MODEL {
			t.Errorf("role %q default = %q, want %q", r.Key, r.DefaultModel, openrouter.PRODUCT_DEFAULT_MODEL)
		}
	}
}
