//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// setupRegistryRepo runs migrations and returns an adapter + its pool.
func setupRegistryRepo(t *testing.T) (*ModelRegistryRepository, context.Context) {
	t.Helper()
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return NewModelRegistryRepository(pool), context.Background()
}

// TestModelRegistryRepository_GetInvokable exercises REQ-GATE-001 at the
// repo layer. Seeded data: gemma = active+approved → invokable;
// z-ai/glm-5.1 = registered+unreviewed → ErrModelNotInvokable.
func TestModelRegistryRepository_GetInvokable(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Active_approved_returns_entry", func(t *testing.T) {
		e, err := r.GetInvokable(ctx, "google/gemma-4-31b-it")
		if err != nil {
			t.Fatalf("GetInvokable gemma: %v", err)
		}
		if !e.Invokable() {
			t.Fatalf("gemma should be invokable per REQ-GATE-001")
		}
		if !e.IsProductDefault {
			t.Fatalf("gemma should be flagged as product default")
		}
	})

	t.Run("Registered_unreviewed_returns_NotInvokable", func(t *testing.T) {
		_, err := r.GetInvokable(ctx, "z-ai/glm-5.1")
		if !errors.Is(err, cpn.ErrModelNotInvokable) {
			t.Fatalf("z-ai/glm-5.1 should be ErrModelNotInvokable, got %v", err)
		}
	})

	t.Run("Missing_row_returns_NotFound", func(t *testing.T) {
		_, err := r.GetInvokable(ctx, "does/not/exist")
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("missing row should be ErrModelNotFound, got %v", err)
		}
	})

	t.Run("Empty_id_returns_InvalidInput", func(t *testing.T) {
		_, err := r.GetInvokable(ctx, "")
		if !errors.Is(err, cpn.ErrInvalidInput) {
			t.Fatalf("empty id should be ErrInvalidInput, got %v", err)
		}
	})
}

func TestModelRegistryRepository_GetByID(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Non_invokable_row_still_returned", func(t *testing.T) {
		// GetByID is the admin path — it must return unreviewed rows so the
		// admin UI can display them.
		e, err := r.GetByID(ctx, "z-ai/glm-5.1")
		if err != nil {
			t.Fatalf("GetByID glm: %v", err)
		}
		if e.Invokable() {
			t.Fatalf("glm is seeded as not-invokable but adapter returns invokable=true")
		}
		if e.License.Status != cpn.LicenseUnreviewed {
			t.Fatalf("glm license=%q want unreviewed", e.License.Status)
		}
		if e.Lifecycle.State != cpn.LifecycleRegistered {
			t.Fatalf("glm lifecycle=%q want registered", e.Lifecycle.State)
		}
	})

	t.Run("Full_field_hydration", func(t *testing.T) {
		e, err := r.GetByID(ctx, "anthropic/claude-opus-4-6")
		if err != nil {
			t.Fatalf("GetByID opus: %v", err)
		}
		if e.Vendor != "anthropic" || e.Family != "claude" || e.Version != "opus-4.6" {
			t.Fatalf("identity fields wrong: %+v", e)
		}
		if e.Pricing.InputPerToken == 0 || e.Pricing.OutputPerToken == 0 {
			t.Fatalf("pricing not hydrated: %+v", e.Pricing)
		}
		if len(e.Routes) == 0 || e.Routes[0].ProviderAdapter != "openrouter" {
			t.Fatalf("routes[0] not hydrated: %+v", e.Routes)
		}
		if e.License.Kind != "proprietary-api" || e.License.Name == nil {
			t.Fatalf("license not hydrated: %+v", e.License)
		}
	})
}

func TestModelRegistryRepository_GetProductDefault(t *testing.T) {
	r, ctx := setupRegistryRepo(t)
	e, err := r.GetProductDefault(ctx)
	if err != nil {
		t.Fatalf("GetProductDefault: %v", err)
	}
	if e.RegistryID != "google/gemma-4-31b-it" {
		t.Fatalf("product default = %q want google/gemma-4-31b-it", e.RegistryID)
	}
	if !e.IsProductDefault {
		t.Fatalf("IsProductDefault flag not set on the default row")
	}
}

func TestModelRegistryRepository_ListInvokable(t *testing.T) {
	r, ctx := setupRegistryRepo(t)
	rows, err := r.ListInvokable(ctx)
	if err != nil {
		t.Fatalf("ListInvokable: %v", err)
	}
	// Per seeds 020 + 021: 18 total rows.
	//   Non-invokable: z-ai/glm-5.1 (unreviewed license) and
	//                  google/gemini-3.1-flash-lite-preview (deprecated by 021).
	//   Invokable:     18 - 2 = 16.
	const wantInvokable = 16
	if len(rows) != wantInvokable {
		t.Fatalf("ListInvokable returned %d rows, want %d", len(rows), wantInvokable)
	}
	for _, e := range rows {
		if !e.Invokable() {
			t.Fatalf("ListInvokable returned non-invokable row %s", e.RegistryID)
		}
	}
}

