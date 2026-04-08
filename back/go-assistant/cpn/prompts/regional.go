// Package prompts holds shared, language- and region-aware prompt fragments
// used by CPN LLM transitions. The regional registry maps BCP-47 variant
// codes to a short "user context" preamble that the engine prepends to a
// transition's static SystemPrompt at invocation time, so the LLM can
// disambiguate idioms in the speaker's regional register without bloating
// the global prompt.
package prompts

// VariantInfo describes a single supported regional variant.
type VariantInfo struct {
	// Lang is the BCP-47 base language ("es", "en").
	Lang string
	// Label is a human-readable name shown in onboarding.
	Label string
	// Preamble is the LLM-facing context block; see PreambleFor.
	Preamble string
}

// SupportedVariants is the canonical, exhaustive set of regional variants
// the platform recognises. Any value not in this map MUST be rejected by
// the onboarding handler (SEC-002).
var SupportedVariants = map[string]VariantInfo{
	"es-CO": {Lang: "es", Label: "Español (Colombia)", Preamble: preambleEsCO},
	"es-MX": {Lang: "es", Label: "Español (México)", Preamble: preambleEsMX},
	"es-AR": {Lang: "es", Label: "Español (Argentina)", Preamble: preambleEsAR},
	"es-ES": {Lang: "es", Label: "Español (España)", Preamble: preambleEsES},
	"en-GB": {Lang: "en", Label: "English (UK)", Preamble: preambleEnGB},
	"en-US": {Lang: "en", Label: "English (US)", Preamble: preambleEnUS},
	"en-AU": {Lang: "en", Label: "English (Australia)", Preamble: preambleEnAU},
}

// IsSupported reports whether code is an exact (case-sensitive) match for
// a known variant. Codes like "es-co" or "pt-BR" are rejected.
func IsSupported(code string) bool {
	if code == "" {
		return false
	}
	_, ok := SupportedVariants[code]
	return ok
}

// DefaultVariant returns the language-default variant code for a given
// base language. Unknown languages collapse to "es-CO" (REQ-008 fallback).
func DefaultVariant(lang string) string {
	switch lang {
	case "en":
		return "en-GB"
	case "es":
		return "es-CO"
	default:
		return "es-CO"
	}
}

// PreambleFor returns the rendered preamble for a variant code. If code is
// unknown or empty, the global default (es-CO) is returned so callers never
// get an empty string.
func PreambleFor(variant string) string {
	if v, ok := SupportedVariants[variant]; ok {
		return v.Preamble
	}
	return SupportedVariants[DefaultVariant("")].Preamble
}

// Preambles. Each is descriptive (GUD-001), lists 2–4 disambiguating idioms
// (GUD-002), and stays under ~80 tokens (CON-004 ≈ 320 chars).

const preambleEsCO = `USER CONTEXT — REGIONAL REGISTER
The user writes in Español (Colombia). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "cuenta / cuéntame": often "tell me the story / spill it", not "count".
- "regálame X": polite "please pass me X", not literal gifting.
- "ahorita": vague near-future, not "right now".
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEsMX = `USER CONTEXT — REGIONAL REGISTER
The user writes in Español (México). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "ahorita": vague soon, not literally "right now".
- "mande": polite "pardon? / what did you say?", not a command.
- "órale": broad reaction word — surprise, agreement, or encouragement.
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEsAR = `USER CONTEXT — REGIONAL REGISTER
The user writes in Español (Argentina). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "che": vocative filler ("hey"), not a name.
- "bárbaro / genial": "great / cool", not literal.
- "tipo": hedge meaning "like / sort of", not "type".
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEsES = `USER CONTEXT — REGIONAL REGISTER
The user writes in Español (España). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "vale": "ok / got it", a discourse marker, not "it is worth".
- "venga": "come on / alright", not the verb "to come".
- "tío / tía": "dude / mate", not literal aunt/uncle.
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEnGB = `USER CONTEXT — REGIONAL REGISTER
The user writes in English (UK). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "you alright?": a greeting, not a welfare check.
- "cheers": "thanks / bye", not a toast.
- "quite good": mildly positive, sometimes lukewarm — calibrate accordingly.
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEnUS = `USER CONTEXT — REGIONAL REGISTER
The user writes in English (US). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "what's up?": a greeting, not a literal question.
- "I could care less": idiomatic "I don't care", same meaning as "couldn't".
- "real quick": hedge meaning "briefly", not a speed instruction.
Use this only to interpret intent and tone, not to imitate the dialect.`

const preambleEnAU = `USER CONTEXT — REGIONAL REGISTER
The user writes in English (Australia). In this register, treat the following as conversational unless the broader sentence clearly demands otherwise:
- "how ya going?": a greeting, not a travel question.
- "no worries": "you're welcome / it's fine".
- "heaps": "a lot", quantifier not literal piles.
Use this only to interpret intent and tone, not to imitate the dialect.`
