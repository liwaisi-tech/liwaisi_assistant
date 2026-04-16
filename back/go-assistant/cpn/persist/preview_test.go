package persist

import "testing"

// Tests for RenderablePreview, the shared sidebar-preview computation used by
// both the Postgres and in-memory session backends. See
// spec-process-bugfix-a2ui-rehydration-completion.md REQ-201..204,
// AC-201..204, INV-302.

func TestRenderablePreview(t *testing.T) {
	const maxLen = 120

	cases := []struct {
		name              string
		contentsNewestTop []string
		want              string
	}{
		{
			name:              "empty_session_returns_empty",
			contentsNewestTop: nil,
			want:              "",
		},
		{
			name:              "plain_text_returned_as_is",
			contentsNewestTop: []string{"Hola. ¿En qué puedo ayudarte hoy?"},
			want:              "Hola. ¿En qué puedo ayudarte hoy?",
		},
		{
			name: "a2ui_with_top_level_title_returns_title",
			contentsNewestTop: []string{
				`$$a2ui:{"components":[{"props":{"title":"Cuéntame más sobre tu feria"},"children":[]}]}`,
			},
			want: "Cuéntame más sobre tu feria",
		},
		{
			name: "a2ui_with_only_child_label_returns_label",
			contentsNewestTop: []string{
				`$$a2ui:{"components":[{"props":{},"children":[{"props":{"label":"¿Qué tipo de artículo vas a mostrar?"}}]}]}`,
			},
			want: "¿Qué tipo de artículo vas a mostrar?",
		},
		{
			name: "a2ui_invalid_json_falls_back_to_next_message",
			contentsNewestTop: []string{
				`$$a2ui:{not-valid-json`,
				"Quiero una feria",
			},
			want: "Quiero una feria",
		},
		{
			name: "a2ui_unparseable_with_no_fallback_returns_empty",
			contentsNewestTop: []string{
				`$$a2ui:{not-valid-json`,
			},
			want: "",
		},
		{
			name: "routing_json_skipped_falls_back_to_next_message",
			contentsNewestTop: []string{
				`{"restated_goal":"Necesitas ayuda","questions":[{"id":"q1"}]}`,
				"Quiero que me ayudes mañana tengo una feria",
			},
			want: "Quiero que me ayudes mañana tengo una feria",
		},
		{
			name: "classifier_json_skipped",
			contentsNewestTop: []string{
				`{"classification":"task","confidence":0.95}`,
				"Hola",
			},
			want: "Hola",
		},
		{
			name: "leading_whitespace_a2ui_recognized",
			contentsNewestTop: []string{
				"\n  $$a2ui:{\"components\":[{\"props\":{\"title\":\"Form\"}}]}",
			},
			want: "Form",
		},
		{
			name: "long_text_truncated_to_120_chars",
			contentsNewestTop: []string{
				stringOfLen('a', 200),
			},
			want: stringOfLen('a', maxLen),
		},
		{
			name: "long_a2ui_title_truncated_to_120_chars",
			contentsNewestTop: []string{
				`$$a2ui:{"components":[{"props":{"title":"` + stringOfLen('b', 200) + `"}}]}`,
			},
			want: stringOfLen('b', maxLen),
		},
		{
			name: "empty_strings_skipped",
			contentsNewestTop: []string{
				"",
				"   ",
				"Hola",
			},
			want: "Hola",
		},
		{
			name:              "marker_substring_only_returns_empty",
			contentsNewestTop: []string{`$$a2ui:`},
			want:              "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderablePreview(tc.contentsNewestTop)
			if got != tc.want {
				t.Errorf("RenderablePreview(%v)\n  got  = %q\n  want = %q", tc.contentsNewestTop, got, tc.want)
			}
			if len(got) > maxLen {
				t.Errorf("preview exceeds maxLen=%d: len=%d", maxLen, len(got))
			}
		})
	}
}

// TestRenderablePreview_NeverContainsMarker is the INV-302 belt-and-braces
// guard: across every fixture and every reasonable A2UI permutation the
// output must not contain the literal $$a2ui: marker. Regression guard
// against future code paths that might accidentally pass-through the prefix.
func TestRenderablePreview_NeverContainsMarker(t *testing.T) {
	inputs := [][]string{
		{`$$a2ui:{"components":[{"props":{"title":"X"}}]}`},
		{`$$a2ui:{not-valid-json`},
		{`$$a2ui:{"components":[]}`},
		{`$$a2ui:`},
		{`{"restated_goal":"…","questions":[]}`, `$$a2ui:{"components":[]}`},
	}
	for i, in := range inputs {
		got := RenderablePreview(in)
		if containsMarker(got) {
			t.Errorf("case %d: preview leaked $$a2ui: marker → %q", i, got)
		}
	}
}

func stringOfLen(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

func containsMarker(s string) bool {
	const marker = "$$a2ui:"
	for i := 0; i+len(marker) <= len(s); i++ {
		if s[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}
