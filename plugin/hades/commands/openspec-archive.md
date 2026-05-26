---
name: openspec:archive
description: Three-tier review (routine/attention/decision) and merge
arguments:
  - name: feature_name
    type: string
    required: true
---

# Archive phase for: {{feature_name}}

Per spec §3.3 (Modo C híbrido):

1. Trigger archive via daemon `/v1/swarms/<id>/archive`. Daemon's
   audit MCP produces archive briefing JSON.
2. Render briefing in three tiers:
   - **TIER-ROUTINE**: auto-mergeable, single approval
   - **TIER-ATTENTION**: review-only (no decision required)
   - **TIER-DECISION**: needs operator call (cross-task conflict,
     audit flag, test failure operator wants to ship anyway)
3. Operator confirms each tier-decision item.
4. Apply deltas to `openspec/specs/<area>.md`.
5. Commit with trailers: `Zen-Trace-Id`, `Zen-Provider`, `Zen-Audit-Passed`.
6. **NEVER add Claude/Anthropic/AI attribution** (inv-zen-004 — the
   plugin's `tool.execute.before` hook regex-rejects).
7. Cleanup worktrees + branches per project's archive strategy.

Plan 6 implements the wiring.
