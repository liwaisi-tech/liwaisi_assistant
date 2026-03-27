package cpn

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ── Test Helpers ─────────────────────────────────────────────────────────────

// makeHistory creates a history with N observer + M raw messages.
func makeHistory(observers, raws int) []Message {
	h := make([]Message, 0, observers+raws)
	for i := 0; i < observers; i++ {
		h = append(h, Message{
			ID: fmt.Sprintf("obs-%d", i), Role: RoleObserver,
			Content: fmt.Sprintf("summary %d", i), CPNRole: fmt.Sprintf("worker-%d", i),
		})
	}
	for i := 0; i < raws; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		h = append(h, Message{
			ID: fmt.Sprintf("raw-%d", i), Role: role,
			Content: fmt.Sprintf("message %d", i),
		})
	}
	return h
}

// ── BuildContext Tests ───────────────────────────────────────────────────────

func TestBuildContext_ObserverMessagesAlwaysIncluded(t *testing.T) {
	history := makeHistory(3, 10)
	ctx := BuildContext("system prompt", history, 3) // windowSize=3 → last 6 raw

	// Count observer-formatted messages.
	var obsCount int
	for _, m := range ctx.Messages {
		if strings.HasPrefix(m.Content, "[Summary from ") {
			obsCount++
		}
	}
	if obsCount != 3 {
		t.Errorf("expected 3 observer messages, got %d", obsCount)
	}
}

func TestBuildContext_SlidingWindow_OverLimit(t *testing.T) {
	history := makeHistory(0, 20)          // 20 raw messages
	ctx := BuildContext("sys", history, 5) // windowSize=5 → last 10

	if len(ctx.Messages) != 10 {
		t.Errorf("expected 10 messages (sliding window), got %d", len(ctx.Messages))
	}
}

func TestBuildContext_SlidingWindow_UnderLimit(t *testing.T) {
	history := makeHistory(0, 4)           // 4 raw messages
	ctx := BuildContext("sys", history, 5) // windowSize=5 → max 10, all 4 fit

	if len(ctx.Messages) != 4 {
		t.Errorf("expected 4 messages (all fit), got %d", len(ctx.Messages))
	}
}

func TestBuildContext_EmptyHistory(t *testing.T) {
	ctx := BuildContext("You are helpful.", nil, 10)

	if len(ctx.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(ctx.Messages))
	}
	if ctx.SystemPrompt != "You are helpful." {
		t.Errorf("expected system prompt preserved, got %q", ctx.SystemPrompt)
	}
	if ctx.InputTokenEstimate <= 0 {
		t.Errorf("expected positive token estimate for non-empty system prompt, got %d", ctx.InputTokenEstimate)
	}
}

func TestBuildContext_ObserversOnly(t *testing.T) {
	history := makeHistory(5, 0)
	ctx := BuildContext("sys", history, 10)

	if len(ctx.Messages) != 5 {
		t.Errorf("expected 5 observer messages only, got %d", len(ctx.Messages))
	}
}

func TestBuildContext_RawOnly(t *testing.T) {
	history := makeHistory(0, 6)
	ctx := BuildContext("sys", history, 10)

	if len(ctx.Messages) != 6 {
		t.Errorf("expected 6 raw messages only, got %d", len(ctx.Messages))
	}
}

func TestBuildContext_ObserverBeforeRaw(t *testing.T) {
	history := makeHistory(2, 4)
	ctx := BuildContext("sys", history, 10)

	if len(ctx.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(ctx.Messages))
	}
	// First two should be observer summaries.
	for i := 0; i < 2; i++ {
		if !strings.HasPrefix(ctx.Messages[i].Content, "[Summary from ") {
			t.Errorf("message %d should be observer summary, got %q", i, ctx.Messages[i].Content)
		}
	}
	// Remaining should be raw messages.
	for i := 2; i < len(ctx.Messages); i++ {
		if strings.HasPrefix(ctx.Messages[i].Content, "[Summary from ") {
			t.Errorf("message %d should be raw, got observer summary", i)
		}
	}
}

func TestBuildContext_ObserverFormat(t *testing.T) {
	history := []Message{
		{ID: "o1", Role: RoleObserver, Content: "analyzed repo", CPNRole: "repo-analyzer"},
	}
	ctx := BuildContext("sys", history, 10)

	if len(ctx.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ctx.Messages))
	}
	want := "[Summary from repo-analyzer]: analyzed repo"
	if ctx.Messages[0].Content != want {
		t.Errorf("got %q, want %q", ctx.Messages[0].Content, want)
	}
}

