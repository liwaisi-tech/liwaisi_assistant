package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host/gate"
)

func newHostPolicyHandler(t *testing.T) (*Handlers, *gate.Holder) {
	t.Helper()
	p, err := gate.LoadFromBytes([]byte(`
version: 1
defaults:
  sandbox: readonly
safe_patterns:
  - "^ls"
`))
	if err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	h := &Handlers{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		HostPolicy: gate.NewHolder(p),
	}
	return h, h.HostPolicy.(*gate.Holder)
}

func TestHostPolicyGet_ReturnsYAML(t *testing.T) {
	h, _ := newHostPolicyHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/host/policy", nil)
	w := httptest.NewRecorder()
	h.HandleHostPolicyGet(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "safe_patterns") {
		t.Errorf("expected safe_patterns in body, got %q", w.Body.String())
	}
}

func TestHostPolicyPut_ValidReplaces(t *testing.T) {
	h, holder := newHostPolicyHandler(t)
	body := []byte(`
version: 1
defaults:
  sandbox: none
safe_patterns:
  - "^echo"
`)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/host/policy", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleHostPolicyPut(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	got := holder.Get()
	if len(got.SafePat) == 0 || got.SafePat[0] != "^echo" {
		t.Errorf("policy not swapped, got %+v", got.SafePat)
	}
}

func TestHostPolicyPut_InvalidReturns422(t *testing.T) {
	h, holder := newHostPolicyHandler(t)
	original := holder.Get()
	body := []byte(`safe_patterns: [ "[invalid" ]`)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/host/policy", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleHostPolicyPut(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	// Policy is unchanged (AC-006).
	current := holder.Get()
	if current != original {
		t.Errorf("policy was swapped despite validation failure")
	}
}
