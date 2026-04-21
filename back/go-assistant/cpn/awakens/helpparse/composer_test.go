package helpparse

import (
	"errors"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestCompose_FiveTransitionsPerBinary(t *testing.T) {
	t.Parallel()
	binaries := []HelpInput{
		{Binary: "git", Path: "/usr/bin/git"},
		{Binary: "rg", Path: "/usr/bin/rg"},
	}
	c, err := Compose("sess-x", binaries, Deps{})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	// 5 transitions per binary × 2 binaries = 10.
	if len(c.Transitions) != 10 {
		t.Fatalf("want 10 transitions, got %d", len(c.Transitions))
	}
	// Spot-check each kind exists for git.
	for _, id := range []string{
		TransitionInvokeLongPrefix + "git",
		TransitionInvokeShortPrefix + "git",
		TransitionMergePrefix + "git",
		TransitionLLMParsePrefix + "git",
		TransitionValidatePrefix + "git",
	} {
		if _, ok := c.Transitions[id]; !ok {
			t.Fatalf("missing transition %s", id)
		}
	}
	// Places: trigger + 5 per binary × 2 = 11.
	if len(c.Places) != 11 {
		t.Fatalf("want 11 places, got %d; got=%v", len(c.Places), placeKeys(c))
	}
	// Trigger seeded with 2 tokens per binary.
	trigger := c.Places[PlaceTriggerID]
	if n := trigger.Len(); n != 4 {
		t.Fatalf("want 4 trigger tokens, got %d", n)
	}
}

func TestCompose_EmptyBinaries(t *testing.T) {
	t.Parallel()
	_, err := Compose("sess", nil, Deps{})
	if !errors.Is(err, ErrNoBinaries) {
		t.Fatalf("want ErrNoBinaries, got %v", err)
	}
}

func TestCompose_TooManyBinaries(t *testing.T) {
	t.Parallel()
	in := make([]HelpInput, MaxBinaries+1)
	for i := range in {
		in[i] = HelpInput{Binary: "b" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Path: "/bin/x"}
	}
	_, err := Compose("sess", in, Deps{})
	if !errors.Is(err, ErrTooManyBinaries) {
		t.Fatalf("want ErrTooManyBinaries, got %v", err)
	}
}

func TestCompose_InvalidEntry(t *testing.T) {
	t.Parallel()
	_, err := Compose("sess", []HelpInput{{Binary: "", Path: "/bin/x"}}, Deps{})
	if err == nil {
		t.Fatal("expected error for empty binary")
	}
	_, err = Compose("sess", []HelpInput{{Binary: "git", Path: ""}}, Deps{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestCompose_DeduplicatesByBinary(t *testing.T) {
	t.Parallel()
	in := []HelpInput{
		{Binary: "git", Path: "/usr/bin/git"},
		{Binary: "git", Path: "/usr/local/bin/git"},
	}
	c, err := Compose("sess", in, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Transitions) != 5 {
		t.Fatalf("want 5 transitions (1 binary), got %d", len(c.Transitions))
	}
}

func TestCompose_FlatTopology(t *testing.T) {
	t.Parallel()
	c, err := Compose("sess", []HelpInput{{Binary: "git", Path: "/usr/bin/git"}}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	for _, trn := range c.Transitions {
		if trn.Kind == cpn.NodeKindInstantiate || trn.Kind == cpn.NodeKindSubNet {
			t.Fatalf("non-flat transition %s kind=%s", trn.ID, trn.Kind)
		}
	}
}

func TestResultPlaceIDs_SortedUnique(t *testing.T) {
	t.Parallel()
	ids := ResultPlaceIDs([]HelpInput{
		{Binary: "rg", Path: "/bin/rg"},
		{Binary: "git", Path: "/bin/git"},
		{Binary: "git", Path: "/other/git"},
	})
	want := []string{PlaceHelpResultPrefix + "git", PlaceHelpResultPrefix + "rg"}
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("got %v want %v", ids, want)
	}
}

func placeKeys(c *cpn.CPN) []string {
	out := make([]string, 0, len(c.Places))
	for k := range c.Places {
		out = append(out, k)
	}
	return out
}

// sanity: every result place is present for every binary.
func TestCompose_ResultPlacesExist(t *testing.T) {
	t.Parallel()
	in := []HelpInput{
		{Binary: "git", Path: "/x/git"},
		{Binary: "rg", Path: "/x/rg"},
	}
	c, err := Compose("sess", in, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ResultPlaceIDs(in) {
		if _, ok := c.Places[id]; !ok {
			t.Fatalf("missing result place %s; have %v", id, strings.Join(placeKeys(c), ","))
		}
	}
}
