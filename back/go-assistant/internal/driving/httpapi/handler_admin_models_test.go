package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// fakeRegistry is an in-memory cpn.ModelRegistry suitable for HTTP handler
// tests. It implements the full port; only the methods the B3 handlers call
// are exercised, others return ErrModelNotFound.
type fakeRegistry struct {
	entries       map[string]*cpn.ModelRegistryEntry
	productDef    string
	roleDefaults  map[string]string
	insertErr     error
	updateErr     error
	deleteErr     error
	setDefErr     error
	setLicenseErr error
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		entries:      map[string]*cpn.ModelRegistryEntry{},
		roleDefaults: map[string]string{},
	}
}

func (f *fakeRegistry) put(e *cpn.ModelRegistryEntry) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = e.CreatedAt
	}
	if e.ID == "" {
		e.ID = "uuid-" + e.RegistryID
	}
	if f.productDef == e.RegistryID {
		e.IsProductDefault = true
	}
	f.entries[e.RegistryID] = e
}

func (f *fakeRegistry) GetInvokable(_ context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	e, ok := f.entries[id]
	if !ok {
		return nil, cpn.ErrModelNotFound
	}
	if !e.Invokable() {
		return nil, cpn.ErrModelNotInvokable
	}
	return e, nil
}
func (f *fakeRegistry) GetByID(_ context.Context, id string) (*cpn.ModelRegistryEntry, error) {
	e, ok := f.entries[id]
	if !ok {
		return nil, cpn.ErrModelNotFound
	}
	e.IsProductDefault = (f.productDef == id)
	return e, nil
}
func (f *fakeRegistry) GetProductDefault(_ context.Context) (*cpn.ModelRegistryEntry, error) {
	if f.productDef == "" {
		return nil, cpn.ErrModelNotFound
	}
	e := f.entries[f.productDef]
	e.IsProductDefault = true
	return e, nil
}
func (f *fakeRegistry) ListInvokable(_ context.Context) ([]*cpn.ModelRegistryEntry, error) {
	out := make([]*cpn.ModelRegistryEntry, 0, len(f.entries))
	for _, e := range f.entries {
		if e.Invokable() {
			e.IsProductDefault = (f.productDef == e.RegistryID)
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RegistryID < out[j].RegistryID })
	return out, nil
}
func (f *fakeRegistry) ListAll(_ context.Context, filter cpn.ModelListFilter) ([]*cpn.ModelRegistryEntry, int, error) {
	out := make([]*cpn.ModelRegistryEntry, 0, len(f.entries))
	for _, e := range f.entries {
		if filter.Vendor != "" && e.Vendor != filter.Vendor {
			continue
		}
		if filter.LifecycleState != "" && e.Lifecycle.State != filter.LifecycleState {
			continue
		}
		if filter.LicenseStatus != "" && e.License.Status != filter.LicenseStatus {
			continue
		}
		if filter.Invokable != nil && e.Invokable() != *filter.Invokable {
			continue
		}
		if filter.Search != "" {
			s := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(e.RegistryID), s) &&
				!strings.Contains(strings.ToLower(e.DisplayName), s) {
				continue
			}
		}
		e.IsProductDefault = (f.productDef == e.RegistryID)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RegistryID < out[j].RegistryID })
	total := len(out)
	if filter.Page > 0 {
		size := filter.PageSize
		if size <= 0 {
			size = 20
		}
		start := (filter.Page - 1) * size
		if start >= len(out) {
			return nil, total, nil
		}
		end := min(start+size, len(out))
		out = out[start:end]
	}
	return out, total, nil
}
func (f *fakeRegistry) Insert(_ context.Context, e *cpn.ModelRegistryEntry) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, ok := f.entries[e.RegistryID]; ok {
		return cpn.ErrDuplicateRegistryID
	}
	// Enforce REQ-LIC-003 invariant in the fake.
	e.License.Status = cpn.LicenseUnreviewed
	e.Lifecycle.State = cpn.LifecycleRegistered
	e.Lifecycle.RegisteredAt = time.Now().UTC()
	f.put(e)
	return nil
}
func (f *fakeRegistry) Update(_ context.Context, e *cpn.ModelRegistryEntry, ifMatch cpn.UpdatedAt) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	cur, ok := f.entries[e.RegistryID]
	if !ok {
		return cpn.ErrModelNotFound
	}
	if !time.Time(ifMatch).Equal(cur.UpdatedAt) {
		return cpn.ErrRegistryConflict
	}
	e.UpdatedAt = time.Now().UTC()
	e.CreatedAt = cur.CreatedAt
	f.entries[e.RegistryID] = e
	return nil
}
func (f *fakeRegistry) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.entries[id]; !ok {
		return cpn.ErrModelNotFound
	}
	if f.productDef == id {
		return cpn.ErrCannotDeleteDefault
	}
	delete(f.entries, id)
	return nil
}
func (f *fakeRegistry) SetProductDefault(_ context.Context, id string, _ string) error {
	if f.setDefErr != nil {
		return f.setDefErr
	}
	e, ok := f.entries[id]
	if !ok {
		return cpn.ErrModelNotFound
	}
	if !e.Invokable() {
		// Auto-promote if license approved + lifecycle registered|disabled, like the real adapter.
		okLic := e.License.Status == cpn.LicenseApprovedCommercial ||
			e.License.Status == cpn.LicenseApprovedNonCommerc ||
			e.License.Status == cpn.LicenseRestricted
		if okLic && (e.Lifecycle.State == cpn.LifecycleRegistered || e.Lifecycle.State == cpn.LifecycleDisabled) {
			e.Lifecycle.State = cpn.LifecycleActive
			now := time.Now().UTC()
			e.Lifecycle.ActivatedAt = &now
		} else {
			return cpn.ErrModelNotInvokable
		}
	}
	f.productDef = id
	return nil
}
func (f *fakeRegistry) SetLicenseReview(_ context.Context, id string, rev cpn.LicenseReview) error {
	if f.setLicenseErr != nil {
		return f.setLicenseErr
	}
	e, ok := f.entries[id]
	if !ok {
		return cpn.ErrModelNotFound
	}
	e.License.Status = rev.Status
	e.License.ReviewedBy = &rev.ReviewerID
	now := time.Now().UTC()
	e.License.ReviewedAt = &now
	if e.Lifecycle.State == cpn.LifecyclePendingLicenseReview {
		e.Lifecycle.State = cpn.LifecycleRegistered
	}
	e.UpdatedAt = now
	return nil
}
func (f *fakeRegistry) GetRoleDefault(_ context.Context, role string) (string, error) {
	if id, ok := f.roleDefaults[role]; ok {
		return id, nil
	}
	return "", cpn.ErrModelNotFound
}
func (f *fakeRegistry) SetRoleDefault(_ context.Context, role, id string) error {
	f.roleDefaults[role] = id
	return nil
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

func activeEntry(registryID, vendor, display string) *cpn.ModelRegistryEntry {
	return &cpn.ModelRegistryEntry{
		RegistryID:  registryID,
		Vendor:      vendor,
		Family:      "family",
		Version:     "1.0",
		DisplayName: display,
		License: cpn.License{
			Status: cpn.LicenseApprovedCommercial,
			Source: "manual",
			Kind:   "proprietary-api",
		},
		Lifecycle: cpn.Lifecycle{
			State:        cpn.LifecycleActive,
			RegisteredAt: time.Now().UTC(),
		},
		Routes: []cpn.Route{{ProviderAdapter: "openrouter", ProviderModelID: registryID, Priority: 1, Enabled: true}},
	}
}

func newAdminModelsHandlers(reg cpn.ModelRegistry) *Handlers {
	return &Handlers{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ModelRegistry: reg,
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestAdminListModels_EmptyWhenNoEntries(t *testing.T) {
	reg := newFakeRegistry()
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body)
	}
	var body AdminModelListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Total != 0 || len(body.Items) != 0 {
		t.Fatalf("expected empty list, got %+v", body)
	}
}

func TestAdminListModels_ReturnsInvokableAndNonInvokable(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("anthropic/claude-haiku-4-5", "anthropic", "Haiku 4.5"))
	pending := activeEntry("x/y", "x", "Y")
	pending.Lifecycle.State = cpn.LifecycleRegistered
	pending.License.Status = cpn.LicenseUnreviewed
	reg.put(pending)
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListModels(rr, req)

	var body AdminModelListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(body.Items))
	}

	byID := map[string]ModelRegistryEntryResponse{}
	for _, it := range body.Items {
		byID[it.RegistryID] = it
	}
	if !byID["anthropic/claude-haiku-4-5"].Invokable {
		t.Error("haiku should be invokable")
	}
	if byID["x/y"].Invokable {
		t.Error("pending model should not be invokable")
	}
}

