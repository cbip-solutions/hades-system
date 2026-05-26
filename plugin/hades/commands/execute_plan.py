# SPDX-License-Identifier: MIT
"""/hades:execute-plan handler — subagent-driven execution of phase plan.

raw_args: plan_path (required) — the phase plan file to execute.
"""

from __future__ import annotations

_PROMPT = """# /execute-plan — Implement phase plan via subagent dispatch

You are executing a phase plan for HADES. Execution is task-by-task subagent dispatch with review gates.

## 1. Load the executing-plans + subagent-driven-development skills

```
skill_load("superpowers:executing-plans")
skill_load("superpowers:subagent-driven-development")
```

## 2. Pre-flight Stage 0 reality-check (BEFORE first dispatch)

Per `feedback_methodology_and_conventions.md` §13 Stage 0 + memoria `feedback_plan_template_drift.md`:

```bash
# Extract every package.Symbol reference from the phase plan-file:
grep -ohE '[a-z][a-z_]+\\.[A-Z][a-zA-Z]+' \\
  {plan_path} \\
  | sort -u > /tmp/plan-symbols.txt

# For each unfamiliar symbol, verify it exists in current codebase:
while read sym; do
  pkg="${{sym%.*}}"; name="${{sym##*.}}"
  hits=$(grep -rln "type $name \\|func .*$name(" internal/ 2>/dev/null | head -3)
  [ -z "$hits" ] && echo "MISSING: $sym"
done < /tmp/plan-symbols.txt
```

Decision tree:
- 0-2 missing: include "verify against codebase; adapt with deviation note" in implementer prompt
- 3-5 missing: inline-edit plan-file in main session (one `docs(plan-N): ...` commit) THEN dispatch
- ≥6 missing OR fundamental shape mismatch: dispatch dedicated doc-revision Opus subagent; halt implementer

## 3. Per-task execution loop

For each task in phase file:

1. Mark task in_progress (TaskUpdate)
2. Dispatch implementer subagent (general-purpose; model per master plan dispatch matrix)
3. Wait for DONE / DONE_WITH_CONCERNS / NEEDS_CONTEXT / BLOCKED
4. If NEEDS_CONTEXT: provide info, re-dispatch
5. If BLOCKED: assess (context vs scope vs reasoning); break or escalate
6. Dispatch spec compliance reviewer (general-purpose)
7. If ISSUES: dispatch fix subagent → re-review (don't accept "close enough")
8. Dispatch code quality reviewer (`superpowers:code-reviewer`)
9. If CHANGES_REQUESTED: dispatch fix subagent → re-review
10. Mark task complete (TaskUpdate)
11. Next task or next phase

**Run all in foreground** (NOT background). Background only for plan-writing.

## 4. Doctrine — no tech debt, no defer

Per project CLAUDE.md + memoria `feedback_no_tech_debt.md` + `feedback_no_defer.md`:

- Reviewer "Minor" tag is severity, NOT permission to skip
- Every Minor surfacing real coverage gap / missing test for documented behavior / uncovered branch on existing code MUST be fixed before next task
- Only skip: forward-looking design notes that depend on future phase's input; plan-verbatim style preferences; cosmetic-only changes

## 5. Hard gates per task commit

```bash
make build && make lint && make test && make verify-invariants
go test -race ./... -count=2
GOOS=linux go build ./...
```

For Plan 12 Phase A/B/D Python plugin code:
```bash
ruff check plugin/hades/
mypy plugin/hades/
pytest plugin/hades/tests/ -v --cov=plugin/hades --cov-report=term --cov-fail-under=85
```

Coverage targets: ≥85% new code; ≥90% security/correctness-critical (per project CLAUDE.md "Hard rules" #5).

## 6. NO Claude attribution (inv-zen-004)

Every commit message: `feat(scope): subject` (imperative, lowercase, no trailing period). NO `Co-Authored-By: prohibited assistant`. NO `Generated with prohibited assistant`. Plugin hook regex-rejects.

## Cross-references

- docs/METHODOLOGY.md §4 plan-execution
- feedback_methodology_and_conventions.md §4
- feedback_plan_template_drift.md (Stage 0 reality-check)
"""


def execute_plan_handler(raw_args: str) -> str | None:
    """/hades:execute-plan handler. raw_args is plan_path (required)."""
    plan_path = raw_args.strip()
    if not plan_path:
        return (
            "ERROR: /hades:execute-plan requires a plan_path argument.\n"
            "Usage: /hades:execute-plan <path-to-phase-plan>"
        )
    return _PROMPT.format(plan_path=plan_path)
