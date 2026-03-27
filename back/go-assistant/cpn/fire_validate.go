package cpn

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// MaxCorrectionCap is the hard safety cap on correction loop iterations.
// Even if MaxCorrections is set higher, the loop is bounded to this value.
const MaxCorrectionCap = 10

// ValidateConfig configures schema validation for NodeKindValidate transitions.
// Implements Building Block 4 (Validation).
type ValidateConfig struct {
	// Schema is the target type for JSON round-trip validation.
	// Supports Go struct pointers (field type checking) and json.RawMessage (JSON validity).
	// Ignored when ValidateFunc is set.
	Schema any

	// ValidateFunc is a custom validation function.
	// Takes precedence over Schema when set.
	// Returns nil if valid, or an error describing the failure.
	ValidateFunc func(payload any) error

	// MaxCorrections is the maximum number of LLM correction attempts.
	// 0 means no corrections — validation failure is immediate.
	// Clamped to MaxCorrectionCap (10) at runtime.
	MaxCorrections int

	// CorrectionLLMID is the Transition ID of the LLM used for corrections.
	// If empty, no correction is attempted regardless of MaxCorrections.
	CorrectionLLMID string

	// OnSuccess is an optional transform applied after successful validation.
	// If nil, the payload passes through unchanged.
	OnSuccess func(validated any) any
}

// correctionSystemPrompt is the system prompt for correction LLM calls.
const correctionSystemPrompt = "You are a JSON correction assistant. Fix the JSON to match the schema. Return ONLY valid JSON, no explanation."

// fireValidate executes a NodeKindValidate transition.
//
// Flow:
//  1. Validate consumed token payload (ValidateFunc or Schema round-trip)
//  2. On success: apply OnSuccess transform, deposit to OutputPlaces
//  3. On failure + corrections available: call correction LLM, re-validate
//  4. On failure + corrections exhausted: ErrorPlace routing or ErrValidationFailed
func fireValidate(ctx context.Context, t *Transition, c *CPN, consumed []Token) error {
	if t.ValidateConfig == nil {
		return fmt.Errorf("transition %s: nil ValidateConfig", t.ID)
	}

	if len(consumed) == 0 {
		return fmt.Errorf("transition %s: no consumed tokens", t.ID)
	}

	cfg := t.ValidateConfig
	payload := consumed[0].Payload
	originalColor := consumed[0].Color

	// Step 1: Validate payload.
	err := validatePayload(payload, cfg)
	if err == nil {
		// Valid on first try — deposit.
		return depositValidated(t, c, payload, originalColor, cfg)
	}

	// Step 2: Correction loop.
	maxCorr := min(cfg.MaxCorrections, MaxCorrectionCap)

	if maxCorr <= 0 || cfg.CorrectionLLMID == "" {
		// No corrections available — route error.
		return routeValidationError(t, c, err, payload)
	}

	currentPayload := payload
	lastErr := err

	for i := range maxCorr {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Call correction LLM.
		corrected, corrErr := callCorrectionLLM(ctx, c, cfg.CorrectionLLMID, currentPayload, lastErr.Error())
		if corrErr != nil {
			return fmt.Errorf("transition %s: correction LLM (iteration %d): %w", t.ID, i+1, corrErr)
		}

		// Re-validate corrected output.
		currentPayload = corrected
		lastErr = validatePayload(currentPayload, cfg)
		if lastErr == nil {
			// Correction succeeded — deposit.
			return depositValidated(t, c, currentPayload, originalColor, cfg)
		}
	}

	// All corrections exhausted.
	return routeValidationError(t, c, lastErr, payload)
}

// depositValidated applies OnSuccess and deposits to all OutputPlaces.
func depositValidated(t *Transition, c *CPN, payload any, color ColorSet, cfg *ValidateConfig) error {
	if cfg.OnSuccess != nil {
		payload = cfg.OnSuccess(payload)
	}

	result := Token{
		Color:       color,
		Payload:     payload,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindValidate,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}

	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return fmt.Errorf("transition %s: output place %s not found", t.ID, pid)
		}
		tok := result // copy per output place
		tok.Space = p.Space
		if err := p.Deposit(&tok); err != nil {
			return fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return nil
}