func TestAdminListModels_Filter(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("anthropic/claude-haiku-4-5", "anthropic", "Haiku"))
	reg.put(activeEntry("google/gemini-1.5", "google", "Gemini 1.5"))
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models?vendor=google", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListModels(rr, req)

	var body AdminModelListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if len(body.Items) != 1 || body.Items[0].Vendor != "google" {
		t.Fatalf("expected 1 google item, got %+v", body.Items)
	}
}

// TestAdminListModels_Pagination — AC-003. Total MUST reflect the
// filter-wide count, not the page length.
func TestAdminListModels_Pagination(t *testing.T) {
	reg := newFakeRegistry()
	for i := range 25 {
		reg.put(activeEntry(fmt.Sprintf("v/model-%02d", i), "v", fmt.Sprintf("Model %02d", i)))
	}
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models?size=10&page=1", nil)
	rr := httptest.NewRecorder()
	h.HandleAdminListModels(rr, req)

	var body AdminModelListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Total != 25 {
		t.Fatalf("total = %d, want 25", body.Total)
	}
	if len(body.Items) != 10 {
		t.Fatalf("items = %d, want 10 (page 1, size 10)", len(body.Items))
	}
	if body.Page != 1 || body.Size != 10 {
		t.Fatalf("page/size = %d/%d, want 1/10", body.Page, body.Size)
	}
}

