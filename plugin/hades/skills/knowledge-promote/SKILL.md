---
name: knowledge-promote
description: |
  HADES knowledge promotion: promote a project-local item to global
  cross-project memory (the release design D aggregator.Promote, audit-logged). Use
  when operator invokes /hades:knowledge-promote or decides item should be global.
license: Proprietary
agentskills_version: 1.0
keywords:
  - knowledge-promote
  - global-memory
  - aggregator
  - audit-chain
  - the release design
  - hades
---

# HADES — knowledge-promote skill (knowledge promotion)

This skill promotes a knowledge item from project-local to global cross-project
memory via the release design D's `aggregator.Promote()`. Triggered by `/hades:knowledge-promote`.

## When to use

- Operator invokes `/hades:knowledge-promote <item-id> <reason>`
- When a project-local finding should be visible to all in-scope projects
- After verifying item via `/hades:knowledge-query` (check it's worth promoting)

## Workflow

### 1. Pre-flight visibility check

Verify item exists + operator can see it:
```bash
curl --unix-socket /tmp/hades-system.sock -s "http://unix/v1/knowledge/<id>" | jq '.'
```

### 2. POST promote to daemon

```bash
curl --unix-socket /tmp/hades-system.sock \
     -X POST \
     -d '{"reason":"<reason>"}' \
     "http://unix/v1/knowledge/<id>/promote"
```

Daemon: adds to `global_pins` table + anchors `KnowledgePromoted` event in
the release design Tessera audit chain with operator identity from keychain.

### 3. Invariants

- Reason REQUIRED (load-bearing for audit chain readability)
- Reason MUST NOT contain Claude/Anthropic/AI attribution (inv-hades-004)
- 409 if already promoted; 422 if capa-firewall project (needs explicit override)

### 4. Reverse: unpromote

```bash
hades knowledge unpromote <id> --reason "..."
```

CLI only (no slash; demoting is rare + less time-critical).

## Cross-references

- spec §9.1 the release design D aggregator.Promote
- spec §4.6 audit chain integration
- inv-hades-163 privacy boundary (promotion crosses it)
- /hades:knowledge-promote slash command handler
