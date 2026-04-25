package helpparse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// makeInvokeExecutor returns a closure that runs a single help variant
// (--help or -h) for `binary` through the sandbox + host-gate. Any failure
// (gate deny, adapter error, timeout, non-zero exit with empty stdout) is
// captured as a HelpRaw with an Err field rather than a Go error — the
// AND-join-then-merge stage tolerates per-variant failure and only bails when
// BOTH variants are empty.
func makeInvokeExecutor(binary, path, variant string, deps Deps) func(context.Context, cpn.Token) (cpn.Token, error) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	timeout := deps.Timeout
	if timeout <= 0 {
		timeout = DefaultHelpTimeout
	}
	if timeout < MinHelpTimeout {
		timeout = MinHelpTimeout
	}
	return func(ctx context.Context, _ cpn.Token) (cpn.Token, error) {
		raw := HelpRaw{Binary: binary, Variant: variant}
		if deps.HostAdapter == nil {
			raw.Err = "host adapter not wired"
			raw.ExitCode = GateDenyExitCode
			return helpRawToken(raw), nil
		}
		command := fmt.Sprintf("%s %s", path, variant)
		if deps.HostGate != nil {
			gateOp := cpn.GateOp{
				Kind:    "exec",
				Command: command,
				Origin:  cpn.GateOriginAwakensNative,
			}
			if err := deps.HostGate.Check(ctx, gateOp); err != nil {
				raw.Err = "gate: " + err.Error()
				raw.ExitCode = GateDenyExitCode
				if deps.OnHelpInvoked != nil {
					deps.OnHelpInvoked(ctx, binary, variant, 0, GateDenyExitCode)
				}
				return helpRawToken(raw), nil
			}
		}
		execCmd, execArgs := path, []string{variant}
		if !deps.Sandbox.IsZero() {
			execCmd, execArgs = deps.Sandbox.Wrap(execCmd, execArgs)
		}
		start := clock()
		res, err := deps.HostAdapter.Exec(ctx, cpn.ExecRequest{
			Command:      execCmd,
			Args:         execArgs,
			Timeout:      timeout,
			AllowNonZero: true,
		})
		elapsed := clock().Sub(start)
		if err != nil {
			if isTimeoutErr(ctx, err) {
				raw.ExitCode = TimeoutExitCode
				raw.Err = "timeout"
			} else {
				raw.ExitCode = TimeoutExitCode
				raw.Err = "exec: " + err.Error()
			}
			if deps.OnHelpInvoked != nil {
				deps.OnHelpInvoked(ctx, binary, variant, elapsed, raw.ExitCode)
			}
			return helpRawToken(raw), nil
		}
		raw.ExitCode = res.ExitCode
		// Many CLIs write --help to stdout; some (older GNU) to stderr.
		// Prefer stdout, fall back to stderr if stdout is empty.
		text := string(res.Stdout)
		if strings.TrimSpace(text) == "" {
			text = string(res.Stderr)
		}
		if len(text) > MaxHelpOutputBytes {
			text = text[:MaxHelpOutputBytes]
		}
		raw.HelpText = text
		if deps.OnHelpInvoked != nil {
			deps.OnHelpInvoked(ctx, binary, variant, elapsed, raw.ExitCode)
		}
		return helpRawToken(raw), nil
	}
}

// makeMergeHandler consumes the two variant tokens (long + short) and emits
// ONE merged HelpRaw whose HelpText is the AND-join (long first, then short,
// separated by "\n"). If both variants are empty, the merged token carries an
// Err marker so the downstream LLM-parse stage skips it.
func makeMergeHandler(binary string, outPlace string) func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var longRaw, shortRaw HelpRaw
		for _, tok := range consumed {
			r, ok := tok.Payload.(HelpRaw)
			if !ok {
				return nil, fmt.Errorf("help-merge[%s]: token payload %T", binary, tok.Payload)
			}
			switch r.Variant {
			case HelpVariantLong:
				longRaw = r
			case HelpVariantShort:
				shortRaw = r
			}
		}
		merged := HelpRaw{Binary: binary, Variant: "merged"}
		longText := strings.TrimSpace(longRaw.HelpText)
		shortText := strings.TrimSpace(shortRaw.HelpText)
		if longText == "" && shortText == "" {
			merged.Err = "no help output"
			merged.ExitCode = GateDenyExitCode
		} else {
			// Canonical byte-stable concatenation for SourceSHA256.
			merged.HelpText = longRaw.HelpText + "\n" + shortRaw.HelpText
		}
		return map[string]cpn.Token{outPlace: helpRawToken(merged)}, nil
	}
}

// sourceSHA256 returns the REQ-for-SC-12 deterministic hash of the raw help
// text (long || "\n" || short). Exported for tests.
func sourceSHA256(longText, shortText string) string {
	sum := sha256.Sum256([]byte(longText + "\n" + shortText))
	return hex.EncodeToString(sum[:])
}

