//go:build integration

package postgres

import (
	"context"
	"testing"
)

// TestMigration_020_ModelRegistry covers the acceptance criteria from
// spec/spec-architecture-model-registry-and-a2ui-management.md §5:
//
//	AC-REG-001: 9 rows inserted, registry_config singleton initialized to gemma.
//	AC-REG-002: ON DELETE RESTRICT on the default; CHECK (id = 1) on registry_config.
//	AC-REG-003: 6 model_role_defaults rows, all pointing to gemma.
func TestMigration_020_ModelRegistry(t *testing.T) {
	pool, dsn := setupPostgres(t)
	if err := RunMigrations(dsn, migrationsPath()); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	ctx := context.Background()

	t.Run("Tables_exist", func(t *testing.T) {
		assertTableExists(t, pool, "models")
		assertTableExists(t, pool, "registry_config")
		assertTableExists(t, pool, "model_role_defaults")
	})

	t.Run("Models_columns", func(t *testing.T) {
		for _, col := range []string{
			"id", "registry_id", "vendor", "family", "version", "display_name",
			"context_length", "tokenizer",
			"pricing_input_per_token", "pricing_output_per_token",
			"license_kind", "license_spdx_id", "license_source", "license_status",
			"lifecycle_state", "lifecycle_registered_at",
			"routes", "source_metadata", "created_at", "updated_at",
		} {
			assertColumnExists(t, pool, "models", col)
		}
	})

	t.Run("No_is_product_default_column", func(t *testing.T) {
		// REQ-REG-008: is_product_default is NOT a column on models.
		// The product default lives in the registry_config singleton.
		var exists bool
		row := pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT FROM information_schema.columns
				WHERE table_name = 'models' AND column_name = 'is_product_default'
			)`)
		if err := row.Scan(&exists); err != nil {
			t.Fatalf("check column: %v", err)
		}
		if exists {
			t.Fatal("models.is_product_default MUST NOT exist; use registry_config pointer")
		}
	})

	t.Run("AC_REG_001_seed_counts", func(t *testing.T) {
		// 9 rows from migration 020 + 9 rows from migration 021 = 18.
		const wantModels = 18
		var modelCount int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM models").Scan(&modelCount); err != nil {
			t.Fatalf("count models: %v", err)
		}
		if modelCount != wantModels {
			t.Fatalf("AC-REG-001: expected %d seed models, got %d", wantModels, modelCount)
		}

		var cfgCount int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM registry_config").Scan(&cfgCount); err != nil {
			t.Fatalf("count registry_config: %v", err)
		}
		if cfgCount != 1 {
			t.Fatalf("AC-REG-001: expected exactly 1 registry_config row, got %d", cfgCount)
		}

		var defaultRegistryID string
		err := pool.QueryRow(ctx, `
			SELECT m.registry_id
			FROM registry_config rc
			JOIN models m ON m.id = rc.product_default_model_id
		`).Scan(&defaultRegistryID)
		if err != nil {
			t.Fatalf("resolve default: %v", err)
		}
		if defaultRegistryID != "google/gemma-4-31b-it" {
			t.Fatalf("AC-REG-001: expected default 'google/gemma-4-31b-it', got %q", defaultRegistryID)
		}
	})

	t.Run("AC_REG_002_singleton_check", func(t *testing.T) {
		// CHECK (id = 1) on registry_config rejects a second row.
		_, err := pool.Exec(ctx, `
			INSERT INTO registry_config (id, product_default_model_id)
			VALUES (2, (SELECT id FROM models LIMIT 1))
		`)
		if err == nil {
			t.Fatal("AC-REG-002: CHECK (id = 1) should reject id=2 insert")
		}
	})

	t.Run("AC_REG_002_fk_restrict_prevents_default_delete", func(t *testing.T) {
		// ON DELETE RESTRICT prevents deleting the row referenced by registry_config.
		var defaultUUID string
		if err := pool.QueryRow(ctx,
			"SELECT product_default_model_id FROM registry_config WHERE id = 1").Scan(&defaultUUID); err != nil {
			t.Fatalf("get default id: %v", err)
		}
		_, err := pool.Exec(ctx, "DELETE FROM models WHERE id = $1", defaultUUID)
		if err == nil {
			t.Fatal("AC-REG-002: ON DELETE RESTRICT should reject deleting the default row")
		}
	})

	t.Run("AC_REG_002_default_not_null", func(t *testing.T) {
		// product_default_model_id is NOT NULL — cannot unset.
		_, err := pool.Exec(ctx,
			"UPDATE registry_config SET product_default_model_id = NULL WHERE id = 1")
		if err == nil {
			t.Fatal("AC-REG-002: product_default_model_id NOT NULL should reject UPDATE to NULL")
		}
	})

	t.Run("AC_REG_003_role_defaults", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM model_role_defaults").Scan(&count); err != nil {
			t.Fatalf("count role defaults: %v", err)
		}
		if count != 6 {
			t.Fatalf("AC-REG-003: expected 6 role defaults, got %d", count)
		}

		// Migration 022 repoints each role at its cost/effectiveness-optimal
		// registry_id. 020 seeded all six to gemma; this assertion reflects
		// the full chain.
		wantRoles := map[string]string{
			"classifier":   "google/gemini-2.5-flash-lite",
			"structured":   "google/gemini-2.5-flash",
			"reasoning":    "google/gemini-2.5-pro",
			"long-context": "google/gemini-2.5-flash",
			"summarize":    "google/gemini-2.5-flash-lite",
			"thinking":     "anthropic/claude-opus-4-7",
		}

		rows, err := pool.Query(ctx, "SELECT role, registry_id FROM model_role_defaults ORDER BY role")
		if err != nil {
			t.Fatalf("query role defaults: %v", err)
		}
		defer rows.Close()

		seen := map[string]bool{}
		for rows.Next() {
			var role, registryID string
			if err := rows.Scan(&role, &registryID); err != nil {
				t.Fatalf("scan: %v", err)
			}
			want, ok := wantRoles[role]
			if !ok {
				t.Fatalf("unexpected role %q", role)
			}
			seen[role] = true
			if registryID != want {
				t.Errorf("role %q points to %q, want %q", role, registryID, want)
			}
		}
		for r := range wantRoles {
			if !seen[r] {
				t.Errorf("role %q missing", r)
			}
		}
	})

	t.Run("Curated_licenses_per_REQ_SEED_006", func(t *testing.T) {
		// REQ-SEED-006: known-safe commercial-API models enter `active`;
		// community-licensed Llama/Qwen enter `registered` + `unreviewed`.
		cases := []struct {
			registryID    string
			wantLicense   string
			wantLifecycle string
		}{
			{"anthropic/claude-opus-4-6", "approved-commercial", "active"},
			{"anthropic/claude-sonnet-4-6", "approved-commercial", "active"},
			{"anthropic/claude-haiku-4-5-20251001", "approved-commercial", "active"},
			{"google/gemma-4-31b-it", "approved-commercial", "active"},
			{"google/gemma-4-26b-a4b-it", "approved-commercial", "active"},
			// Migration 021 deprecates this row in favour of google/gemini-3-flash-preview.
			{"google/gemini-3.1-flash-lite-preview", "approved-commercial", "deprecated"},
			{"google/gemini-2.5-flash-lite", "approved-commercial", "active"},
			{"google/gemini-2.0-flash-001", "approved-commercial", "active"},
			{"z-ai/glm-5.1", "unreviewed", "registered"},
		}
		for _, c := range cases {
			var license, lifecycle string
			err := pool.QueryRow(ctx,
				"SELECT license_status, lifecycle_state FROM models WHERE registry_id = $1",
				c.registryID,
			).Scan(&license, &lifecycle)
			if err != nil {
				t.Fatalf("query %s: %v", c.registryID, err)
			}
			if license != c.wantLicense {
				t.Errorf("%s: license_status = %q, want %q", c.registryID, license, c.wantLicense)
			}
			if lifecycle != c.wantLifecycle {
				t.Errorf("%s: lifecycle_state = %q, want %q", c.registryID, lifecycle, c.wantLifecycle)
			}
		}
	})

	t.Run("Idempotent", func(t *testing.T) {
		// Running the migrations a second time must not error or duplicate rows.
		const wantModels = 18
		if err := RunMigrations(dsn, migrationsPath()); err != nil {
			t.Fatalf("second migration run: %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM models").Scan(&count); err != nil {
			t.Fatalf("count after second run: %v", err)
		}
		if count != wantModels {
			t.Fatalf("idempotent seed broken: got %d models after second run, want %d", count, wantModels)
		}
	})
}
