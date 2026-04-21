package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// envLexiconPath is the operator-controlled override path for the lexicon
// YAML (spec-architecture-brae-toolbox-taxonomy.md REQ-LEX-001, PAT-003).
const envLexiconPath = "BRAE_LEXICON_PATH"

// maxLexiconBytes caps the YAML payload (SEC-004).
const maxLexiconBytes = 64 * 1024

// maxLexiconEntries caps the number of distinct tags (SEC-004, CON-003).
const maxLexiconEntries = 256

// kindValueKind / kindValueDomain are the only admissible "kind" values.
const (
	kindValueKind   = "kind"
	kindValueDomain = "domain"
)

//go:embed lexicon.yaml
var embeddedLexiconYAML []byte

// ── Named errors ──────────────────────────────────────────────────────────

// ErrLexiconTooLarge signals a YAML payload above maxLexiconBytes (SEC-004).
var ErrLexiconTooLarge = errors.New("tools: lexicon yaml exceeds 64 KiB cap")

// ErrLexiconTooManyEntries signals the lexicon contains more than the
// maxLexiconEntries cap (SEC-004, CON-003).
var ErrLexiconTooManyEntries = errors.New("tools: lexicon exceeds 256-entry cap")

// ErrLexiconInvalidEntry signals a malformed entry in the YAML source
// (unknown kind, invalid tag token, duplicate tag, or empty required field).
var ErrLexiconInvalidEntry = errors.New("tools: invalid lexicon entry")

// ── YAML surface ──────────────────────────────────────────────────────────

type lexiconFile struct {
	Version int                `yaml:"version"`
	Entries []cpn.LexiconEntry `yaml:"entries"`
}

// ── Concrete Lexicon implementation ───────────────────────────────────────

// lexicon implements cpn.Lexicon. Entries preserve YAML insertion order for
// All(); the byTag map provides O(1) lookup for the port methods.
type lexicon struct {
	entries []cpn.LexiconEntry
	byTag   map[string]cpn.LexiconEntry
}

// IsKnown reports whether tag is present. Tag MUST be pre-normalised.
func (l *lexicon) IsKnown(tag string) bool {
	if l == nil {
		return false
	}
	_, ok := l.byTag[tag]
	return ok
}

// Kind returns the classification of tag.
func (l *lexicon) Kind(tag string) (string, bool) {
	if l == nil {
		return "", false
	}
	e, ok := l.byTag[tag]
	if !ok {
		return "", false
	}
	return e.Kind, true
}

// Describe returns the one-line description of tag.
func (l *lexicon) Describe(tag string) (string, bool) {
	if l == nil {
		return "", false
	}
	e, ok := l.byTag[tag]
	if !ok {
		return "", false
	}
	return e.Description, true
}

// All returns a snapshot of every entry in stable YAML order.
func (l *lexicon) All() []cpn.LexiconEntry {
	if l == nil {
		return nil
	}
	out := make([]cpn.LexiconEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// ── Loader ────────────────────────────────────────────────────────────────

// LoadLexicon is the top-level entrypoint wired into server bootstrap
// (REQ-006, REQ-LEX-001). Source precedence:
//
//  1. BRAE_LEXICON_PATH env var set and non-empty — load from file, fail fast
//     on any error (including unreadable, oversized, or malformed). No silent
//     fallback to embedded.
//  2. Otherwise — load the //go:embed default shipped with the binary.
//
// On success the loader emits a structured slog.Info line identifying the
// source (`lexicon.source=embedded` or `lexicon.source=file path=…`).
func LoadLexicon(ctx context.Context) (cpn.Lexicon, error) {
	if path := os.Getenv(envLexiconPath); path != "" {
		return loadFromPath(ctx, path)
	}
	return loadEmbedded(ctx)
}

func loadEmbedded(_ context.Context) (cpn.Lexicon, error) {
	lex, err := parseLexiconBytes(embeddedLexiconYAML)
	if err != nil {
		return nil, fmt.Errorf("tools: load embedded lexicon: %w", err)
	}
	slog.Info("lexicon loaded", "lexicon.source", "embedded", "entries", len(lex.entries))
	return lex, nil
}

func loadFromPath(_ context.Context, path string) (cpn.Lexicon, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tools: load lexicon from %s=%q: %w", envLexiconPath, path, err)
	}
	defer f.Close()

	// Read with a hard cap + 1 byte so we can detect overflow without
	// slurping arbitrary amounts of memory (SEC-004).
	limited := io.LimitReader(f, maxLexiconBytes+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("tools: load lexicon from %s=%q: %w", envLexiconPath, path, err)
	}
	if len(buf) > maxLexiconBytes {
		return nil, fmt.Errorf("tools: load lexicon from %s=%q: %w", envLexiconPath, path, ErrLexiconTooLarge)
	}

	lex, err := parseLexiconBytes(buf)
	if err != nil {
		return nil, fmt.Errorf("tools: load lexicon from %s=%q: %w", envLexiconPath, path, err)
	}
	slog.Info("lexicon loaded", "lexicon.source", "file", "path", path, "entries", len(lex.entries))
	return lex, nil
}

func parseLexiconBytes(buf []byte) (*lexicon, error) {
	if len(buf) > maxLexiconBytes {
		return nil, ErrLexiconTooLarge
	}
	var file lexiconFile
	dec := yaml.NewDecoder(bytesReader(buf))
	dec.KnownFields(false)
	if err := dec.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: empty yaml", ErrLexiconInvalidEntry)
		}
		return nil, fmt.Errorf("%w: %v", ErrLexiconInvalidEntry, err)
	}
	if len(file.Entries) > maxLexiconEntries {
		return nil, ErrLexiconTooManyEntries
	}

	byTag := make(map[string]cpn.LexiconEntry, len(file.Entries))
	out := make([]cpn.LexiconEntry, 0, len(file.Entries))
	for i, e := range file.Entries {
		if e.Tag == "" {
			return nil, fmt.Errorf("%w: entry %d has empty tag", ErrLexiconInvalidEntry, i)
		}
		norm, ok := NormalizeHashtag(e.Tag)
		if !ok || norm != e.Tag {
			return nil, fmt.Errorf("%w: entry %d tag %q is not pre-normalised", ErrLexiconInvalidEntry, i, e.Tag)
		}
		if e.Kind != kindValueKind && e.Kind != kindValueDomain {
			return nil, fmt.Errorf("%w: entry %d tag %q has invalid kind %q (want %q or %q)",
				ErrLexiconInvalidEntry, i, e.Tag, e.Kind, kindValueKind, kindValueDomain)
		}
		if _, dup := byTag[e.Tag]; dup {
			return nil, fmt.Errorf("%w: duplicate tag %q", ErrLexiconInvalidEntry, e.Tag)
		}
		entry := cpn.LexiconEntry{Tag: e.Tag, Kind: e.Kind, Description: e.Description}
		byTag[e.Tag] = entry
		out = append(out, entry)
	}
	return &lexicon{entries: out, byTag: byTag}, nil
}

// bytesReader wraps a byte slice as an io.Reader without pulling in bytes.
// (The yaml decoder asks for an io.Reader and the stdlib has bytes.NewReader
// — we use it through this shim for clarity.)
func bytesReader(b []byte) io.Reader { return &byteSliceReader{b: b} }

type byteSliceReader struct {
	b   []byte
	pos int
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}
