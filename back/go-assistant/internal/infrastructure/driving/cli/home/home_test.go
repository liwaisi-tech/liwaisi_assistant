package home

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		envHome    string
		wantSuffix string
		wantErr    bool
	}{
		{
			name:       "explicit path",
			path:       "/tmp/custom-liwaisi",
			wantSuffix: "/tmp/custom-liwaisi",
		},
		{
			name:       "from BRAE_HOME env",
			path:       "",
			envHome:    "/tmp/env-liwaisi",
			wantSuffix: "/tmp/env-liwaisi",
		},
		{
			name:       "falls back to user home",
			path:       "",
			envHome:    "",
			wantSuffix: ".liwaisi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envHome != "" {
				t.Setenv(envLiwaisiHome, tt.envHome)
			} else {
				t.Setenv(envLiwaisiHome, "")
			}

			h, err := New(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got := h.Root(); !containsSuffix(got, tt.wantSuffix) {
				t.Errorf("Root() = %q, want suffix %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestInit(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}

	if err := h.Init(); err != nil {
		t.Fatalf("Init(): %v", err)
	}

	expectedDirs := []Dir{Boot, Config, Workspace, Data, Bin, Tmp}
	for _, d := range expectedDirs {
		path := h.Path(d)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Path(%d) = %q: directory does not exist: %v", d, path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("Path(%d) = %q: expected directory, got file", d, path)
		}
	}

	expectedFiles := []string{
		"boot/init.yaml",
		"boot/profile.yaml",
		"config/liwaisi.yaml",
	}
	for _, rel := range expectedFiles {
		path := filepath.Join(root, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("default config %q does not exist: %v", rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("default config %q: expected file, got directory", rel)
		}
		if info.Size() == 0 {
			t.Errorf("default config %q: file is empty", rel)
		}
	}
}

func TestInit_Idempotent(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}

	if err := h.Init(); err != nil {
		t.Fatalf("first Init(): %v", err)
	}

	configPath := filepath.Join(root, "config/liwaisi.yaml")
	sentinel := []byte("# custom user config\ntheme: light\n")
	if err := os.WriteFile(configPath, sentinel, filePerm); err != nil {
		t.Fatalf("writing sentinel: %v", err)
	}

	if err := h.Init(); err != nil {
		t.Fatalf("second Init(): %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config after second Init: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("Init() overwrote existing config:\ngot:  %q\nwant: %q", got, sentinel)
	}
}

func TestPath(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}

	tests := []struct {
		name string
		dir  Dir
		want string
	}{
		{"boot", Boot, filepath.Join(root, "boot")},
		{"config", Config, filepath.Join(root, "config")},
		{"workspace", Workspace, filepath.Join(root, "workspace")},
		{"data", Data, filepath.Join(root, "data")},
		{"bin", Bin, filepath.Join(root, "bin")},
		{"tmp", Tmp, filepath.Join(root, "tmp")},
		{"unknown falls back to root", Dir(99), root},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.Path(tt.dir); got != tt.want {
				t.Errorf("Path(%d) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestWorkspacePath(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}

	tests := []struct {
		name    string
		project string
		want    string
	}{
		{"simple name", "my-project", filepath.Join(root, "workspace", "my-project")},
		{"nested name", "org/repo", filepath.Join(root, "workspace", "org/repo")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.WorkspacePath(tt.project); got != tt.want {
				t.Errorf("WorkspacePath(%q) = %q, want %q", tt.project, got, tt.want)
			}
		})
	}
}

func TestCleanTmp(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	if err := h.Init(); err != nil {
		t.Fatalf("Init(): %v", err)
	}

	tmpDir := h.Path(Tmp)

	oldFile := filepath.Join(tmpDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("old"), filePerm); err != nil {
		t.Fatalf("writing old file: %v", err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, past, past); err != nil {
		t.Fatalf("setting old file time: %v", err)
	}

	newFile := filepath.Join(tmpDir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new"), filePerm); err != nil {
		t.Fatalf("writing new file: %v", err)
	}

	if err := h.CleanTmp(1 * time.Hour); err != nil {
		t.Fatalf("CleanTmp(): %v", err)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file should have been removed, stat err = %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new file should still exist, stat err = %v", err)
	}
}

func TestCleanTmp_NonExistentDir(t *testing.T) {
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := h.CleanTmp(1 * time.Hour); err != nil {
		t.Errorf("CleanTmp() on non-existent dir should not error, got: %v", err)
	}
}

func containsSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
