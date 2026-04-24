package gate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubScanner struct {
	err   error
	calls int
}

func (s *stubScanner) ScanArtifact(_ string, _ []byte) error {
	s.calls++
	return s.err
}

func TestPolicyHostGate_CheckArtifact_NoScanner(t *testing.T) {
	g := &PolicyHostGate{}
	if err := g.CheckArtifact(context.Background(), "review-code", []byte(`{}`)); err != nil {
		t.Errorf("nil scanner should be a no-op, got %v", err)
	}
}

func TestPolicyHostGate_CheckArtifact_AcceptsAndForwards(t *testing.T) {
	s := &stubScanner{}
	g := &PolicyHostGate{ArtifactScanner: s}
	if err := g.CheckArtifact(context.Background(), "review-code", []byte(`{"role":"x"}`)); err != nil {
		t.Errorf("clean artifact should pass, got %v", err)
	}
	if s.calls != 1 {
		t.Errorf("scanner not called: %d", s.calls)
	}
}

func TestPolicyHostGate_CheckArtifact_PropagatesDenial(t *testing.T) {
	s := &stubScanner{err: errors.New("forbidden pattern")}
	g := &PolicyHostGate{ArtifactScanner: s}
	err := g.CheckArtifact(context.Background(), "review-code", []byte(`os.Setenv`))
	if err == nil {
		t.Fatal("expected denial to propagate")
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error should be the scanner's, got %v", err)
	}
}
