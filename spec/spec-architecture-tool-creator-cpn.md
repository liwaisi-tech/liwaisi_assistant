# spec-architecture-tool-creator-cpn

Status: draft
Owner: liwaisi
Related: spec-architecture-cpn-agent-architect, spec-architecture-brae-context-and-tool-hygiene, GAP-5 tool-forge (`cmd/server/topologies_tool_forge.go`)

## 1. Purpose

Give brae the ability to **author full Go micro-backend tools** end-to-end, autonomously, from a user request or an autonomous decision to build one. Output is a hexagonal-architecture Go project in `workspace/tools/src/<name>/` (git-initialised, `go mod init`-ed, scaffolded) that builds with `make`, tests with ≥85% coverage, installs with `./install.sh`, and ends up registered in brae's tool registry — callable in the main conversation CPN on the next turn.

**Distinct from `tool-forge` (GAP-5)**, which produces single-source, single-binary scripts from one LLM prompt. tool-creator produces **projects**: versioned, testable, hexagonal Go modules with a proper SDLC (spec → team review → totals → TDD → package → register).

## 2. Non-goals

- **HITL gates.** The container sandbox + policy gate is the safety boundary. tool-creator runs autonomously; every decision is LLM- or code-made, never human-gated. (See `feedback_minimize_hitl.md`.)
- **Replacing tool-forge.** Forge stays for fast single-binary cases; tool-creator is for tools that warrant a project structure.
- **Cross-language.** Go only in v1. The scaffold templates, Makefile, golangci config are Go-shaped.
- **Remote execution.** All build/test steps run inside brae's container via the existing `NodeKindBash` + policy gate.
- **Self-modifying templates.** v1 ships with fixed `embed.FS` templates; future work may let brae evolve them.

## 3. Trigger & routing

Two entry conditions, both flow to a single sub-CPN instantiation:

1. **Explicit user intent.** "build me a tool that X", "create a brae tool for Y". Classified by the main conversation CPN's intent router (existing transition, extended).
2. **Autonomous need.** During tool-match in the main CPN, if the Architect (spec §11) concludes no existing tool covers a required capability *and* forge's single-binary shape is insufficient (multi-file logic, persistence, non-trivial parsing), it emits a `TaskSpec` token that routes here instead of to forge.

Routing is a `NodeKindInstantiate` against `ToolCreatorFlowName = "tool-creator"`, persisted once at server start, identical to how `tool-forge` is registered.

## 4. Topology

