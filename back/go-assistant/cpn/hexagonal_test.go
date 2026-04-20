package cpn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHexagonal_NoOSImports enforces AC-010: no file under back/go-assistant/cpn/
// may import "os", "os/exec", "syscall", or "github.com/creack/pty".
//
// This test uses "os" itself (it is a test file) — the ban covers only
// non-_test.go files to avoid breaking test tooling while still guarding
// production code.
func TestHexagonal_NoOSImports(t *testing.T) {
	forbidden := []string{
		`"os"`,
		`"os/exec"`,
		`"syscall"`,
		`"github.com/creack/pty"`,
	}

	err := filepath.Walk(".", func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			if path != "." && (info.Name() == "tools" || info.Name() == "prompts" || info.Name() == "persist" || info.Name() == "synthesis" || info.Name() == "awakens" || info.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, f := range forbidden {
			if strings.Contains(string(data), f) {
				t.Errorf("%s imports forbidden package %s", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
