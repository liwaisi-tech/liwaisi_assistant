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

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

func newAdminToolsHandlers(reg *tools.Registry) *Handlers {
	return &Handlers{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ToolRegistry: reg,
	}
}

func seedAgentTool(t *testing.T, reg *tools.Registry, ns, name, ver string) {
	t.Helper()
	ctx := context.Background()
	e := &tools.ToolEntry{
		Namespace:  ns,
		Name:       name,
		Version:    ver,
		Origin:     tools.OriginAgentAuthored,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
		HelpText:   "seed",
	}
	if err := reg.RegisterEntry(ctx, e); err != nil {
		t.Fatalf("seed %s/%s@%s: %v", ns, name, ver, err)
	}
}

func TestAdminListTools_Empty(t *testing.T) {
	reg := tools.NewRegistry()
	h := newAdminToolsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tools", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListTools(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rr.Code, rr.Body)
	}
	var body adminToolListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Total != 0 {
		t.Fatalf("expected 0 items, got %d", body.Total)
	}
}

func TestAdminListTools_FilterByOrigin(t *testing.T) {
	reg := tools.NewRegistry()
	seedAgentTool(t, reg, "brae", "http-get", "1.0.0")

	// Add a non-agent-authored entry via the legacy Register so it lands
	// with Origin=builtin.
	_ = reg.Register(&tools.ToolSchema{Name: "identity", Namespace: "system", Version: "1.0.0"}, nil)

	h := newAdminToolsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tools?origin=agent-authored", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListTools(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body adminToolListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Total != 1 {
		t.Fatalf("expected 1 agent-authored, got %d", body.Total)
	}
	if body.Items[0].Origin != tools.OriginAgentAuthored {
		t.Fatalf("item origin = %s", body.Items[0].Origin)
	}
}

func TestAdminGetTool_NotFound(t *testing.T) {
	reg := tools.NewRegistry()
	h := newAdminToolsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tools/brae/missing@1.0.0", nil)
	req.SetPathValue("qn", "brae/missing@1.0.0")
	rr := httptest.NewRecorder()
	h.HandleAdminGetTool(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d want 404; body=%s", rr.Code, rr.Body)
	}
}

func TestAdminGetTool_Found(t *testing.T) {
	reg := tools.NewRegistry()
	seedAgentTool(t, reg, "brae", "http-get", "1.0.0")

	h := newAdminToolsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tools/brae/http-get@1.0.0", nil)
	req.SetPathValue("qn", "brae/http-get@1.0.0")
	rr := httptest.NewRecorder()
	h.HandleAdminGetTool(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body)
	}
	var body adminToolResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.QualifiedName != "brae/http-get@1.0.0" {
		t.Fatalf("qn = %q want %q", body.QualifiedName, "brae/http-get@1.0.0")
	}
}

func TestAdminDeprecateTool_Success(t *testing.T) {
	reg := tools.NewRegistry()
	seedAgentTool(t, reg, "brae", "http-get", "1.0.0")

	h := newAdminToolsHandlers(reg)
	body, _ := json.Marshal(adminDeprecateRequest{QualifiedName: "brae/http-get@1.0.0", Reason: "drift"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tools/deprecate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleAdminDeprecateTool(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	var resp adminToolResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.Deprecated {
		t.Fatalf("expected deprecated=true")
	}
}

func TestAdminDeprecateTool_MissingQN(t *testing.T) {
	reg := tools.NewRegistry()
	h := newAdminToolsHandlers(reg)

	body, _ := json.Marshal(adminDeprecateRequest{Reason: "no-id"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tools/deprecate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.HandleAdminDeprecateTool(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400", rr.Code)
	}
}

func TestAdminDeleteTool_ForbiddenOnBuiltin(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&tools.ToolSchema{Name: "identity", Namespace: "system", Version: "1.0.0"}, nil)

	h := newAdminToolsHandlers(reg)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tools/system/identity@1.0.0", nil)
	req.SetPathValue("qn", "system/identity@1.0.0")
	rr := httptest.NewRecorder()
	h.HandleAdminDeleteTool(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d want 409 forbidden; body=%s", rr.Code, rr.Body)
	}
}

func TestAdminDeleteTool_AllowedOnAgentAuthored(t *testing.T) {
	reg := tools.NewRegistry()
	seedAgentTool(t, reg, "brae", "http-get", "1.0.0")

	h := newAdminToolsHandlers(reg)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tools/brae/http-get@1.0.0", nil)
	req.SetPathValue("qn", "brae/http-get@1.0.0")
	rr := httptest.NewRecorder()
	h.HandleAdminDeleteTool(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d want 200; body=%s", rr.Code, rr.Body)
	}
}

func TestAdminTools_NoRegistry(t *testing.T) {
	h := &Handlers{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tools", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListTools(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d want 503", rr.Code)
	}
}
