---
name: new-feature
description: Design and spec new features for liwaisi_assistant as comprehensive GitHub issues. Combines expert panel analysis, SOTA research, foundational document alignment, and hexagonal architecture design into a single command.
triggers:
  - new feature
  - design feature
  - feature spec
  - feature issue
argument-hint: <FEATURE_DESCRIPTION> [--ref-issue NUMBER] [--cu CODE] [--no-research] [--dry-run]
allowed-tools:
  - Bash(gh *)
  - Bash(find *)
  - Bash(ls *)
  - Bash(wc *)
  - Bash(cat *)
  - Read
  - Glob
  - Grep
  - WebSearch
  - WebFetch
---

# New Feature Specification Skill

You are designing a new feature for the `liwaisi_assistant/back/go-assistant` project and will produce a comprehensive GitHub issue that matches the quality and depth of existing feature issues (#16, #21, #25, #27).

## Argument Parsing

Parse `$ARGUMENTS` to extract:

- **Feature description** — Everything before any `--` flags. This is REQUIRED.
- `--ref-issue NUMBER` — Reference issue number to calibrate quality against (default: fetch the latest feature issue)
- `--cu CODE` — ClickUp task code for branch naming (e.g., `86b7hqxvc`)
- `--no-research` — Skip Phase 3 (SOTA web research)
- `--dry-run` — Display the full issue body without creating it on GitHub

If no feature description is provided, display this usage help and stop:

```
Usage: /new-feature <FEATURE_DESCRIPTION> [options]

Options:
  --ref-issue NUMBER   Reference issue for quality calibration (default: latest)
  --cu CODE            ClickUp task code for branch naming
  --no-research        Skip SOTA web research phase
  --dry-run            Preview issue body without creating it

Example:
  /new-feature Add a rate-limiting middleware for the HTTP server --ref-issue 27
  /new-feature Add persistent conversation memory --no-research --dry-run
```

## Expert Panel Perspective

Throughout all phases, think from these perspectives simultaneously:

- **Senior Go Engineers** — Idiomatic Go, concurrency patterns, error handling, goroutine safety, `context.Context` propagation, interface-driven design
- **Software Architects** — Hexagonal architecture compliance, SOLID principles, dependency injection, clean separation of domain/application/infrastructure layers
- **Solution Architects** — Integration points, scalability, security boundaries, observability, deployment considerations
- **Product Managers** — User value proposition, scope control, incremental delivery, acceptance criteria that verify real outcomes

## Phase 1: Internalize Foundational Documents

Read ALL three foundational documents in `.docs/` at the repository root:

```
.docs/building-blocks-ai-agents.md
.docs/building-effective-agents.md
.docs/building-agents-with-go-and-openrouter.md
```

If any of these files are missing or unreadable, **stop immediately** and tell the user:

```
ERROR: Missing foundational document(s). The .docs/ directory must contain:
  - building-blocks-ai-agents.md
  - building-effective-agents.md
  - building-agents-with-go-and-openrouter.md
```

While reading, extract **3-5 quotes** that are directly relevant to the feature being designed. These will be used in the Motivation section. Each quote must:
- Be an exact excerpt (not paraphrased)
- Be wrapped in `> *"..."*`
- Include attribution: `> — \`.docs/{filename}\``
- Be genuinely relevant to the feature's design decisions

## Phase 2: Explore Codebase & Reference Issue

### 2.1 Project Structure

Understand the current project layout:

```bash
find back/go-assistant -type f -name "*.go" | head -80
```

Use Glob and Grep to identify:
- Existing toolsets and how they're registered
- Domain entities, value objects, and port interfaces
- Application services pattern
- Infrastructure adapters pattern
- How `internal/` is organized (domain, application, infrastructure layers)
- The `tool.Set` interface and existing implementations

### 2.2 Reference Issue

Fetch the reference issue to calibrate quality:

```bash
gh issue view <REF_ISSUE_NUMBER> --json title,body,labels
```

Study its structure, depth, and quality bar. The new issue must match or exceed this standard.

### 2.3 Similar Features

Search the codebase for patterns related to the new feature. Look for:
- Existing code that touches the same domain
- Interfaces the new feature might implement or extend
- Configuration patterns the feature should follow
- Test patterns used in similar components

## Phase 3: SOTA Research (skip with `--no-research`)

If the `--no-research` flag is set, skip this phase entirely and note in the issue:

```
> *SOTA research skipped via `--no-research` flag.*
```

Otherwise, perform 2-4 web searches related to:
- Current best practices for the feature's domain (e.g., "Go rate limiting middleware 2026")
- How similar features are implemented in comparable projects
- Relevant Go libraries and their trade-offs
- Security considerations specific to this feature type

Synthesize findings into:
- A **comparison table** of approaches/libraries
- **Key insights** (numbered list)
- **"Our Position"** paragraph explaining which approach fits liwaisi_assistant's architecture

## Phase 4: Expert Design

This is the core intellectual work. Apply the expert panel perspectives to design the feature within the project's hexagonal architecture:

### Go Engineering Design
- Define Go structs, interfaces, and function signatures
- Plan concurrency patterns (if applicable)
- Design error types and error handling strategy
- Identify where `context.Context` propagation is needed

### Architectural Placement
- Map feature components to hexagonal layers: domain entities/value objects, output ports, input ports, application services, infrastructure adapters
- Identify which existing packages to extend vs. new packages to create
- Plan dependency injection wiring

### Integration Design
- How the feature connects to existing components
- API or tool interface exposed to the LLM
- Configuration requirements
- Security boundaries

### Delivery Planning
- Break into 4-7 incremental implementation phases
- Each phase should be independently testable
- Define 8-15 acceptance criteria per phase as checkbox items
- Identify dependencies between phases

## Phase 5: Compose Issue Body

Read the canonical template:

```
back/go-assistant/.claude/skills/new-feature/references/issue-template.md
```

Write the full issue body following the 9-section structure exactly. Apply these quality rules:

### Quality Checklist (self-review before proceeding to Phase 6)

Verify ALL of the following before creating the issue:

- [ ] **Title** follows `feat: Add {feature name} — {one-line value proposition}`
- [ ] **Summary** has 2-3 paragraphs with bold keywords, covers WHAT + WHY + VALUE + HOW
- [ ] **Motivation** includes 3+ foundational doc quotes with exact attribution
- [ ] **Motivation** has "Why Now" numbered list and "Design Philosophy" bullets
- [ ] At least **1 Architecture Decision** section with comparison table of alternatives
- [ ] **Technical Specification** includes Go structs or JSON schemas
- [ ] At least **1 Mermaid diagram** (sequence, class, or flowchart)
- [ ] **4-7 Implementation Phases** with files to create/modify listed
- [ ] Each phase has **8-15 checkbox acceptance criteria**
- [ ] **Testing Strategy** includes unit test table and 80%+ coverage target
- [ ] **5+ Out of Scope** items with brief justification
- [ ] **References** section links all cited sources
- [ ] Issue body is **15,000+ characters** (verify with `wc -c`)
- [ ] No placeholder text, no TODOs, no "TBD" sections

If any check fails, fix it before proceeding.

## Phase 6: Create Issue

### 6.1 Write to Temp File

The issue body will be 15K-55K characters — too large for inline `--body`. Write it to a temp file:

```bash
cat << 'ISSUE_EOF' > /tmp/new-feature-issue-body.md
{FULL ISSUE BODY HERE}
ISSUE_EOF
```

Verify the file:

```bash
wc -c /tmp/new-feature-issue-body.md
```

### 6.2 Dry Run Check

If `--dry-run` was specified:
- Display the full issue body to the user
- Show the character count
- Show the proposed title
- Stop here — do NOT create the issue

Tell the user:

```
DRY RUN COMPLETE
Title: feat: Add {X} — {Y}
Body: {N} characters
Labels: enhancement

To create the issue, run:
  /new-feature {same description} --ref-issue {N}
```

### 6.3 Create on GitHub

```bash
gh issue create \
  --title "feat: Add {feature name} — {value proposition}" \
  --body-file /tmp/new-feature-issue-body.md \
  --label "enhancement"
```

If `gh` fails with an authentication error, tell the user:

```
ERROR: GitHub CLI not authenticated. Run:
  gh auth login
```

If creation fails for any other reason, inform the user that the body is saved at `/tmp/new-feature-issue-body.md` as backup.

### 6.4 Post-Creation Summary

After successful creation, display:

```
Issue created successfully!
  URL: {issue URL}
  Title: {title}
  Characters: {count}
  Sections: {count}
  Phases: {count}
  Acceptance criteria: {total count}

The issue body is also saved at /tmp/new-feature-issue-body.md
```

## Error Handling

| Condition | Action |
|-----------|--------|
| No feature description provided | Show usage help and stop |
| Missing `.docs/` files | Error message listing missing files, stop |
| `gh` not authenticated | Instruct user to run `gh auth login` |
| Reference issue not found | Warn and continue without reference calibration |
| Web search fails | Warn, note in SOTA section, continue |
| Issue creation fails | Save body to `/tmp/new-feature-issue-body.md`, show error |
