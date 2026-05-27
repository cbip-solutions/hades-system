---
name: execute-plan
description: |
  HADES subagent-driven plan execution: release stage reality-check, per-task
  dispatch loop with review gates, no-tech-debt doctrine, hard gates per commit.
  Use when operator invokes /hades:execute-plan or when implementing a phase plan.
license: Proprietary
agentskills_version: 1.0
keywords:
  - execute-plan
  - subagent-dispatch
  - review-gates
  - release stage
  - no-tech-debt
  - hades
---

# HADES — execute-plan skill (subagent-driven plan execution)

This skill provides HADES's plan execution methodology. Triggered by the
`/hades:execute-plan` slash command which loads `superpowers:executing-plans` +
`superpowers:subagent-driven-development`.

## When to use

- Operator invokes `/hades:execute-plan <plan-path>`
- When implementing a phase plan via subagent dispatch

## Workflow

### 1. Load execution skills explicitly

```
skill_load("superpowers:executing-plans")
skill_load("superpowers:subagent-driven-development")
```

### 2. release stage reality-check (BEFORE first dispatch)

Per `feedback_plan_template_drift.md`:
- Extract package.Symbol references from plan-file
- Verify each against current codebase
- Decision tree: 0-2 missing → note in prompt; 3-5 → fix plan first; ≥6 → halt

### 3. Per-task execution loop

For each task:
1. Mark in_progress (TaskUpdate)
2. Dispatch implementer subagent (foreground, not background)
3. Wait for result
4. Dispatch spec compliance reviewer
5. Dispatch code quality reviewer (`superpowers:code-reviewer`)
6. Fix reviewer findings (no tech debt, no defer)
7. Mark complete (TaskUpdate)

### 4. Doctrine — no tech debt, no defer

Reviewer "Minor" tag is severity, NOT permission to skip. Every Minor surfacing
real coverage gap MUST be fixed before next task.

### 5. Hard gates per task commit

```bash
make build && make lint && make test && make verify-invariants
go test -race ./... -count=2
GOOS=linux go build ./...
```

## Cross-references

- docs/METHODOLOGY.md §4 plan-execution
- feedback_plan_template_drift.md (release stage reality-check)
- feedback_no_tech_debt.md
- /hades:execute-plan slash command handler
