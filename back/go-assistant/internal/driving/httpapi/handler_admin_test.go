package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/config"
)

// memStore is a minimal in-memory config store for handler tests.
type memStore struct {
	entries map[string]*config.ConfigEntry
}

func newMemStore() *memStore { return &memStore{entries: make(map[string]*config.ConfigEntry)} }

func (m *memStore) Get(_ context.Context, key string) (*config.ConfigEntry, error) {
	e := m.entries[key]
	if e == nil {
		return nil, nil
	}
	cp := *e
	return &cp, nil
}
func (m *memStore) Set(_ context.Context, entry *config.ConfigEntry) error {
	cp := *entry
	m.entries[entry.Key] = &cp
	return nil
}
func (m *memStore) Delete(_ context.Context, key string) error {
	delete(m.entries, key)
	return nil
}
func (m *memStore) List(_ context.Context) ([]*config.ConfigEntry, error) {
	var result []*config.ConfigEntry
	for _, e := range m.entries {
		cp := *e
		result = append(result, &cp)
	}
	return result, nil
}

func setupAdminHandlers(adminEmail string) (*Handlers, *config.Provider) {
	store := newMemStore()
	provider := config.NewProvider(store)
	h := &Handlers{
		Logger:         slog.Default(),
		ConfigProvider: provider,
		AdminEmail:     adminEmail,
	}
	return h, provider
}

func TestHandleConfigStatus(t *testing.T) {
	h, _ := setupAdminHandlers("admin@test.com")

	req := httptest.NewRequest("GET", "/api/v1/admin/config/status", nil)
	w := httptest.NewRecorder()

	h.HandleConfigStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PlatformStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// openrouter_api_key is required and not set.
	if resp.Ready {
		t.Fatal("expected platform not ready")
	}
	if len(resp.MissingRequired) == 0 {
		t.Fatal("expected missing required keys")
	}
}

func TestHandleSetAndListConfig(t *testing.T) {
	h, _ := setupAdminHandlers("admin@test.com")

	// Set a config value.
	body := bytes.NewBufferString(`{"value": "sk-test-key"}`)
	setReq := httptest.NewRequest("PUT", "/api/v1/admin/config/openrouter_api_key", body)
	setReq.SetPathValue("key", "openrouter_api_key")
	ctx := auth.NewContext(setReq.Context(), &auth.AuthenticatedUser{Sub: "admin", Email: "admin@test.com"})
	setReq = setReq.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleSetConfig(w, setReq)
	if w.Code != http.StatusOK {
		t.Fatalf("set: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List configs and verify the value is masked.
	listReq := httptest.NewRequest("GET", "/api/v1/admin/config", nil)
	listReq = listReq.WithContext(ctx)
	w = httptest.NewRecorder()

	h.HandleListConfig(w, listReq)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	var resp AdminConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var apiKeyItem *config.ConfigItem
	for i := range resp.Items {
		if resp.Items[i].Key == "openrouter_api_key" {
			apiKeyItem = &resp.Items[i]
			break
		}
	}
	if apiKeyItem == nil {
		t.Fatal("openrouter_api_key not found in list")
	}
	if apiKeyItem.Source != "db" {
		t.Fatalf("expected source 'db', got %q", apiKeyItem.Source)
	}
	// Should be masked since it's a secret.
	if apiKeyItem.Value == "sk-test-key" {
		t.Fatal("secret value should be masked in list response")
	}
}

func TestHandleSetConfigUnknownKey(t *testing.T) {
	h, _ := setupAdminHandlers("admin@test.com")

	body := bytes.NewBufferString(`{"value": "test"}`)
	req := httptest.NewRequest("PUT", "/api/v1/admin/config/unknown_key", body)
	req.SetPathValue("key", "unknown_key")
	ctx := auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "admin", Email: "admin@test.com"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleSetConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdminMiddlewareForbidsNonAdmin(t *testing.T) {
	mw := AdminMiddleware("admin@test.com")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "user1", Email: "other@test.com"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAdminMiddlewareAllowsDevUser(t *testing.T) {
	mw := AdminMiddleware("admin@test.com")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.NewContext(req.Context(), &auth.AuthenticatedUser{Sub: "dev-user", Email: "dev@localhost"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleDeleteConfig(t *testing.T) {
	h, _ := setupAdminHandlers("admin@test.com")
	ctx := auth.NewContext(context.Background(), &auth.AuthenticatedUser{Sub: "admin", Email: "admin@test.com"})

	// Set a value first.
	body := bytes.NewBufferString(`{"value": "to-delete"}`)
	setReq := httptest.NewRequest("PUT", "/api/v1/admin/config/default_model", body)
	setReq.SetPathValue("key", "default_model")
	setReq = setReq.WithContext(ctx)
	w := httptest.NewRecorder()
	h.HandleSetConfig(w, setReq)

	// Delete it.
	delReq := httptest.NewRequest("DELETE", "/api/v1/admin/config/default_model", nil)
	delReq.SetPathValue("key", "default_model")
	delReq = delReq.WithContext(ctx)
	w = httptest.NewRecorder()
	h.HandleDeleteConfig(w, delReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
