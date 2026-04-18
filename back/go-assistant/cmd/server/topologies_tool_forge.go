package main

// topologies_tool_forge.go — GAP-5 tool-forge CPN.
//
// Spec: spec/spec-architecture-tool-forge-cpn.md
//
// Shape:
//
//   p-forge-request ──► t-probe-compiler ──► p-compiler-choice
//                                              │
//                                              ▼
//                       p-forge-request ──► t-design-spec ──► p-spec
//                                                                │
//                                                                ▼
//                                              p-spec ──► t-emit-source ──► p-source
//                                                                              │
//                                                                              ▼
//                                              p-source ──► t-write-source ──► p-source-written
//                                                                                 │
//                                                                                 ▼
//                                         p-source-written ──► t-compile ──► p-compiled-binary
//                                                                                │
//                                                                                ▼
//                                       p-compiled-binary ──► t-smoke-test ──► p-smoke-ok
//                                                                                │
//                                                         p-spec ──► t-write-helptext ──► p-help-text
//                                                         p-spec ──► t-write-manpage  ──► p-man-page
//                                                                                │
//                                        p-smoke-ok + p-help-text + p-man-page ──► t-build-manifest ──► p-manifest
//                                                                                                          │
//                                                                 p-manifest ──► t-register ──► p-registered
//
// All transitions route errors to p-forge-errors.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// ToolForgeFlowName is the FlowRegistry name for this topology.
const ToolForgeFlowName = "tool-forge"

// Forge place IDs (exported for tests).
const (
	PlaceForgeRequest        = "p-forge-request"
	PlaceForgeCompilerChoice = "p-compiler-choice"
	PlaceForgeSpec           = "p-spec"
	PlaceForgeSource         = "p-source"
	PlaceForgeSourceWritten  = "p-source-written"
	PlaceForgeCompiledBinary = "p-compiled-binary"
	PlaceForgeHelpText       = "p-help-text"
	PlaceForgeManPage        = "p-man-page"
	PlaceForgeSmokeOK        = "p-smoke-ok"
	PlaceForgeManifest       = "p-manifest"
	PlaceForgeRegistered     = "p-registered"
	PlaceForgeErrors         = "p-forge-errors"
)

// compileTimeout is the per-compile-step timeout per REQ-041.
const compileTimeout = 30 * time.Second

// smokeTimeout is the --help probe timeout per REQ-050.
const smokeTimeout = 2 * time.Second

// ToolForgeDeps captures the collaborators the forge topology needs at
// construction time.
type ToolForgeDeps struct {
	HostCapRepo persist.HostCapabilityRepository
	HostID      string
}

