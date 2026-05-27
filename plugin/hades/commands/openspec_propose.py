# SPDX-License-Identifier: MIT
"""/hades:openspec-propose handler — Begin the propose phase for a new feature (Modo C híbrido).

Port from CC-format plugin/hades/commands/openspec-propose.md workflow logic.
Per invariant: explicitly loads superpowers:brainstorming skill (discover-then-call semantics).
"""

from __future__ import annotations

_PROMPT = """# HADES /openspec-propose — Propose phase for {feature_name}

You are starting the propose phase for feature `{feature_name}` via HADES. Per spec §3.1 + invariant, follow this flow:

## 1. Load brainstorming skill explicitly

```
skill_load("superpowers:brainstorming")
```

The skill cannot be auto-triggered by keyword on Hermes (verified Spike-3, same as OpenCode discover-then-call semantics) — explicit invocation is required per invariant.

## 2. Apply HADES project-doctrine override (research-first)

BEFORE first Q&A question, dispatch research SOTA per ADR-0006 + memoria `feedback_research_first_brainstorm.md`. See `/hades:brainstorm` slash command for details. This applies to all `/hades:openspec-propose` flows.

## 3. Brainstorm with HADES adaptations

Per `feedback_methodology_and_conventions.md` §2 + §3:
- Output format: OpenSpec (proposal/design/tasks/deltas)
- Write to: `openspec/changes/{feature_name}/`
- ONE question per message; multiple choice A/B/C/D with explicit recommendation
- ~10-15 questions total
- After foundational wizard completes, write the four `.md` files
- Announce "doc-live mode active" — operator can edit directly; daemon's file watcher surfaces diffs in subsequent turns

## 4. Doctrine awareness

Read `project instructions` to determine project doctrine:
- `max-scope`: tasks.md must include "tradeoff hacia menos justificado" when not at full scope
- `capa-firewall`: claim-strength tier per assertion (Empirical / Interpretation / Posterior); subagents WRITE but do NOT commit (advisory mode)
- `default`: stock templates

## 5. When operator runs `/propose-done`

Invoke pre-flight (the release design wires daemon endpoint that runs RAG audit on tasks.md against the codebase).

## 6. Begin

Begin now. Ask one question at a time.

## Cross-references

- spec §3.1 propose phase
- invariant explicit skill loading (discover-then-call)
- ADR-0006 research-sota-always-integrated
- feedback_research_first_brainstorm.md
- /hades:brainstorm (companion workflow command)
- /hades:openspec-apply (next step after tasks.md ready)
"""

_PROMPT_NO_FEATURE = """HADES: feature name required for openspec-propose
  /hades:openspec-propose requires a feature name to create the openspec proposal directory.
  → Invoke /hades:openspec-propose <feature-name> where feature-name is alphanumeric + hyphens only (openspec/changes/<feature-name>/ will be created)
"""


def openspec_propose_handler(raw_args: str) -> str | None:
    """/hades:openspec-propose handler. raw_args: feature_name (required)."""
    feature_name = raw_args.strip()
    if not feature_name:
        return _PROMPT_NO_FEATURE
    return _PROMPT.format(feature_name=feature_name)