func TestBuildContext_ObserverRole(t *testing.T) {
	history := []Message{
		{ID: "o1", Role: RoleObserver, Content: "summary", CPNRole: "worker"},
	}
	ctx := BuildContext("sys", history, 10)

	if len(ctx.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ctx.Messages))
	}
	if ctx.Messages[0].Role != "assistant" {
		t.Errorf("observer should map to assistant role, got %q", ctx.Messages[0].Role)
	}
}

func TestBuildContext_ZeroWindowSize(t *testing.T) {
	history := makeHistory(2, 10)
	ctx := BuildContext("sys", history, 0) // no raw messages

	if len(ctx.Messages) != 2 {
		t.Errorf("expected 2 observer messages only (zero window), got %d", len(ctx.Messages))
	}
}

func TestBuildContext_NegativeWindowSize(t *testing.T) {
	history := makeHistory(2, 10)
	ctx := BuildContext("sys", history, -1) // treated as zero

	if len(ctx.Messages) != 2 {
		t.Errorf("expected 2 observer messages only (negative window), got %d", len(ctx.Messages))
	}
}

func TestBuildContext_TokenEstimate(t *testing.T) {
	history := makeHistory(1, 4)
	ctx := BuildContext("You are a helpful assistant.", history, 10)

	if ctx.InputTokenEstimate <= 0 {
		t.Errorf("expected positive token estimate, got %d", ctx.InputTokenEstimate)
	}
}

func TestBuildContext_DoesNotModifyHistory(t *testing.T) {
	history := makeHistory(2, 10)
	original := make([]Message, len(history))
	copy(original, history)

	BuildContext("sys", history, 3)

	for i := range history {
		if history[i].ID != original[i].ID || history[i].Content != original[i].Content {
			t.Fatalf("BuildContext modified history at index %d", i)
		}
	}
}

// ── filterRaw Tests ──────────────────────────────────────────────────────────

func TestFilterRaw_ExcludesObserver(t *testing.T) {
	history := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleObserver, Content: "summary"},
		{Role: RoleAssistant, Content: "hello"},
	}
	raw := filterRaw(history)
	if len(raw) != 2 {
		t.Errorf("expected 2 raw messages, got %d", len(raw))
	}
}

func TestFilterRaw_EmptyHistory(t *testing.T) {
	raw := filterRaw(nil)
	if len(raw) != 0 {
		t.Errorf("expected 0 raw messages, got %d", len(raw))
	}
}

func TestFilterRaw_PreservesOrder(t *testing.T) {
	history := []Message{
		{ID: "1", Role: RoleUser, Content: "a"},
		{ID: "2", Role: RoleAssistant, Content: "b"},
		{ID: "3", Role: RoleUser, Content: "c"},
	}
	raw := filterRaw(history)
	if len(raw) != 3 {
		t.Fatalf("expected 3, got %d", len(raw))
	}
	for i, want := range []string{"1", "2", "3"} {
		if raw[i].ID != want {
			t.Errorf("raw[%d].ID = %q, want %q", i, raw[i].ID, want)
		}
	}
}

// ── estimateTokens Tests ─────────────────────────────────────────────────────

func TestEstimateTokens_EmptyInput(t *testing.T) {
	est := estimateTokens("", nil)
	if est != 0 {
		t.Errorf("expected 0, got %d", est)
	}
}

func TestEstimateTokens_Approximation(t *testing.T) {
	// 40 chars → ~10 tokens + 4 overhead = 14 per message.
	msgs := []LLMMessage{
		{Role: "user", Content: strings.Repeat("a", 40)},
	}
	est := estimateTokens("", msgs)
	// 40/4 + 4 = 14
	if est != 14 {
		t.Errorf("expected 14, got %d", est)
	}
}

func TestEstimateTokens_IncludesSystemPrompt(t *testing.T) {
	prompt := strings.Repeat("x", 100) // 100/4 = 25 tokens
	est := estimateTokens(prompt, nil)
	if est != 25 {
		t.Errorf("expected 25, got %d", est)
	}
}

func TestEstimateTokens_CombinedPromptAndMessages(t *testing.T) {
	prompt := strings.Repeat("x", 40) // 40/4 = 10
	msgs := []LLMMessage{
		{Role: "user", Content: strings.Repeat("a", 20)}, // 20/4 + 4 = 9
	}
	est := estimateTokens(prompt, msgs)
	// 10 + 9 = 19
	if est != 19 {
		t.Errorf("expected 19, got %d", est)
	}
}

// ── CompressSubNetSummary Tests ──────────────────────────────────────────────

func TestCompressSubNetSummary_CreatesObserverMessage(t *testing.T) {
	tokens := []Token{
		{Payload: "result-1"},
		{Payload: "result-2"},
	}
	msg := CompressSubNetSummary("child-1", "analyzer", 1, tokens)

	if msg.Role != RoleObserver {
		t.Errorf("expected RoleObserver, got %q", msg.Role)
	}
}