// BuildToolForgeTopology constructs the tool-forge CPN.
// When deps.HostCapRepo is nil, t-probe-compiler defaults to "bash" language.
func BuildToolForgeTopology(sessionID string, deps ToolForgeDeps) *cpn.CPN {
	places := map[string]*cpn.Place{
		PlaceForgeRequest:        cpn.NewPlace(PlaceForgeRequest, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeCompilerChoice: cpn.NewPlace(PlaceForgeCompilerChoice, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeSpec:           cpn.NewPlace(PlaceForgeSpec, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeSource:         cpn.NewPlace(PlaceForgeSource, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeSourceWritten:  cpn.NewPlace(PlaceForgeSourceWritten, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeCompiledBinary: cpn.NewPlace(PlaceForgeCompiledBinary, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeHelpText:       cpn.NewPlace(PlaceForgeHelpText, cpn.ColorString, cpn.SpaceComputation),
		PlaceForgeManPage:        cpn.NewPlace(PlaceForgeManPage, cpn.ColorString, cpn.SpaceComputation),
		PlaceForgeSmokeOK:        cpn.NewPlace(PlaceForgeSmokeOK, cpn.ColorJSON, cpn.SpaceComputation),
		PlaceForgeManifest:       cpn.NewPlace(PlaceForgeManifest, cpn.ColorToolManifest, cpn.SpaceComputation),
		PlaceForgeRegistered:     cpn.NewPlace(PlaceForgeRegistered, cpn.ColorArtifact, cpn.SpaceComputation),
		PlaceForgeErrors:         cpn.NewPlace(PlaceForgeErrors, cpn.ColorError, cpn.SpaceComputation),
	}

	transitions := map[string]*cpn.Transition{}

	// t-probe-compiler: reads host capabilities (via closure), picks language.
	tProbeCompiler := cpn.NewTransition("t-probe-compiler", cpn.NodeKindTool,
		[]string{PlaceForgeRequest}, []string{PlaceForgeCompilerChoice, PlaceForgeRequest})
	tProbeCompiler.ToolHandler = buildProbeCompilerHandler(deps)
	transitions["t-probe-compiler"] = tProbeCompiler

	// t-design-spec: LLM designs the tool spec from request + compiler choice.
	tDesignSpec := cpn.NewTransition("t-design-spec", cpn.NodeKindLLM,
		[]string{PlaceForgeRequest, PlaceForgeCompilerChoice}, []string{PlaceForgeSpec})
	tDesignSpec.LLMConfig = &cpn.LLMConfig{
		MaxTokens:   2048,
		RequireJSON: true,
		StreamOutput: false,
	}
	tDesignSpec.SystemPrompt = `You are a software architect. Given a forge request and a compiler choice,
produce a concise spec JSON with keys: name, language, pseudocode, rationale, test_plan, expected_flags.
Use only the standard library of the chosen language. Keep the design under 400 LOC.
Output ONLY valid JSON.`
	transitions["t-design-spec"] = tDesignSpec

	// t-emit-source: LLM writes source code from the spec.
	tEmitSource := cpn.NewTransition("t-emit-source", cpn.NodeKindLLM,
		[]string{PlaceForgeSpec}, []string{PlaceForgeSource})
	tEmitSource.LLMConfig = &cpn.LLMConfig{
		MaxTokens:   4096,
		RequireJSON: true,
		StreamOutput: false,
	}
	tEmitSource.SystemPrompt = `You are a systems programmer. Given a tool spec, write the complete source file.
Rules: implement --help (exit 0, print usage/synopsis), use only standard library, no network calls beyond libc.
Output JSON with keys: path, content, sha256 (compute sha256 of content as hex string), language.`
	transitions["t-emit-source"] = tEmitSource

	// t-write-source: bash transition that writes source to disk.
	tWriteSource := cpn.NewTransition("t-write-source", cpn.NodeKindBash,
		[]string{PlaceForgeSource}, []string{PlaceForgeSourceWritten})
	tWriteSource.BashConfig = &cpn.BashConfig{
		Command: "sh",
		Args: []string{"-c", `
set -euo pipefail
INPUT="$BRAE_TOKEN_PAYLOAD"
CONTENT=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['content'])" 2>/dev/null || echo "")
PATH_VAL=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['path'])" 2>/dev/null || echo "")
if [ -z "$PATH_VAL" ] || [ -z "$CONTENT" ]; then
  echo '{"error":"missing path or content"}' >&2; exit 1
fi
mkdir -p "$(dirname "$PATH_VAL")"
printf '%s' "$CONTENT" > "$PATH_VAL"
echo "{\"path\":\"$PATH_VAL\",\"written\":true}"
`},
		Timeout:          10 * time.Second,
		AllowNonZeroExit: false,
	}
	transitions["t-write-source"] = tWriteSource

	// t-compile: bash transition that compiles the source.
	tCompile := cpn.NewTransition("t-compile", cpn.NodeKindBash,
		[]string{PlaceForgeSourceWritten}, []string{PlaceForgeCompiledBinary})
	tCompile.BashConfig = &cpn.BashConfig{
		Command: "sh",
		Args:    []string{"-c", buildCompileScript()},
		Timeout: compileTimeout,
		AllowNonZeroExit: false,
	}
	transitions["t-compile"] = tCompile

	// t-smoke-test: runs --help and validates output.
	tSmokeTest := cpn.NewTransition("t-smoke-test", cpn.NodeKindBash,
		[]string{PlaceForgeCompiledBinary}, []string{PlaceForgeSmokeOK})
	tSmokeTest.BashConfig = &cpn.BashConfig{
		Command: "sh",
		Args:    []string{"-c", smokeTestScript()},
		Timeout: smokeTimeout,
		AllowNonZeroExit: true,
	}
	transitions["t-smoke-test"] = tSmokeTest

	// t-write-helptext: LLM synthesises human-readable help text.
	tWriteHelpText := cpn.NewTransition("t-write-helptext", cpn.NodeKindLLM,
		[]string{PlaceForgeSpec}, []string{PlaceForgeHelpText})
	tWriteHelpText.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    512,
		RequireJSON:  false,
		StreamOutput: false,
	}
	tWriteHelpText.SystemPrompt = `Write a one-line help text for the tool described in the spec. Output ONLY the help string, no JSON.`
	transitions["t-write-helptext"] = tWriteHelpText

	// t-write-manpage: LLM synthesises Markdown man page.
	tWriteManPage := cpn.NewTransition("t-write-manpage", cpn.NodeKindLLM,
		[]string{PlaceForgeSpec}, []string{PlaceForgeManPage})
	tWriteManPage.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    1024,
		RequireJSON:  false,
		StreamOutput: false,
	}
	tWriteManPage.SystemPrompt = `Write a Markdown man page for the tool described in the spec. Include SYNOPSIS, DESCRIPTION, OPTIONS, EXAMPLES sections.`
	transitions["t-write-manpage"] = tWriteManPage

	// t-build-manifest: tool transition assembles the ToolManifest from upstream tokens.
	tBuildManifest := cpn.NewTransition("t-build-manifest", cpn.NodeKindTool,
		[]string{PlaceForgeSmokeOK, PlaceForgeHelpText, PlaceForgeManPage}, []string{PlaceForgeManifest})
	tBuildManifest.ToolHandler = buildManifestHandler()
	transitions["t-build-manifest"] = tBuildManifest

	// t-register: registers the tool in the tool registry.
	tRegister := cpn.NewTransition("t-register", cpn.NodeKindRegisterTool,
		[]string{PlaceForgeManifest}, []string{PlaceForgeRegistered})
	transitions["t-register"] = tRegister

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s-tool-forge", sessionID),
		ToolForgeFlowName,
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 8

	return c
}

// buildProbeCompilerHandler returns a ToolHandler that peeks at host
// capabilities and picks a compiler language, then passes the request through.
func buildProbeCompilerHandler(deps ToolForgeDeps) func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(ctx context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		if len(consumed) == 0 {
			return nil, fmt.Errorf("t-probe-compiler: no input token")
		}
		req := consumed[0]

		language := "bash"
		compilerPath := ""

		if deps.HostCapRepo != nil && deps.HostID != "" {
			if snap, err := deps.HostCapRepo.LatestForHost(ctx, deps.HostID); err == nil {
				if snap.HasCapability("can-compile-c") {
					language = "c"
					compilerPath = "gcc"
					// prefer clang if gcc absent
					for _, b := range snap.Binaries {
						if b.Name == "gcc" && b.Present {
							compilerPath = b.Path
							break
						}
						if b.Name == "clang" && b.Present && compilerPath == "" {
							compilerPath = b.Path
						}
					}
				} else if snap.HasCapability("can-run-python") {
					language = "python"
				}
			}
		}

		choice := map[string]string{
			"language":      language,
			"compiler_path": compilerPath,
		}
		choiceJSON, _ := json.Marshal(choice)

		choiceTok := cpn.Token{
			Color:   cpn.ColorJSON,
			Space:   cpn.SpaceComputation,
			Payload: string(choiceJSON),
		}
		return map[string]cpn.Token{
			PlaceForgeCompilerChoice: choiceTok,
			PlaceForgeRequest:        req,
		}, nil
	}
}

// buildManifestHandler assembles a RegisterToolConfig token from smoke-ok,
// help-text, and man-page tokens.
func buildManifestHandler() func(context.Context, []cpn.Token) (map[string]cpn.Token, error) {
	return func(_ context.Context, consumed []cpn.Token) (map[string]cpn.Token, error) {
		var smokePayload, helpText, manPage string
		for _, tok := range consumed {
			switch tok.Color {
			case cpn.ColorJSON:
				smokePayload = fmt.Sprintf("%v", tok.Payload)
			case cpn.ColorString:
				// first ColorString = help text, second = man page
				if helpText == "" {
					helpText = fmt.Sprintf("%v", tok.Payload)
				} else {
					manPage = fmt.Sprintf("%v", tok.Payload)
				}
			}
		}

		// Extract binary path from smoke payload JSON.
		var smokeData struct {
			BinaryPath string `json:"binary_path"`
			SHA256     string `json:"sha256"`
			Name       string `json:"name"`
		}
		_ = json.Unmarshal([]byte(smokePayload), &smokeData)

		// Derive namespace/name from binary path or smoke data.
		name := smokeData.Name
		if name == "" && smokeData.BinaryPath != "" {
			parts := strings.Split(smokeData.BinaryPath, "/")
			name = parts[len(parts)-1]
		}
		if name == "" {
			name = "unknown"
		}

		cfg := cpn.RegisterToolConfig{
			Namespace:    "brae",
			Name:         name,
			Version:      "0.1.0",
			HelpText:     helpText,
			ManPage:      manPage,
			BinaryPath:   smokeData.BinaryPath,
			BinarySHA256: smokeData.SHA256,
			Origin:       "agent-authored",
			RegisteredBy: "tool-forge",
		}

		return map[string]cpn.Token{
			PlaceForgeManifest: {
				Color:   cpn.ColorToolManifest,
				Space:   cpn.SpaceComputation,
				Payload: cfg,
			},
		}, nil
	}
}

// buildCompileScript returns the bash script for t-compile (REQ-040).
func buildCompileScript() string {
	return `
set -euo pipefail
INPUT="$BRAE_TOKEN_PAYLOAD"
SRC_PATH=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['path'])" 2>/dev/null)
LANG=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('language','bash'))" 2>/dev/null)
TOOL_NAME=$(basename "${SRC_PATH%.*}")
BIN_DIR="$HOME/.local/brae/bin"
mkdir -p "$BIN_DIR"
BIN_PATH="$BIN_DIR/$TOOL_NAME"
case "$LANG" in
  c)
    gcc -O2 -Wall -Werror -o "$BIN_PATH" "$SRC_PATH" 2>&1
    ;;
  python)
    cp "$SRC_PATH" "$BIN_PATH"
    chmod 0755 "$BIN_PATH"
    ;;
  bash|sh|*)
    cp "$SRC_PATH" "$BIN_PATH"
    chmod 0755 "$BIN_PATH"
    ;;
esac
SHA256=$(sha256sum "$BIN_PATH" | awk '{print $1}')
SIZE=$(stat -c%s "$BIN_PATH")
echo "{\"binary_path\":\"$BIN_PATH\",\"sha256\":\"$SHA256\",\"size_bytes\":$SIZE,\"name\":\"$TOOL_NAME\"}"
`
}

// smokeTestScript returns the bash script for t-smoke-test (REQ-050).
func smokeTestScript() string {
	return `
set -uo pipefail
INPUT="$BRAE_TOKEN_PAYLOAD"
BIN_PATH=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['binary_path'])" 2>/dev/null)
NAME=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('name','tool'))" 2>/dev/null)
SHA256=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('sha256',''))" 2>/dev/null)
HELP_OUTPUT=$("$BIN_PATH" --help 2>&1 || true)
if echo "$HELP_OUTPUT" | head -c 2000 | grep -qiE 'usage|synopsis|options'; then
  echo "{\"ok\":true,\"binary_path\":\"$BIN_PATH\",\"sha256\":\"$SHA256\",\"name\":\"$NAME\"}"
else
  echo "{\"ok\":false,\"reason\":\"--help did not match usage/synopsis/options\"}" >&2
  exit 1
fi
`
}
