package awakens

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