func TestModelRegistryRepository_ListAll_Filters(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	// Seeds 020 + 021 land 18 rows.
	//   6 anthropic (opus-4-6, sonnet-4-6, haiku-4-5-20251001, opus-4-7, sonnet-4-5, haiku-4-5)
	//   4 gemma     (4-31b-it, 4-26b-a4b-it, 3-27b-it, 3-12b-it)
	//   7 gemini    (3.1-flash-lite-preview [deprecated], 2.5-flash-lite, 2.0-flash-001,
	//                3-pro-preview, 3-flash-preview, 2.5-pro, 2.5-flash)
	//   1 z-ai      (glm-5.1, unreviewed)
	// Non-invokable: z-ai/glm-5.1 + google/gemini-3.1-flash-lite-preview = 2.
	const (
		wantTotal        = 18
		wantAnthropic    = 6
		wantGeminiSearch = 7
		wantInvokable    = 16
	)

	t.Run("All_rows", func(t *testing.T) {
		rows, err := r.ListAll(ctx, cpn.ModelListFilter{})
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(rows) != wantTotal {
			t.Fatalf("expected %d seeded rows, got %d", wantTotal, len(rows))
		}
	})

	t.Run("By_vendor", func(t *testing.T) {
		rows, err := r.ListAll(ctx, cpn.ModelListFilter{Vendor: "anthropic"})
		if err != nil {
			t.Fatalf("ListAll vendor: %v", err)
		}
		if len(rows) != wantAnthropic {
			t.Fatalf("anthropic rows = %d, want %d", len(rows), wantAnthropic)
		}
	})

	t.Run("By_invokable_true", func(t *testing.T) {
		yes := true
		rows, err := r.ListAll(ctx, cpn.ModelListFilter{Invokable: &yes})
		if err != nil {
			t.Fatalf("ListAll invokable=true: %v", err)
		}
		if len(rows) != wantInvokable {
			t.Fatalf("invokable rows = %d, want %d", len(rows), wantInvokable)
		}
	})

	t.Run("By_invokable_false", func(t *testing.T) {
		no := false
		rows, err := r.ListAll(ctx, cpn.ModelListFilter{Invokable: &no})
		if err != nil {
			t.Fatalf("ListAll invokable=false: %v", err)
		}
		got := registryIDs(rows)
		want := map[string]bool{
			"google/gemini-3.1-flash-lite-preview": false,
			"z-ai/glm-5.1":                         false,
		}
		if len(got) != len(want) {
			t.Fatalf("non-invokable rows = %v, want keys %v", got, want)
		}
		for _, id := range got {
			if _, ok := want[id]; !ok {
				t.Fatalf("unexpected non-invokable row %q (have %v)", id, got)
			}
			want[id] = true
		}
		for id, saw := range want {
			if !saw {
				t.Fatalf("missing non-invokable row %q (got %v)", id, got)
			}
		}
	})

	t.Run("Search_by_display_name", func(t *testing.T) {
		rows, err := r.ListAll(ctx, cpn.ModelListFilter{Search: "Gemini"})
		if err != nil {
			t.Fatalf("ListAll search: %v", err)
		}
		if len(rows) != wantGeminiSearch {
			t.Fatalf("gemini search rows = %d, want %d", len(rows), wantGeminiSearch)
		}
	})

	t.Run("Pagination", func(t *testing.T) {
		// 18 total at pageSize=4 → pages of 4,4,4,4,2.
		page1, err := r.ListAll(ctx, cpn.ModelListFilter{Page: 1, PageSize: 4})
		if err != nil {
			t.Fatalf("ListAll page1: %v", err)
		}
		if len(page1) != 4 {
			t.Fatalf("page1 len = %d, want 4", len(page1))
		}
		page5, err := r.ListAll(ctx, cpn.ModelListFilter{Page: 5, PageSize: 4})
		if err != nil {
			t.Fatalf("ListAll page5: %v", err)
		}
		if len(page5) != 2 {
			t.Fatalf("page5 len = %d, want 2 (18 total, 4+4+4+4+2)", len(page5))
		}
	})
}

