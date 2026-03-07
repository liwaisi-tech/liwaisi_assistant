package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestToolCallFilter_Process(t *testing.T) {
	tests := []struct {
		name         string
		tokens       []string
		wantOutputs  []string
		wantCaptured bool
	}{
		{
			name:         "passthrough without XML tags",
			tokens:       []string{"hello ", "world"},
			wantOutputs:  []string{"hello ", "world"},
			wantCaptured: false,
		},
		{
			name: "complete tool call in single token",
			tokens: []string{
				"before" + toolCallTagOpen + `<invoke name="test"></invoke>` + toolCallTagClose,
			},
			wantOutputs:  []string{"before"},
			wantCaptured: true,
		},
		{
			name: "open tag split across two tokens",
			tokens: []string{
				"prefix" + toolCallTagOpen + `<invoke name="t">`,
				`<parameter name="k">v</parameter></invoke>` + toolCallTagClose,
			},
			wantOutputs:  []string{"prefix", ""},
			wantCaptured: true,
		},
		{
			name: "content before and after tool call block",
			tokens: []string{
				"before" + toolCallTagOpen + `<invoke name="t"></invoke>` + toolCallTagClose + "after",
			},
			wantOutputs:  []string{"beforeafter"},
			wantCaptured: true,
		},
		{
			name:         "empty tokens",
			tokens:       []string{"", "", "hello"},
			wantOutputs:  []string{"", "", "hello"},
			wantCaptured: false,
		},
		{
			name: "tag arrives token by token",
			tokens: []string{
				toolCallTagOpen,
				`<invoke name="read_file">`,
				`<parameter name="path">main.go</parameter>`,
				`</invoke>`,
				toolCallTagClose,
			},
			wantOutputs:  []string{"", "", "", "", ""},
			wantCaptured: true,
		},
		{
			name: "tool call with trailing content after close tag",
			tokens: []string{
				toolCallTagOpen + `<invoke name="t"></invoke>` + toolCallTagClose + " trailing",
			},
			wantOutputs:  []string{"trailing"},
			wantCaptured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newToolCallFilter()
			var outputs []string

			for _, token := range tt.tokens {
				out := f.Process(token)
				outputs = append(outputs, out)
			}

			if len(outputs) != len(tt.wantOutputs) {
				t.Fatalf("got %d outputs, want %d", len(outputs), len(tt.wantOutputs))
			}
			for i, got := range outputs {
				if got != tt.wantOutputs[i] {
					t.Errorf("output[%d] = %q, want %q", i, got, tt.wantOutputs[i])
				}
			}
			if f.HasCaptured() != tt.wantCaptured {
				t.Errorf("HasCaptured() = %v, want %v", f.HasCaptured(), tt.wantCaptured)
			}
		})
	}
}

func TestToolCallFilter_SequentialCaptures(t *testing.T) {
	f := newToolCallFilter()

	token1 := toolCallTagOpen + `<invoke name="first"></invoke>` + toolCallTagClose
	out1 := f.Process(token1)
	if out1 != "" {
		t.Errorf("first Process output = %q, want empty", out1)
	}
	if !f.HasCaptured() {
		t.Fatal("expected captured after first tool call")
	}
	first := f.Captured()
	if !strings.Contains(first, "first") {
		t.Errorf("first captured = %q, want containing 'first'", first)
	}

	f.ClearCaptured()
	if f.HasCaptured() {
		t.Fatal("expected no capture after ClearCaptured")
	}

	token2 := toolCallTagOpen + `<invoke name="second"></invoke>` + toolCallTagClose
	out2 := f.Process(token2)
	if out2 != "" {
		t.Errorf("second Process output = %q, want empty", out2)
	}
	if !f.HasCaptured() {
		t.Fatal("expected captured after second tool call")
	}
	second := f.Captured()
	if !strings.Contains(second, "second") {
		t.Errorf("second captured = %q, want containing 'second'", second)
	}
}

