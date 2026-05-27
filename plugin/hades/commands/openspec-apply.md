---
name: openspec:apply
description: Run the swarm to implement tasks.md (parallel subagents)
arguments:
  - name: feature_name
    type: string
    required: true
---

# Apply phase for: {{feature_name}}

You are starting the apply phase for `{{feature_name}}`. Per spec §3.2:

1. Read `openspec/changes/{{feature_name}}/tasks.md`.
2. POST to daemon `/v1/swarms` with project + feature + tasks JSON.
   Daemon spawns subagents per task following routing.toml + dispatcher.
3. Stream SSE events from `http://unix/v1/swarms/<id>/events`.
4. As tasks reach phases (codegen / tests / fix-loop / commit), surface
   summaries to the operator. Be calm-by-default: don't surface every
   transition, only attention items (escalations + completions).

The daemon owns subagent lifecycle. Closing this OpenCode session does
NOT abort the swarm. Reopening this session re-attaches.

HADES design implements the wiring that makes this command functional.
