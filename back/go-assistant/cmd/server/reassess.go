package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Env config ──────────────────────────────────────────────────────────────

// Default values for the iterative clarification loop (§4.6 of the spec).
// Mirrors the pattern of classifierConfidenceThreshold() at topologies.go:47-60.
const (
	defaultClarifyMaxRounds             = 3
	defaultReassessClarifyThreshold     = 0.40
	defaultReassessProceedThreshold     = 0.20
	defaultReassessConvergenceDeltaStop = 0.10

	minClarifyMaxRounds = 1
	maxClarifyMaxRounds = 5
)

// reassessConfig holds the env-driven tunables for the clarification loop.
// Parsed fresh on each guard firing so tests and operators can tweak values
// at runtime without rebuilding. The read cost is negligible — two strconv
// calls per transition firing, never in a hot path.
type reassessConfig struct {
	MaxRounds            int
	ClarifyThreshold     float64 // REASSESS_AMBIGUITY_THRESHOLD_CLARIFY
	ProceedThreshold     float64 // REASSESS_AMBIGUITY_THRESHOLD_PROCEED
	ConvergenceDeltaStop float64 // REASSESS_CONVERGENCE_DELTA_STOP
}

// loadReassessConfig reads the four env vars documented in spec §4.6. Invalid
// or out-of-range values fall back to defaults silently — the safety-net
// behavior is more important than shouting about a typo.
//
// CON-001: CLARIFY_MAX_ROUNDS MUST be in [1, 5]; values outside the range
// fall back to the default 3.
func loadReassessConfig() reassessConfig {
	return reassessConfig{
		MaxRounds:            parseMaxRounds(os.Getenv("CLARIFY_MAX_ROUNDS")),
		ClarifyThreshold:     parseUnitFloat(os.Getenv("REASSESS_AMBIGUITY_THRESHOLD_CLARIFY"), defaultReassessClarifyThreshold),
		ProceedThreshold:     parseUnitFloat(os.Getenv("REASSESS_AMBIGUITY_THRESHOLD_PROCEED"), defaultReassessProceedThreshold),
		ConvergenceDeltaStop: parseUnitFloat(os.Getenv("REASSESS_CONVERGENCE_DELTA_STOP"), defaultReassessConvergenceDeltaStop),
	}
}

func parseMaxRounds(s string) int {
	if s == "" {
		return defaultClarifyMaxRounds
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return defaultClarifyMaxRounds
	}
	if n < minClarifyMaxRounds || n > maxClarifyMaxRounds {
		return defaultClarifyMaxRounds
	}
	return n
}

// parseUnitFloat parses s as a float in [0.0, 1.0]; falls back to def for
// invalid or out-of-range inputs.
func parseUnitFloat(s string, def float64) float64 {
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	if f < 0.0 || f > 1.0 {
		return def
	}
	return f
}

// ── Token schemas ───────────────────────────────────────────────────────────

// roundToken is the p-round payload (§4.1). Seeded with {n:0, reset:false} on
// unified-topology instantiation (REQ-001). Incremented by t-followup
// (REQ-006/007); consumed by planning exits (REQ-008).
type roundToken struct {
	N     int  `json:"n"`
	Reset bool `json:"reset"`
}

// reassessResult is the t-reassess output (§4.2). Unused conditional keys are
// omitted by the LLM (REQ-022) — optional fields remain zero-value on parse.
type reassessResult struct {
	ResidualAmbiguity     float64  `json:"residual_ambiguity"`
	ConvergenceDelta      float64  `json:"convergence_delta"`
	MissingDimensions     []string `json:"missing_dimensions,omitempty"`
	ResolvedDimensions    []string `json:"resolved_dimensions,omitempty"`
	ContradictionDetected bool     `json:"contradiction_detected,omitempty"`
	FrustrationSignal     bool     `json:"frustration_signal,omitempty"`
	TopicShift            bool     `json:"topic_shift,omitempty"`
	Decision              string   `json:"decision,omitempty"`
	Rationale             string   `json:"rationale,omitempty"`

	NextQuestions     *questionnaireSpec `json:"next_questions,omitempty"`
	StatedAssumptions []string           `json:"stated_assumptions,omitempty"`
	UserChoice        *userChoice        `json:"user_choice,omitempty"`
}

// userChoice is the escape-hatch binary surface payload (§4.4).
type userChoice struct {
	Prompt  string             `json:"prompt"`
	Options []userChoiceOption `json:"options"`
}

type userChoiceOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ── Parsers ─────────────────────────────────────────────────────────────────

// parseRoundToken decodes the first token carrying a round-token payload.
// The payload may be:
//   - a JSON string (the canonical shape deposited by t-followup)
//   - a map[string]any (when a token is constructed in-process without a
//     JSON round-trip — e.g. the seeded initial marking)
//
// Returns (roundToken{}, false) on malformed or missing input so the caller's
// fail-open branch (REQ-011) fires.
func parseRoundToken(tokens []*cpn.Token) (roundToken, bool) {
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		if rt, ok := roundFromPayload(tok.Payload); ok {
			return rt, true
		}
	}
	return roundToken{}, false
}

func roundFromPayload(p any) (roundToken, bool) {
	switch v := p.(type) {
	case nil:
		return roundToken{}, false
	case string:
		if strings.TrimSpace(v) == "" {
			return roundToken{}, false
		}
		return roundFromJSON([]byte(v))
	case roundToken:
		return v, true
	case map[string]any:
		// The token may have been deposited without JSON round-trip.
		b, err := json.Marshal(v)
		if err != nil {
			return roundToken{}, false
		}
		return roundFromJSON(b)
	default:
		return roundToken{}, false
	}
}

