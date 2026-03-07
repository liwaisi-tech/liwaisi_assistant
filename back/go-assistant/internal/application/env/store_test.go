package env_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
)

func newTestStore(t *testing.T) (*env.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "env.yaml")
	return env.NewStore(path, nil), path
}

func TestStore_Load_MissingFile(t *testing.T) {
	store, _ := newTestStore(t)
	n, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("Load() = %d, want 0 for missing file", n)
	}
}

func TestStore_Load_ValidFile(t *testing.T) {
	store, path := newTestStore(t)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	content := "variables:\n  MY_KEY: my-value\n  OTHER_KEY: other-value\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("Load() = %d, want 2", n)
	}

	if v := os.Getenv("MY_KEY"); v != "my-value" {
		t.Errorf("os.Getenv(MY_KEY) = %q, want %q", v, "my-value")
	}
	if v := os.Getenv("OTHER_KEY"); v != "other-value" {
		t.Errorf("os.Getenv(OTHER_KEY) = %q, want %q", v, "other-value")
	}

	t.Cleanup(func() {
		os.Unsetenv("MY_KEY")
		os.Unsetenv("OTHER_KEY")
	})
}

func TestStore_Load_RegistersWithRedactor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	redactor := env.NewRedactor()
	store := env.NewStore(path, redactor)

	content := "variables:\n  SECRET_KEY: super-secret-value\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := redactor.Redact("output contains super-secret-value here")
	if strings.Contains(got, "super-secret-value") {
		t.Fatal("SECURITY VIOLATION: loaded value not registered with redactor")
	}
	if !strings.Contains(got, "[REDACTED:SECRET_KEY]") {
		t.Error("expected redaction placeholder for SECRET_KEY")
	}

	t.Cleanup(func() { os.Unsetenv("SECRET_KEY") })
}

func TestStore_Set(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
		errMsg  string
	}{
		{
			name:  "valid key and value",
			key:   "MY_API_KEY",
			value: "secret123",
		},
		{
			name:    "empty key rejected",
			key:     "",
			value:   "val",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "lowercase key rejected",
			key:     "my_api_key",
			value:   "val",
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name:    "key starting with digit rejected",
			key:     "1_BAD_KEY",
			value:   "val",
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name:    "key with special chars rejected",
			key:     "MY-KEY",
			value:   "val",
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name:    "empty value rejected",
			key:     "GOOD_KEY",
			value:   "",
			wantErr: true,
			errMsg:  "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := newTestStore(t)
			err := store.Set(context.Background(), tt.key, tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err, tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if v := os.Getenv(tt.key); v != tt.value {
				t.Errorf("os.Getenv(%s) = %q, want %q", tt.key, v, tt.value)
			}
			t.Cleanup(func() { os.Unsetenv(tt.key) })
		})
	}
}

func TestStore_Set_FilePermissions(t *testing.T) {
	store, path := newTestStore(t)

	if err := store.Set(context.Background(), "TEST_PERM_KEY", "value"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("expected file permissions 0600, got %04o", perm)
	}

	t.Cleanup(func() { os.Unsetenv("TEST_PERM_KEY") })
}

func TestStore_Set_PreservesExistingVars(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "FIRST_KEY", "first"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "SECOND_KEY", "second"); err != nil {
		t.Fatal(err)
	}

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("List() returned %d keys, want 2", len(keys))
	}

	t.Cleanup(func() {
		os.Unsetenv("FIRST_KEY")
		os.Unsetenv("SECOND_KEY")
	})
}

func TestStore_Set_OverwritesExisting(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "OVERWRITE_KEY", "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "OVERWRITE_KEY", "new"); err != nil {
		t.Fatal(err)
	}

	if v := os.Getenv("OVERWRITE_KEY"); v != "new" {
		t.Errorf("os.Getenv(OVERWRITE_KEY) = %q, want %q", v, "new")
	}

	t.Cleanup(func() { os.Unsetenv("OVERWRITE_KEY") })
}

func TestStore_Set_RegistersWithRedactor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	redactor := env.NewRedactor()
	store := env.NewStore(path, redactor)

	if err := store.Set(context.Background(), "REDACT_KEY", "secret-val-123"); err != nil {
		t.Fatal(err)
	}

	got := redactor.Redact("output has secret-val-123")
	if strings.Contains(got, "secret-val-123") {
		t.Fatal("SECURITY VIOLATION: set value not registered with redactor")
	}

	t.Cleanup(func() { os.Unsetenv("REDACT_KEY") })
}

