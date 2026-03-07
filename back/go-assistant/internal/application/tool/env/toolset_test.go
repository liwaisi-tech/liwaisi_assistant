package env_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appenv "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	envtool "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/env"
)

func setupRegistry(t *testing.T) (*tool.Registry, *appenv.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yaml")
	store := appenv.NewStore(path, nil)
	ts := envtool.NewToolSet(store)
	reg := tool.NewRegistry()
	ts.Register(reg)
	return reg, store, path
}

func TestToolSet_RegistersBothTools(t *testing.T) {
	reg, _, _ := setupRegistry(t)
	defs := reg.Definitions()

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Function.Name] = true
	}

	if !names["check_env_variable"] {
		t.Error("check_env_variable not registered")
	}
	if !names["reload_env"] {
		t.Error("reload_env not registered")
	}
}

func TestCheckEnvVariable(t *testing.T) {
	tests := []struct {
		name       string
		envKey     string
		envValue   string
		queryKey   string
		wantStatus string
	}{
		{
			name:       "variable is set in process env",
			envKey:     "CHECK_TEST_SET",
			envValue:   "some-value",
			queryKey:   "CHECK_TEST_SET",
			wantStatus: "set",
		},
		{
			name:       "variable is not set",
			queryKey:   "CHECK_TEST_MISSING",
			wantStatus: "not_set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, _, _ := setupRegistry(t)

			if tt.envValue != "" {
				t.Setenv(tt.envKey, tt.envValue)
			}

			args, _ := json.Marshal(map[string]string{"name": tt.queryKey})
			result, err := reg.Execute(context.Background(), "check_env_variable", args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(result), &resp); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			if resp["status"] != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp["status"], tt.wantStatus)
			}
		})
	}
}

func TestCheckEnvVariable_NeverReturnsValue(t *testing.T) {
	reg, _, _ := setupRegistry(t)

	secretValue := "super-secret-api-key-12345"
	t.Setenv("SECRET_CHECK_KEY", secretValue)

	args, _ := json.Marshal(map[string]string{"name": "SECRET_CHECK_KEY"})
	result, err := reg.Execute(context.Background(), "check_env_variable", args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, secretValue) {
		t.Fatal("SECURITY VIOLATION: tool response contains the secret value")
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "set" {
		t.Errorf("expected status=set, got %q", resp["status"])
	}
}

func TestCheckEnvVariable_EmptyName(t *testing.T) {
	reg, _, _ := setupRegistry(t)

	args, _ := json.Marshal(map[string]string{"name": ""})
	_, err := reg.Execute(context.Background(), "check_env_variable", args)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCheckEnvVariable_InvalidJSON(t *testing.T) {
	reg, _, _ := setupRegistry(t)

	_, err := reg.Execute(context.Background(), "check_env_variable", json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReloadEnv(t *testing.T) {
	reg, _, path := setupRegistry(t)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	content := "variables:\n  RELOAD_KEY_A: val-a\n  RELOAD_KEY_B: val-b\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Execute(context.Background(), "reload_env", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}
	if resp["variables_loaded"].(float64) != 2 {
		t.Errorf("variables_loaded = %v, want 2", resp["variables_loaded"])
	}

	if v := os.Getenv("RELOAD_KEY_A"); v != "val-a" {
		t.Errorf("RELOAD_KEY_A = %q, want %q", v, "val-a")
	}

	t.Cleanup(func() {
		os.Unsetenv("RELOAD_KEY_A")
		os.Unsetenv("RELOAD_KEY_B")
	})
}

func TestReloadEnv_NeverReturnsValues(t *testing.T) {
	reg, _, path := setupRegistry(t)

	secretValue := "reload-secret-xyz"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	content := "variables:\n  RELOAD_SECRET: " + secretValue + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Execute(context.Background(), "reload_env", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result, secretValue) {
		t.Fatal("SECURITY VIOLATION: reload_env response contains a secret value")
	}
	if strings.Contains(result, "RELOAD_SECRET") {
		t.Fatal("SECURITY VIOLATION: reload_env response contains a variable name")
	}

	t.Cleanup(func() { os.Unsetenv("RELOAD_SECRET") })
}

func TestReloadEnv_MissingFile(t *testing.T) {
	reg, _, _ := setupRegistry(t)

	result, err := reg.Execute(context.Background(), "reload_env", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["variables_loaded"].(float64) != 0 {
		t.Errorf("variables_loaded = %v, want 0", resp["variables_loaded"])
	}
}
