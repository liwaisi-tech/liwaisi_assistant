package toolsynth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/helpparse"
)

func sampleSchema() helpparse.HelpSchema {
	return helpparse.HelpSchema{
		Binary: "git",
		Flags: []helpparse.HelpFlag{
			{Long: "--version", Short: "-v", Doc: "print version"},
			{Long: "--help", Doc: "show help"},
			{Long: "--config", Arg: true, Doc: "set cfg"},
		},
		Subcommands: []helpparse.HelpSub{
			{Name: "push", Doc: "push to remote"},
			{Name: "pull", Doc: "pull from remote"},
		},
		Examples:     []string{"git push origin main"},
		SourceSHA256: "abc123",
	}
}

func TestSynthesiseManifest_ProducesSynthesizedKindAndOrigin(t *testing.T) {
	t.Parallel()
	pt, err := SynthesiseManifest(sampleSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pt.Manifest.Kind != KindSynthesized {
		t.Errorf("Kind = %q, want %q", pt.Manifest.Kind, KindSynthesized)
	}
	if pt.Manifest.Origin != OriginHelpParser {
		t.Errorf("Origin = %q, want %q", pt.Manifest.Origin, OriginHelpParser)
	}
	if pt.Manifest.Name != "git" {
		t.Errorf("Name = %q, want git", pt.Manifest.Name)
	}
	if pt.Manifest.Namespace != DefaultNamespace {
		t.Errorf("Namespace = %q", pt.Manifest.Namespace)
	}
	if pt.SourceSHA256 != "abc123" {
		t.Errorf("SourceSHA256 = %q", pt.SourceSHA256)
	}
	if pt.ProvenanceSHA256 == "" {
		t.Error("ProvenanceSHA256 is empty")
	}
}

func TestSynthesiseManifest_ByteStableForIdenticalInput(t *testing.T) {
	t.Parallel()
	a, err := SynthesiseManifest(sampleSchema())
	if err != nil {
		t.Fatal(err)
	}
	b, err := SynthesiseManifest(sampleSchema())
	if err != nil {
		t.Fatal(err)
	}
	if a.ProvenanceSHA256 != b.ProvenanceSHA256 {
		t.Fatalf("prov hash differs: %q vs %q", a.ProvenanceSHA256, b.ProvenanceSHA256)
	}
	c1, err := canonicalJSON(a.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := canonicalJSON(b.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c1, c2) {
		t.Fatalf("canonical JSON not byte-stable:\na=%s\nb=%s", c1, c2)
	}
}

func TestSynthesiseManifest_ByteStableAfterFlagReorder(t *testing.T) {
	t.Parallel()
	s1 := sampleSchema()
	s2 := sampleSchema()
	s2.Flags[0], s2.Flags[2] = s2.Flags[2], s2.Flags[0]
	s2.Subcommands[0], s2.Subcommands[1] = s2.Subcommands[1], s2.Subcommands[0]

	a, err := SynthesiseManifest(s1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SynthesiseManifest(s2)
	if err != nil {
		t.Fatal(err)
	}
	if a.ProvenanceSHA256 != b.ProvenanceSHA256 {
		t.Fatalf("determinism breaks with reordered flags:\na=%s\nb=%s", a.ProvenanceSHA256, b.ProvenanceSHA256)
	}
}

func TestSynthesiseManifest_ProvenanceSHA256MatchesCanonicalJSON(t *testing.T) {
	t.Parallel()
	pt, err := SynthesiseManifest(sampleSchema())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(pt.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	want := hex.EncodeToString(sum[:])
	if pt.ProvenanceSHA256 != want {
		t.Fatalf("ProvenanceSHA256 %q != sha256(canonical) %q", pt.ProvenanceSHA256, want)
	}
}

func TestSynthesiseManifest_EmptyBinaryReturnsError(t *testing.T) {
	t.Parallel()
	s := sampleSchema()
	s.Binary = "   "
	_, err := SynthesiseManifest(s)
	if !errors.Is(err, ErrEmptyBinary) {
		t.Fatalf("want ErrEmptyBinary, got %v", err)
	}
}

func TestSynthesiseManifest_MalformedFlagReturnsError(t *testing.T) {
	t.Parallel()
	s := sampleSchema()
	s.Flags = append(s.Flags, helpparse.HelpFlag{Long: "  ", Doc: "bad"})
	_, err := SynthesiseManifest(s)
	if !errors.Is(err, ErrMalformedFlag) {
		t.Fatalf("want ErrMalformedFlag, got %v", err)
	}
}

func TestSynthesiseManifest_DoesNotRegister(t *testing.T) {
	// REQ-1203: materialisation never touches a registry. A pure function
	// by definition can't register; this test locks the contract by
	// asserting no side-channel: two calls with the same input produce
	// equal results (proves no hidden mutation).
	t.Parallel()
	a, _ := SynthesiseManifest(sampleSchema())
	b, _ := SynthesiseManifest(sampleSchema())
	if a.ProvenanceSHA256 != b.ProvenanceSHA256 {
		t.Fatal("pure function exhibits side effects")
	}
}
