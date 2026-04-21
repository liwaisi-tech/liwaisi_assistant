package tools

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

type normalizeCase struct {
	Name string   `json:"name"`
	In   []string `json:"in"`
	Out  []string `json:"out"`
}

type normalizeFixtures struct {
	Cases []normalizeCase `json:"cases"`
}

func loadFixtures(t *testing.T) []normalizeCase {
	t.Helper()
	b, err := os.ReadFile("testdata/normalize_fixtures.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var f normalizeFixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	return f.Cases
}

func TestNormalizeSet_Fixtures(t *testing.T) {
	cases := loadFixtures(t)
	if len(cases) < 20 {
		t.Fatalf("spec §6 requires ≥20 normalization fixtures, got %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := NormalizeSet(tc.In)
			want := tc.Out
			if len(want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("NormalizeSet(%q) = %q, want %q", tc.In, got, want)
			}
		})
	}
}

func TestNormalizeHashtag_SingleToken(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"pdf", "pdf", true},
		{"#pdf", "pdf", true},
		{"  PDF  ", "pdf", true},
		{"web-scrape", "web-scrape", true},
		{"web_scrape", "", false},
		{"", "", false},
		{"pdf!", "", false},
		{"1pdf", "", false},
		{"a23456789012345678901234567890123", "", false}, // 33 chars
	}
	for _, tc := range tests {
		got, ok := NormalizeHashtag(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("NormalizeHashtag(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestValidateExplicit_Valid(t *testing.T) {
	got, err := ValidateExplicit([]string{"tools", "read", "pdf"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"tools", "read", "pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateExplicit_InvalidToken_AC005(t *testing.T) {
	_, err := ValidateExplicit([]string{"Foo!"})
	if !errors.Is(err, ErrInvalidHashtag) {
		t.Fatalf("want ErrInvalidHashtag, got %v", err)
	}
}

func TestValidateExplicit_TooMany_AC006(t *testing.T) {
	tags := make([]string, MaxHashtagsPerTool+1)
	for i := range tags {
		tags[i] = "tag-" + string(rune('a'+i))
	}
	_, err := ValidateExplicit(tags)
	if !errors.Is(err, ErrTooManyHashtags) {
		t.Fatalf("want ErrTooManyHashtags, got %v", err)
	}
}

func TestValidateExplicit_Dedup(t *testing.T) {
	got, err := ValidateExplicit([]string{"pdf", "PDF", "Pdf"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

// AC-011: canonical ordering — mixed case + whitespace + leading hash all
// collapse to a single lower-case token.
func TestNormalizeSet_AC011(t *testing.T) {
	got := NormalizeSet([]string{"#PDF", "pdf", " PDF "})
	want := []string{"pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AC-011: got %q want %q", got, want)
	}
}