func TestAdminGetModel_404(t *testing.T) {
	h := newAdminModelsHandlers(newFakeRegistry())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models/nope", nil)
	req.SetPathValue("registryID", "nope")
	rr := httptest.NewRecorder()
	h.HandleAdminGetModel(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "model_not_found") {
		t.Errorf("body missing code: %s", rr.Body)
	}
}

func TestAdminRegisterModel_HappyPath(t *testing.T) {
	reg := newFakeRegistry()
	h := newAdminModelsHandlers(reg)

	body := RegisterModelRequest{
		RegistryID:  "vendor/new-model",
		Vendor:      "vendor",
		Family:      "fam",
		Version:     "1.0",
		DisplayName: "New Model",
		Routes:      []cpn.Route{{ProviderAdapter: "openrouter", ProviderModelID: "vendor/new-model", Priority: 1, Enabled: true}},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminRegisterModel(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body)
	}
	var resp ModelRegistryEntryResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	// REQ-LIC-003: insert forces license=unreviewed, lifecycle=registered.
	if resp.License.Status != cpn.LicenseUnreviewed {
		t.Errorf("license status = %q, want unreviewed", resp.License.Status)
	}
	if resp.Lifecycle.State != cpn.LifecycleRegistered {
		t.Errorf("lifecycle = %q, want registered", resp.Lifecycle.State)
	}
}

func TestAdminRegisterModel_RejectsEmptyRoutes(t *testing.T) {
	h := newAdminModelsHandlers(newFakeRegistry())
	raw, _ := json.Marshal(RegisterModelRequest{
		RegistryID:  "vendor/x",
		Vendor:      "vendor",
		DisplayName: "X",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminRegisterModel(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid_input") {
		t.Errorf("body missing invalid_input: %s", rr.Body)
	}
}

func TestAdminUpdateModel_IfMatchConflict(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("a/b", "a", "A"))
	h := newAdminModelsHandlers(reg)

	desc := "updated"
	raw, _ := json.Marshal(UpdateModelRequest{
		IfMatch:     "2020-01-01T00:00:00Z", // stale
		Description: &desc,
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/models/a/b", bytes.NewReader(raw))
	req.SetPathValue("registryID", "a/b")
	rr := httptest.NewRecorder()
	h.HandleAdminUpdateModel(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "if_match_mismatch") {
		t.Errorf("body missing if_match_mismatch: %s", rr.Body)
	}
}

func TestAdminUpdateModel_AppliesFields(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("a/b", "a", "A"))
	h := newAdminModelsHandlers(reg)

	current, _ := reg.GetByID(context.Background(), "a/b")
	newDesc := "detailed description"
	raw, _ := json.Marshal(UpdateModelRequest{
		IfMatch:     current.UpdatedAt.Format(time.RFC3339Nano),
		Description: &newDesc,
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/models/a/b", bytes.NewReader(raw))
	req.SetPathValue("registryID", "a/b")
	rr := httptest.NewRecorder()
	h.HandleAdminUpdateModel(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body)
	}
	var resp ModelRegistryEntryResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Description != newDesc {
		t.Errorf("description = %q, want %q", resp.Description, newDesc)
	}
}

func TestAdminDeleteModel_CannotDeleteDefault(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("a/b", "a", "A"))
	reg.productDef = "a/b"
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/models/a/b", nil)
	req.SetPathValue("registryID", "a/b")
	rr := httptest.NewRecorder()
	h.HandleAdminDeleteModel(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "cannot_delete_default") {
		t.Errorf("body missing cannot_delete_default: %s", rr.Body)
	}
}

func TestAdminDeleteModel_HappyPath(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("a/b", "a", "A"))
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/models/a/b", nil)
	req.SetPathValue("registryID", "a/b")
	rr := httptest.NewRecorder()
	h.HandleAdminDeleteModel(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body)
	}
	if _, ok := reg.entries["a/b"]; ok {
		t.Error("entry should be gone")
	}
}

