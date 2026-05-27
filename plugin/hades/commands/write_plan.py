# SPDX-License-Identifier: MIT
"""/hades:write-plan handler — TDD-task-decomposed plan-writing workflow.

raw_args: spec_path (required) — the design spec to convert to implementation plan files.
"""

from __future__ import annotations

_PROMPT = """# /write-plan — Generate plan files from spec

You are starting plan-writing for HADES. Plan-writing converts a frozen design spec into TDD-task-decomposed implementation plan files.

## 1. Load the writing-plans skill explicitly

```
skill_load("superpowers:writing-plans")
```

## 2. Read the spec

Read `{spec_path}`. Verify it is frozen (per memoria `feedback_spec_hierarchy_and_plan_types.md`) — at least 6 design sections approved + Q&A complete.

## 3. Apply HADES methodology

Per `feedback_methodology_and_conventions.md` §3:

### Master + phase files pattern (REQUIRED)
- Master file: `design records` (~150-200 lines)
- Phase files: `design records` (~700-3000 lines each)
- Phase letters: A through K/L/N depending on plan size

### Master plan structure
- Phase index table (Phase / File / Scope / Q-source / Tasks / LOC / Critical-path)
- Quality gates per phase
- Doctrine applied
- Phase ordering DAG
- Subagent dispatch model selection
- Reference paths
- Known integration adjustments

### Plan-writer dispatch strategy
1. Write master inline (~150 lines, single message)
2. Dispatch N writer subagents in parallel (one per phase) using `general-purpose`
3. Each writer prompt includes: required reading, scope, decisions, Q-tags, types/functions, tasks list, output path, format rules
4. Run with `run_in_background: true` for parallelism (BUT see watchdog mitigation below)
5. Wait for all notifications

### CRITICAL — Watchdog mitigation

Background subagents have **600s stream watchdog**. Composing >2000-line markdown in a single Write tool call → silent thinking exceeds 600s → killed.

Mitigation: incremental Write+Edit per memoria `feedback_plan_writer_watchdog_strategy.md`:
- Initial Write with file header + Task 1 only
- Sequential Edits per remaining task (each Edit = streamed tool call)
- Final Edit appends verification checklist

If background writer fails 2+ times → dispatch foreground (NOT `run_in_background`).

## 4. Two-stage self-review (mandatory for ≥4 parallel writers)

Per `feedback_methodology_and_conventions.md` §13:

### release stage — Mechanical greps
- Placeholder scan (TBD/FIXME/XXX/implement-later)
- Claude attribution scan
- Type-name uniqueness across phases
- Module path consistency
- inv-hades-XXX coverage

### release stage — Code-reviewer subagent dispatch (MANDATORY)
- Dispatch `superpowers:code-reviewer` foreground
- Cross-phase signature/field-set drift
- Skipping release stage = compile errors guaranteed during execution

### When findings ≥1 CRITICAL
Doctrine "no defer" + "no tech debt" prohibit pushing with known CRITICAL findings. Fix inline before commit.

## 5. Output

Plan files committed (no push). Operator runs `/execute-plan` next.

## Cross-references

- docs/METHODOLOGY.md §3 plan-writing
- feedback_methodology_and_conventions.md §3 + §13
- feedback_plan_writer_watchdog_strategy.md
"""


def write_plan_handler(raw_args: str) -> str | None:
    """/hades:write-plan handler. raw_args is spec_path (required)."""
    spec_path = raw_args.strip()
    if not spec_path:
        return (
            "ERROR: /hades:write-plan requires a spec_path argument.\n"
            "Usage: /hades:write-plan <path-to-design-spec>"
        )
    return _PROMPT.format(spec_path=spec_path)
