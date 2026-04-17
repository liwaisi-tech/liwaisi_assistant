//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration_021_ModelRegistryExpand verifies that the 021 migration adds
// the current-generation Anthropic + Google models and deprecates the stale
// gemini-3.1-flash-lite-preview row — without disturbing the product default
// or role defaults configured by 020.
func TestMigration_021_ModelRegistryExpand(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()

	t.Run("New_rows_inserted", func(t *testing.T) {
		cases := []struct {
			registryID    string
			wantVendor    string
			wantFamily    string
			wantLifecycle string
			wantLicense   string
		}{
			{"anthropic/claude-opus-4-7", "anthropic", "claude", "active", "approved-commercial"},
			{"anthropic/claude-sonnet-4-5", "anthropic", "claude", "active", "approved-commercial"},
			{"anthropic/claude-haiku-4-5", "anthropic", "claude", "active", "approved-commercial"},
			{"google/gemini-3-pro-preview", "google", "gemini", "active", "approved-commercial"},
			{"google/gemini-3-flash-preview", "google", "gemini", "active", "approved-commercial"},
			{"google/gemini-2.5-pro", "google", "gemini", "active", "approved-commercial"},
			{"google/gemini-2.5-flash", "google", "gemini", "active", "approved-commercial"},
			{"google/gemma-3-27b-it", "google", "gemma", "active", "approved-commercial"},
			{"google/gemma-3-12b-it", "google", "gemma", "active", "approved-commercial"},
		}
		for _, c := range cases {
			var vendor, family, lifecycle, license string
			var ctxLen int
			err := pool.QueryRow(ctx,
				`SELECT vendor, family, lifecycle_state, license_status, context_length
				 FROM models WHERE registry_id = $1`, c.registryID,
			).Scan(&vendor, &family, &lifecycle, &license, &ctxLen)
			if err != nil {
				t.Fatalf("query %s: %v", c.registryID, err)
			}
			if vendor != c.wantVendor || family != c.wantFamily {
				t.Errorf("%s: vendor/family = %q/%q, want %q/%q",
					c.registryID, vendor, family, c.wantVendor, c.wantFamily)
			}
			if lifecycle != c.wantLifecycle {
				t.Errorf("%s: lifecycle = %q, want %q", c.registryID, lifecycle, c.wantLifecycle)
			}
			if license != c.wantLicense {
				t.Errorf("%s: license = %q, want %q", c.registryID, license, c.wantLicense)
			}
			if ctxLen == 0 {
				t.Errorf("%s: context_length should be set by 021 seed, got 0", c.registryID)
			}
		}
	})

	t.Run("Deprecation_chain", func(t *testing.T) {
		var lifecycle, reason string
		var replacedBy, deprecatedAt sql.NullString
		err := pool.QueryRow(ctx,
			`SELECT lifecycle_state, lifecycle_replaced_by, lifecycle_reason,
			        lifecycle_deprecated_at::text
			 FROM models WHERE registry_id = 'google/gemini-3.1-flash-lite-preview'`,
		).Scan(&lifecycle, &replacedBy, &reason, &deprecatedAt)
		if err != nil {
			t.Fatalf("query deprecated row: %v", err)
		}
		if lifecycle != "deprecated" {
			t.Errorf("lifecycle = %q, want deprecated", lifecycle)
		}
		if !replacedBy.Valid || replacedBy.String != "google/gemini-3-flash-preview" {
			t.Errorf("replaced_by = %+v, want google/gemini-3-flash-preview", replacedBy)
		}
		if !deprecatedAt.Valid {
			t.Error("lifecycle_deprecated_at should be set")
		}
		if reason == "" {
			t.Error("lifecycle_reason should be non-empty")
		}
	})

	t.Run("Product_default_unchanged", func(t *testing.T) {
		var registryID string
		err := pool.QueryRow(ctx, `
			SELECT m.registry_id
			FROM registry_config rc
			JOIN models m ON m.id = rc.product_default_model_id
		`).Scan(&registryID)
		if err != nil {
			t.Fatalf("resolve default: %v", err)
		}
		if registryID != "google/gemma-4-31b-it" {
			t.Errorf("product default changed to %q; 021 must not touch registry_config", registryID)
		}
	})

	// NOTE: 021's in-isolation behaviour on model_role_defaults was "untouched",
	// but migration 022 subsequently repoints every role. Assertions on the
	// final role mapping live in TestMigration_020_ModelRegistry/AC_REG_003_role_defaults.

	t.Run("Routes_point_to_openrouter", func(t *testing.T) {
		newIDs := []string{
			"anthropic/claude-opus-4-7",
			"anthropic/claude-sonnet-4-5",
			"anthropic/claude-haiku-4-5",
			"google/gemini-3-pro-preview",
			"google/gemini-3-flash-preview",
			"google/gemini-2.5-pro",
			"google/gemini-2.5-flash",
			"google/gemma-3-27b-it",
			"google/gemma-3-12b-it",
		}
		for _, id := range newIDs {
			var adapter string
			err := pool.QueryRow(ctx,
				`SELECT routes->0->>'provider_adapter' FROM models WHERE registry_id = $1`, id,
			).Scan(&adapter)
			if err != nil {
				t.Fatalf("query route for %s: %v", id, err)
			}
			if adapter != "openrouter" {
				t.Errorf("%s: primary route adapter = %q, want openrouter", id, adapter)
			}
		}
	})
}
