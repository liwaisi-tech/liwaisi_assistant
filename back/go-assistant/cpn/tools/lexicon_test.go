package tools

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Helpers ───────────────────────────────────────────────────────────────

// captureSlog swaps the default slog logger for one writing to buf, and
// returns a teardown that restores the prior default.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

func setEnv(t *testing.T, key, val string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// ── Tests ─────────────────────────────────────────────────────────────────

// AC-007: env var unset → embedded lexicon loads and is non-empty.
func TestLoadLexicon_Embedded(t *testing.T) {
	unsetEnv(t, envLexiconPath)
	buf := captureSlog(t)

	lex, err := LoadLexicon(context.Background())
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	if lex == nil {
		t.Fatal("lexicon is nil")
	}
	if !lex.IsKnown("tools") {
		t.Errorf("expected IsKnown(\"tools\") = true")
	}
	if all := lex.All(); len(all) == 0 {
		t.Errorf("expected non-empty lexicon")
	}
	if !strings.Contains(buf.String(), "lexicon.source=embedded") {
		t.Errorf("expected slog to log lexicon.source=embedded; got %q", buf.String())
	}
}

// AC-012: valid override file → loader uses it and logs lexicon.source=file.
func TestLoadLexicon_OverrideHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lex.yaml")
	yaml := `version: 1
entries:
  - tag: custom
    kind: domain
    description: A domain-specific tag for testing.
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setEnv(t, envLexiconPath, path)
	buf := captureSlog(t)

	lex, err := LoadLexicon(context.Background())
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	if !lex.IsKnown("custom") {
		t.Errorf("expected IsKnown(\"custom\") = true")
	}
	if lex.IsKnown("tools") {
		t.Errorf("override should NOT contain embedded 'tools' tag")
	}
	out := buf.String()
	if !strings.Contains(out, "lexicon.source=file") {
		t.Errorf("expected lexicon.source=file in slog; got %q", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("expected path %q in slog output; got %q", path, out)
	}
}

// AC-013: env var set to missing file → fail-fast, error names the env var.
func TestLoadLexicon_OverrideMissingFile(t *testing.T) {
	setEnv(t, envLexiconPath, "/definitely/missing/lexicon.yaml")
	_, err := LoadLexicon(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), envLexiconPath) {
		t.Errorf("error must mention %s; got %q", envLexiconPath, err.Error())
	}
}

// AC-008: YAML > 64 KiB → ErrLexiconTooLarge.
func TestLoadLexicon_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.yaml")
	// Valid prefix then pad with a large comment to exceed 64 KiB.
	var b bytes.Buffer
	b.WriteString("version: 1\nentries:\n  - tag: tools\n    kind: kind\n    description: seed\n# ")
	for b.Len() <= maxLexiconBytes {
		b.WriteString("padding padding padding padding padding padding padding ")
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setEnv(t, envLexiconPath, path)

	_, err := LoadLexicon(context.Background())
	if err == nil {
		t.Fatal("expected error for oversized lexicon")
	}
	if !errors.Is(err, ErrLexiconTooLarge) {
		t.Errorf("expected ErrLexiconTooLarge, got %v", err)
	}
}

// Validation: over 256 entries rejected.
func TestLoadLexicon_TooManyEntries(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("version: 1\nentries:\n")
	for i := 0; i < maxLexiconEntries+1; i++ {
		// Keep the payload under 64 KiB by using minimal descriptions.
		b.WriteString("  - tag: t")
		b.WriteString(pad(i))
		b.WriteString("\n    kind: kind\n    description: x\n")
	}
	if b.Len() > maxLexiconBytes {
		t.Skipf("generated fixture too large (%d bytes)", b.Len())
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setEnv(t, envLexiconPath, path)

	_, err := LoadLexicon(context.Background())
	if err == nil {
		t.Fatal("expected error for > 256 entries")
	}
	if !errors.Is(err, ErrLexiconTooManyEntries) {
		t.Errorf("expected ErrLexiconTooManyEntries, got %v", err)
	}
}

// Validation: unknown kind string rejected.
func TestLoadLexicon_InvalidKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	yaml := `version: 1
entries:
  - tag: weird
    kind: behaviour
    description: invalid kind value
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setEnv(t, envLexiconPath, path)
	_, err := LoadLexicon(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
	if !errors.Is(err, ErrLexiconInvalidEntry) {
		t.Errorf("expected ErrLexiconInvalidEntry, got %v", err)
	}
}

// Validation: non-normalised tag rejected.
func TestLoadLexicon_NonNormalisedTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	yaml := `version: 1
entries:
  - tag: "#PDF"
    kind: domain
    description: hash-prefixed upper-case
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setEnv(t, envLexiconPath, path)
	_, err := LoadLexicon(context.Background())
	if err == nil {
		t.Fatal("expected error for non-normalised tag")
	}
	if !errors.Is(err, ErrLexiconInvalidEntry) {
		t.Errorf("expected ErrLexiconInvalidEntry, got %v", err)
	}
}

// pad turns n into a hyphen-free suffix usable as tag characters a..z0..9.
func pad(n int) string {
	// tag must match [a-z][a-z0-9-]{0,31}; we already emitted a leading "t".
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%36]}, out...)
		n /= 36
	}
	return string(out)
}