// routeValidationError deposits a ColorError token to ErrorPlace or returns ErrValidationFailed.
func routeValidationError(t *Transition, c *CPN, validationErr error, originalPayload any) error {
	if t.ErrorPlace == "" {
		return fmt.Errorf("transition %s: %w: %s", t.ID, ErrValidationFailed, validationErr.Error())
	}

	ep, ok := c.Places[t.ErrorPlace]
	if !ok {
		return fmt.Errorf("transition %s: error place %s not found", t.ID, t.ErrorPlace)
	}

	errPayload := map[string]any{
		"error":            validationErr.Error(),
		"original_payload": originalPayload,
	}

	errToken := &Token{
		Color:       ColorError,
		Payload:     errPayload,
		Space:       ep.Space,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindValidate,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}

	if err := ep.Deposit(errToken); err != nil {
		return fmt.Errorf("transition %s: deposit to error place %s: %w", t.ID, t.ErrorPlace, err)
	}

	return nil
}

// validatePayload dispatches to ValidateFunc or validateAgainstSchema.
func validatePayload(payload any, cfg *ValidateConfig) error {
	if cfg.ValidateFunc != nil {
		return cfg.ValidateFunc(payload)
	}
	if cfg.Schema != nil {
		return validateAgainstSchema(payload, cfg.Schema)
	}
	// Both nil — always valid (no-op validation).
	return nil
}

// validateAgainstSchema validates via JSON round-trip into the schema type.
// Marshals the payload to JSON, then unmarshals into a zero-value of the schema type.
func validateAgainstSchema(payload, schema any) error {
	// Marshal the payload to JSON.
	var jsonBytes []byte
	var err error

	switch v := payload.(type) {
	case string:
		jsonBytes = []byte(v)
	case []byte:
		jsonBytes = v
	case json.RawMessage:
		jsonBytes = []byte(v)
	default:
		jsonBytes, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("cannot marshal payload to JSON: %w", err)
		}
	}

	// Create a zero-value of the schema type and unmarshal into it.
	schemaType := reflect.TypeOf(schema)
	if schemaType.Kind() == reflect.Pointer {
		schemaType = schemaType.Elem()
	}
	target := reflect.New(schemaType).Interface()

	if err := json.Unmarshal(jsonBytes, target); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// callCorrectionLLM sends the invalid payload to a correction LLM.
// Uses c.LLMClient.Complete() directly with a focused correction prompt.
func callCorrectionLLM(ctx context.Context, c *CPN, correctionLLMID string, invalidPayload any, schemaError string) (any, error) {
	if c.LLMClient == nil {
		return nil, fmt.Errorf("nil LLMClient on CPN")
	}

	prompt := buildCorrectionPrompt(invalidPayload, schemaError)

	messages := []*LLMMessage{
		{Role: "system", Content: correctionSystemPrompt},
		{Role: "user", Content: prompt},
	}

	// Look up the correction LLM transition for model config.
	var model string
	var maxTokens int
	if corrTrans, ok := c.Transitions[correctionLLMID]; ok && corrTrans.LLMConfig != nil {
		model = corrTrans.LLMConfig.Model
		maxTokens = corrTrans.LLMConfig.MaxTokens
	}
	if model == "" {
		model = "structured"
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	req := &LLMRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: 0,
		ResponseFmt: "json_object",
		SessionID:   c.SessionID,
	}

	resp, err := c.LLMClient.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Strip code fences and return the corrected output.
	corrected := stripCodeFences(resp.Content)
	return corrected, nil
}

// buildCorrectionPrompt formats the correction request for the LLM.
func buildCorrectionPrompt(invalidPayload any, schemaError string) string {
	var payloadStr string
	switch v := invalidPayload.(type) {
	case string:
		payloadStr = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			payloadStr = fmt.Sprintf("%v", v)
		} else {
			payloadStr = string(b)
		}
	}

	return fmt.Sprintf("The following JSON output is invalid:\n\n---\n%s\n---\n\nSchema validation error: %s\n\nFix the JSON to match the expected schema. Return ONLY the corrected JSON, no explanation or markdown.",
		payloadStr, schemaError)
}

// stripCodeFences removes markdown code fences from LLM output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)

	// Remove ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		// Find end of first line (the opening fence).
		idx := strings.Index(s, "\n")
		if idx != -1 {
			s = s[idx+1:]
		}
		// Remove closing fence.
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}

	return s
}
