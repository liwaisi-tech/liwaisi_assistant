package openrouter

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestParseSSEStream_ValidStream(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":""}]}

data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"World"},"finish_reason":""}]}

data: {"id":"1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	tokens := make([]string, 0, 2)
	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			gotDone = true
			continue
		}
		tokens = append(tokens, chunk.Content)
	}

	if !gotDone {
		t.Error("never received done signal")
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if tokens[0] != "Hello " || tokens[1] != "World" {
		t.Errorf("tokens = %v, want [Hello , World]", tokens)
	}
}

func TestParseSSEStream_DoneSentinel(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}]}

data: [DONE]
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	tokens := make([]string, 0, 1)
	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			gotDone = true
			continue
		}
		tokens = append(tokens, chunk.Content)
	}

	if !gotDone {
		t.Error("expected done from [DONE] sentinel")
	}
	if len(tokens) != 1 || tokens[0] != "Hi" {
		t.Errorf("tokens = %v, want [Hi]", tokens)
	}
}

func TestParseSSEStream_EmptyChoices(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[]}

data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":""}]}

data: [DONE]
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	tokens := make([]string, 0, 1)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		tokens = append(tokens, chunk.Content)
	}

	if len(tokens) != 1 || tokens[0] != "ok" {
		t.Errorf("tokens = %v, want [ok]", tokens)
	}
}

func TestParseSSEStream_MalformedJSON(t *testing.T) {
	sseData := `data: {invalid json}
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	chunk := <-ch
	if chunk.Err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestParseSSEStream_ErrorFinishReason(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"error"}]}
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	chunk := <-ch
	if chunk.Err == nil {
		t.Error("expected error for finish_reason=error")
	}
}

func TestParseSSEStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}]}
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(ctx, body)

	chunk := <-ch
	if chunk.Err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestParseSSEStream_IgnoresComments(t *testing.T) {
	sseData := `: this is a comment
data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":""}]}

data: [DONE]
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	tokens := make([]string, 0, 1)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		tokens = append(tokens, chunk.Content)
	}

	if len(tokens) != 1 || tokens[0] != "ok" {
		t.Errorf("tokens = %v, want [ok]", tokens)
	}
}

func TestParseSSEStream_ToolCallsFinishReason(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":""}]}

data: {"id":"1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			gotDone = true
		}
	}

	if !gotDone {
		t.Error("expected done signal for finish_reason=tool_calls")
	}
}

func TestParseSSEStream_EmptyContent(t *testing.T) {
	sseData := `data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":""},"finish_reason":""}]}

data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"real"},"finish_reason":""}]}

data: [DONE]
`
	body := io.NopCloser(strings.NewReader(sseData))
	ch := parseSSEStream(context.Background(), body)

	tokens := make([]string, 0, 1)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		tokens = append(tokens, chunk.Content)
	}

	if len(tokens) != 1 || tokens[0] != "real" {
		t.Errorf("tokens = %v, want [real] (empty content should be skipped)", tokens)
	}
}
