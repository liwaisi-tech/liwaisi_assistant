---
title: Tool Forge CPN — LLM Designs → Bash Compiles → Register → Invoke
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, cpn, forge, compilation, brae, gap-5]
---

# Introduction

`brae`'s defining trait is that it does not *download* software — it *builds* what it needs from what the host already offers. The tool-forge is the CPN that makes this real: an LLM drafts source, bash compiles it, a validator probes `--help`, a HITL gate approves the first run, and the registry persists the result. All the plumbing (GAPs 1–4, 6) has been specified; this spec composes them into the concrete topology and the filesystem convention that backs it.

## 1. Purpose & Scope

**Purpose.** Let `brae` resolve a capability request ("I need to fetch a URL body") by authoring and installing a binary, then invoking it — with full provenance and reversibility.

**In scope.**
- A topology `tool-forge-cpn` registered under `flow_name="tool-forge"`.
- Filesystem convention under `$HOME/.local/brae/`: `src/<tool>/`, `bin/`, `share/man/man1/`, `logs/forge/`.
- LLM transitions: `t-design-spec`, `t-emit-source`, `t-write-helptext`, `t-write-manpage`.
- Bash transitions: `t-probe-compiler`, `t-write-source`, `t-compile`, `t-install`, `t-smoke-test` (runs `./bin --help` and `./bin --version` if present).
- Validator: `t-validate-help` — `./bin --help` must print a non-empty, regex-matching usage line.
- HITL: the register step calls GAP-3 register with a binary, which triggers GAP-6 first-run approval.
- Fallback: if `can-compile-c == false` in the capability registry, the forge degrades to a Python/Bash script tool (no compilation); if those are also absent, the forge fails with `ErrNoCompilerAvailable`.
- Tests: successful C build, missing-compiler fallback, failed compile → error place, help-validation failure → error place, register success.

**Out of scope.**
- Source-code quality metrics / static analysis — v1 trusts the LLM + smoke test.
- Multi-file builds / complex Makefiles — v1 handles single-source-file C programs and single-script Python/Bash tools. Complex builds deferred.
- Tool evolution / patching — v1 authors fresh versions only; patch flow is a separate feature.

**Audience.** `golang-pro`.

## 2. Definitions

- **Forge request**: Input token `{capability string, examples []string, constraints []string}` describing what the tool must do.
- **Source artefact**: Text file under `$HOME/.local/brae/src/<tool>/src.c` (or `.py`, `.sh`).
- **Binary artefact**: Executable under `$HOME/.local/brae/bin/<tool>`.
- **Smoke test**: `./bin --help && (./bin --version || true)`, parsed by regex.

## 3. Requirements, Constraints & Guidelines

### Topology

- **REQ-001**: Places (all `SpaceComputation` unless noted):
  - `p-forge-request` (`ColorJSON`) — input.
  - `p-compiler-choice` (`ColorJSON`) — `{language: "c"|"python"|"bash", compiler_path?: string}`.
  - `p-spec` (`ColorJSON`) — LLM-produced spec: `{name, signature, pseudocode, rationale, test_plan}`.
  - `p-source` (`ColorJSON`) — `{path, content, sha256}`.
  - `p-compiled-binary` (`ColorJSON`) — `{path, sha256, size_bytes}`.
  - `p-help-text` (`ColorString`).
  - `p-man-page` (`ColorString`).
  - `p-smoke-ok` (`ColorJSON`).
  - `p-manifest` (`ColorToolManifest`).
  - `p-registered` (`ColorArtifact`) — terminal.
  - `p-forge-errors` (`ColorError`) — error place.
- **REQ-002**: Transitions (all with `ErrorPlace=p-forge-errors`):
  - `t-probe-compiler` (`NodeKindTool`) — reads `p-host-capabilities` (peek), picks a language, deposits `p-compiler-choice`.
  - `t-design-spec` (`NodeKindLLM`) — consumes request + compiler choice, emits spec.
  - `t-emit-source` (`NodeKindLLM`) — consumes spec, emits source JSON (validated structure).
  - `t-write-source` (`NodeKindBash`) — writes source to disk under `$HOME/.local/brae/src/<tool>/`.
  - `t-compile` (`NodeKindBash`) — runs the compiler (for C: `gcc -O2 -o bin/<tool> src.c`), captures stdout/stderr.
  - `t-smoke-test` (`NodeKindBash`) — runs `./bin/<tool> --help`, asserts regex `/usage|Usage|SYNOPSIS/i` matches.
  - `t-write-helptext` (`NodeKindLLM`) — synthesises `HelpText` from spec + --help output.
  - `t-write-manpage` (`NodeKindLLM`) — synthesises a Markdown man page.
  - `t-build-manifest` (`NodeKindTool`) — assembles `ToolEntry` fields into a `p-manifest` token.
  - `t-register` (`NodeKindRegisterTool`) — persists to registry.
