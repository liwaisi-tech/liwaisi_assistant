package cpn

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFireLLM_LedgerSinkInstalledForToolCall is the end-to-end guard
// for the spec's central failure mode: an LLM invokes a state-changing
// tool, the tool records a ledger entry through the context-installed
// LedgerSink, the entry survives the sliding window, and a SECOND LLM
// firing many turns later still sees the path in its workspace
// preamble.
//
// Mocked LLM behaviour:
//  1. First Complete call → tool_call for fake_write_file
//  2. fake_write_file executor emits EmitLedgerSuccess via context
//  3. Second Complete call → final assistant text
//  4. Test asserts the ledger entry is in c.History
//  5. Pad history with 80 noise messages
//  6. BuildContextWithPolicy (assistant role) MUST surface the path in
//     its system prompt preamble
//
// This is the exact failure surfaced in the production session
// d616720d6f7407fd44d5196153c3318e (140 messages, brae forgot it had
// written ~/workspace/pg_explorer/main.go and re-created it).
func TestFireLLM_LedgerSinkInstalledForToolCall(t *testing.T) {
	const targetPath = "~/workspace/pg_explorer/main.go"

	var callCount atomic.Int32
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			n := callCount.Add(1)
			if n == 1 {
				return LLMResponse{
					ToolCalls: []*LLMToolCall{
						{
							ID:        "tc-write-1",
							ToolName:  "fake_write_file",
							Arguments: json.RawMessage(`{"path":"` + targetPath + `","content":"package main"}`),
						},
					},
				}, nil
			}
			return LLMResponse{Content: "Wrote main.go. Compiling next."}, nil
		},
	}

	// Fake executor that imitates what tools.makeWriteFile does on
	// success: emit a ledger entry through the context-installed sink
	// and return a lean receipt (no content echo).
	var execCalledWithSink atomic.Bool
	writeTrans := &Transition{
		ID:       "fake_write_file",
		Kind:     NodeKindTool,
		ToolName: "fake_write_file",
		Executor: func(ctx context.Context, in Token) (Token, error) {
			// AC: the LLM tool-call dispatch path MUST install a
			// LedgerSink before invoking the executor — without
			// it, brae forgets every tool call as soon as the
			// sliding window slides past it.
			if _, ok := LedgerSinkFromContext(ctx); ok {
				execCalledWithSink.Store(true)
			}
			EmitLedgerSuccess(ctx, LedgerVerbWrite, targetPath, "1.2KB")
			return Token{
				Color:   ColorJSON,
				Payload: json.RawMessage(`{"path":"` + targetPath + `","bytes":12,"created":true}`),
			}, nil
		},
	}

	llmTrans := newBasicLLMTransition()
	llmTrans.LLMTools = []string{"fake_write_file"}

	cpn := newTestCPNForLLM(mock, map[string]*Transition{
		llmTrans.ID:   llmTrans,
		writeTrans.ID: writeTrans,
	})

	consumed := []Token{{Color: ColorString, Payload: "write the file"}}
	if _, _, err := fireLLM(context.Background(), llmTrans, cpn, consumed); err != nil {
		t.Fatalf("fireLLM: %v", err)
	}

	// 1. The executor must have seen a LedgerSink on context.
	if !execCalledWithSink.Load() {
		t.Errorf("LedgerSink was NOT installed on tool-call ctx — ledger emission would be dropped silently")
	}

	// 2. The CPN history must contain the ledger entry.
	var ledger *Message
	for _, m := range cpn.History {
		if IsLedgerEntry(m) {
			ledger = m
			break
		}
	}
	if ledger == nil {
		t.Fatalf("ledger entry not found in c.History; got: %+v", cpn.History)
	}
	if !strings.Contains(ledger.Content, "pg_explorer/main.go") {
		t.Errorf("ledger entry missing target path: %q", ledger.Content)
	}

	// Backdate the ledger so the preamble timestamp is stable.
	ledger.Timestamp = time.Now().Add(-30 * time.Minute)

	// 3. Pad with 80 noise messages — well past the sliding window.
	for i := 0; i < 80; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		cpn.History = append(cpn.History, &Message{
			Role: role, Content: "noise", Timestamp: time.Now().Add(time.Duration(-29+i) * time.Minute),
		})
	}

	// 4. Simulate the next LLM transition firing — assemble its
	// context. The workspace preamble MUST contain the path.
	policy := NewDefaultPolicyResolver().For(RoleAssistantTransition)
	policy.SystemPrompt = "you are brae"
	cw := BuildContextWithPolicy(RoleAssistantTransition, cpn.History, policy)

	if !strings.Contains(cw.SystemPrompt, "pg_explorer/main.go") {
		t.Errorf("workspace preamble lost the path 80 messages later — the bug this spec was written to fix is back:\n%s", cw.SystemPrompt)
	}
}

// TestFireLLM_ClassifierRolePolicyApplied confirms that fire_llm's
// resolver-derived policy correctly demotes a transition tagged as
// "classifier" via LLMConfig.Role into the no-persona, no-preamble
// classifier policy. Regression guard for AC-002.
func TestFireLLM_ClassifierRolePolicyApplied(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, req *LLMRequest) (LLMResponse, error) {
			// The classifier role MUST not get a workspace preamble in
			// its system prompt, even if the history contains ledger
			// entries that would normally drive one.
			for _, m := range req.Messages {
				if m.Role == "system" && strings.Contains(m.Content, "[workspace state @") {
					t.Errorf("classifier transition unexpectedly received workspace preamble:\n%s", m.Content)
				}
			}
			return LLMResponse{Content: "label_a"}, nil
		},
	}

	// Seed history with a ledger entry that WOULD generate a preamble
	// for the assistant role.
	ledger, _ := LedgerSuccess(LedgerVerbWrite, "/tmp/x.go", "100B")
	ledger.Timestamp = time.Now().Add(-10 * time.Minute)

	trans := &Transition{
		ID: "classify-1", Kind: NodeKindLLM,
		InputPlaces: []string{"P:INPUT"}, OutputPlaces: []string{"P:OUTPUT"},
		SystemPrompt: "Output JSON only.",
		LLMConfig: &LLMConfig{
			Model:     "classifier",
			Role:      "classifier", // routes to RoleClassifyTransition policy
			MaxTokens: 50,
		},
	}
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	cpn.History = []*Message{ledger}

	consumed := []Token{{Color: ColorString, Payload: "input text"}}
	if _, _, err := fireLLM(context.Background(), trans, cpn, consumed); err != nil {
		t.Fatalf("fireLLM: %v", err)
	}
}