// makeLLMParseHandler builds the handler for the LLM-parse transition. It
// reads the merged HelpRaw, runs the FallbackChain to call the LLM, parses
// and validates the response, and emits a HelpResult on the result place.
//
// Failures at any stage are surfaced as HelpResult{Err, Stage} rather than
// hard errors — the reducer filters them out (REQ-1104).
func makeLLMParseHandler(binary, parsedPlace string, deps Deps) func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var merged HelpRaw
		for _, tok := range consumed {
			r, ok := tok.Payload.(HelpRaw)
			if !ok {
				return nil, fmt.Errorf("help-llm[%s]: token %T", binary, tok.Payload)
			}
			merged = r
		}
		// Split merged back into long/short for deterministic SourceSHA256.
		parts := strings.SplitN(merged.HelpText, "\n", 2)
		longPart := merged.HelpText
		shortPart := ""
		if len(parts) == 2 {
			longPart, shortPart = parts[0], parts[1]
		}
		hash := sourceSHA256(longPart, shortPart)

		result := HelpResult{Binary: binary}
		if merged.Err != "" || strings.TrimSpace(merged.HelpText) == "" {
			result.Err = ErrNoHelpOutput.Error()
			result.Stage = "invoke"
			if deps.OnHelpFailed != nil {
				deps.OnHelpFailed(ctx, binary, result.Stage, result.Err)
			}
			return map[string]cpn.Token{parsedPlace: helpResultToken(result)}, nil
		}

		var rawJSON []byte
		execFn := func(ctx context.Context, model string) error {
			if deps.LLM == nil {
				return errors.New("llm client not wired")
			}
			req := &cpn.LLMRequest{
				Model:       model,
				Temperature: 0,
				MaxTokens:   2048,
				ResponseFmt: "json_object",
				Messages: []*cpn.LLMMessage{
					{Role: "system", Content: helpParseSystemPrompt(binary)},
					{Role: "user", Content: merged.HelpText},
				},
			}
			resp, err := deps.LLM.Complete(ctx, req)
			if err != nil {
				return err
			}
			rawJSON = []byte(resp.Content)
			return nil
		}
		var parseErr error
		if deps.Chain != nil {
			parseErr = deps.Chain.Run(ctx, execFn)
		} else {
			parseErr = execFn(ctx, "")
		}
		if parseErr != nil {
			result.Err = parseErr.Error()
			result.Stage = "llm"
			if deps.OnHelpFailed != nil {
				deps.OnHelpFailed(ctx, binary, result.Stage, result.Err)
			}
			return map[string]cpn.Token{parsedPlace: helpResultToken(result)}, nil
		}

		schema, verr := ValidateSchemaJSON(rawJSON)
		if verr != nil {
			result.Err = verr.Error()
			result.Stage = "validate"
			if deps.OnHelpFailed != nil {
				deps.OnHelpFailed(ctx, binary, result.Stage, result.Err)
			}
			return map[string]cpn.Token{parsedPlace: helpResultToken(result)}, nil
		}
		// Prefer the LLM-provided binary label only if it matches; otherwise
		// overwrite with the authoritative input name.
		if schema.Binary != binary {
			schema.Binary = binary
		}
		schema.SourceSHA256 = hash
		result.Schema = schema
		if deps.OnHelpParsed != nil {
			deps.OnHelpParsed(ctx, binary, len(schema.Flags), len(schema.Subcommands))
		}
		return map[string]cpn.Token{parsedPlace: helpResultToken(result)}, nil
	}
}

// makeValidateHandler is intentionally a thin pass-through: the real
// validation runs inline in makeLLMParseHandler so a validation failure can
// record its Stage without flowing through another place. This handler just
// forwards the HelpResult to the final result place. It exists as a distinct
// transition to match the 5-step pipeline spec (REQ-1103 separation).
func makeValidateHandler(binary, outPlace string) func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var r HelpResult
		for _, tok := range consumed {
			rr, ok := tok.Payload.(HelpResult)
			if !ok {
				return nil, fmt.Errorf("help-validate[%s]: token %T", binary, tok.Payload)
			}
			r = rr
		}
		// Double-check: if Stage is "" but Schema is empty, mark invalid.
		if r.Err == "" && strings.TrimSpace(r.Schema.Binary) == "" {
			r.Err = ErrSchemaValidation.Error()
			r.Stage = "validate"
		}
		// Force-revalidate successful results — cheap, catches mutations.
		if r.Err == "" {
			blob, _ := json.Marshal(r.Schema)
			if _, err := ValidateSchemaJSON(blob); err != nil {
				r.Err = err.Error()
				r.Stage = "validate"
				r.Schema = HelpSchema{}
			}
		}
		return map[string]cpn.Token{outPlace: helpResultToken(r)}, nil
	}
}

// helpParseSystemPrompt returns the system prompt steering the LLM toward a
// strict HelpSchema JSON object. Binary is injected so the model can populate
// the "binary" field without inventing a name.
func helpParseSystemPrompt(binary string) string {
	return fmt.Sprintf(`You are an expert CLI documentation extractor.
Given the raw --help / -h output of the binary %q, emit a JSON object with
EXACTLY these keys:

  {
    "binary": %q,
    "flags": [ { "long": "--foo", "short": "-f", "arg": true, "doc": "..." } ],
    "subcommands": [ { "name": "push", "doc": "..." } ],
    "examples": [ "..." ]
  }

Rules:
- Output ONLY the JSON object. No prose, no markdown fences.
- Omit short, arg, doc when absent. Do not invent entries.
- Every flag MUST have a non-empty "long". Positional args are not flags.
- If the help text lists no subcommands, use "subcommands": [].
- If no examples are shown, use "examples": [].
`, binary, binary)
}

func isTimeoutErr(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, cpn.ErrTimeoutHost) {
		return true
	}
	return false
}