func TestParseXMLToolCall(t *testing.T) {
	tests := []struct {
		name     string
		xml      string
		wantNil  bool
		wantName string
		wantArgs map[string]string
	}{
		{
			name: "valid invoke with multiple parameters",
			xml: `<invoke name="execute_command">
				<parameter name="command">echo hello</parameter>
				<parameter name="timeout_seconds">30</parameter>
			</invoke>`,
			wantName: "execute_command",
			wantArgs: map[string]string{
				"command":         "echo hello",
				"timeout_seconds": "30",
			},
		},
		{
			name: "valid invoke with JSON parameter value",
			xml: `<invoke name="read_file">
				<parameter name="path">{"nested":true}</parameter>
			</invoke>`,
			wantName: "read_file",
			wantArgs: map[string]string{
				"path": `{"nested":true}`,
			},
		},
		{
			name:    "missing invoke tag",
			xml:     `<something name="test"></something>`,
			wantNil: true,
		},
		{
			name:    "missing name attribute on invoke",
			xml:     `<invoke id="nope"></invoke>`,
			wantNil: true,
		},
		{
			name:     "invoke with no parameters",
			xml:      `<invoke name="reload_env"></invoke>`,
			wantName: "reload_env",
			wantArgs: map[string]string{},
		},
		{
			name:    "empty string",
			xml:     "",
			wantNil: true,
		},
		{
			name: "parameter value with special characters",
			xml: `<invoke name="grep">
				<parameter name="pattern">func\s+main</parameter>
			</invoke>`,
			wantName: "grep",
			wantArgs: map[string]string{
				"pattern": `func\s+main`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := parseXMLToolCall(tt.xml)

			if tt.wantNil {
				if tc != nil {
					t.Fatalf("expected nil, got ToolCall with name=%q", tc.Function.Name)
				}
				return
			}

			if tc == nil {
				t.Fatal("expected non-nil ToolCall, got nil")
			}
			if tc.Function.Name != tt.wantName {
				t.Errorf("Function.Name = %q, want %q", tc.Function.Name, tt.wantName)
			}
			if tc.Type != "function" {
				t.Errorf("Type = %q, want %q", tc.Type, "function")
			}
			if !strings.HasPrefix(tc.ID, "xml-"+tt.wantName+"-") {
				t.Errorf("ID = %q, want prefix %q", tc.ID, "xml-"+tt.wantName+"-")
			}

			var parsedArgs map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsedArgs); err != nil {
				t.Fatalf("failed to unmarshal arguments: %v", err)
			}

			if len(parsedArgs) != len(tt.wantArgs) {
				t.Fatalf("got %d args, want %d", len(parsedArgs), len(tt.wantArgs))
			}

			for key, wantVal := range tt.wantArgs {
				rawVal, ok := parsedArgs[key]
				if !ok {
					t.Errorf("missing argument %q", key)
					continue
				}
				var gotVal string
				if err := json.Unmarshal(rawVal, &gotVal); err != nil {
					if json.Valid([]byte(wantVal)) {
						if string(rawVal) != wantVal {
							t.Errorf("arg[%q] = %s, want %s", key, rawVal, wantVal)
						}
						continue
					}
					t.Errorf("arg[%q] unmarshal error: %v (raw: %s)", key, err, rawVal)
					continue
				}
				if gotVal != wantVal {
					t.Errorf("arg[%q] = %q, want %q", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestDrainStream(t *testing.T) {
	t.Run("channel closes immediately", func(t *testing.T) {
		ch := make(chan valueobject.StreamChunk)
		close(ch)
		drainStream(ch)
	})

	t.Run("channel has error chunk", func(t *testing.T) {
		ch := make(chan valueobject.StreamChunk, 2)
		ch <- valueobject.StreamChunk{Content: "partial"}
		ch <- valueobject.StreamChunk{Err: fmt.Errorf("test error")}
		drainStream(ch)
	})

	t.Run("channel has Done chunk", func(t *testing.T) {
		ch := make(chan valueobject.StreamChunk, 2)
		ch <- valueobject.StreamChunk{Content: "partial"}
		ch <- valueobject.StreamChunk{Done: true}
		drainStream(ch)
	})

	t.Run("channel has remaining content then closes", func(t *testing.T) {
		ch := make(chan valueobject.StreamChunk, 3)
		ch <- valueobject.StreamChunk{Content: "a"}
		ch <- valueobject.StreamChunk{Content: "b"}
		close(ch)
		drainStream(ch)
	})
}