- **REQ-003**: The forge MUST NOT invoke LLMs to execute commands — all OS work flows through `NodeKindBash` under the gate. LLMs only produce structured documents.

### Filesystem convention

- **REQ-010**: Root directory: `$HOME/.local/brae/`. Created on first forge run if absent.
- **REQ-011**: Per-tool layout:
  ```
  $HOME/.local/brae/
  ├── src/<namespace>/<name>/
  │   ├── src.c | src.py | src.sh
  │   ├── spec.json
  │   └── build.log
  ├── bin/<name>            (executable, 0755)
  ├── share/man/man1/<name>.1.md
  └── logs/forge/<forge_run_id>.log
  ```
- **REQ-012**: All writes go through `HostAdapter.WriteFile`, which enforces the path jail (SEC-002 of GAP-1).
- **REQ-013**: Every authored file's path + SHA-256 MUST be recorded in the authored_artifacts ledger (GAP-10) for rollback.

### Compiler choice

- **REQ-020**: `t-probe-compiler` decision table (by capability):
  - `can-compile-c == true` AND (fast runtime required OR memory constrained) → `c`, use `gcc` or `clang`.
  - `can-run-python == true` AND human-readable preferred → `python`.
  - else → `bash`.
- **REQ-021**: If none satisfies, deposit on `p-forge-errors` with reason `no-compiler`.

### LLM prompts

- **REQ-030**: `t-design-spec` system prompt MUST include the target language, `HostCapabilitySnapshot` summary, and an explicit "must use only standard library" constraint for C.
- **REQ-031**: `t-emit-source` MUST return source wrapped in `<source>` delimiters and must specify expected CLI flags (`--help`, `--version`, command-line args).
- **REQ-032**: LLM transitions MUST use `RequireJSON=true` for structured steps (spec, source JSON) and `Streaming=false` — streaming is unnecessary and complicates parsing.

### Bash transitions

- **REQ-040**: `t-compile` for C MUST use: `gcc -O2 -Wall -Werror -o <binpath> <srcpath>`. `-Werror` fails the compile on warnings — strict by design.
- **REQ-041**: Compile timeout: 30 s. On timeout → error place.
- **REQ-042**: Sandbox profile: `fsjail` (no network, read-only root, writes only under the forge work dir).

### Smoke test

- **REQ-050**: `./bin --help` MUST:
  - Exit with code 0 or 1 (Unix convention — some tools return 1 on --help).
  - Emit to stdout within 2 s.
  - Match regex `/(?i)usage|synopsis|options/` in the first 2000 characters.
- **REQ-051**: On failure, the tool is NOT registered. Source and binary are moved to `$HOME/.local/brae/quarantine/<forge_run_id>/` for audit.

### Provenance

- **REQ-060**: `ToolEntry.Provenance` MUST be populated with: authoring CPN ID, source SHA-256, binary SHA-256, prompt digest (sha256 of the full LLM prompt template + task), forge run ID (ULID).
- **REQ-061**: The `build.log` MUST capture compiler stdout/stderr verbatim and the full LLM transcript (responses only, prompts recorded by digest).

### Fallbacks & degradation

- **CON-001**: If `gcc` and `clang` absent but `cc` present, use `cc`.
- **CON-002**: If no C compiler and no Python, author a pure-bash tool using the caution-listed primitives.
- **CON-003**: If the capability cannot be satisfied by any available runtime, emit a user-facing error (via forge output) saying exactly what's missing ("needs: curl or wget; found: neither").

### Guidelines

- **GUD-001**: Prefer small, focused tools (< 400 LOC). If the LLM proposes something larger, force a decomposition.
- **GUD-002**: Every authored tool MUST accept `--help` and print a usage line. Non-negotiable.
- **GUD-003**: Name tools `namespace/name` where `namespace == "brae"` by default; users may override.

## 4. Interfaces & Data Contracts

### Forge request JSON

```json
{
  "capability": "http.get",
  "description": "Fetch the body of an HTTPS URL and print to stdout.",
  "inputs":  [{"name":"url","type":"string","required":true}],
  "outputs": [{"name":"stdout","type":"string","description":"response body"}],
  "constraints": ["<5MB binary","no network libs beyond libc/openssl"]
}
```

### Spec document

```json
{
  "name": "brae/http-get",
  "language": "c",
  "pseudocode": "…",
  "rationale": "…",
  "test_plan": ["./http-get https://example.com | head -c 100"],
  "expected_flags": ["--help","--version"]
}
```

### Source document

```json
{
  "path": "$HOME/.local/brae/src/brae/http-get/src.c",
  "content": "#include <stdio.h>\n…",
  "sha256": "…"
}
```

### ToolEntry manifest (produced by `t-build-manifest`)

See GAP-3 for full shape. Forge populates `Namespace="brae"`, `Version="0.1.0"` (bump-on-republish).

## 5. Acceptance Criteria

