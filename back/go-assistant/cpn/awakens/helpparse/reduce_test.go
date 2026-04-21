package helpparse

import "testing"

func TestReduceHelpResults_Mixed(t *testing.T) {
	t.Parallel()
	in := []HelpResult{
		{Binary: "git", Schema: HelpSchema{Binary: "git"}},
		{Binary: "fakenohelp", Err: "no help output", Stage: "invoke"},
		{Binary: "rg", Schema: HelpSchema{Binary: "rg"}},
	}
	out := ReduceHelpResults(in)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].Binary != "git" || out[1].Binary != "rg" {
		t.Fatalf("wrong order: %+v", out)
	}
}

func TestReduceHelpResults_AllFailed(t *testing.T) {
	t.Parallel()
	in := []HelpResult{
		{Binary: "a", Err: "oops"},
		{Binary: "b", Err: "oops"},
	}
	if got := ReduceHelpResults(in); len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}

func TestReduceHelpFailures(t *testing.T) {
	t.Parallel()
	in := []HelpResult{
		{Binary: "git", Schema: HelpSchema{Binary: "git"}},
		{Binary: "fakenohelp", Err: "no help output", Stage: "invoke"},
	}
	f := ReduceHelpFailures(in)
	if len(f) != 1 || f[0].Binary != "fakenohelp" {
		t.Fatalf("got %+v", f)
	}
}

func TestReduceHelpResults_DropsEmptyBinary(t *testing.T) {
	t.Parallel()
	in := []HelpResult{
		{Binary: "git", Schema: HelpSchema{Binary: ""}},
	}
	if got := ReduceHelpResults(in); len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}
