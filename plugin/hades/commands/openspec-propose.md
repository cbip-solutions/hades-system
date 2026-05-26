---
name: openspec:propose
description: Begin the propose phase for a new feature (Modo C híbrido)
arguments:
  - name: feature_name
    type: string
    required: true
---

# Propose phase for: {{feature_name}}

You are starting the propose phase for feature `{{feature_name}}`. Per
spec §3.1 + inv-zen-015, follow this flow:

1. **Load the brainstorming skill** explicitly:
   ```
   skill_load("superpowers:brainstorming")
   ```
   The skill cannot be auto-triggered by keyword on OpenCode (R6 verified
   discover-then-call semantics) — explicit invocation is required.

2. **Follow brainstorming with these zen-swarm adaptations:**
   - Output format: OpenSpec (proposal/design/tasks/deltas)
   - Write to: `openspec/changes/{{feature_name}}/`
   - When foundational questions are settled, write the four `.md` files
     and announce "doc-live mode active" — the operator can now edit
     directly, and the daemon's file watcher will surface diffs in
     subsequent conversation turns

3. **Doctrine awareness**: read `AGENTS.md` to determine project doctrine:
   - `max-scope`: tasks.md must include "tradeoff hacia menos justificado"
     when not at full scope
   - `capa-firewall`: claim-strength tier per assertion (Empirical /
     Interpretation / Posterior); subagents WRITE but do NOT commit
     (advisory mode)
   - `default`: stock templates

4. **When operator runs `/propose-done`**: invoke pre-flight (Plan 9
   wires daemon endpoint that runs RAG audit on tasks.md against the
   codebase).

Begin now. Ask one question at a time.