func TestAdminSetDefault_RejectsNotInvokable(t *testing.T) {
	reg := newFakeRegistry()
	pending := activeEntry("a/b", "a", "A")
	pending.License.Status = cpn.LicenseUnreviewed
	pending.Lifecycle.State = cpn.LifecycleRegistered
	reg.put(pending)
	h := newAdminModelsHandlers(reg)

	raw, _ := json.Marshal(SetDefaultRequest{RegistryID: "a/b"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/set-default", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminSetDefault(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "model_not_invokable") {
		t.Errorf("body missing model_not_invokable: %s", rr.Body)
	}
}

func TestAdminSetDefault_HappyPath(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("a/b", "a", "A"))
	h := newAdminModelsHandlers(reg)

	raw, _ := json.Marshal(SetDefaultRequest{RegistryID: "a/b"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/set-default", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminSetDefault(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body)
	}
	if reg.productDef != "a/b" {
		t.Errorf("productDef = %q, want a/b", reg.productDef)
	}
}

func TestAdminLicenseReview_HappyPath(t *testing.T) {
	reg := newFakeRegistry()
	pending := activeEntry("a/b", "a", "A")
	pending.License.Status = cpn.LicenseUnreviewed
	pending.Lifecycle.State = cpn.LifecyclePendingLicenseReview
	reg.put(pending)
	h := newAdminModelsHandlers(reg)

	raw, _ := json.Marshal(LicenseReviewRequest{
		RegistryID: "a/b",
		Status:     cpn.LicenseApprovedCommercial,
		Note:       "ok",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/license-review", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminLicenseReview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body)
	}
	got := reg.entries["a/b"]
	if got.License.Status != cpn.LicenseApprovedCommercial {
		t.Errorf("license = %q, want approved-commercial", got.License.Status)
	}
	// pending-license-review auto-advances to registered per AC-LIC-003.
	if got.Lifecycle.State != cpn.LifecycleRegistered {
		t.Errorf("lifecycle = %q, want registered", got.Lifecycle.State)
	}
}

func TestAdminLicenseReview_InvalidStatus(t *testing.T) {
	h := newAdminModelsHandlers(newFakeRegistry())
	raw, _ := json.Marshal(LicenseReviewRequest{RegistryID: "a/b", Status: "nonsense"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/license-review", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.HandleAdminLicenseReview(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid_input") {
		t.Errorf("body missing invalid_input: %s", rr.Body)
	}
}

// TestHandleGetModels_UsesRegistryWhenWired covers the public endpoint's
// registry path: when a ModelRegistry is attached, /api/v1/models returns
// the DB-backed default, invokable list, and registry array.
func TestHandleGetModels_UsesRegistryWhenWired(t *testing.T) {
	reg := newFakeRegistry()
	reg.put(activeEntry("anthropic/claude-opus-4-6", "anthropic", "Opus"))
	reg.put(activeEntry("google/gemma", "google", "Gemma"))
	reg.productDef = "anthropic/claude-opus-4-6"
	reg.roleDefaults["reasoning"] = "anthropic/claude-opus-4-6"
	h := newAdminModelsHandlers(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	rr := httptest.NewRecorder()
	h.HandleGetModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body ModelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Default != "anthropic/claude-opus-4-6" {
		t.Errorf("default = %q, want opus", body.Default)
	}
	if len(body.AvailableModels) != 2 {
		t.Errorf("available_models = %v, want 2 entries", body.AvailableModels)
	}
	if len(body.Registry) != 2 {
		t.Errorf("registry = %d, want 2", len(body.Registry))
	}
	// Role default applied.
	var reasoning ModelRoleResponse
	for _, r := range body.Roles {
		if r.Key == "reasoning" {
			reasoning = r
		}
	}
	if reasoning.DefaultModel != "anthropic/claude-opus-4-6" {
		t.Errorf("reasoning default = %q, want opus", reasoning.DefaultModel)
	}
}
