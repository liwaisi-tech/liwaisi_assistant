package cpn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// fireSynthesize executes a NodeKindSynthesize transition (GAP-4).
//
// Flow:
//  1. Validate CPN has LLMClient + FlowRepository + SafeRegistry wired.
//  2. Render the prompt: substitute TaskPlaceholder with the consumed
//     token's payload, append the safe-primitive catalogue + JSON schema.
//  3. Call the LLM. Parse the response (fenced between <topology> tags).
//  4. Run the linter. On failure with correction budget remaining, retry
//     with the lint output appended to the prompt.
//  5. Canonicalise + persist via FlowRepository.SaveAuthored (idempotent).
//  6. Deposit a ColorFlowRef token on every OutputPlace.
//
// Spec: spec/spec-architecture-cpn-synthesis-instantiate.md §3.
func fireSynthesize(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
	if t.SynthesizeConfig == nil {
		return nil, 0, fmt.Errorf("transition %s: SynthesizeConfig missing", t.ID)
	}
	if c.LLMClient == nil {
		return nil, 0, fmt.Errorf("transition %s: no LLMClient on CPN", t.ID)
	}
	if c.FlowRepository == nil {
		return nil, 0, fmt.Errorf("transition %s: no FlowRepository on CPN", t.ID)
	}
	if c.SafeRegistry == nil {
		return nil, 0, fmt.Errorf("transition %s: no SafeRegistry on CPN", t.ID)
	}

	task := extractTaskPayload(consumed)
	catalogue := c.SafeRegistry.Catalogue()
	base := buildSynthesizePrompt(t.SynthesizeConfig, catalogue, task)

	maxCorrections := t.SynthesizeConfig.MaxCorrections
	if maxCorrections < 0 {
		maxCorrections = 0
	}

	var (
		topoJSON     json.RawMessage
		lintFeedback string
		totalCost    float64
	)
	for attempt := 0; attempt <= maxCorrections; attempt++ {
		prompt := base
		if lintFeedback != "" {
			prompt = prompt + "\n\nPREVIOUS ATTEMPT FEEDBACK:\n" + lintFeedback + "\n\nTry again."
		}
		req := &LLMRequest{
			Model:       t.SynthesizeConfig.Model,
			MaxTokens:   t.SynthesizeConfig.MaxTokens,
			Temperature: float64(t.SynthesizeConfig.Temperature),
			Messages: []*LLMMessage{
				{Role: "system", Content: prompt},
				{Role: "user", Content: task},
			},
			SessionID: c.SessionID,
		}
		resp, err := c.LLMClient.Complete(ctx, req)
		if err != nil {
			return nil, totalCost, fmt.Errorf("transition %s: LLM call: %w", t.ID, err)
		}
		totalCost += resp.CostUSD

		parsed, parseErr := parseTopologyResponse(resp.Content)
		if parseErr != nil {
			slog.WarnContext(ctx, "synthesize: parse failed",
				"transition_id", t.ID, "attempt", attempt, "error", parseErr)
			lintFeedback = fmt.Sprintf("parse error: %v", parseErr)
			if attempt == maxCorrections {
				return nil, totalCost, routeSynthErr(t, c, ErrTopologyParseFailed, parseErr.Error())
			}
			continue
		}

		result := lintTopology(parsed, c.SafeRegistry, t.SynthesizeConfig.SizeCap)
		if result.Passed() {
			topoJSON = parsed
			break
		}
		lintFeedback = fmt.Sprintf("lint errors: %v", result.Err())
		slog.WarnContext(ctx, "synthesize: lint failed",
			"transition_id", t.ID, "attempt", attempt, "feedback", lintFeedback)
		if attempt == maxCorrections {
			return nil, totalCost, routeSynthErr(t, c, result.Err(), lintFeedback)
		}
	}

	if topoJSON == nil {
		// Defensive.
		return nil, totalCost, routeSynthErr(t, c, ErrTopologyParseFailed, "empty topology")
	}

	canonical, err := canonicaliseTopology(topoJSON)
	if err != nil {
		return nil, totalCost, fmt.Errorf("transition %s: canonicalise: %w", t.ID, err)
	}

	digest := topologyDigest(canonical)
	promptSum := sha256.Sum256([]byte(base))
	prov := AuthoredFlowProvenance{
		AuthoredByCPNID:        c.ID,
		AuthoredFromPromptHash: hex.EncodeToString(promptSum[:]),
		SessionID:              c.SessionID,
	}
	summary := summaryForDigest(t.SynthesizeConfig.Summary, digest)
	flowID, _, err := c.FlowRepository.SaveAuthored(
		ctx,
		canonical,
		summary,
		digest.SizePlaces,
		digest.SizeTransitions,
		digest.ReferencedPrimitives,
		prov,
	)
	if err != nil {
		return nil, totalCost, fmt.Errorf("transition %s: save authored: %w", t.ID, err)
	}

	flowRef := FlowRef{FlowID: flowID, Summary: summary}
	outTok := Token{
		Color:       ColorFlowRef,
		Payload:     flowRef,
		OriginID:    c.ID,
		OriginDepth: c.Depth,
		OriginKind:  NodeKindSynthesize,
		SessionID:   c.SessionID,
		Timestamp:   time.Now(),
	}
	snap := outTok.Snapshot()

	for _, pid := range t.OutputPlaces {
		p, ok := c.Places[pid]
		if !ok {
			return nil, totalCost, fmt.Errorf("transition %s: output place %s missing", t.ID, pid)
		}
		tok := outTok
		tok.Space = p.Space
		if err := p.Deposit(&tok); err != nil {
			return nil, totalCost, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, err)
		}
	}

	return []TokenSnapshot{snap}, totalCost, nil
}

