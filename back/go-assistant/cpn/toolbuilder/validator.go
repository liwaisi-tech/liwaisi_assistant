package toolbuilder

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ActionOutputValidator validates an LLM output against an ActionSpec's
// OutputSchema before the token is deposited into the output place.
//
// PR1 shipped NoOpValidator. PR2 ships StrictJSONValidator which
// enforces: well-formed JSON, no unknown top-level fields, required
// fields present, and action-specific structural constraints declared
// via StructuralRules. PR3+ can swap in a full JSON-schema library if
// the constraint surface grows beyond what StructuralRules expresses.
//
// Validators are stateless and must be safe for concurrent calls.
type ActionOutputValidator interface {
	Validate(action ActionSpec, rawOutput []byte) error
}

// NoOpValidator accepts every payload. Retained for tests and as a
// safety default when no strict validator is installed.
type NoOpValidator struct{}

func (NoOpValidator) Validate(_ ActionSpec, _ []byte) error { return nil }

// StructuralRules declares the per-action constraints StrictJSONValidator
// enforces. Kept as a small Go struct rather than embedding a full
// JSON-schema library: the PR2 action set has a uniform-enough shape
// that a hand-rolled check is clearer than a schema engine.
type StructuralRules struct {
	// TopLevelRequired are top-level JSON fields that MUST be present.
	TopLevelRequired []string
	// TopLevelAllowed is the CLOSED set of permitted top-level fields.
	// additionalProperties=false is enforced against this set. Empty
	// means "any field allowed at top level" (only required-field
	// enforcement runs).
	TopLevelAllowed []string
	// MaxBytes caps the raw payload size. 0 means no cap.
	MaxBytes int
}

// rulesByAction maps ActionID → StructuralRules. Actions not in the
// map fall through to a forgiving "must be valid JSON" check.
var rulesByAction = map[string]StructuralRules{
	ActionReviewSpec: {
		TopLevelRequired: []string{"role", "approved", "blocking_issues", "suggestions"},
		TopLevelAllowed:  []string{"role", "approved", "blocking_issues", "suggestions", "confidence"},
		MaxBytes:         16 * 1024,
	},
	ActionReviewCode: {
		TopLevelRequired: []string{"role", "approved", "findings"},
		TopLevelAllowed:  []string{"role", "approved", "findings", "style_notes", "suggested_patches"},
		MaxBytes:         128 * 1024,
	},
	ActionRefineSpec: {
		TopLevelRequired: []string{"name", "purpose", "refine_count"},
		TopLevelAllowed:  []string{"name", "purpose", "non_goals", "domain_model", "ports", "adapters", "cli_surface", "test_plan", "risks", "refine_count"},
		MaxBytes:         32 * 1024,
	},
	ActionDecomposeTotals: {
		TopLevelRequired: []string{"spec_name", "totals"},
		TopLevelAllowed:  []string{"spec_name", "totals"},
		MaxBytes:         32 * 1024,
	},
	ActionPlanSubtasks: {
		TopLevelRequired: []string{"spec_name", "by_total"},
		TopLevelAllowed:  []string{"spec_name", "by_total"},
		MaxBytes:         64 * 1024,
	},
}

// StrictJSONValidator enforces the rules above. It is the default
// validator installed by NewSubAgentCatalog when called with
// CatalogOption-strict (see registry.go).
type StrictJSONValidator struct{}

func (StrictJSONValidator) Validate(action ActionSpec, raw []byte) error {
	rules, ok := rulesByAction[action.ID]
	if !ok {
		// Unknown action: require well-formed JSON only. This keeps the
		// validator forward-compatible with actions added before their
		// rules are registered, while still catching garbage output.
		var any map[string]any
		if err := json.Unmarshal(raw, &any); err != nil {
			return fmt.Errorf("validate %s: not a JSON object: %w", action.ID, err)
		}
		return nil
	}
	if rules.MaxBytes > 0 && len(raw) > rules.MaxBytes {
		return fmt.Errorf("validate %s: payload %d bytes exceeds max %d", action.ID, len(raw), rules.MaxBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("validate %s: not a JSON object: %w", action.ID, err)
	}
	// Required-field check.
	var missing []string
	for _, f := range rules.TopLevelRequired {
		if _, ok := obj[f]; !ok {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("validate %s: missing required fields: %s", action.ID, strings.Join(missing, ", "))
	}
	// additionalProperties=false enforcement.
	if len(rules.TopLevelAllowed) > 0 {
		allowed := make(map[string]struct{}, len(rules.TopLevelAllowed))
		for _, f := range rules.TopLevelAllowed {
			allowed[f] = struct{}{}
		}
		var unknown []string
		for k := range obj {
			if _, ok := allowed[k]; !ok {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			return fmt.Errorf("validate %s: unknown fields: %s", action.ID, strings.Join(unknown, ", "))
		}
	}
	return nil
}
