---
name: start
description: |
  Recover a HADES session: read .hades/session.md TL;DR, check git status,
  identify active plan, present concise session-resume summary, and await
  operator direction.
keywords:
  - hades
  - session-resume
  - handoff
  - recovery
  - context-restoration
version: 0.12.0
license: Apache-2.0
agentskills_version: 1.0
---

# HADES — Session start / recovery skill

Use this skill at the beginning of a Hermes session in a HADES project,
or after a `/clear` that wiped conversation context. Synthesizes the
session-resume documents into a concise restart point.

Operator entry point: invoke `/hades:start` (slash command registered
by the plugin). The slash command handler (in
`plugin/hades/commands/start.py`) executes the procedure below.

> **Note**: Steps 1, 3, 4 are mechanized in the slash command handler
> (`commands/start.py`); steps 2 (project memory) + 5 (5-line summary
> presentation) + 6 (await direction) apply to the LLM consuming this
> skill in conversation context.

## Inputs

None — this skill reads project files autonomously.

## Steps

1. **Read .hades/session.md** (root of repo). Extract:
   - `## TL;DR` — current state in 1-3 sentences
   - `## Repo state` — last commit + branch + tag (if any)
   - `## Active plan status` — which plan/phase is in flight
   - `## Pending dispatches` — background subagents not recoverable
   - `## Pending operator actions` — items awaiting operator
   - `## Suggested first-message` — primer for operator response

2. **Read project memory** at:
   `~/.claude/projects/-path-to-projects-hades-system/memory/MEMORY.md`.
   Confirm methodology + doctrine memory entries present.

3. **Check git state**:
   - `git status` — uncommitted changes
   - `git log --oneline -10` — recent commits
   - `git tag --list 'v*' --sort=-creatordate | head -5` — recent tags

4. **Check daemon state** (if applicable):
   - `pgrep -f zen-swarm-ctld` — daemon running
   - If running: `curl --unix-socket /tmp/zen-swarm.sock http://localhost/v1/health`

5. **Present 5-line session summary** to operator:

   ```
   ## HADES session resume

   - **State**: [from TL;DR]
   - **Repo**: branch <name>, last commit <hash> "<subject>"
   - **Active plan**: release item release track — <status>
   - **Pending operator actions**: <list or "none">
   - **Suggested next**: <from .hades/session.md "Suggested first-message">

   ¿procedo con <suggested next>, o cambiamos prioridad?
   ```

6. **Await operator direction**.

## Doctrine reminders (apply to ALL responses post-/start)

- **Max-scope always** — pick most complete solution
- **Build the final product, not the stages** — refactor toward final shape
- **No stubs** — production code complete day 1
- **No defer, no tech debt** — fix coverage gaps + missing tests before next task
- **No Claude attribution in commits** (invariant; gated by pre_tool_call callback)
- **Tag safety gate**: NEVER push tags without operator approval

## Edge cases

- **.hades/session.md missing**: instruct operator to invoke `/hades:handoff`
  in a prior session, OR proceed with `git status + log` summary alone
- **Methodology memory missing or stale (>14d)**: surface to operator,
  recommend re-reading `docs/METHODOLOGY.md`
- **Daemon not running**: state explicitly; some operations (caronte code-graph,
  /v1/events) unavailable until `zen-swarm-ctld` started

## See also

- `/hades:handoff` — sister skill: snapshot state to .hades/session.md + commit
- `~/.claude/projects/-path-to-projects-hades-system/memory/reference_session_continuity.md`
- Project project instructions (this skill replaces the CC-specific
  `.claude/commands/start.md` under Hermes substrate)
