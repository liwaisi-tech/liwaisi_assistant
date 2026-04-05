package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// ── Validate Test Helpers ───────────────────────────────────────────────────

// validatorThatAccepts returns a ValidateFunc that always succeeds.
func validatorThatAccepts() func(any) error {
	return func(any) error { return nil }
}

// validatorThatRejects returns a ValidateFunc that fails failCount times then succeeds.
func validatorThatRejects(failCount int) func(any) error {
	var calls atomic.Int32
	return func(any) error {
		c := int(calls.Add(1))
		if c <= failCount {
			return fmt.Errorf("invalid field X (call %d)", c)
		}
		return nil
	}
}

func newTestCPNForValidate(mock *mockLLMClient, transitions map[string]*Transition) *CPN {
	places := map[string]*Place{
		"P:INPUT": {
			ID:    "P:INPUT",
			Space: SpaceComputation,
			Color: ColorJSON,
		},
		"P:OUTPUT": {
			ID:    "P:OUTPUT",
			Space: SpaceComputation,
			Color: ColorJSON,
		},
	}

	return &CPN{
		ID:                "test-cpn",
		Role:              "worker",
		Depth:             2,
		SessionID:         "session-1",
		LLMClient:         mock,
		History:           nil,
		ContextWindowSize: DefaultContextWindowSize,
		Places:            places,
		Transitions:       transitions,
	}
}