- **AC-001**: Given a host with `gcc` and a request `{capability: "http.get"}`, when the forge runs, then a binary exists at `$HOME/.local/brae/bin/http-get`, `./http-get --help` works, and the tool is registered.
- **AC-002**: Given a host without any C compiler but with `python3`, when the forge runs, then a Python script is authored and registered (language fallback).
- **AC-003**: Given a compile error (LLM emitted invalid C), when `t-compile` fails, then the source is moved to quarantine, an error token is routed to `p-forge-errors`, and no registration occurs.
- **AC-004**: Given a binary that does not support `--help`, when smoke test runs, then the forge fails and the binary is quarantined.
- **AC-005**: Given a successful forge run, when GAP-6 is live, then a HITL prompt is issued on the first invocation of the new tool; acceptance triggers execution.
- **AC-006**: After a forge run, `authored_artifacts` (GAP-10) contains at least two rows (source, binary).
- **AC-007**: Concurrent forge runs for different capabilities MUST NOT collide on filesystem paths (scope by forge_run_id prefix during scratch writes).
- **AC-008**: The forge MUST complete a simple C build (echo-ish tool) end-to-end in < 15 s on a warm host.

## 6. Test Automation Strategy

- Unit: compiler choice decision table; help-text regex.
- Integration (CI ubuntu-latest): full forge run with stubbed LLM returning a canonical "echo" C program; asserts binary exists and runs.
- Integration (fallback): temporarily mask `gcc` on PATH; verify Python/Bash fallback selected.
- Failure tests: LLM returns unparseable source; compile produces warning (strict -Werror fails); smoke test regex miss.
- Coverage ≥ 85% on the forge topology package.

## 7. Rationale & Context

**Why C, not just Python?** Small binary, no runtime dependency, fast cold-start. Matches the "build from the OS only" philosophy. Python is a fallback when C is unavailable or impractical.

**Why -Werror?** Warnings-as-errors is the cheapest way to force the LLM to produce tighter code. If the compile is strict, the prompt will adapt on retry.

**Why mandate --help?** Discoverability and validation. The forge cannot register something it can't self-describe, and the LLM scaffolding needs a machine-readable entry point.

**Why quarantine on failure?** Don't delete; the user debugging "why didn't the forge produce something" benefits from having the failed artefact to inspect. Retention policy lives in GAP-10.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-1 (bash/host adapter) — hard.
- **EXT-002**: GAP-2 (capability registry) — hard; drives compiler choice.
- **EXT-003**: GAP-3 (tool registry) — register step.
- **EXT-004**: GAP-6 (gate) — first-run approval.
- **EXT-005**: GAP-10 (provenance) — artefact ledger.

- **INF-001**: Linux with at least one of: `gcc`, `clang`, `cc`, `python3`, `bash`.
- **PLT-001**: Go ≥ 1.22.

## 9. Examples & Edge Cases

### Canonical "http-get" forge

1. User: "fetch this URL" → classifier decides: missing tool → forge.
2. `t-probe-compiler` → picks `c` (gcc present).
3. `t-design-spec` → spec with `libcurl` or pure-socket TLS via openssl.
4. `t-emit-source` → src.c emitted.
5. `t-write-source` → writes file.
6. `t-compile` → `gcc -O2 -Wall -Werror -o bin/http-get src/brae/http-get/src.c -lssl -lcrypto` → success.
7. `t-smoke-test` → `./bin/http-get --help` shows usage → pass.
8. `t-write-helptext` + `t-write-manpage`.
9. `t-build-manifest`, `t-register` → GAP-6 approves → tool usable.

### Edge — LLM emits code using an unavailable header

Compile fails. Source + log moved to quarantine. The forge retries once with an updated prompt including the error output; if second attempt also fails → error place.

### Edge — smoke test regex miss

Binary runs but doesn't print "usage". Forge fails; user gets a clear error.

### Edge — network requirement and sandbox incompat

If the spec requires network (e.g. build-time fetch), the gate denies unless the sandbox profile allows. Forge emits error; user must authorise.

## 10. Validation Criteria

- No forge run writes outside `$HOME/.local/brae/`.
- Every registered tool has a reproducible source in `src/` and a recorded compile command in `build.log`.
- The smoke test is executed on every build before registration — no skip path.

## 11. Related Specifications / Further Reading

- [spec-architecture-host-adapter-nodekind-bash.md](./spec-architecture-host-adapter-nodekind-bash.md) — GAP-1.
- [spec-architecture-host-discovery-capability-registry.md](./spec-architecture-host-discovery-capability-registry.md) — GAP-2.
- [spec-architecture-dynamic-tool-registry.md](./spec-architecture-dynamic-tool-registry.md) — GAP-3.
- [spec-architecture-host-gate-security-policy.md](./spec-architecture-host-gate-security-policy.md) — GAP-6.
- [spec-architecture-authored-artifact-provenance.md](./spec-architecture-authored-artifact-provenance.md) — GAP-10.