```
 p-request (ColorJSON)
      │
      ▼
 t-triage (LLM) ──► p-triaged (ColorJSON: {ready: bool, clarify_question?: string})
      │
      ▼  (guard: ready == true; else a clarify_question streams back to user via existing SSE;
      │   when user replies, a fresh request re-enters p-request)
      │
 ┌────┴──────────────────────────────────────────┐
 ▼                    ▼                          ▼
 t-investigate     t-scaffold-workspace       t-draft-spec-v0
   (Tool/Bash:       (Bash: mkdir -p           (LLM: request + role prompt
    find tools/       workspace/tools/src/X,    → hexagonal-DDD spec JSON)
    src/*,            git init, go mod init,
    list existing     render templates from
    modules)          embed.FS)
   │                  │                          │
   ▼                  ▼                          ▼
 p-existing-     p-workspace-ready            p-spec-v0
  tools          (ColorArtifact: path)        (ColorJSON)
      │                │                          │
      └────────────────┴──────────────────────────┘
                       │ (join: all three)
                       ▼
               t-enrich-spec (Tool: merges existing-tools + workspace path
                                into spec-v0)
                       │
                       ▼
                   p-spec-draft
                       │
         ┌─────────────┼─────────────┬─────────────┬─────────────┬─────────────┐
         ▼             ▼             ▼             ▼             ▼             ▼
   t-review-go    t-review-ai   t-review-devops t-review-qa t-review-arch t-review-pm
     (LLM)         (LLM)          (LLM)          (LLM)        (LLM)        (LLM)
         │             │             │             │             │             │
         └─────────────┴─────────────┴─────────────┴─────────────┴─────────────┘
                                     │ (6 tokens → p-reviews)
                                     ▼
                               t-aggregate-reviews (Tool, Go)
                                     │
                       ┌─────────────┴─────────────┐
                       ▼                           ▼
                 p-spec-approved             p-refine-request
                 (ColorJSON)                 (ColorJSON, w/ blocking_issues)
                       │                           │
                       │                           ▼
                       │                     t-refine-spec (LLM)
                       │                           │
                       │                           ▼
                       │                       p-spec-draft (loop; max 2 refinements,
                       │                                     counter on token metadata)
                       ▼
                 t-decompose-totals (LLM: spec → N TaskSpec tokens)
                       │
                       ▼
                 p-totals  (multi-token, N = 1..8)
                       │
                 t-authorize-totals (LLM: emits N DoIt tokens after well-formedness check)
                       │
                       ▼
                 p-doit  (multi-token, N)
                       │
                       │   ┌── p-totals (1 token)
                       └───┤
                           └── p-doit   (1 token)    — 1:1 join, per total
                                  │
                                  ▼
                           t-gate-total (Tool, Go; consumes 1 from each place per firing;
                                         deposits 1 merged TaskSpec into p-total-ready)
                                  │
                                  ▼
                           p-total-ready (multi-token; N firings)
                                  │
                                  ▼
                           t-plan-subtasks (LLM: total → M subtasks with
                                            {parallel_safe: bool, deps: []}, minimal-context each)
                                  │
                                  ▼
                           p-subtasks (multi-token; per total)
                                  │
                                  ▼
                           t-tdd-loop (Tool handler: for each subtask, iterate up to K=4:
                                       LLM writes failing test → LLM impl → bash runs
                                       `go test ./... -cover` → on pass+coverage ≥85%,
                                       emits ColorArtifact; on K exhausted, ColorError → p-errors)
                                  │
                                  ▼
                           p-tested (multi-token; one per total)
                                  │
                          ┌───────┴─────────┐ (N tokens aggregated)
                          ▼
                    t-package (Bash: final `go build ./...`, `make test-coverage`,
                               writes manifest JSON with binary sha256, help, usage)
                          │
                          ▼
                    p-packaged (ColorToolManifest)
                          │
                          ▼
                    t-register (NodeKindRegisterTool)
                          │
                          ▼
                    p-registered (ColorArtifact: {name, namespace, binary_path, version})

 All transitions route errors to p-errors (ColorError).
```

## 5. Places

| ID | Color | Space | Purpose |
|---|---|---|---|
| `p-request` | JSON | Computation | Incoming tool-build request (normalised intent, name, description). |
| `p-triaged` | JSON | Computation | Triage verdict `{ready, clarify_question?}`. |
| `p-existing-tools` | JSON | Computation | Summary of tools already present in `workspace/tools/src/`. |
| `p-workspace-ready` | Artifact | Computation | Path + git branch of the freshly scaffolded repo. |
| `p-spec-v0` | JSON | Computation | First-draft spec. |
| `p-spec-draft` | JSON | Computation | Enriched/refined spec; refinement counter in `Payload.RefineCount`. |
| `p-reviews` | JSON | Computation | Multi-token (6). Each is an expert review `{role, approved, blocking, suggestions}`. |
| `p-spec-approved` | JSON | Computation | Approved spec. |
| `p-refine-request` | JSON | Computation | Feedback bundle when reviewers block. |
| `p-totals` | TaskSpec | Computation | Multi-token (N). Top-level workstreams. |
| `p-doit` | JSON | Computation | Multi-token (N). LLM-emitted authorisation tokens. Semantic match to `p-totals` by `TotalID`. |
| `p-total-ready` | TaskSpec | Computation | Multi-token (N). Authorised workstream. |
| `p-subtasks` | TaskSpec | Computation | Multi-token. Per-total subtasks. |
| `p-tested` | Artifact | Computation | Multi-token (N). Per-total TDD success artifact. |
| `p-packaged` | ToolManifest | Computation | Final manifest ready for registration. |
| `p-registered` | Artifact | Computation | Terminal: registered tool metadata. |
| `p-errors` | Error | Computation | Terminal error sink. |