func TestModelRegistryRepository_Insert_ForcesInitialState(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	approved := cpn.LicenseApprovedCommercial
	entry := &cpn.ModelRegistryEntry{
		RegistryID:  "test/acme-1",
		Vendor:      "test",
		Family:      "acme",
		Version:     "1",
		DisplayName: "Test ACME 1",
		License: cpn.License{
			Kind: "community",
			// Attempt to seed as approved — Insert MUST ignore this per REQ-LIC-003.
			Status: approved,
		},
		Lifecycle: cpn.Lifecycle{
			// Attempt to seed as active — Insert MUST ignore this.
			State: cpn.LifecycleActive,
		},
	}
	if err := r.Insert(ctx, entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := r.GetByID(ctx, "test/acme-1")
	if err != nil {
		t.Fatalf("GetByID after insert: %v", err)
	}
	if got.License.Status != cpn.LicenseUnreviewed {
		t.Fatalf("Insert license_status = %q, want unreviewed (REQ-LIC-003)", got.License.Status)
	}
	if got.Lifecycle.State != cpn.LifecycleRegistered {
		t.Fatalf("Insert lifecycle_state = %q, want registered (REQ-LIC-003)", got.Lifecycle.State)
	}
}

func TestModelRegistryRepository_Insert_DuplicateRejected(t *testing.T) {
	r, ctx := setupRegistryRepo(t)
	entry := &cpn.ModelRegistryEntry{
		RegistryID:  "google/gemma-4-31b-it", // already in seed
		Vendor:      "google",
		Family:      "gemma",
		Version:     "4-31b-it",
		DisplayName: "duplicate attempt",
	}
	err := r.Insert(ctx, entry)
	if err == nil {
		t.Fatalf("Insert duplicate should have failed")
	}
}

func TestModelRegistryRepository_Update_Optimistic(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Matching_ifMatch_succeeds", func(t *testing.T) {
		orig, err := r.GetByID(ctx, "anthropic/claude-opus-4-6")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		orig.DisplayName = "Claude Opus Renamed"
		if err := r.Update(ctx, orig, cpn.UpdatedAt(orig.UpdatedAt)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		updated, err := r.GetByID(ctx, "anthropic/claude-opus-4-6")
		if err != nil {
			t.Fatalf("GetByID after update: %v", err)
		}
		if updated.DisplayName != "Claude Opus Renamed" {
			t.Fatalf("display_name = %q, want Claude Opus Renamed", updated.DisplayName)
		}
	})

	t.Run("Stale_ifMatch_returns_Conflict", func(t *testing.T) {
		orig, err := r.GetByID(ctx, "anthropic/claude-sonnet-4-6")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		orig.DisplayName = "Mutation A"
		if err := r.Update(ctx, orig, cpn.UpdatedAt(orig.UpdatedAt)); err != nil {
			t.Fatalf("first Update: %v", err)
		}
		// Use the ORIGINAL timestamp again — should conflict.
		orig.DisplayName = "Mutation B"
		err = r.Update(ctx, orig, cpn.UpdatedAt(orig.UpdatedAt))
		if !errors.Is(err, cpn.ErrRegistryConflict) {
			t.Fatalf("stale ifMatch should return ErrRegistryConflict, got %v", err)
		}
	})

	t.Run("Missing_row_returns_NotFound", func(t *testing.T) {
		err := r.Update(ctx, &cpn.ModelRegistryEntry{RegistryID: "missing/row"}, cpn.UpdatedAt(time.Now()))
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("missing row should return ErrModelNotFound, got %v", err)
		}
	})
}

func TestModelRegistryRepository_Delete(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Cannot_delete_default", func(t *testing.T) {
		err := r.Delete(ctx, "google/gemma-4-31b-it")
		if !errors.Is(err, cpn.ErrCannotDeleteDefault) {
			t.Fatalf("deleting default should return ErrCannotDeleteDefault, got %v", err)
		}
	})

	t.Run("Delete_non_default_succeeds_after_role_rebind", func(t *testing.T) {
		// z-ai/glm-5.1 is not the default and not referenced by a role; delete OK.
		if err := r.Delete(ctx, "z-ai/glm-5.1"); err != nil {
			t.Fatalf("Delete glm: %v", err)
		}
		_, err := r.GetByID(ctx, "z-ai/glm-5.1")
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("after delete GetByID should return ErrModelNotFound, got %v", err)
		}
	})

	t.Run("Delete_missing_row", func(t *testing.T) {
		err := r.Delete(ctx, "nope/nope")
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("missing row should return ErrModelNotFound, got %v", err)
		}
	})
}