func TestStore_Delete(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "DEL_KEY", "val"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "DEL_KEY"); err != nil {
		t.Fatal(err)
	}

	if _, ok := os.LookupEnv("DEL_KEY"); ok {
		t.Error("expected DEL_KEY to be unset after Delete()")
	}

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("List() returned %v, want empty", keys)
	}
}

func TestStore_Delete_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)

	if err := store.Set(context.Background(), "SOME_KEY", "val"); err != nil {
		t.Fatal(err)
	}

	err := store.Delete(context.Background(), "NO_SUCH_KEY")
	if err == nil {
		t.Fatal("expected error for deleting non-existent key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err)
	}

	t.Cleanup(func() { os.Unsetenv("SOME_KEY") })
}

func TestStore_Delete_EmptyKey(t *testing.T) {
	store, _ := newTestStore(t)
	err := store.Delete(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("Delete('') should return 'empty' error, got %v", err)
	}
}

func TestStore_Delete_UnregistersFromRedactor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	redactor := env.NewRedactor()
	store := env.NewStore(path, redactor)
	ctx := context.Background()

	if err := store.Set(ctx, "UNDEL_KEY", "topsecret"); err != nil {
		t.Fatal(err)
	}

	got := redactor.Redact("has topsecret")
	if !strings.Contains(got, "[REDACTED:UNDEL_KEY]") {
		t.Fatal("expected redaction before delete")
	}

	if err := store.Delete(ctx, "UNDEL_KEY"); err != nil {
		t.Fatal(err)
	}

	got = redactor.Redact("has topsecret")
	if strings.Contains(got, "[REDACTED") {
		t.Errorf("expected no redaction after delete, got %q", got)
	}
}

func TestStore_List(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if keys != nil {
		t.Errorf("List() on missing file = %v, want nil", keys)
	}

	if err := store.Set(ctx, "ZEBRA_KEY", "z"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "ALPHA_KEY", "a"); err != nil {
		t.Fatal(err)
	}

	keys, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("List() returned %d keys, want 2", len(keys))
	}
	if keys[0] != "ALPHA_KEY" || keys[1] != "ZEBRA_KEY" {
		t.Errorf("List() = %v, want [ALPHA_KEY ZEBRA_KEY]", keys)
	}

	t.Cleanup(func() {
		os.Unsetenv("ZEBRA_KEY")
		os.Unsetenv("ALPHA_KEY")
	})
}

func TestStore_Has(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	found, _, err := store.Has(ctx, "HAS_TEST_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("Has() returned true for non-existent key")
	}

	t.Setenv("HAS_TEST_KEY", "from-process")
	found, source, err := store.Has(ctx, "HAS_TEST_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("Has() returned false for key set in process env")
	}
	if source != "process_env" {
		t.Errorf("Has() source = %q, want %q", source, "process_env")
	}
}

func TestStore_Has_EmptyKey(t *testing.T) {
	store, _ := newTestStore(t)
	_, _, err := store.Has(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("Has('') should return 'empty' error, got %v", err)
	}
}

func TestStore_Has_NeverReturnsValue(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	secretValue := "super-secret-value-12345"
	t.Setenv("HAS_SECRET", secretValue)

	found, source, err := store.Has(ctx, "HAS_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Has() returned false for set key")
	}
	if strings.Contains(source, secretValue) {
		t.Fatal("SECURITY VIOLATION: Has() returned the secret value in source")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(5)
		go func() {
			defer wg.Done()
			_ = store.Set(ctx, "CONC_KEY", "val")
		}()
		go func() {
			defer wg.Done()
			_, _ = store.Load(ctx)
		}()
		go func() {
			defer wg.Done()
			_, _ = store.List(ctx)
		}()
		go func() {
			defer wg.Done()
			_, _, _ = store.Has(ctx, "CONC_KEY")
		}()
		go func() {
			defer wg.Done()
			_ = store.Delete(ctx, "CONC_KEY")
		}()
	}

	wg.Wait()
	t.Cleanup(func() { os.Unsetenv("CONC_KEY") })
}