// routeSynthErr returns an error that the executor's ErrorPlace routing
// can deposit as a ColorError token. We don't try to shortcut the routing
// here — returning the error lets the executor apply its standard rules.
func routeSynthErr(_ *Transition, _ *CPN, err error, detail string) error {
	if detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

// extractTaskPayload pulls the task text from the first consumed token.
func extractTaskPayload(consumed []Token) string {
	if len(consumed) == 0 {
		return ""
	}
	switch v := consumed[0].Payload.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case json.RawMessage:
		return string(v)
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", consumed[0].Payload)
}

// buildSynthesizePrompt composes the LLM system prompt: the author's
// SystemPrompt (with TaskPlaceholder substituted), the machine-readable
// safe catalogue, and the CPNTopology schema hint.
func buildSynthesizePrompt(cfg *SynthesizeConfig, catalogue json.RawMessage, task string) string {
	body := cfg.SystemPrompt
	if body == "" {
		body = defaultSynthesizeSystemPrompt
	}
	if cfg.TaskPlaceholder != "" {
		body = strings.ReplaceAll(body, cfg.TaskPlaceholder, task)
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\nCATALOGUE (JSON):\n")
	if len(catalogue) == 0 {
		b.WriteString("[]")
	} else {
		b.Write(catalogue)
	}
	b.WriteString("\n\nTOPOLOGY SCHEMA (informal):\n")
	b.WriteString(topologySchemaHint)
	b.WriteString("\n\nRESPONSE FORMAT:\n")
	b.WriteString("Return your topology as JSON wrapped exactly between <topology> and </topology> delimiters. Nothing else outside the delimiters.")
	return b.String()
}

const defaultSynthesizeSystemPrompt = `You are the CPN synthesiser for brae.
Produce a Colored Petri Net topology that solves the task below.

RULES:
- You may reference only the primitives listed in the CATALOGUE below.
- Max 50 places, 50 transitions, 200 arcs.
- Respect space isolation: surface → observation → computation (no surface→computation edges except via HITL).
- Every topology MUST include at least one terminal place.
- You MUST NOT emit a register_tool transition.
`

const topologySchemaHint = `{
  "id": "string",
  "role": "string",
  "mode": "mas" | "centaurian",
  "places": { "<id>": {"id":"<id>","color":"STRING|JSON|ARTIFACT|...","space":"surface|computation|observation"} },
  "transitions": { "<id>": {"id":"<id>","kind":"tool|llm|validate|subnet|hitl","inputPlaces":["..."],"outputPlaces":["..."],"guardFunc":"<safe name>","executorFunc":"<safe name>","factoryFunc":"<safe name>"} }
}`

// parseTopologyResponse extracts JSON between <topology>...</topology>
// delimiters. Returns the raw block as a json.RawMessage.
func parseTopologyResponse(raw string) (json.RawMessage, error) {
	payload := extractTopologyBlock(raw)
	if payload == "" {
		return nil, fmt.Errorf("no <topology>...</topology> block found")
	}
	var any any
	if err := json.Unmarshal([]byte(payload), &any); err != nil {
		return nil, fmt.Errorf("decode topology: %w", err)
	}
	return json.RawMessage(payload), nil
}

func extractTopologyBlock(raw string) string {
	const open = "<topology>"
	const closeTag = "</topology>"
	i := strings.Index(raw, open)
	if i < 0 {
		return firstJSONObject(raw)
	}
	j := strings.Index(raw[i+len(open):], closeTag)
	if j < 0 {
		return firstJSONObject(raw[i+len(open):])
	}
	return strings.TrimSpace(raw[i+len(open) : i+len(open)+j])
}

func firstJSONObject(s string) string {
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func summaryForDigest(explicit string, digest TopologyDigest) string {
	if explicit != "" {
		return explicit
	}
	if digest.Role != "" {
		return digest.Role
	}
	return digest.Name
}
