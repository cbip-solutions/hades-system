# SPDX-License-Identifier: MIT
"""/hades:openspec-apply handler — Run the swarm to implement tasks.md (parallel subagents).

Port from CC-format plugin/hades/commands/openspec-apply.md workflow logic.
Name convention: "openspec:apply" → "openspec-apply" (Hermes does NOT allow ':' in slash names
per spike §3 verification).
"""

from __future__ import annotations

_PROMPT = """# HADES /openspec-apply — Apply phase for {feature_name}

You are starting the apply phase for `{feature_name}` via HADES. Per spec §3.2:

## 1. Read tasks file

```bash
cat openspec/changes/{feature_name}/tasks.md
```

If file missing, abort with: "ERROR: openspec/changes/{feature_name}/tasks.md missing — run /openspec-propose first."

## 2. POST to daemon

```bash
TASKS_JSON=$(jq -R -s . < openspec/changes/{feature_name}/tasks.md)
# pending endpoint registration: /v1/project/active resolves active project alias
PROJECT=$(curl --unix-socket /tmp/zen-swarm.sock -s http://unix/v1/project/active)

curl --unix-socket /tmp/zen-swarm.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"project":"'"$PROJECT"'","feature":"{feature_name}","tasks":'"$TASKS_JSON"'}}' \\
     http://unix/v1/swarms
```

Response includes `swarm_id`. HADES daemon spawns subagents per task following Plan 4 routing.toml + Plan 3 dispatcher.

## 3. Stream SSE events

```bash
SWARM_ID=<from response>
# pending endpoint registration: swarm SSE event stream awaits zen migrate
curl --unix-socket /tmp/zen-swarm.sock --no-buffer \\
     "http://unix/v1/swarms/$SWARM_ID/events"
```

As tasks reach phases (codegen / tests / fix-loop / commit), surface summaries to operator. Be calm-by-default: surface only attention items (escalations + completions).

## 4. Daemon owns subagent lifecycle

Closing this Hermes session does NOT abort the swarm. Reopening this session re-attaches via `/hades:openspec-resume {feature_name}`.

## 5. NO Claude attribution in any swarm-emitted commit (inv-zen-004)

Per inv-zen-004 + project CLAUDE.md "Hard rules" #1: every commit message emitted by swarm subagents MUST NOT contain Claude/Anthropic/AI attribution. Plan 11 substrate hook regex-rejects.

## 6. Cross-references

- spec §3.2 apply phase
- Plan 4 routing.toml + Plan 3 dispatcher
- Plan 5 worktree manager
- Plan 6 archive phase (next step)
- /hades:openspec-resume (resume mid-flight)
- /hades:openspec-archive (post-implementation merge)
"""

_PROMPT_NO_FEATURE = """HADES: feature name required for openspec-apply
  /hades:openspec-apply requires a feature name to identify the tasks.md to run.
  → Run /hades:openspec-propose <feature-name> first to create the tasks.md, then invoke /hades:openspec-apply <feature-name>
"""


def openspec_apply_handler(raw_args: str) -> str | None:
    """/hades:openspec-apply handler. raw_args: feature_name (required)."""
    feature_name = raw_args.strip()
    if not feature_name:
        return _PROMPT_NO_FEATURE
    return _PROMPT.format(feature_name=feature_name)
