# SPDX-License-Identifier: MIT
"""/hades:brainstorm handler — research-first brainstorming workflow.

Per inv-hades-015 + spec §1 Q9, Hermes is discover-then-call (not auto-trigger); this handler
returns a prompt that explicitly invokes superpowers:brainstorming after the project-doctrine
override pass (research SOTA dispatch per ADR-0006 + memoria feedback_research_first_brainstorm.md).
"""

from __future__ import annotations

_PROMPT = """# /brainstorm — Refine design via Q&A

You are starting a design brainstorm session for HADES. This command applies the project-doctrine override on top of generic `superpowers:brainstorming` per memoria `feedback_research_first_brainstorm.md`.

## 1. Load the brainstorming skill explicitly

```
skill_load("superpowers:brainstorming")
```

Per inv-hades-015 + spec §1 Q9, Hermes is discover-then-call (not auto-trigger). Explicit invocation required.

## 2. Apply HADES project-doctrine override

BEFORE first Q&A question, dispatch research SOTA per ADR-0006 + memoria `feedback_research_first_brainstorm.md`:

- Identify 3-5 SOTA topics relevant to the brainstorm scope
- Dispatch parallel research subagents (general-purpose) with research-cheap profile
- Each returns compact digest (markdown) with citations
- Aggregate findings + present to operator BEFORE Q1
- Acknowledge time-sensitivity per category (timeless / empirical-time-sensitive / verified-recent)

This is the project-doctrine override. Generic superpowers:brainstorming starts Q&A immediately; HADES research-first.

## 3. Q&A pattern (one question per message)

Per `feedback_methodology_and_conventions.md` §2:
- ONE question per message — overwhelming otherwise
- Multiple choice A/B/C/D with explicit recommendation
- Each option includes Pro/Con bullets
- ALWAYS state your recommendation: "Mi recomendación: D. Razones: ..."
- ~10-15 questions per brainstorm typical
- Probe ALL dimensions: architecture, data flow, errors, performance, security, operator UX, structure

## 4. Topic context

{topic_section}

## 5. Sections after Q&A close

After ~10 questions answered, transition to **6 design sections** (one per message, operator approves each):
1. Architecture + components
2. Data flow
3. Error handling
4. Testing strategy
5. Operator UX
6. Security model + invariantes compile-checked

Each section ends with "¿OK sección N?" — operator answers `y` to confirm.

## 6. Output

Write design spec to `design records` per `feedback_spec_hierarchy_and_plan_types.md` Layer 3 plan-level convention.

## Cross-references

- docs/METHODOLOGY.md §2 brainstorm methodology
- ADR-0006 research-sota-always-integrated
- feedback_research_first_brainstorm.md (project-doctrine override pattern)
"""


def brainstorm_handler(raw_args: str) -> str | None:
    """/hades:brainstorm handler. raw_args is optional topic seed."""
    topic = raw_args.strip()
    if topic:
        topic_section = f"Topic seed: **{topic}**"
    else:
        topic_section = "No topic seed — operator describes scope in first response."
    return _PROMPT.format(topic_section=topic_section)