func newBasicValidateTransition() *Transition {
	return &Transition{
		ID:           "validate-output",
		Kind:         NodeKindValidate,
		InputPlaces:  []string{"P:INPUT"},
		OutputPlaces: []string{"P:OUTPUT"},
		ValidateConfig: &ValidateConfig{
			ValidateFunc: validatorThatAccepts(),
		},
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestFireValidate_ValidPassThrough(t *testing.T) {
	trans := newBasicValidateTransition()
	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorJSON, Payload: `{"intent":"greeting"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}

	tokens, _ := output.Peek()
	if tokens[0].Payload != `{"intent":"greeting"}` {
		t.Errorf("expected payload passthrough, got %v", tokens[0].Payload)
	}
}

func TestFireValidate_ValidWithOnSuccess(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.OnSuccess = func(validated any) any {
		return map[string]any{"transformed": true, "original": validated}
	}

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorJSON, Payload: `{"key":"value"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	payload, ok := tokens[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", tokens[0].Payload)
	}
	if payload["transformed"] != true {
		t.Error("expected OnSuccess to transform the payload")
	}
}

func TestFireValidate_InvalidOneCorrection(t *testing.T) {
	// Validator fails once then succeeds.
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = validatorThatRejects(1)
	trans.ValidateConfig.MaxCorrections = 2
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"fixed":"value"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)

	consumed := []Token{{Color: ColorJSON, Payload: `{"bad":"data"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}

	// Verify the LLM was called exactly once.
	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 LLM correction call, got %d", len(calls))
	}
}

func TestFireValidate_InvalidMaxCorrections_ErrorPlace(t *testing.T) {
	// Validator always fails.
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("always invalid") }
	trans.ValidateConfig.MaxCorrections = 2
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"
	trans.ErrorPlace = "P:ERROR"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"still":"bad"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)
	cpn.Places["P:ERROR"] = &Place{
		ID:    "P:ERROR",
		Space: SpaceComputation,
		Color: ColorError,
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{"original":"data"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil error (ErrorPlace routing), got: %v", err)
	}

	// Verify error token was deposited.
	errorPlace := cpn.Places["P:ERROR"]
	if errorPlace.Len() != 1 {
		t.Fatalf("expected 1 error token, got %d", errorPlace.Len())
	}

	tokens, _ := errorPlace.Peek()
	if tokens[0].Color != ColorError {
		t.Errorf("expected ColorError, got %s", tokens[0].Color)
	}

	errPayload, ok := tokens[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", tokens[0].Payload)
	}
	if _, hasErr := errPayload["error"]; !hasErr {
		t.Error("error token missing 'error' field")
	}
	if _, hasOrig := errPayload["original_payload"]; !hasOrig {
		t.Error("error token missing 'original_payload' field")
	}

	// Verify LLM was called MaxCorrections times.
	calls := mock.getCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 correction LLM calls, got %d", len(calls))
	}
}

func TestFireValidate_InvalidMaxCorrections_NoErrorPlace(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("always invalid") }
	trans.ValidateConfig.MaxCorrections = 1
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"
	// No ErrorPlace.

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"still":"bad"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)

	consumed := []Token{{Color: ColorJSON, Payload: `{"data":"invalid"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got: %v", err)
	}
}

func TestFireValidate_NoCorrectionLLMID(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("invalid") }
	trans.ValidateConfig.MaxCorrections = 5
	trans.ValidateConfig.CorrectionLLMID = "" // No correction LLM.
	trans.ErrorPlace = "P:ERROR"

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	cpn.Places["P:ERROR"] = &Place{
		ID:    "P:ERROR",
		Space: SpaceComputation,
		Color: ColorError,
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil (ErrorPlace routing), got: %v", err)
	}

	if cpn.Places["P:ERROR"].Len() != 1 {
		t.Error("expected error token deposited to ErrorPlace")
	}
}

func TestFireValidate_MaxCorrectionsZero(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("invalid") }
	trans.ValidateConfig.MaxCorrections = 0
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got: %v", err)
	}
}

func TestFireValidate_MaxCorrectionsCapped(t *testing.T) {
	var callCount atomic.Int32
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("always invalid") }
	trans.ValidateConfig.MaxCorrections = 100 // Should be capped to 10.
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"
	trans.ErrorPlace = "P:ERROR"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			callCount.Add(1)
			return LLMResponse{Content: `{"bad":"json"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)
	cpn.Places["P:ERROR"] = &Place{
		ID:    "P:ERROR",
		Space: SpaceComputation,
		Color: ColorError,
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil (ErrorPlace routing), got: %v", err)
	}

	// Should be capped at MaxCorrectionCap (10), not 100.
	if int(callCount.Load()) != MaxCorrectionCap {
		t.Errorf("expected %d correction calls (capped), got %d", MaxCorrectionCap, callCount.Load())
	}
}

func TestFireValidate_ValidateFuncPrecedence(t *testing.T) {
	type TestSchema struct {
		Name string `json:"name"`
	}

	funcCalled := false
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(payload any) error {
		funcCalled = true
		return nil
	}
	trans.ValidateConfig.Schema = &TestSchema{} // Should be ignored.

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorJSON, Payload: `{"name":"test"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !funcCalled {
		t.Error("expected ValidateFunc to be called (precedence over Schema)")
	}
}

func TestFireValidate_StructSchemaRoundTrip(t *testing.T) {
	type ClassificationResult struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
	}

	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = nil // Use schema.
	trans.ValidateConfig.Schema = &ClassificationResult{}

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorJSON, Payload: `{"intent":"greeting","confidence":0.95}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}
}

func TestFireValidate_StructSchemaRoundTrip_Invalid(t *testing.T) {
	type StrictSchema struct {
		Name string `json:"name"`
	}

	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = nil
	trans.ValidateConfig.Schema = &StrictSchema{}

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	// Invalid JSON string.
	consumed := []Token{{Color: ColorJSON, Payload: "not valid json at all"}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for invalid JSON, got: %v", err)
	}
}

func TestFireValidate_NilSchema_AlwaysValid(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = nil
	trans.ValidateConfig.Schema = nil

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorJSON, Payload: "anything goes"}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil error for nil schema + nil func, got: %v", err)
	}

	if cpn.Places["P:OUTPUT"].Len() != 1 {
		t.Error("expected token deposited")
	}
}

func TestFireValidate_NilValidateConfig(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig = nil

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err == nil {
		t.Fatal("expected error for nil ValidateConfig")
	}
	if !strings.Contains(err.Error(), "nil ValidateConfig") {
		t.Errorf("expected 'nil ValidateConfig' error, got: %v", err)
	}
}

func TestFireValidate_NoConsumedTokens(t *testing.T) {
	trans := newBasicValidateTransition()
	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	_, _, err := fireValidate(context.Background(), trans, cpn, []Token{})
	if err == nil {
		t.Fatal("expected error for empty consumed")
	}
	if !strings.Contains(err.Error(), "no consumed tokens") {
		t.Errorf("expected 'no consumed tokens' error, got: %v", err)
	}
}

func TestFireValidate_ContextCancelled(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("invalid") }
	trans.ValidateConfig.MaxCorrections = 5
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"fixed":"data"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)

	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, _, err := fireValidate(ctx, trans, cpn, consumed)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestFireValidate_CorrectionLLMError(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("invalid") }
	trans.ValidateConfig.MaxCorrections = 2
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{}, fmt.Errorf("LLM unavailable")
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)

	consumed := []Token{{Color: ColorJSON, Payload: `{}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err == nil {
		t.Fatal("expected error from correction LLM")
	}
	if !strings.Contains(err.Error(), "LLM unavailable") {
		t.Errorf("expected 'LLM unavailable' in error, got: %v", err)
	}
}

func TestFireValidate_MultipleOutputPlaces(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.OutputPlaces = []string{"P:OUTPUT", "P:OUTPUT2"}

	transitions := map[string]*Transition{trans.ID: trans}
	places := map[string]*Place{
		"P:INPUT": {
			ID:    "P:INPUT",
			Space: SpaceComputation,
			Color: ColorJSON,
		},
		"P:OUTPUT": {
			ID:    "P:OUTPUT",
			Space: SpaceComputation,
			Color: ColorJSON,
		},
		"P:OUTPUT2": {
			ID:    "P:OUTPUT2",
			Space: SpaceComputation,
			Color: ColorJSON,
		},
	}

	cpn := &CPN{
		ID:                "test-cpn",
		Depth:             2,
		SessionID:         "session-1",
		ContextWindowSize: DefaultContextWindowSize,
		Places:            places,
		Transitions:       transitions,
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{"data":"test"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cpn.Places["P:OUTPUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUTPUT, got %d", cpn.Places["P:OUTPUT"].Len())
	}
	if cpn.Places["P:OUTPUT2"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUTPUT2, got %d", cpn.Places["P:OUTPUT2"].Len())
	}
}

func TestFireValidate_OriginMetadata(t *testing.T) {
	trans := newBasicValidateTransition()
	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorJSON, Payload: `{"data":"test"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	tok := tokens[0]

	if tok.OriginID != "test-cpn" {
		t.Errorf("expected OriginID=test-cpn, got %q", tok.OriginID)
	}
	if tok.OriginDepth != 2 {
		t.Errorf("expected OriginDepth=2, got %d", tok.OriginDepth)
	}
	if tok.OriginKind != NodeKindValidate {
		t.Errorf("expected OriginKind=NodeKindValidate, got %q", tok.OriginKind)
	}
	if tok.SessionID != "session-1" {
		t.Errorf("expected SessionID=session-1, got %q", tok.SessionID)
	}
	if tok.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
}

func TestFireValidate_ErrorTokenFormat(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = func(any) error { return fmt.Errorf("field X missing") }
	trans.ValidateConfig.MaxCorrections = 0
	trans.ErrorPlace = "P:ERROR"

	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})
	cpn.Places["P:ERROR"] = &Place{
		ID:    "P:ERROR",
		Space: SpaceComputation,
		Color: ColorError,
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{"incomplete":true}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil (ErrorPlace routing), got: %v", err)
	}

	errorPlace := cpn.Places["P:ERROR"]
	tokens, _ := errorPlace.Peek()
	tok := tokens[0]

	if tok.Color != ColorError {
		t.Errorf("expected ColorError, got %s", tok.Color)
	}

	payload, ok := tok.Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", tok.Payload)
	}

	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "field X missing") {
		t.Errorf("expected error message with 'field X missing', got: %s", errMsg)
	}

	if payload["original_payload"] == nil {
		t.Error("expected original_payload in error token")
	}
}

func TestFireValidate_PreservesOriginalTokenColor(t *testing.T) {
	trans := newBasicValidateTransition()
	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorJSON, Payload: `{"data":"test"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	if tokens[0].Color != ColorJSON {
		t.Errorf("expected ColorJSON (preserved from input), got %s", tokens[0].Color)
	}
}

func TestFireValidate_CorrectionLLMNonJSON(t *testing.T) {
	// Validator fails twice (correction returns non-JSON, re-validation also fails).
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = nil
	trans.ValidateConfig.Schema = &struct{ Name string }{}
	trans.ValidateConfig.MaxCorrections = 1
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"
	trans.ErrorPlace = "P:ERROR"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			// Return non-JSON — should count as a failed correction.
			return LLMResponse{Content: "I can't fix this sorry"}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)
	cpn.Places["P:ERROR"] = &Place{
		ID:    "P:ERROR",
		Space: SpaceComputation,
		Color: ColorError,
	}

	consumed := []Token{{Color: ColorJSON, Payload: "not json"}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected nil (ErrorPlace routing), got: %v", err)
	}

	if cpn.Places["P:ERROR"].Len() != 1 {
		t.Error("expected error token in ErrorPlace after non-JSON correction")
	}
}

// ── Helper Function Tests ───────────────────────────────────────────────────

func TestValidateAgainstSchema_Valid(t *testing.T) {
	type TestSchema struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	err := validateAgainstSchema(`{"name":"Alice","age":30}`, &TestSchema{})
	if err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidateAgainstSchema_Invalid(t *testing.T) {
	type TestSchema struct {
		Name string `json:"name"`
	}

	err := validateAgainstSchema("not json", &TestSchema{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateAgainstSchema_NilSchema(t *testing.T) {
	// Should not be called with nil (validatePayload guards), but if it is:
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil schema")
		}
	}()
	_ = validateAgainstSchema(`{}`, nil)
}

func TestValidateAgainstSchema_MapPayload(t *testing.T) {
	type TestSchema struct {
		Key string `json:"key"`
	}

	payload := map[string]any{"key": "value"}
	err := validateAgainstSchema(payload, &TestSchema{})
	if err != nil {
		t.Fatalf("expected valid for map payload, got: %v", err)
	}
}

func TestValidateAgainstSchema_RawMessagePayload(t *testing.T) {
	type TestSchema struct {
		Key string `json:"key"`
	}

	payload := json.RawMessage(`{"key":"value"}`)
	err := validateAgainstSchema(payload, &TestSchema{})
	if err != nil {
		t.Fatalf("expected valid for RawMessage payload, got: %v", err)
	}
}

func TestValidateAgainstSchema_NonPointerSchema(t *testing.T) {
	type TestSchema struct {
		Name string `json:"name"`
	}

	// Pass non-pointer — should still work.
	err := validateAgainstSchema(`{"name":"test"}`, TestSchema{})
	if err != nil {
		t.Fatalf("expected valid for non-pointer schema, got: %v", err)
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no fences",
			input:    `{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
		{
			name:     "json fence",
			input:    "```json\n{\"key\":\"value\"}\n```",
			expected: `{"key":"value"}`,
		},
		{
			name:     "plain fence",
			input:    "```\n{\"key\":\"value\"}\n```",
			expected: `{"key":"value"}`,
		},
		{
			name:     "with whitespace",
			input:    "  ```json\n{\"key\":\"value\"}\n```  ",
			expected: `{"key":"value"}`,
		},
		{
			name:     "empty content",
			input:    "```json\n```",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripCodeFences(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildCorrectionPrompt(t *testing.T) {
	prompt := buildCorrectionPrompt(`{"bad":"data"}`, "missing field 'name'")

	if !strings.Contains(prompt, `{"bad":"data"}`) {
		t.Error("expected payload in prompt")
	}
	if !strings.Contains(prompt, "missing field 'name'") {
		t.Error("expected schema error in prompt")
	}
	if !strings.Contains(prompt, "Fix the JSON") {
		t.Error("expected correction instruction in prompt")
	}
}

func TestBuildCorrectionPrompt_MapPayload(t *testing.T) {
	payload := map[string]any{"key": "value"}
	prompt := buildCorrectionPrompt(payload, "invalid type")

	if !strings.Contains(prompt, "key") {
		t.Error("expected map payload serialized in prompt")
	}
}

func TestFireValidate_DispatchIntegration(t *testing.T) {
	// Verify dispatch routes NodeKindValidate to fireValidate.
	trans := newBasicValidateTransition()
	cpn := newTestCPNForValidate(nil, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorJSON, Payload: `{"data":"test"}`}}

	_, _, err := dispatch(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("dispatch to fireValidate failed: %v", err)
	}

	if cpn.Places["P:OUTPUT"].Len() != 1 {
		t.Error("expected token deposited via dispatch -> fireValidate")
	}
}

func TestFireValidate_CorrectionPromptContent(t *testing.T) {
	// Verify the correction prompt includes only payload and error, no history.
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = validatorThatRejects(1)
	trans.ValidateConfig.MaxCorrections = 1
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "structured",
			MaxTokens: 512,
		},
	}

	var capturedReq *LLMRequest
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			capturedReq = req
			return LLMResponse{Content: `{"fixed":"data"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)
	// Add history that should NOT appear in correction prompt.
	cpn.History = []*Message{
		{Role: RoleUser, Content: "secret user message"},
	}

	consumed := []Token{{Color: ColorJSON, Payload: `{"bad":"data"}`}}

	_, _, err := fireValidate(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify correction LLM received exactly 2 messages (system + user).
	if capturedReq == nil {
		t.Fatal("expected LLM to be called")
	}
	if len(capturedReq.Messages) != 2 {
		t.Errorf("expected 2 messages (system + user), got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "system" {
		t.Error("expected first message to be system")
	}
	if capturedReq.Messages[1].Role != "user" {
		t.Error("expected second message to be user")
	}

	// Verify no history leaked into the prompt.
	for _, msg := range capturedReq.Messages {
		if strings.Contains(msg.Content, "secret user message") {
			t.Error("history should NOT leak into correction prompt (SEC-004)")
		}
	}
}

func TestFireValidate_CorrectionLLMUsesStructuredModel(t *testing.T) {
	trans := newBasicValidateTransition()
	trans.ValidateConfig.ValidateFunc = validatorThatRejects(1)
	trans.ValidateConfig.MaxCorrections = 1
	trans.ValidateConfig.CorrectionLLMID = "llm:fix-json"

	corrLLM := &Transition{
		ID:   "llm:fix-json",
		Kind: NodeKindLLM,
		LLMConfig: &LLMConfig{
			Model:     "my-custom-model",
			MaxTokens: 256,
		},
	}

	var capturedReq *LLMRequest
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			capturedReq = req
			return LLMResponse{Content: `{"fixed":"data"}`}, nil
		},
	}

	transitions := map[string]*Transition{trans.ID: trans, corrLLM.ID: corrLLM}
	cpn := newTestCPNForValidate(mock, transitions)

	consumed := []Token{{Color: ColorJSON, Payload: `{"bad":"data"}`}}

	_, _, _ = fireValidate(context.Background(), trans, cpn, consumed)

	if capturedReq == nil {
		t.Fatal("expected LLM to be called")
	}
	if capturedReq.Model != "my-custom-model" {
		t.Errorf("expected model=my-custom-model, got %q", capturedReq.Model)
	}
	if capturedReq.MaxTokens != 256 {
		t.Errorf("expected MaxTokens=256, got %d", capturedReq.MaxTokens)
	}
	if capturedReq.ResponseFmt != "json_object" {
		t.Errorf("expected ResponseFmt=json_object, got %q", capturedReq.ResponseFmt)
	}
}
