package filemanagement

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustNewSandbox(t *testing.T, root string) *Sandbox {
	t.Helper()
	sb, err := NewSandbox(root)
	if err != nil {
		t.Fatalf("NewSandbox(%q): %v", root, err)
	}
	return sb
}

func TestSandbox_Resolve(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "simple file",
			path: "hello.txt",
			want: filepath.Join(root, "hello.txt"),
		},
		{
			name: "nested path",
			path: "a/b/c.txt",
			want: filepath.Join(root, "a", "b", "c.txt"),
		},
		{
			name: "dot resolves to root",
			path: ".",
			want: root,
		},
		{
			name: "empty resolves to root",
			path: "",
			want: root,
		},
		{
			name: "clean dot-slash prefix",
			path: "./a/b.txt",
			want: filepath.Join(root, "a", "b.txt"),
		},
		{
			name: "clean redundant separators",
			path: "a//b///c.txt",
			want: filepath.Join(root, "a", "b", "c.txt"),
		},
		{
			name: "inner traversal that stays inside",
			path: "a/../b.txt",
			want: filepath.Join(root, "b.txt"),
		},
		{
			name:    "traversal escapes root",
			path:    "../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "single traversal escapes root",
			path:    "../outside",
			wantErr: true,
		},
		{
			name:    "absolute path rejected",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path with tilde rejected",
			path:    "/home/user/.liwaisi/workspace/file.txt",
			wantErr: true,
		},
	}

	sb := mustNewSandbox(t, root)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sb.Resolve(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Resolve(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestSandbox_Resolve_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Unix-like OS")
	}

	root := t.TempDir()
	outside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "legit"), 0o750); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "legit", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	sb := mustNewSandbox(t, root)

	_, err := sb.Resolve("legit/escape/secret.txt")
	if err == nil {
		t.Error("Resolve() should reject symlink that escapes sandbox")
	}
}

func TestSandbox_Resolve_LeafSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests require Unix-like OS")
	}

	root := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o640); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "evil")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	sb := mustNewSandbox(t, root)

	_, err := sb.Resolve("evil")
	if err == nil {
		t.Error("Resolve() should reject leaf-level symlink that escapes sandbox")
	}
}

func TestSandbox_Root(t *testing.T) {
	sb := mustNewSandbox(t, "/some/path")
	if got := sb.Root(); got != "/some/path" {
		t.Errorf("Root() = %q, want %q", got, "/some/path")
	}
}

func TestSandbox_IsSensitiveFile(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		blocked bool
	}{
		{"env.yaml blocked", "env.yaml", true},
		{"dotenv blocked", ".env", true},
		{"dotenv.local blocked", ".env.local", true},
		{"dotenv.production blocked", ".env.production", true},
		{"private key blocked", "server.key", true},
		{"pem blocked", "cert.pem", true},
		{"p12 blocked", "keystore.p12", true},
		{"pfx blocked", "cert.pfx", true},
		{"secrets file blocked", "my_secrets.txt", true},
		{"credential file blocked", "db_credential.json", true},
		{"keystore blocked", "app.keystore", true},
		{"jks blocked", "app.jks", true},
		{"id_rsa blocked", "id_rsa", true},
		{"id_ed25519 blocked", "id_ed25519", true},
		{"id_ecdsa blocked", "id_ecdsa", true},
		{"nested env.yaml blocked", "config/env.yaml", true},
		{"nested .env blocked", "backend/.env", true},
		{"production.env blocked", "production.env", true},
		{"staging.env blocked", "staging.env", true},
		{"app.env blocked", "app.env", true},
		{"case insensitive", "ENV.YAML", true},
		{"normal go file allowed", "main.go", false},
		{"normal yaml allowed", "config.yaml", false},
		{"normal json allowed", "package.json", false},
		{"readme allowed", "README.md", false},
		{"go.mod allowed", "go.mod", false},
		{"Makefile allowed", "Makefile", false},
	}

	sb := mustNewSandbox(t, "/some/root")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sb.IsSensitiveFile(tt.path)
			if got != tt.blocked {
				t.Errorf("IsSensitiveFile(%q) = %v, want %v", tt.path, got, tt.blocked)
			}
		})
	}
}

func TestSandbox_WithCustomSensitivePatterns(t *testing.T) {
	sb, err := NewSandbox("/some/root", WithSensitivePatterns([]string{"custom.secret"}))
	if err != nil {
		t.Fatal(err)
	}

	if !sb.IsSensitiveFile("custom.secret") {
		t.Error("custom pattern should be blocked")
	}
	if sb.IsSensitiveFile(".env") {
		t.Error("default pattern should NOT be blocked with custom override")
	}
}

func TestNewSandbox_Validation(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		wantErr string
	}{
		{
			name:    "empty root rejected",
			root:    "",
			wantErr: "sandbox root must be an absolute path",
		},
		{
			name:    "relative root rejected",
			root:    "relative/path",
			wantErr: "sandbox root must be an absolute path",
		},
		{
			name: "absolute root accepted",
			root: "/absolute/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb, err := NewSandbox(tt.root)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				if sb != nil {
					t.Error("sandbox should be nil on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sb == nil {
				t.Fatal("sandbox should not be nil")
			}
		})
	}
}