## 6. Transitions (selected)

### t-triage `NodeKindLLM`
Reads `p-request`. Emits `{ready: true}` for specific requests (name + clear purpose + at least one verb for behaviour) or `{ready: false, clarify_question: "..."}` for ambiguous ones. Guard on `t-investigate`/`t-scaffold`/`t-draft-spec` checks `ready == true`. When `ready == false`, `t-triage` also emits a user-facing SSE message via `EventSink` containing the question; brae's main conversation CPN handles the reply by reposting a fresh `p-request` token. At most ONE clarification round (request count tracked in session metadata); second-round ambiguity proceeds with best-effort interpretation.

### t-investigate `NodeKindTool`
Scans `workspace/tools/src/*` (directory list via bash `ls`, `go.mod` extraction, README first-line summary). Emits `{tools: [{name, module, summary}]}`. Deterministic; no LLM. Useful context for the draft-spec LLM and for avoiding name collisions.

### t-scaffold-workspace `NodeKindBash`
Bash script:
```bash
set -euo pipefail
cd ~/workspace/tools/src
[ -e "$NAME" ] && { echo '{"error":"name collision"}' >&2; exit 1; }
mkdir -p "$NAME" && cd "$NAME"
git init -q -b main
go mod init "brae.tools/$NAME"
# Templates are pre-rendered by the Go side and delivered via env var BRAE_SCAFFOLD_DIR.
cp -a "$BRAE_SCAFFOLD_DIR"/. .
git add -A && git -c user.email=brae@liwaisi.local -c user.name=brae commit -q -m "chore: scaffold $NAME"
printf '{"path":"%s","branch":"main","commit":"%s"}' "$PWD" "$(git rev-parse HEAD)"
```
The `BRAE_SCAFFOLD_DIR` is populated by the Go-side scaffold renderer which materialises `embed.FS` templates into a temp dir *before* the bash transition fires. This keeps template rendering in Go (typed, testable) and file placement in bash (policy-gated).

### t-draft-spec-v0 `NodeKindLLM`
Prompt (abridged):
> You are a Go software architect. Produce a hexagonal, DDD-flavoured spec for a CLI tool named `{NAME}` described as: `{DESC}`. Output JSON `{name, purpose, non_goals, domain_model, ports, adapters, cli_surface, test_plan, risks}`. Keep it small: one aggregate, ≤3 ports, ≤3 adapters, ≤200 LOC total.

### t-review-* (6 parallel LLMs)
Each consumes `p-spec-draft` (non-destructively via a fan-out pattern: t-enrich-spec deposits 6 clones, one per reviewer place, then all 6 drain into `p-reviews`). Each review is `{role, approved: bool, blocking_issues: []string, suggestions: []string}`. Role prompts are short and context-minimal — see `cpn/toolbuilder/team.go`.

### t-aggregate-reviews `NodeKindTool` (Go)
Pure function: `reviews → verdict`. Approve when `len(blocking_issues) == 0` across all reviews, OR when `RefineCount >= 2` (ship best-effort). Otherwise package blocking_issues into `p-refine-request`.

### t-decompose-totals `NodeKindLLM`
Prompt produces a list of **totals** — high-level deliverables. Shape:
```json
{"totals":[{"id":"T1","name":"domain","deliverables":["aggregate","value-objects"],"parallel_safe":true,"depends_on":[]}, ...]}
```
Constraint: `1 ≤ N ≤ 8`. Dependencies form a DAG (linter rejects cycles).

