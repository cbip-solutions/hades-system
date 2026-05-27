---
name: write-plan
description: |
  HADES TDD-task-decomposed plan writing: master + phase files pattern,
  parallel phase-writer dispatch, watchdog mitigation, two-stage self-review.
  Use when operator invokes /hades:write-plan or after design spec is frozen.
license: Proprietary
agentskills_version: 1.0
keywords:
  - write-plan
  - plan-writing
  - TDD
  - parallel-writers
  - watchdog-mitigation
  - hades
---

# HADES — write-plan skill (plan-writing methodology)

This skill provides HADES's plan-writing methodology, invoked by the
`/hades:write-plan` slash command. It calls `skill_load("superpowers:writing-plans")`
then applies project-doctrine overrides.

## When to use

- Operator invokes `/hades:write-plan <spec-path>`
- After design spec is frozen (6 sections approved + Q&A complete)

## Workflow

### 1. Load writing-plans skill explicitly

```
skill_load("superpowers:writing-plans")
```

### 2. Read + verify spec is frozen

Verify at least 6 design sections approved + Q&A complete per
`feedback_spec_hierarchy_and_plan_types.md`.

### 3. Master + phase files pattern

- Master: `design records` (~150-200 lines)
- Phase files: `design records` (~700-3000 lines each)

### 4. Watchdog mitigation (CRITICAL)

Background subagents have 600s stream watchdog. Use incremental Write+Edit per
`feedback_plan_writer_watchdog_strategy.md`:
- Initial Write with file header + Task 1 only
- Sequential Edits per remaining task
- Final Edit appends verification checklist

### 5. Two-stage self-review (mandatory for ≥4 parallel writers)

release stage — Mechanical greps (placeholder/attribution/uniqueness/module-path/inv-zen coverage).
release stage — Code-reviewer subagent dispatch (MANDATORY; catches cross-phase signature drift).

## Cross-references

- docs/METHODOLOGY.md §3 plan-writing
- feedback_methodology_and_conventions.md §3 + §13
- feedback_plan_writer_watchdog_strategy.md
- /hades:write-plan slash command handler
