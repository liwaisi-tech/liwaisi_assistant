package awakens

// SystemPrompt is the awakening directive handed to the LLM as the system
// message for the `brae-awakens` turn. It encodes §9.6 verbatim plus the
// §4.5 allow-list hint and the §9.4 retry rule.
//
// Keep this string stable — tests hash-fingerprint it as part of the prompt
// digest on registered tools.
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

// PromptDigestMarker is a stable label the tool-forge registrations use as
// their Provenance.PromptDigest. It lets operators trace a registered tool
// back to the awakening flow.
const PromptDigestMarker = "awakens/v0.1"
