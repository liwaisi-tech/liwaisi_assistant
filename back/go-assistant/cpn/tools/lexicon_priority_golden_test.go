package tools

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type goldenFixture struct {
	Version       int       `json:"version"`
	Formula       string    `json:"formula"`
	Input         []TagStat `json:"input"`
	ExpectedTop32 []string  `json:"expected_top_32"`
}

func loadGoldenFixture(t *testing.T) goldenFixture {
	t.Helper()
	buf, err := os.ReadFile("testdata/lexicon_priority_golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fx goldenFixture
	if err := json.Unmarshal(buf, &fx); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	return fx
}

// TestSelectTopTagsByFreqRecency_Golden (AC-011, REQ-1801, REQ-1802).
func TestSelectTopTagsByFreqRecency_Golden(t *testing.T) {
	t.Parallel()
	fx := loadGoldenFixture(t)
	if len(fx.Input) == 0 || len(fx.ExpectedTop32) != 32 {
		t.Fatalf("fixture malformed: inputs=%d top=%d", len(fx.Input), len(fx.ExpectedTop32))
	}
	got := SelectTopTagsByFreqRecency(fx.Input, 32)
	if !reflect.DeepEqual(got, fx.ExpectedTop32) {
		t.Fatalf("golden mismatch\n got:  %v\n want: %v", got, fx.ExpectedTop32)
	}
}

func TestSelectTopTagsByFreqRecency_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty_input_returns_nil", func(t *testing.T) {
		t.Parallel()
		if got := SelectTopTagsByFreqRecency(nil, 32); got != nil {
			t.Errorf("want nil for nil input, got %v", got)
		}
		if got := SelectTopTagsByFreqRecency([]TagStat{}, 32); got != nil {
			t.Errorf("want nil for empty input, got %v", got)
		}
	})

	t.Run("n_zero_returns_nil", func(t *testing.T) {
		t.Parallel()
		in := []TagStat{{Tag: "a", Freq: 1, Recency: 1}}
		if got := SelectTopTagsByFreqRecency(in, 0); got != nil {
			t.Errorf("want nil for n=0, got %v", got)
		}
	})

	t.Run("n_negative_returns_nil", func(t *testing.T) {
		t.Parallel()
		in := []TagStat{{Tag: "a", Freq: 1, Recency: 1}}
		if got := SelectTopTagsByFreqRecency(in, -5); got != nil {
			t.Errorf("want nil for n<0, got %v", got)
		}
	})

	t.Run("n_exceeds_input_returns_all", func(t *testing.T) {
		t.Parallel()
		in := []TagStat{
			{Tag: "b", Freq: 1, Recency: 0},
			{Tag: "a", Freq: 2, Recency: 0},
			{Tag: "c", Freq: 3, Recency: 0},
		}
		got := SelectTopTagsByFreqRecency(in, 99)
		want := []string{"c", "a", "b"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("ties_break_alphabetically", func(t *testing.T) {
		t.Parallel()
		in := []TagStat{
			{Tag: "zeta", Freq: 10, Recency: 0.5},
			{Tag: "alpha", Freq: 10, Recency: 0.5},
			{Tag: "mike", Freq: 10, Recency: 0.5},
		}
		got := SelectTopTagsByFreqRecency(in, 3)
		want := []string{"alpha", "mike", "zeta"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("does_not_mutate_input", func(t *testing.T) {
		t.Parallel()
		in := []TagStat{
			{Tag: "b", Freq: 1, Recency: 0},
			{Tag: "a", Freq: 9, Recency: 0},
		}
		snapshot := make([]TagStat, len(in))
		copy(snapshot, in)
		_ = SelectTopTagsByFreqRecency(in, 2)
		if !reflect.DeepEqual(in, snapshot) {
			t.Fatalf("input mutated: got %v, want %v", in, snapshot)
		}
	})

	t.Run("deterministic_across_calls", func(t *testing.T) {
		t.Parallel()
		fx := loadGoldenFixture(t)
		a := SelectTopTagsByFreqRecency(fx.Input, 32)
		b := SelectTopTagsByFreqRecency(fx.Input, 32)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("non-deterministic: a=%v b=%v", a, b)
		}
	})
}
