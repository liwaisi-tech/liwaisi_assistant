// Package helpparse implements SC-11: per-binary --help / -h introspection +
// LLM-powered schema extraction. Given a set of binaries discovered during
// awakening that lack a known manifest, Compose builds an ephemeral sub-CPN
// that invokes each binary's help, merges long/short variants (AND-join best-
// effort per REQ-1101), asks the LLM to extract a structured HelpSchema,
// validates against an embedded JSON schema (REQ-1103), and emits one
// HelpSchema per successfully-parsed binary. Parse failures are silently
// skipped so awakening never aborts (REQ-1104).
//
// SC-11 is library-only: Compose is consumed by SC-12 (manifest synthesis),
// NOT by the root awakening topology. Wiring happens in a later chunk.
package helpparse

import (
	"context"
	"errors"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/fanout"
)

const (
	MaxBinaries         = 16
	DefaultHelpTimeout  = 3 * time.Second
	MinHelpTimeout      = 200 * time.Millisecond
	MaxHelpOutputBytes  = 64 * 1024
	HelpVariantLong     = "--help"
	HelpVariantShort    = "-h"
	GateDenyExitCode    = -2
	TimeoutExitCode     = -1
)

const (
	PlaceTriggerID           = "p-help-trigger"
	PlaceHelpLongPrefix      = "p-help-long-"
	PlaceHelpShortPrefix     = "p-help-short-"
	PlaceHelpMergedPrefix    = "p-help-merged-"
	PlaceHelpParsedPrefix    = "p-help-parsed-"
	PlaceHelpResultPrefix    = "p-help-result-"
	TransitionInvokeLongPrefix  = "t-help-invoke-long-"
	TransitionInvokeShortPrefix = "t-help-invoke-short-"
	TransitionMergePrefix       = "t-help-merge-"
	TransitionLLMParsePrefix    = "t-help-llm-parse-"
	TransitionValidatePrefix    = "t-help-validate-"
)

// HelpInput is one binary the awakening flow wants parsed.
type HelpInput struct {
	Binary string
	Path   string
}

// HelpRaw carries the raw help output captured by the invoke transitions.
type HelpRaw struct {
	Binary   string
	HelpText string
	Variant  string
	ExitCode int
	Err      string
}

// HelpFlag is one parsed flag entry.
type HelpFlag struct {
	Long  string `json:"long"`
	Short string `json:"short,omitempty"`
	Arg   bool   `json:"arg,omitempty"`
	Doc   string `json:"doc,omitempty"`
}

// HelpSub is one parsed subcommand entry.
type HelpSub struct {
	Name string `json:"name"`
	Doc  string `json:"doc,omitempty"`
}

// HelpSchema is the LLM-emitted structured extract consumed by SC-12.
type HelpSchema struct {
	Binary       string     `json:"binary"`
	Flags        []HelpFlag `json:"flags"`
	Subcommands  []HelpSub  `json:"subcommands"`
	Examples     []string   `json:"examples"`
	SourceSHA256 string     `json:"source_sha256"`
}

// HelpResult is the per-binary reducer input. Success carries Schema;
// failure carries Err with Stage telling the reducer WHY it was dropped.
type HelpResult struct {
	Binary string
	Schema HelpSchema
	Err    string
	Stage  string
}

// Deps are the dependencies wired into the sub-CPN by the caller.
type Deps struct {
	HostAdapter cpn.HostAdapter
	HostGate    cpn.HostGate
	Sandbox     fanout.Sandbox
	LLM         cpn.LLMClient
	Chain       FallbackRunner
	Clock       func() time.Time
	Timeout     time.Duration

	OnHelpInvoked func(ctx context.Context, binary, variant string, duration time.Duration, exitCode int)
	OnHelpParsed  func(ctx context.Context, binary string, flagCount, subCount int)
	OnHelpFailed  func(ctx context.Context, binary, stage, reason string)
}

// FallbackRunner abstracts the SC-15 FallbackChain contract. The concrete
// awakens.FallbackChain satisfies it; tests can plug in their own runner
// without importing the awakens package and introducing an import cycle.
type FallbackRunner interface {
	Run(ctx context.Context, exec func(ctx context.Context, model string) error) error
}

// ErrNoBinaries is returned by Compose when binaries is empty.
var ErrNoBinaries = errors.New("helpparse: no binaries to parse")

// ErrTooManyBinaries is returned when len(binaries) exceeds MaxBinaries.
var ErrTooManyBinaries = errors.New("helpparse: binary count exceeds max")

// ErrNoHelpOutput is returned when both help variants failed for a binary.
var ErrNoHelpOutput = errors.New("helpparse: both --help and -h produced no output")

// ErrSchemaValidation is returned when the LLM output fails validation.
var ErrSchemaValidation = errors.New("helpparse: schema validation failed")

// helpRawToken wraps a HelpRaw into a ColorArtifact token.
func helpRawToken(r HelpRaw) cpn.Token {
	return cpn.Token{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: r}
}

// helpResultToken wraps a HelpResult into a ColorArtifact token.
func helpResultToken(r HelpResult) cpn.Token {
	return cpn.Token{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: r}
}

// helpSchemaToken wraps a HelpSchema into a ColorArtifact token.
func helpSchemaToken(s HelpSchema) cpn.Token {
	return cpn.Token{Color: cpn.ColorArtifact, Space: cpn.SpaceComputation, Payload: s}
}