func TestModelRegistryRepository_SetProductDefault(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Swap_to_already_active_target", func(t *testing.T) {
		if err := r.SetProductDefault(ctx, "anthropic/claude-opus-4-6", "admin@example.com"); err != nil {
			t.Fatalf("SetProductDefault opus: %v", err)
		}
		def, err := r.GetProductDefault(ctx)
		if err != nil {
			t.Fatalf("GetProductDefault: %v", err)
		}
		if def.RegistryID != "anthropic/claude-opus-4-6" {
			t.Fatalf("default = %q, want opus", def.RegistryID)
		}

		// The original gemma row must no longer carry the flag.
		gemma, err := r.GetByID(ctx, "google/gemma-4-31b-it")
		if err != nil {
			t.Fatalf("GetByID gemma: %v", err)
		}
		if gemma.IsProductDefault {
			t.Fatalf("gemma still flagged as default after swap")
		}
	})

	t.Run("Cannot_set_unreviewed_as_default", func(t *testing.T) {
		err := r.SetProductDefault(ctx, "z-ai/glm-5.1", "admin@example.com")
		if !errors.Is(err, cpn.ErrModelNotInvokable) {
			t.Fatalf("unreviewed target should return ErrModelNotInvokable, got %v", err)
		}
	})

	t.Run("Cannot_set_missing_target", func(t *testing.T) {
		err := r.SetProductDefault(ctx, "not/here", "admin@example.com")
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("missing target should return ErrModelNotFound, got %v", err)
		}
	})
}

func TestModelRegistryRepository_SetLicenseReview(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Approve_unreviewed_row", func(t *testing.T) {
		err := r.SetLicenseReview(ctx, "z-ai/glm-5.1", cpn.LicenseReview{
			Status:     cpn.LicenseApprovedNonCommerc,
			ReviewerID: "legal@example.com",
			Note:       "GLM community license cleared for non-commercial use",
		})
		if err != nil {
			t.Fatalf("SetLicenseReview: %v", err)
		}
		got, err := r.GetByID(ctx, "z-ai/glm-5.1")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.License.Status != cpn.LicenseApprovedNonCommerc {
			t.Fatalf("license_status = %q, want approved-non-commercial", got.License.Status)
		}
		if got.License.ReviewedBy == nil || *got.License.ReviewedBy != "legal@example.com" {
			t.Fatalf("reviewed_by not persisted: %v", got.License.ReviewedBy)
		}
		if got.License.ReviewedAt == nil {
			t.Fatalf("reviewed_at not set")
		}
	})

	t.Run("Invalid_status_rejected", func(t *testing.T) {
		err := r.SetLicenseReview(ctx, "z-ai/glm-5.1", cpn.LicenseReview{
			Status:     "not-a-real-status",
			ReviewerID: "legal@example.com",
		})
		if !errors.Is(err, cpn.ErrInvalidInput) {
			t.Fatalf("bogus status should return ErrInvalidInput, got %v", err)
		}
	})
}

func TestModelRegistryRepository_RoleDefaults(t *testing.T) {
	r, ctx := setupRegistryRepo(t)

	t.Run("Seeded_roles_resolve_to_optimized_models", func(t *testing.T) {
		// Migration 022 sets each role default to its cost/effectiveness pick.
		want := map[string]string{
			"classifier":   "google/gemini-2.5-flash-lite",
			"structured":   "google/gemini-2.5-flash",
			"reasoning":    "google/gemini-2.5-pro",
			"long-context": "google/gemini-2.5-flash",
			"summarize":    "google/gemini-2.5-flash-lite",
			"thinking":     "anthropic/claude-opus-4-7",
		}
		for role, wantID := range want {
			id, err := r.GetRoleDefault(ctx, role)
			if err != nil {
				t.Fatalf("GetRoleDefault %s: %v", role, err)
			}
			if id != wantID {
				t.Errorf("role %s default = %q, want %q", role, id, wantID)
			}
		}
	})

	t.Run("Update_role_to_existing_model", func(t *testing.T) {
		if err := r.SetRoleDefault(ctx, "reasoning", "anthropic/claude-opus-4-6"); err != nil {
			t.Fatalf("SetRoleDefault: %v", err)
		}
		id, err := r.GetRoleDefault(ctx, "reasoning")
		if err != nil {
			t.Fatalf("GetRoleDefault after set: %v", err)
		}
		if id != "anthropic/claude-opus-4-6" {
			t.Fatalf("reasoning default = %q, want opus", id)
		}
	})

	t.Run("Update_role_to_missing_model_returns_NotFound", func(t *testing.T) {
		err := r.SetRoleDefault(ctx, "reasoning", "missing/model")
		if !errors.Is(err, cpn.ErrModelNotFound) {
			t.Fatalf("missing target should return ErrModelNotFound, got %v", err)
		}
	})
}

func registryIDs(rows []*cpn.ModelRegistryEntry) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.RegistryID)
	}
	return out
}
