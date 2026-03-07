---
name: skill-creator
description: |
  Create new skills or improve existing ones for the liwaisi agent. Use when the
  user says "create a skill", "make a skill for", "turn this into a skill",
  "I want a skill that", or asks to package a workflow as a reusable skill.
metadata:
  author: liwaisi
  version: "1.0.0"
  domain: meta
  builtin: "true"
---
# Skill Creator

You are creating a new skill for the liwaisi agent. Skills are packaged procedural
knowledge stored as SKILL.md files that teach the agent how to accomplish
multi-step workflows. Follow these steps carefully.

## Step 1: Capture Intent

Ask the user:
1. **What does the skill do?** (e.g., "scaffold a Go project", "review code")
2. **When should it trigger?** (what phrases would a user say?)
3. **What is the expected output?** (files created, report generated, etc.)
4. **Does it need reference files?** (templates, checklists, conventions)

## Step 2: Research the Domain

Use available tools to understand what the skill needs:
- Use `tree` and `read_file` to study existing examples if relevant
- Identify which tools the skill will orchestrate (read_file, write_file, etc.)
- Note any conventions or patterns the skill should follow

## Step 3: Write the SKILL.md

Create the skill file using the format below. The file MUST have:

### YAML Frontmatter (between `---` delimiters)

```yaml
---
name: skill-name-here
description: |
  Clear description of what the skill does and when to use it.
  Include trigger phrases: "create a ...", "scaffold ...", "review ...".
  Keep under 500 characters. No angle brackets (< or >).
metadata:
  author: user
  version: "1.0.0"
  domain: the-domain
---
```

**Frontmatter rules:**
- `name`: lowercase letters, numbers, and hyphens only (3-50 chars, no leading/trailing hyphen)
- `description`: MUST include trigger phrases that tell the agent when to activate
- `description`: Be "pushy" — describe clearly so the agent knows when this skill applies

### Markdown Body (after closing `---`)

Write imperative instructions the agent will follow. Include:
- Step-by-step workflow with numbered steps
- Which tools to use at each step (reference by exact tool name)
- Expected inputs and outputs
- Edge cases and error handling
- Examples where helpful

### Reference Files (optional)

If the skill needs templates, checklists, or reference material:
1. Create a `references/` subdirectory in the skill directory
2. Place files there (e.g., `references/template.md`)
3. In the SKILL.md body, instruct the agent to use `read_skill_resource` to load them

## Step 4: Save the Skill

Use `create_directory` and `write_file` to save the skill. All paths are
relative to your workspace root:

```
skills/{skill-name}/SKILL.md
```

For reference files:
```
skills/{skill-name}/references/{filename}
```

## Step 5: Verify

Call `list_skills` to confirm the new skill appears in the registry.
If it does not appear, check:
- The SKILL.md file exists in the correct location
- The YAML frontmatter is valid (proper delimiters, valid name, description present)
- The name follows the naming rules

Report the result to the user and suggest they test the skill by asking a
question that matches its trigger phrases.
