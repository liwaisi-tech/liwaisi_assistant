package toolbuilder

// ActionOutputValidator validates an LLM output against an ActionSpec's
// OutputSchema before the token is deposited into the output place.
//
// PR1 ships NoOpValidator (passthrough) so the plumbing lands without
// coupling PR1 to JSON-schema tooling. PR2 adds StrictJSONValidator
// that enforces additionalProperties=false, required fields, enums,
// and maxLength — swapped in via SubAgentCatalog.Validator with zero
// changes to Compose() or any caller.
type ActionOutputValidator interface {
	Validate(action ActionSpec, rawOutput []byte) error
}

// NoOpValidator accepts every payload. Default for PR1.
type NoOpValidator struct{}

func (NoOpValidator) Validate(_ ActionSpec, _ []byte) error { return nil }
