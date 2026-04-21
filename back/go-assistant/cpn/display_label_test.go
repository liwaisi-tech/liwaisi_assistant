package cpn

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveDisplayLabel_Catalog(t *testing.T) {
	cases := []struct {
		name     string
		kind     NodeKind
		role     string
		meta     TransitionMeta
		wantNil  bool
		wantVerb string
		wantDet  string
	}{
		{name: "llm planner", kind: NodeKindLLM, role: "planner", wantVerb: "Thinking"},
		{name: "llm reasoner", kind: NodeKindLLM, role: "reasoner", wantVerb: "Thinking"},
		{name: "llm summarizer", kind: NodeKindLLM, role: "summarizer", wantVerb: "Summarizing"},
		{name: "llm classifier", kind: NodeKindLLM, role: "classifier", wantVerb: "Deciding"},
		{name: "llm router", kind: NodeKindLLM, role: "router", wantVerb: "Deciding"},
		{name: "llm unknown role falls back to Thinking", kind: NodeKindLLM, role: "weird", wantVerb: "Thinking"},
		{name: "llm empty role falls back to Thinking", kind: NodeKindLLM, role: "", wantVerb: "Thinking"},

		{name: "tool fs_read with path", kind: NodeKindTool, meta: TransitionMeta{ToolName: "fs_read", ToolDetail: "/home/x/file.go"}, wantVerb: "Reading", wantDet: "/home/x/file.go"},
		{name: "tool fs_write with path", kind: NodeKindTool, meta: TransitionMeta{ToolName: "fs_write", ToolDetail: "out.txt"}, wantVerb: "Writing", wantDet: "out.txt"},
		{name: "tool web_search with query", kind: NodeKindTool, meta: TransitionMeta{ToolName: "web_search", ToolDetail: "CPN event semantics"}, wantVerb: "Searching", wantDet: "CPN event semantics"},
		{name: "tool unknown name uses Calling tool with name as detail", kind: NodeKindTool, meta: TransitionMeta{ToolName: "some_unregistered_mcp_tool"}, wantVerb: "Calling tool", wantDet: "some_unregistered_mcp_tool"},
		{name: "tool empty name uses Calling tool with no detail", kind: NodeKindTool, meta: TransitionMeta{}, wantVerb: "Calling tool"},

		{name: "validate", kind: NodeKindValidate, wantVerb: "Validating"},

		{name: "subnet with child role", kind: NodeKindSubNet, meta: TransitionMeta{SubNetRole: "researcher"}, wantVerb: "Delegating", wantDet: "researcher"},
		{name: "subnet no child role", kind: NodeKindSubNet, wantVerb: "Delegating"},

		{name: "hitl", kind: NodeKindHITL, wantVerb: "Waiting for you"},

		{name: "observer is silent", kind: NodeKindObserver, wantNil: true},

		{name: "unknown kind falls back to Working", kind: NodeKind("unknown_future_kind"), wantVerb: "Working"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveDisplayLabel(tc.kind, tc.role, tc.meta)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil DisplayLabel, got nil")
			}
			if got.Verb != tc.wantVerb {
				t.Errorf("verb: got %q, want %q", got.Verb, tc.wantVerb)
			}
			if got.Detail != tc.wantDet {
				t.Errorf("detail: got %q, want %q", got.Detail, tc.wantDet)
			}
		})
	}
}

func TestResolveDisplayLabel_DetailTruncatedTo40Chars(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := ResolveDisplayLabel(NodeKindTool, "", TransitionMeta{ToolName: "fs_read", ToolDetail: long})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if n := utf8.RuneCountInString(got.Detail); n > 40 {
		t.Errorf("detail rune count %d exceeds 40-char ceiling: %q", n, got.Detail)
	}
	if !strings.HasSuffix(got.Detail, "…") {
		t.Errorf("truncated detail should end with ellipsis, got %q", got.Detail)
	}
}

func TestResolveDisplayLabel_DetailExactly40CharsNotTruncated(t *testing.T) {
	exact := strings.Repeat("b", 40)
	got := ResolveDisplayLabel(NodeKindTool, "", TransitionMeta{ToolName: "fs_read", ToolDetail: exact})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.Detail != exact {
		t.Errorf("40-char detail should pass through unchanged, got %q (len=%d)", got.Detail, len(got.Detail))
	}
}

func TestResolveDisplayLabel_PureNoSideEffects(t *testing.T) {
	// Same inputs MUST yield equal outputs across repeated calls (REQ-004 purity).
	meta := TransitionMeta{ToolName: "fs_read", ToolDetail: "x.txt"}
	a := ResolveDisplayLabel(NodeKindTool, "", meta)
	b := ResolveDisplayLabel(NodeKindTool, "", meta)
	if a == nil || b == nil {
		t.Fatal("expected non-nil from both calls")
	}
	if *a != *b {
		t.Errorf("resolver is not pure: %+v vs %+v", a, b)
	}
}

func TestDisplayLabel_JSONOmitsEmptyDetail(t *testing.T) {
	dl := DisplayLabel{Verb: "Thinking"}
	b, err := json.Marshal(dl)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if got != `{"verb":"Thinking"}` {
		t.Errorf("expected detail to be omitted, got %s", got)
	}
}

func TestDisplayLabel_JSONIncludesDetailWhenSet(t *testing.T) {
	dl := DisplayLabel{Verb: "Calling tool", Detail: "web_search"}
	b, err := json.Marshal(dl)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if got != `{"verb":"Calling tool","detail":"web_search"}` {
		t.Errorf("unexpected JSON: %s", got)
	}
}

func TestTransition_SilentFieldDefaultsFalse(t *testing.T) {
	tr := NewTransition("t-x", NodeKindLLM, []string{"in"}, []string{"out"})
	if tr.Silent {
		t.Error("Silent should default to false for non-observer transitions")
	}
}

func TestTransition_ObserverDefaultsSilent(t *testing.T) {
	tr := NewTransition("t-obs", NodeKindObserver, []string{"in"}, []string{"out"})
	if !tr.Silent {
		t.Error("NewTransition(NodeKindObserver, ...) should set Silent=true (REQ-007)")
	}
}
