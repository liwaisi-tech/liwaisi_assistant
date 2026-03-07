---
name: subagent-creator
description: |
  Create new subagents or improve existing ones for the liwaisi agent. Use when the
  user says "create a subagent", "make a subagent for", "define a new agent",
  "I want a specialist for", or asks to package expertise as a reusable subagent.
metadata:
  author: liwaisi
  version: "1.0.0"
  domain: meta
  builtin: "true"
---
# SubAgent Creator

You are creating a new subagent for the liwaisi agent. Subagents are specialized
AI assistants defined as SUBAGENT.md files that the parent agent can spawn to
handle focused tasks. Follow these steps carefully.

## Step 1: Capture Intent

Ask the user:
1. **What does the subagent specialize in?** (e.g., "security review", "database migration", "test writing")
2. **What tools does it need?** (e.g., read_file, grep, execute_command — leave empty for all tools)
3. **What model tier?** (fast for simple tasks, balanced for moderate, capable for complex)
4. **What are its constraints?** (max turns, timeout, specific focus areas)

## Step 2: Research the Domain

Use available tools to understand what the subagent needs:
- Use `list_subagents` to check for existing subagents that might overlap
- Use `list_skills` to see if a skill already covers this use case
- Identify which tools the subagent should have access to
- Note any conventions or patterns the subagent should follow

## Step 3: Write the SUBAGENT.md

Create the subagent file using the format below. The file MUST have:

### YAML Frontmatter (between `---` delimiters)

```yaml
---
name: subagent-name-here
description: |
  Clear description of what the subagent does and when to use it.
  Keep under 500 characters. No angle brackets.
model_tier: fast
allowed_tools:
  - read_file
  - grep
max_turns: 10
timeout: 2m
metadata:
  author: user
  version: "1.0.0"
---
```

**Frontmatter rules:**
- `name`: lowercase letters, numbers, and hyphens only (3-50 chars, no leading/trailing hyphen)
- `description`: concise explanation of the subagent's specialty
- `model_tier`: one of `fast`, `balanced`, or `capable`
- `allowed_tools`: list of tools the subagent can use (omit for all tools)
- `denied_tools`: list of tools the subagent cannot use (alternative to allowed_tools)
- `max_turns`: maximum LLM turns before stopping (default: 10)
- `timeout`: maximum execution time (e.g., "2m", "5m", default: "2m")

### Markdown Body (after closing `---`)

Write the subagent's system instruction as markdown. This becomes the subagent's
persona and expertise. Include:
- Role definition (who the subagent is)
- Specific areas of focus
- Output format expectations
- Constraints and guidelines
- Examples where helpful

Apply the LLM-as-simulator principle: define the subagent as a specific expert
persona rather than a generic assistant.

## Step 4: Save the SubAgent

Use `create_directory` and `write_file` to save the subagent. All paths are
relative to your workspace root:

```
subagents/{subagent-name}/SUBAGENT.md
```

## Step 5: Verify

Call `list_subagents` to confirm the new subagent appears in the registry.
If it does not appear, check:
- The SUBAGENT.md file exists in the correct location
- The YAML frontmatter is valid (proper delimiters, valid name, description present)
- The name follows the naming rules

Report the result to the user and suggest they test the subagent by using
`spawn_subagent` with the new agent's name and a sample task.