### t-authorize-totals `NodeKindLLM` *(replaces HITL DoIt gate)*
Reads `p-totals`, emits N DoIt tokens into `p-doit` if totals are well-formed (names unique, DAG acyclic, deliverables non-empty, no scope drift from spec). On failure, emits a single Error to `p-errors` (shouldn't happen if t-decompose did its job — defence in depth).

### t-gate-total `NodeKindTool`
Joins one token from `p-totals` with one from `p-doit` where `TotalID` matches, deposits on `p-total-ready`. Multi-token firing: executor goroutines drain in parallel (deterministic CPN firing ordering keeps it correct).

### t-plan-subtasks `NodeKindLLM`
Per total. Role prompt: "Split this total into minimal subtasks. For each, include ONLY the context strictly needed: imports to touch, signature to produce, test to satisfy. Mark `parallel_safe` per subtask." Output `{subtasks: [...]}`.

### t-tdd-loop `NodeKindTool` (calls LLMClient + HostAdapter internally)
The crown jewel. For each subtask, up to K=4 iterations:
1. LLM writes a failing test file (context: port signature + test-plan from spec + previous iteration's output/failure).
2. Bash: `go test ./... -run <TestName>` — must FAIL (red).
3. LLM writes the implementation (context: the failing test + domain model).
4. Bash: `go test ./... -run <TestName> -cover` — must PASS (green).
5. If coverage < 85% *for this subtask's package*, iterate with coverage gaps.

Failure after K iterations → ColorError to `p-errors`. Success → ColorArtifact (sha of the tree + coverage %) to `p-tested`.

Parallel subtasks fire in parallel (CPN's natural firing model); sequential subtasks enforce ordering via `depends_on` encoded as guards.

### t-package `NodeKindBash`
Final polish:
```bash
set -euo pipefail
cd "$WORKSPACE_PATH"
make test-coverage      # must succeed, re-checks ≥85%
make build              # produces bin/<name>
make install            # copies to workspace/tools/bin/<name>
sha256sum workspace/tools/bin/<name> | awk '{print $1}'
# emit ToolManifest JSON with help text from `<name> --help`
```

### t-register `NodeKindRegisterTool`
Uses the existing `RegisterToolConfig` shape:
```go
RegisterToolConfig{
    Namespace: "brae.tools",
    Name: <name>,
    Version: "0.1.0",
    HelpText: <from --help>,
    BinaryPath: "workspace/tools/bin/<name>",
    BinarySHA256: <sha>,
    Origin: "agent-authored",
    RegisteredBy: "tool-creator",
}
```

## 7. Scaffold templates (`cpn/toolbuilder/scaffold/templates/`)

Embedded via `//go:embed` and rendered with `text/template` using `ScaffoldParams{Name, Module, Description, Author, Date}`.

```
scaffold/templates/
├── go.mod.tmpl
├── Makefile.tmpl            # targets: test, test-coverage, test-coverage-html, lint, build, install, clean
├── .golangci.yml            # static (no template)
├── .gitignore               # static
├── README.md.tmpl
├── cmd/{{.Name}}/main.go.tmpl
├── internal/
│   ├── domain/doc.go.tmpl
│   ├── app/doc.go.tmpl
│   ├── ports/port.go.tmpl         # stub port interface with one method
│   └── adapters/
│       ├── adapter.go.tmpl
│       └── adapter_test.go.tmpl   # first failing test to seed TDD
├── install.sh.tmpl           # copies bin/<name> → $HOME/workspace/tools/bin/<name>
```

Coverage gate lives in Makefile:
```makefile
test-coverage:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1 | awk '{ if ($$3+0 < 85.0) { print "coverage below 85%"; exit 1 } else print "OK" }'
```

## 8. Integration with existing systems

- **Policy gate** (`infra/host/gate`): all bash transitions (`t-scaffold-workspace`, `t-tdd-loop`'s internal `go test`, `t-package`) route through it. `go build`, `go test`, `git init`, `git commit`, `mkdir`, `cp` are already in the safe-band. `go mod init` is added if missing.
- **LLM client**: injected via existing `LLMClient` port. Each LLM transition has its own `LLMConfig` and minimal `ContextPolicy` (new role constants: `RoleArchitect`, `RoleGoEngineer`, `RoleDevOps`, `RoleQA`, `RolePM`, `RoleAIEngineer`).
- **Tool registry** (`cpn/tools`): `t-register` publishes into the same registry used by the main conversation CPN. The tool becomes callable on the very next turn (registry is live-mutable, GAP-3).
- **Flow registry** (`cpn/persist.FlowRepository`): `ToolCreatorFlowName` registered at startup like `ToolForgeFlowName`.
- **Intent router in main CPN**: existing classifier gains two new output labels: `intent:build-tool-creator` (rich project) and `intent:build-tool-forge` (single binary). Router decides based on complexity heuristics in the prompt.

## 9. Requirements

- **REQ-A01** tool-creator topology is registered at server start as `tool-creator`.
- **REQ-A02** A user request `"build me a tool that …"` routes to tool-creator via the main CPN's intent router when the request implies multi-file logic; else to forge.
- **REQ-A03** Triage emits at most one clarify question per session per tool request.
- **REQ-A04** `t-scaffold-workspace` refuses name collisions in `workspace/tools/src/`.
- **REQ-A05** Scaffolded project compiles (`go build ./...`) and tests (`go test ./...`) on a fresh render with zero modifications.
- **REQ-A06** Spec refinement loops at most 2 times; after that, best-effort spec proceeds.
- **REQ-A07** Totals count `N` satisfies `1 ≤ N ≤ 8`.
- **REQ-A08** DoIt tokens are emitted 1:1 with totals by `t-authorize-totals`, based on automated well-formedness (no HITL).
- **REQ-A09** TDD loop enforces Red → Green ordering and per-package coverage ≥85% before emitting success.
- **REQ-A10** `t-package` fails loud if `make test-coverage` fails; failure routes to `p-errors`, not silently proceeds.
- **REQ-A11** Newly registered tool is callable in the main conversation CPN on the next user turn (no restart).
- **REQ-A12** No `NodeKindHITL` transitions in tool-creator.
- **REQ-A13** All bash executions route through the policy gate and bubble gate errors up.

## 10. Expert-team prompts (roles used by t-review-*)

| Role | Focus | Red flags it reports |
|---|---|---|
| Go Engineer | idioms, stdlib, error handling, generics use | unnecessary interfaces, panic misuse, `interface{}` leaks, non-idiomatic naming |
| AI Engineer | LLM tool-use surface, JSON schema, streaming fit | overstuffed tool input, ambiguous schemas, missing `--help` usable by LLMs |
| DevOps | Makefile, install.sh, sandbox/container fit, path jail | privileged ops, network calls in tests, non-reproducible builds |
| QA | TDD seams, table-driven tests, coverage plan | untestable private state, missing edge cases, weak assertions |
| Architect | ports/adapters cleanliness, dependency direction | domain importing adapters, fat ports, leaky abstractions |
| PM | scope, non-goals clarity, user value | feature creep, unclear user outcome, missing success criteria |

Each role prompt is ≤800 chars and the reviewer sees ONLY the spec — no history, no workspace listing. Minimal context is a hard constraint; brae is acting as multiple engineers and we isolate their view.

## 11. Testing

- Unit: scaffold renderer (golden-file against `testdata/`), aggregator verdict logic, totals DAG validation, DoIt-gate join semantics (multi-token CPN mechanics), TDD loop termination.
- Integration: mock LLM client with scripted responses drives a full build of a trivial `echo-tool` end-to-end; assert `workspace/tools/bin/echo-tool` exists and `p-registered` has a token.
- Package coverage ≥85% (dog-fooding the gate).

## 12. Rollout

- v1 (this spec): topology + scaffold + in-line TDD loop + register. No GAP-4 synthesis of per-total TDD sub-CPNs — the `t-tdd-loop` handler runs the loop procedurally.
- v2: replace `t-tdd-loop` with `NodeKindSynthesize`+`NodeKindInstantiate` so each total spins up a per-total TDD sub-CPN (better observability; each iteration becomes a proper transition firing with events).
- v3: let tool-creator author its own templates by learning from the last M accepted projects.

## 13. Open questions

- Should `workspace/tools/src/` be a git submodule of the main repo or an independent worktree? *Tentative: independent — keeps the main repo's diff clean.*
- How to handle a tool that needs a non-stdlib dep? *Tentative: `t-tdd-loop` may `go get` through a `policy-gate-approved` allowlist of modules. v1 allows stdlib only; v2 opens the allowlist.*
- Should `t-aggregate-reviews` weight roles (e.g., architect > PM)? *Tentative: equal weight in v1; any single `blocking_issues` entry blocks. Simpler to reason about.*
