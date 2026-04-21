package fanout

import "testing"

func TestSlugify_KnownInputs(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"cmd-sh":         "cmd-sh",
		"Cmd-SH":         "cmd-sh",
		"command -v sh":  "command-v-sh",
		"foo__bar!!baz":  "foo-bar-baz",
		"  spaces  ":     "spaces",
		"123numeric":     "123numeric",
		"Python 3.11.6":  "python-3-11-6",
		"UPPER_Case-ID":  "upper-case-id",
		"--leading-dash": "leading-dash",
	}
	for input, want := range cases {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got := slugify(input)
			if got != want {
				t.Fatalf("slugify(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestSlugify_Determinism(t *testing.T) {
	t.Parallel()
	inputs := []string{"command -v sh", "cmd-git", "!@#$%^&*()", ""}
	for _, in := range inputs {
		a := slugify(in)
		b := slugify(in)
		if a != b {
			t.Fatalf("slugify(%q) non-deterministic: %q vs %q", in, a, b)
		}
	}
}

func TestSlugify_EmptyFallback(t *testing.T) {
	t.Parallel()
	// A string that maps entirely to dashes must produce a stable
	// non-empty slug via the SHA1 fallback.
	got := slugify("!@#$")
	if len(got) == 0 {
		t.Fatalf("expected non-empty slug fallback, got empty")
	}
	again := slugify("!@#$")
	if got != again {
		t.Fatalf("fallback non-deterministic: %q vs %q", got, again)
	}
}

func TestSlugify_LongInputCapped(t *testing.T) {
	t.Parallel()
	long := ""
	for range 100 {
		long += "abcdef"
	}
	got := slugify(long)
	if len(got) > 48 {
		t.Fatalf("slug length %d exceeds cap 48: %q", len(got), got)
	}
}
