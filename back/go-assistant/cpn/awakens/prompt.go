package awakens

import (
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// SystemPromptPlan is the first-turn awakening directive handed to the LLM.
// Per spec-architecture-brae-awakening-probe-fanout.md §3 REQ-001/REQ-009,
// the LLM MUST emit an AwakeningProbePlan (JSON) enumerating the binaries it
// wants checked — it MUST NOT execute any shell/tool itself. A deterministic
// host-side fanout runs the probes in parallel afterwards and the aggregator
// assembles the typed AwakeningReport downstream.
//
// Keep this string stable — tests hash-fingerprint it as part of the prompt
// digest on registered tools, and TestPromptPlan_ExampleIsValidPlan below
// round-trips ExamplePlan through fanout.ValidatePlan.
const SystemPromptPlan = `You have just started. You are running inside a Linux container.
You are the "brae-awakens" planner. Your ONLY job on this turn is to enumerate
the binaries and capabilities you want the host to probe. A separate,
deterministic probe-fanout runs every entry you list in parallel and returns
structured exit codes. Do NOT classify present/absent yourself. Do NOT call
any tool. Do NOT execute any shell command. List — do not run.

Output ONLY a single JSON object matching this schema (no prose, no code
fence, no commentary):

{
  "rationale": "one short sentence (<=160 chars) explaining why these probes",
  "timeout_per_probe_ms": <integer between 500 and 2000>,
  "probes": [
    {
      "id":      "cmd-<short-lowercase-slug>",
      "kind":    "binary" | "capability",
      "target":  "<binary or capability name>",
      "command": "<POSIX sh -c payload, <=256 bytes, introspection-only>"
    }
  ]
}

A separate fixed set of metadata probes (OS release, kernel, arch, shell,
user, id, home, hostname) is ALREADY appended by the composer — you do NOT
need to include those. Focus your plan on tool-presence and capability probes.

Rules for the plan:
  1. At most 12 probes total. Pick the most informative set.
  2. Each probe "id" MUST be a short lowercase slug; prefer the "cmd-<name>"
     pattern (e.g. "cmd-sh", "cmd-busybox", "cmd-python3").
  3. "kind" is "binary" for tool-presence checks, "capability" for feature
     checks (e.g. verifying /proc is readable).
  4. "command" MUST be an introspection-only POSIX command. The host gate
     only permits: uname, whoami, id, env, printenv, hostname, arch, and
     prefix patterns "command -v ", "which ", "ls ", "cat /etc/",
     "cat /proc/", "head -n ", "tail -n ", "busybox --list", "getconf ".
     Prefer "command -v <tool>" for binary probes — POSIX + busybox-safe.
  5. Do NOT compose shells (no pipes, redirects, $(), backticks, &&, ||).
  6. Do NOT attempt mutation (rm, mv, cp, chmod, chown, mkdir, touch).
  7. Do NOT read credentials (/data/secrets, ~/.ssh, .env*).
  8. "rationale" MUST be <=160 characters and a single sentence.
  9. "timeout_per_probe_ms" is per-probe; the composer clamps to [500, 2000].

Worked example — an Alpine/busybox happy-path plan with 6 probes:

` + ExamplePlan + `

Emit a plan tailored to whatever environment the session is running in; the
example above is illustrative, not a template to copy verbatim. Reply with
the JSON object alone.`

// ExamplePlan is the canonical worked example embedded in SystemPromptPlan.
// It is a ≤6-probe adaptation of spec §9.1 (the full §9.1 example is 12
// probes; we ship a shorter version to keep the prompt token budget down).
// The test TestPromptPlan_ExampleIsValidPlan round-trips this through
// fanout.ValidatePlan to catch regressions.
const ExamplePlan = `{
  "rationale": "Alpine/busybox container — verify core shell plus language runtimes.",
  "timeout_per_probe_ms": 2000,
  "probes": [
    { "id": "cmd-sh",      "kind": "binary", "target": "sh",      "command": "command -v sh" },
    { "id": "cmd-busybox", "kind": "binary", "target": "busybox", "command": "command -v busybox" },
    { "id": "cmd-bash",    "kind": "binary", "target": "bash",    "command": "command -v bash" },
    { "id": "cmd-awk",     "kind": "binary", "target": "awk",     "command": "command -v awk" },
    { "id": "cmd-python3", "kind": "binary", "target": "python3", "command": "command -v python3" },
    { "id": "cmd-git",     "kind": "binary", "target": "git",     "command": "command -v git" }
  ]
}`

// SystemPrompt is the legacy single-turn awakening directive kept as a
// fallback for any vestigial transition (e.g. `t-awaken-llm-followup`) that
// still expects the model to self-probe and emit an AwakeningReport directly.
// New wiring SHOULD use SystemPromptPlan. This constant is preserved verbatim
// so registered tools' PromptDigest remains stable until the followup
// transition is fully removed.
const SystemPrompt = `You have just started. You are running inside a Linux container.
You have access to one tool named "shell" that can invoke read-only introspection
commands. The gate limits you to: uname, whoami, id, env, printenv, hostname,
arch, and prefix patterns "command -v ", "which ", "ls ", "cat /etc/",
"cat /proc/", "head -n ", "tail -n ", "busybox --list", and "getconf ".

Do NOT attempt:
  - mutating commands (rm, mv, cp, chmod, chown, mkdir, touch, echo > ...)
  - network calls (curl, wget, git clone, ssh, scp)
  - reading credentials (/data/secrets, ~/.ssh, .env*)
  - shell composition (pipes, redirects, $(), backticks, &&, ||)

Steps:
  1. Run a minimal set of introspection commands (≤10) to learn OS, kernel,
     arch, shell, identity, and which common tools are present.
  2. Prefer "command -v <tool>" over "which <tool>"; it is POSIX + busybox-safe.
  3. Report both what is present AND what is notably absent.
  4. Do NOT claim a tool is present unless "command -v <tool>" returned 0.
  5. Keep narrative_md concise (≤8 lines). Verbose directory listings belong
     in probe_trace, not narrative_md.

When done, reply with ONLY a JSON object matching the AwakeningReport schema:

{
  "os": { "name": "...", "version": "...", "kernel": "...", "arch": "..." },
  "shell": { "path": "...", "implementation": "..." },
  "identity": { "user": "...", "uid": 0, "gid": 0, "home": "..." },
  "present_tools": [ { "name": "...", "provider": "...", "version": "..." } ],
  "absent_tools": [ "..." ],
  "capabilities": [ { "name": "...", "satisfied": true, "evidence": ["..."] } ],
  "tools_to_register": [ { "name": "...", "basis": "..." } ],
  "narrative_md": "...",
  "probe_trace": [ { "cmd": "...", "exit": 0 } ]
}

If the gate refuses a command, read the error, adjust, and try another. If a
validation error is reported on your JSON, fix the structure and re-emit.`

// SelectSystemPrompt returns the appropriate system prompt for a given
// awakening transition role. Topology code should call this rather than
// hard-coding a constant so that the plan/legacy split is explicit and
// reversible.
//
//	role == "plan"     -> SystemPromptPlan (new default, probe-fanout)
//	role == "followup" -> SystemPrompt     (legacy, direct AwakeningReport)
//
// Any other role falls back to SystemPromptPlan.
func SelectSystemPrompt(role string) string {
	switch role {
	case "followup", "legacy", "report":
		return SystemPrompt
	case "plan", "":
		return SystemPromptPlan
	default:
		return SystemPromptPlan
	}
}

// PromptDigestMarker is a stable label the tool-forge registrations use as
// their Provenance.PromptDigest. It lets operators trace a registered tool
// back to the awakening flow.
const PromptDigestMarker = "awakens/v0.1"

// lexiconExcerptCap is the hard cap on the rendered LEXICON section
// (spec-architecture-brae-awakening-toolbox-extension.md CON-001, 1.5 KiB).
// Includes everything from the "LEXICON (" header through the final newline
// of the KIND/DOMAIN listings. Anything beyond the cap is truncated from the
// end of the listing (tags continue to be deterministically ordered so replays
// remain byte-identical — REQ-012 / AC-012).
const lexiconExcerptCap = 1536

// lexiconTopN is the deterministic cap per spec §3 REQ-002 / §3 CON-001:
// the 32 highest-priority entries from the lexicon.
const lexiconTopN = 32

// tagInstructions is the tagging header embedded in BuildSystemPromptPlan.
// It is deliberately verbatim §4.2 of the spec so the LLM sees the same
// wording the acceptance tests scan for (AC-003).
const tagInstructions = `You are also responsible for tagging each tool you register.
For every entry of tools_to_register, set:
  - toolbox: one of system|developer|web|image|pdf|data|general
  - hashtags: 2–5 short tokens from the controlled lexicon below.

Always include AT LEAST one kind tag and one domain tag. Prefer fewer,
sharper tags over many fuzzy ones.`

// fewShotExamples reproduces the anchors in spec §4.2 verbatim. Keeping
// them inside a const (rather than interpolating) is intentional — the
// string is a stable artefact the audit pipeline fingerprints.
const fewShotExamples = `Examples (few-shot anchors):
  { "name": "pdf-to-text", "basis": "pdftotext",
    "toolbox": "pdf",  "hashtags": ["tools","pdf","read","transform"] }
  { "name": "curl-fetch", "basis": "curl",
    "toolbox": "web",  "hashtags": ["tools","network","read","web"] }
  { "name": "sqlite-query", "basis": "sqlite3",
    "toolbox": "data", "hashtags": ["tools","data","read","query","sql"] }`

// BuildSystemPromptPlan renders the awakening plan prompt with a
// deterministic 32-entry lexicon excerpt and few-shot hashtag examples
// appended to the base SystemPromptPlan skeleton (spec §3 REQ-002, §5 AC-003).
//
// The excerpt section is sourced from tools.SelectTopTags(lex, 32, nil) —
// curation-driven at v0.1 with no runtime stats — so two calls with the
// same lexicon produce byte-for-byte identical strings (REQ-012, AC-012).
// The excerpt is capped at 1.5 KiB (CON-001); if rendering overflows, the
// tail of the KIND/DOMAIN listings is truncated deterministically.
//
// When lex is nil, the function returns SystemPromptPlan verbatim for
// backwards-compatibility with call sites that pre-date toolbox extension.
func BuildSystemPromptPlan(lex cpn.Lexicon) string {
	if lex == nil {
		return SystemPromptPlan
	}
	excerpt := renderLexiconExcerpt(lex)
	var b strings.Builder
	b.Grow(len(SystemPromptPlan) + len(tagInstructions) + len(fewShotExamples) + len(excerpt) + 32)
	b.WriteString(SystemPromptPlan)
	b.WriteString("\n\n")
	b.WriteString(tagInstructions)
	b.WriteString("\n\n")
	b.WriteString(excerpt)
	b.WriteString("\n")
	b.WriteString(fewShotExamples)
	return b.String()
}

// renderLexiconExcerpt builds the `LEXICON (32 most useful entries):` block.
// It groups tags by kind (`kind` vs `domain`), sorts each group alphabetically
// for deterministic rendering, and truncates the rendered bytes to
// lexiconExcerptCap. Truncation drops whole tags from the END of whichever
// line overflows; partial tags never appear.
func renderLexiconExcerpt(lex cpn.Lexicon) string {
	top := tools.SelectTopTags(lex, lexiconTopN, nil)

	var kinds, domains []string
	for _, e := range top {
		switch e.Kind {
		case "kind":
			kinds = append(kinds, e.Tag)
		case "domain":
			domains = append(domains, e.Tag)
		}
	}
	// Deterministic alphabetical order inside each group so the rendered
	// bytes never depend on SelectTopTags' priority sort for display (the
	// priority sort still governs WHICH 32 tags are in the excerpt).
	sort.Strings(kinds)
	sort.Strings(domains)

	const header = "LEXICON (32 most useful entries):"
	kindLine := "  KIND tags  : " + strings.Join(kinds, ", ")
	domainLine := "  DOMAIN tags: " + strings.Join(domains, ", ")

	// Render, then enforce the 1.5 KiB cap. Truncate tags from the END of
	// whichever line is longest first so both lines stay populated.
	block := header + "\n" + kindLine + "\n" + domainLine
	for len(block) > lexiconExcerptCap {
		switch {
		case len(domains) > 0 && (len(domains) >= len(kinds) || len(kinds) == 0):
			domains = domains[:len(domains)-1]
			domainLine = "  DOMAIN tags: " + strings.Join(domains, ", ")
		case len(kinds) > 0:
			kinds = kinds[:len(kinds)-1]
			kindLine = "  KIND tags  : " + strings.Join(kinds, ", ")
		default:
			return block[:lexiconExcerptCap]
		}
		block = header + "\n" + kindLine + "\n" + domainLine
	}
	return block
}
