package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParams_Validate(t *testing.T) {
	cases := []struct {
		name    string
		p       Params
		wantErr bool
	}{
		{"ok", Params{Name: "echo-tool", Module: "brae.tools/echo-tool", Description: "prints args"}, false},
		{"bad-name-uppercase", Params{Name: "Echo", Module: "m"}, true},
		{"bad-name-empty", Params{Name: "", Module: "m"}, true},
		{"bad-name-special", Params{Name: "echo_tool", Module: "m"}, true},
		{"bad-name-leading-digit", Params{Name: "1tool", Module: "m"}, true},
		{"bad-module-empty", Params{Name: "ok", Module: ""}, true},
		{"bad-module-space", Params{Name: "ok", Module: "foo bar"}, true},
		{"bad-desc-newline", Params{Name: "ok", Module: "m", Description: "line1\nline2"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestRenderInto_ProducesExpectedTree(t *testing.T) {
	dir := t.TempDir()
	params := Params{
		Name:        "echo-tool",
		Module:      "brae.tools/echo-tool",
		Description: "prints each arg on its own line",
		Author:      "brae",
		Date:        time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
	}

	if err := RenderInto(dir, params); err != nil {
		t.Fatalf("RenderInto: %v", err)
	}

	// Files that MUST exist in the rendered tree. Paths are relative to dir.
	mustExist := []string{
		"go.mod",
		"Makefile",
		".golangci.yml",
		".gitignore",
		"README.md",
		"install.sh",
		"cmd/echo-tool/main.go",
		"internal/domain/doc.go",
		"internal/app/app.go",
		"internal/ports/ports.go",
		"internal/adapters/stdout_echo.go",
		"internal/adapters/stdout_echo_test.go",
		"internal/app/app_test.go",
	}
	for _, rel := range mustExist {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// .tmpl suffix must not leak through.
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasSuffix(p, ".tmpl") {
			t.Errorf("rendered tree contains unresolved template: %s", p)
		}
		return nil
	})
}

func TestRenderInto_ModuleAndNameInterpolated(t *testing.T) {
	dir := t.TempDir()
	params := Params{
		Name:        "greeter",
		Module:      "example.com/brae/greeter",
		Description: "says hi",
		Date:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := RenderInto(dir, params); err != nil {
		t.Fatalf("RenderInto: %v", err)
	}

	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module example.com/brae/greeter") {
		t.Errorf("go.mod missing module line:\n%s", goMod)
	}

	makefile, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if !strings.Contains(string(makefile), "BINARY := greeter") {
		t.Errorf("Makefile missing binary name:\n%s", makefile)
	}

	main, err := os.ReadFile(filepath.Join(dir, "cmd/greeter/main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(main), `"example.com/brae/greeter/internal/adapters"`) {
		t.Errorf("main.go missing module-qualified import:\n%s", main)
	}
	if !strings.Contains(string(main), `toolName    = "greeter"`) {
		t.Errorf("main.go missing toolName constant:\n%s", main)
	}
}

func TestRenderInto_InstallShIsExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := RenderInto(dir, Params{Name: "x", Module: "m"}); err != nil {
		t.Fatalf("RenderInto: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "install.sh"))
	if err != nil {
		t.Fatalf("stat install.sh: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("install.sh not executable: mode=%v", info.Mode())
	}
}

func TestRenderInto_RefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "squatter"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := RenderInto(dir, Params{Name: "x", Module: "m"})
	if err == nil {
		t.Fatal("expected error on non-empty dir, got nil")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("error wording: %v", err)
	}
}

func TestRenderInto_CreatesMissingTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "new", "nested", "tool")
	if err := RenderInto(target, Params{Name: "x", Module: "m"}); err != nil {
		t.Fatalf("RenderInto: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		t.Fatalf("go.mod missing: %v", err)
	}
}

func TestRenderInto_RejectsInvalidParams(t *testing.T) {
	err := RenderInto(t.TempDir(), Params{Name: "BAD", Module: "m"})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRemapCmdPath(t *testing.T) {
	cases := []struct {
		in, name, want string
	}{
		{"cmd/app", "mytool", filepath.FromSlash("cmd/mytool")},
		{"cmd/app/main.go", "mytool", filepath.FromSlash("cmd/mytool/main.go")},
		{"internal/app/app.go", "mytool", "internal/app/app.go"},
		{"README.md", "mytool", "README.md"},
	}
	for _, c := range cases {
		got := remapCmdPath(c.in, c.name)
		if got != c.want {
			t.Errorf("remapCmdPath(%q, %q) = %q, want %q", c.in, c.name, got, c.want)
		}
	}
}
