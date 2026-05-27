---
name: handoff
description: |
  Snapshot HADES session state to .hades/session.md, commit with conventional
  message (no AI attribution), prepare for /clear or session end.
  Sister skill to /hades:start.
keywords:
  - hades
  - session-end
  - handoff
  - snapshot
  - state-recovery
version: 0.12.0
license: Apache-2.0
agentskills_version: 1.0
---

# HADES — Session handoff / snapshot skill

Use this skill at the end of a Hermes session before `/clear` OR when the
operator says "handoff", "snapshot state", or "guardar progreso".

Operator entry point: invoke `/hades:handoff` (slash command registered
by the plugin). The slash command handler emits a proposed .hades/session.md
content + proposed commit message; operator reviews and runs the
actual git commit (typically via Hermes' Bash tool in the same session).

> **Note**: Step 1 (synthesize state) is partially mechanized in the
> slash command handler (`commands/handoff.py` collects the git brief
> + existing-HANDOFF read + commit-message template); steps 2 (read
> existing .hades/session.md), 3 (compose updated .hades/session.md fields with
> session-specific TL;DR + plan status + pending dispatches), 4
> (propose commit message wording), and 5 (operator decides apply vs
> skip) apply to the LLM consuming this skill in conversation context.

## Steps

1. **Synthesize current session state**:
   - What was accomplished (1-3 sentences for TL;DR)
   - Last commit hash + subject
   - Branch name + any uncommitted changes
   - Active plan/phase (if executing a plan)
   - Background subagents in flight (CRITICAL: non-recoverable across
     session ends — list them so operator can re-dispatch in next session)
   - Pending operator actions (e.g., "operator must run `bin/hades bypass
     extract-config` interactively")
   - Suggested first-message for next session (specific, actionable;
     ideally a single-letter approval like `procede` or `y`)

2. **Read existing .hades/session.md** (if present) to preserve format conventions
   and any sections this session did NOT touch.

3. **Compose updated .hades/session.md** with sections (template below):

   ```markdown
   # HADES — Session handoff

   _Last updated: <ISO timestamp>_

   ## TL;DR

   <1-3 sentences>

   ## Repo state

   - Branch: <name>
   - Last commit: `<hash>` "<subject>"
   - Uncommitted: <yes/no>; <list if yes>
   - Recent tags: <last 3>

   ## Active plan status

   <release item release track — <status>; or "no active plan">

   ## Pending dispatches

   <Background subagents launched but not yet collected; non-recoverable>

   ## Pending operator actions

   <Items awaiting operator>

   ## Suggested first-message

   <Single-letter approval OR concrete next step>

   ## See also

   - `design records`
   - `docs/METHODOLOGY.md`
   - `local agent memory`
   ```

4. **Propose commit message**:

   ```
   docs(handoff): refresh post <brief-context>
   ```

   Conventional commit, NO AI attribution. invariant gate enforces
   automatically (pre_tool_call callback blocks if violated).

5. **Operator decides**:
   - **Apply locally**: operator runs the proposed git commands in their
     terminal (or via Hermes' Bash tool).
   - **Apply + push**: same as above plus `git push origin <branch>`.
   - **Skip**: no-op.

## Edge cases

- **.hades/session.md doesn't exist yet**: create from scratch using the section
  template above
- **No changes since last commit + HANDOFF unchanged**: skip the empty
  commit; ask operator if the session should end without snapshot
- **Background subagents in flight**: list them under Pending dispatches
  with explicit "NON-RECOVERABLE; will need re-dispatch in next session"
- **Pending operator actions exist (e.g., bypass-config)**: surface
  prominently in TL;DR — operator should not /clear without seeing them

## Doctrine reminders

- **No AI attribution**: handoff commit subject must NOT include any
  AI-attribution marker; pre_tool_call callback gates
- **Tag safety gate**: handoff does NOT push tags; only the conventional
  commit + optional push of main branch
- **Conventional commits**: subject `docs(handoff): <imperative>`
  (no trailing period, lowercase)

## See also

- `/hades:start` — sister skill: read .hades/session.md and resume
- `docs/METHODOLOGY.md` §".hades/session.md maintenance"
- Project project instructions (Hermes-substrate version)