// roundFromJSON parses raw bytes into a roundToken but rejects payloads that
// carry reassess-shaped keys. Without this disambiguation, a guard that feeds
// both p-reassessed and p-round tokens through parseRoundToken would parse the
// reassess JSON into a zero-value roundToken and mask the real round counter
// (they share the consumed[] slice).
func roundFromJSON(b []byte) (roundToken, bool) {
	// Quick reject: reassess payload shape. Substring match is cheap and
	// cannot collide with a legitimate p-round payload (which only carries
	// "n" and "reset").
	for _, needle := range []string{
		`"residual_ambiguity"`,
		`"missing_dimensions"`,
		`"resolved_dimensions"`,
		`"decision"`,
		`"next_questions"`,
		`"stated_assumptions"`,
		`"user_choice"`,
		`"convergence_delta"`,
		`"contradiction_detected"`,
		`"frustration_signal"`,
	} {
		if strings.Contains(string(b), needle) {
			return roundToken{}, false
		}
	}
	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		return roundToken{}, false
	}
	// Accept empty object (REQ-051: legacy rehydrated sessions pre-dating the
	// feature default to n=0). Otherwise require at least one known key.
	if len(probe) > 0 {
		if _, hasN := probe["n"]; !hasN {
			if _, hasReset := probe["reset"]; !hasReset {
				return roundToken{}, false
			}
		}
	}
	var rt roundToken
	if err := json.Unmarshal(b, &rt); err != nil {
		return roundToken{}, false
	}
	return rt, true
}

// parseReassessResult decodes the first token carrying a t-reassess JSON
// payload. Leverages extractJSONObject so reasoning-mode preambles and
// fenced code blocks are tolerated (same tolerance as t-ask).
func parseReassessResult(tokens []*cpn.Token) (reassessResult, bool) {
	for _, tok := range tokens {
		if tok == nil {
			continue
		}
		s, ok := tok.Payload.(string)
		if !ok {
			// Already-decoded result (tests may inject directly).
			if rr, ok := tok.Payload.(reassessResult); ok {
				return rr, true
			}
			continue
		}
		if strings.TrimSpace(s) == "" {
			continue
		}
		raw := extractJSONObject(s)
		if raw == "" {
			continue
		}
		var rr reassessResult
		if err := json.Unmarshal([]byte(raw), &rr); err != nil {
			continue
		}
		return rr, true
	}
	return reassessResult{}, false
}

// ── Guards ──────────────────────────────────────────────────────────────────

// guardResidualAmbiguous fires t-followup when the reassess output indicates
// the loop should re-fire t-clarify (REQ-010).
//
// Returns true iff ALL of:
//   - Parsed reassess result is well-formed.
//   - Parsed round token is well-formed.
//   - residual_ambiguity >= REASSESS_AMBIGUITY_THRESHOLD_CLARIFY
//     AND len(missing_dimensions) > 0.
//   - n + 1 < CLARIFY_MAX_ROUNDS (room for another round).
//   - decision != "proceed_to_plan" (LLM did not explicitly request proceeding).
//   - frustration_signal is NOT pre-empting (request_user_choice with a
//     frustration signal still routes through t-followup so the binary
//     surface can be emitted via t-clarify — REQ-013).
//   - NOT (contradiction_detected == true AND n >= 1) — a second
//     contradiction terminates the loop.
func guardResidualAmbiguous(tokens []*cpn.Token) bool {
	rr, ok := parseReassessResult(tokens)
	if !ok {
		return false
	}
	rt, ok := parseRoundToken(tokens)
	if !ok {
		return false
	}
	cfg := loadReassessConfig()

	// Hard cap: a third round is never permitted.
	if rt.N+1 >= cfg.MaxRounds {
		return false
	}

	// LLM explicitly requested planning.
	if strings.EqualFold(rr.Decision, "proceed_to_plan") {
		return false
	}

	// Second contradiction on round >= 1 terminates.
	if rr.ContradictionDetected && rt.N >= 1 {
		return false
	}

	// request_user_choice escapes (frustration / first contradiction) stay
	// in the loop: t-followup re-fires t-clarify with a binary surface
	// instead of a deeper questionnaire (REQ-013). The binary surface
	// itself does not consume a round budget — t-followup suppresses the
	// increment when decision == request_user_choice.
	if strings.EqualFold(rr.Decision, "request_user_choice") {
		return true
	}

	// Baseline: must have measurable residual ambiguity AND at least one
	// missing dimension to drill into.
	if rr.ResidualAmbiguity < cfg.ClarifyThreshold {
		return false
	}
	if len(rr.MissingDimensions) == 0 {
		return false
	}

	return true
}

// guardResidualResolved is the complement of guardResidualAmbiguous (REQ-011,
// REQ-012). Exactly one of the pair is enabled for any marking with both
// p-reassessed and p-round.
//
// Includes the fail-open branch (AC-007): when the reassess JSON is
// unparseable or the round token is missing, the guard returns TRUE so the
// planner fires with a synthetic assumption instead of deadlocking.
func guardResidualResolved(tokens []*cpn.Token) bool {
	return !guardResidualAmbiguous(tokens)
}