func TestCompressSubNetSummary_SetsMetadata(t *testing.T) {
	tokens := []Token{{Payload: "data"}}
	msg := CompressSubNetSummary("child-42", "planner", 2, tokens)

	if msg.CPNID != "child-42" {
		t.Errorf("CPNID = %q, want %q", msg.CPNID, "child-42")
	}
	if msg.CPNRole != "planner" {
		t.Errorf("CPNRole = %q, want %q", msg.CPNRole, "planner")
	}
	if msg.CPNDepth != 2 {
		t.Errorf("CPNDepth = %d, want 2", msg.CPNDepth)
	}
}

func TestCompressSubNetSummary_HasUUID(t *testing.T) {
	msg := CompressSubNetSummary("c1", "role", 0, nil)

	if msg.ID == "" {
		t.Error("expected non-empty ID")
	}
	uuidRE := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRE.MatchString(msg.ID) {
		t.Errorf("ID %q does not match UUID v4 format", msg.ID)
	}
}

func TestCompressSubNetSummary_HasTimestamp(t *testing.T) {
	before := time.Now().Add(-time.Second)
	msg := CompressSubNetSummary("c1", "role", 0, nil)
	after := time.Now().Add(time.Second)

	if msg.Timestamp.Before(before) || msg.Timestamp.After(after) {
		t.Errorf("timestamp %v not in expected range", msg.Timestamp)
	}
}

func TestCompressSubNetSummary_ContentUsesFormatSummary(t *testing.T) {
	tokens := []Token{{Payload: "hello"}, {Payload: "world"}}
	msg := CompressSubNetSummary("c1", "worker", 1, tokens)

	if !strings.Contains(msg.Content, "hello") || !strings.Contains(msg.Content, "world") {
		t.Errorf("content should contain payload data, got %q", msg.Content)
	}
}

// ── formatSummary Tests ──────────────────────────────────────────────────────

func TestFormatSummary_ConcatenatesPayloads(t *testing.T) {
	tokens := []Token{{Payload: "alpha"}, {Payload: "beta"}}
	s := formatSummary("worker", 1, tokens)

	if !strings.Contains(s, "alpha") {
		t.Errorf("expected 'alpha' in summary, got %q", s)
	}
	if !strings.Contains(s, "beta") {
		t.Errorf("expected 'beta' in summary, got %q", s)
	}
	if !strings.Contains(s, "worker") {
		t.Errorf("expected role in summary, got %q", s)
	}
}

func TestFormatSummary_TruncatesLongContent(t *testing.T) {
	// Create tokens with very long payloads.
	tokens := make([]Token, 100)
	for i := range tokens {
		tokens[i] = Token{Payload: strings.Repeat("x", 100)}
	}
	s := formatSummary("worker", 2, tokens)

	if len(s) > 500 {
		t.Errorf("expected summary <= 500 chars, got %d", len(s))
	}
}

func TestFormatSummary_EmptyTokens(t *testing.T) {
	s := formatSummary("worker", 1, nil)
	if s == "" {
		t.Error("expected non-empty summary even with no tokens")
	}
	if !strings.Contains(s, "0 output") {
		t.Errorf("expected '0 output' for empty tokens, got %q", s)
	}
}

// ── newUUID Tests ────────────────────────────────────────────────────────────

func TestNewUUID_Format(t *testing.T) {
	id := newUUID()
	uuidRE := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRE.MatchString(id) {
		t.Errorf("UUID %q does not match v4 format", id)
	}
}

func TestNewUUID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := newUUID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate UUID on iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewUUID_Version4(t *testing.T) {
	id := newUUID()
	// Version nibble is char at index 14 (0-indexed).
	if id[14] != '4' {
		t.Errorf("expected version 4 at position 14, got %c", id[14])
	}
	// Variant nibble is char at index 19.
	variant := id[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
		t.Errorf("expected variant [89ab] at position 19, got %c", variant)
	}
}

// ── DefaultContextWindowSize Test ────────────────────────────────────────────

func TestDefaultContextWindowSize(t *testing.T) {
	if DefaultContextWindowSize != 10 {
		t.Errorf("expected DefaultContextWindowSize=10, got %d", DefaultContextWindowSize)
	}
}

// ── Transition.SystemPrompt Extension Test ───────────────────────────────────

func TestTransition_SystemPromptField(t *testing.T) {
	tr := &Transition{
		ID:           "llm-1",
		Kind:         NodeKindLLM,
		SystemPrompt: "You are a coding assistant.",
	}
	if tr.SystemPrompt != "You are a coding assistant." {
		t.Errorf("SystemPrompt = %q, want %q", tr.SystemPrompt, "You are a coding assistant.")
	}
}
