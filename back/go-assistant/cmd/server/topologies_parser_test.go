package main

import (
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestExtractJSONObject covers REQ-PAR-001: the parser must tolerate
// reasoning-mode preambles (<think>…</think>), fenced code blocks, prose
// prefixes, and must scan forward when the first balanced object does not
// decode into a useful questionnaireSpec.
//
// This test is authored BEFORE the implementation so the ratchet is
// visible: the pre-fix extractJSONObject fails the <think>, fenced, and
// scan-forward cases.
func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string // expected JSON substring (may be empty when none)
		empty bool   // want == "" is ambiguous; use this explicit flag
	}{
		{
			name:  "empty string returns empty",
			in:    "",
			want:  "",
			empty: true,
		},
		{
			name:  "plain JSON is returned verbatim",
			in:    `{"restated_goal":"x","questions":[]}`,
			want:  `{"restated_goal":"x","questions":[]}`,
			empty: false,
		},
		{
			name:  "prose preamble before JSON is skipped",
			in:    "Claro, aqui tienes:\n" + `{"restated_goal":"x","questions":[]}`,
			want:  `{"restated_goal":"x","questions":[]}`,
			empty: false,
		},
		{
			name:  "think block is stripped",
			in:    "<think>Let me reason about {this is not json} carefully</think>\n" + `{"restated_goal":"x","questions":[]}`,
			want:  `{"restated_goal":"x","questions":[]}`,
			empty: false,
		},
		{
			name:  "think block is stripped (mixed case, multiline)",
			in:    "<THINK>multiline\nreasoning\n{garbage: true}\n</Think>\n" + `{"restated_goal":"y","questions":[]}`,
			want:  `{"restated_goal":"y","questions":[]}`,
			empty: false,
		},
		{
			name:  "fenced JSON block is stripped",
			in:    "```json\n" + `{"restated_goal":"x","questions":[]}` + "\n```",
			want:  `{"restated_goal":"x","questions":[]}`,
			empty: false,
		},
		{
			name:  "think AND fenced JSON together (Gemma 4 31B shape)",
			in:    "<think>\nThe user asked for X.\n{these are notes}\n</think>\n\n```json\n" + `{"restated_goal":"pitch","assumptions":["a"],"questions":[]}` + "\n```",
			want:  `{"restated_goal":"pitch","assumptions":["a"],"questions":[]}`,
			empty: false,
		},
		{
			name:  "balanced but useless leading object; scans forward to real one",
			in:    `{"notes":"ignore me"} and then {"restated_goal":"found","questions":[]}`,
			want:  `{"restated_goal":"found","questions":[]}`,
			empty: false,
		},
		{
			name:  "no brace at all returns empty",
			in:    "just prose, no json here",
			want:  "",
			empty: true,
		},
		{
			name:  "unterminated object returns empty",
			in:    `{"restated_goal":"x","questions":[`,
			want:  "",
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONObject(tt.in)
			if tt.empty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestFirstQuestionnaireFromTokens_Gemma covers AC-005: the extractor +
// json.Unmarshal pipeline handles the full Gemma reasoning+fence shape.
func TestFirstQuestionnaireFromTokens_Gemma(t *testing.T) {
	raw := "<think>Let me reason...</think>\n```json\n" +
		`{"restated_goal":"x","questions":[]}` +
		"\n```"
	tokens := []cpn.Token{{Payload: raw}}
	got, err := firstQuestionnaireFromTokens(tokens)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RestatedGoal != "x" {
		t.Errorf("restated_goal = %q, want %q", got.RestatedGoal, "x")
	}
}

// TestBuildClarifyA2UIPayload_ErrorOnEmpty covers REQ-PAR-002 / AC-006:
// empty LLM output MUST produce (nil, err), never (nil, nil), and the
// error wraps the first bytes of the raw payload for debugging.
func TestBuildClarifyA2UIPayload_ErrorOnEmpty(t *testing.T) {
	tokens := []cpn.Token{{Payload: ""}}
	got, err := buildClarifyA2UIPayload(tokens)
	if err == nil {
		t.Fatalf("expected error, got payload=%v", got)
	}
	if got != nil {
		t.Errorf("expected nil payload on error, got %v", got)
	}
	if !strings.Contains(err.Error(), "empty LLM output") {
		t.Errorf("error should mention empty LLM output; got: %v", err)
	}
}

// TestBuildClarifyA2UIPayload_ErrorIncludesRawPrefix covers REQ-PAR-002:
// the error wraps the first 512 bytes of the raw response so the operator
// can see WHAT the model emitted when debugging from logs alone.
func TestBuildClarifyA2UIPayload_ErrorIncludesRawPrefix(t *testing.T) {
	// 800 bytes of garbage — must be truncated to 512 in the error.
	raw := strings.Repeat("X", 800) + "tail-marker"
	tokens := []cpn.Token{{Payload: raw}}
	_, err := buildClarifyA2UIPayload(tokens)
	if err == nil {
		t.Fatal("expected error on undecodable garbage")
	}
	if strings.Contains(err.Error(), "tail-marker") {
		t.Errorf("error should truncate raw payload to 512 bytes; got full payload")
	}
	if !strings.Contains(err.Error(), strings.Repeat("X", 64)) {
		t.Errorf("error should carry a prefix of the raw payload for debugging; got: %v", err)
	}
}

// TestBuildClarifyA2UIPayload_HappyPath exercises the happy branch: a
// well-formed questionnaire payload must produce an A2UI "questionnaire"
// component with one "choice" child per question and the restated goal /
// assumptions echoed on the outer props.
func TestBuildClarifyA2UIPayload_HappyPath(t *testing.T) {
	raw := `{
	  "restated_goal": "pitch",
	  "assumptions": ["a"],
	  "questions": [
	    {"id":"q1","prompt":"how long?","recommended":"opt-a",
	     "quote_from_user":"mañana","why_it_matters":"changes scope",
	     "options":[{"id":"opt-a","label":"20m"},{"id":"opt-b","label":"60m"}]}
	  ]
	}`
	tokens := []cpn.Token{{Payload: raw, SessionID: "sess-happy"}}

	payload, err := buildClarifyA2UIPayload(tokens)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not a map: %T", payload)
	}
	components, _ := m["components"].([]map[string]any)
	if len(components) != 1 {
		t.Fatalf("components len = %d, want 1", len(components))
	}
	props, _ := components[0]["props"].(map[string]any)
	if props["restatedGoal"] != "pitch" {
		t.Errorf("restatedGoal = %v, want pitch", props["restatedGoal"])
	}
	children, _ := components[0]["children"].([]map[string]any)
	if len(children) != 1 {
		t.Fatalf("children len = %d, want 1", len(children))
	}
	childProps, _ := children[0]["props"].(map[string]any)
	if childProps["id"] != "q1" {
		t.Errorf("child id = %v, want q1", childProps["id"])
	}
}

// TestBuildClarifyA2UIPayload_NeverReturnsNilNil covers REQ-PAR-002: if
// there is nothing decodable the function returns (nil, err), never a
// silently-successful (nil, nil) that would leave the caller guessing.
func TestBuildClarifyA2UIPayload_NeverReturnsNilNil(t *testing.T) {
	cases := []struct {
		name   string
		tokens []cpn.Token
	}{
		{"empty string", []cpn.Token{{Payload: ""}}},
		{"prose no json", []cpn.Token{{Payload: "just words"}}},
		{"unterminated json", []cpn.Token{{Payload: `{"questions":[`}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildClarifyA2UIPayload(tc.tokens)
			if got == nil && err == nil {
				t.Fatal("forbidden (nil, nil) return")
			}
		})
	}
}
