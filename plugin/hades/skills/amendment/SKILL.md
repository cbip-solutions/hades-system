---
name: amendment
description: |
  HADES doctrine-amendment lifecycle: list, show, acknowledge, or deny
  doctrine-change proposals (the release design + the release design). All decisions audit-logged
  via the release design Tessera chain. Use when operator invokes /hades:amendment-* commands.
license: Proprietary
agentskills_version: 1.0
keywords:
  - amendment
  - doctrine-amendment
  - audit-chain
  - the release design
  - lifecycle
  - hades
---

# HADES — amendment skill (doctrine-amendment lifecycle)

This skill covers the HADES doctrine-amendment lifecycle exposed through 4 slash
commands: `/hades:amendment-list`, `/hades:amendment-show`, `/hades:amendment-ack`, `/hades:amendment-deny`.
Each is a separate registered command (Hermes has no subcommand syntax per spike §6).

## When to use

- Operator invokes `/hades:amendment-list` to see pending proposals
- When a subagent proposes a doctrine change that needs operator decision
- When reviewing and acting on amendment proposals before a planning session

## Workflow

### List

```bash
curl --unix-socket /tmp/hades-system.sock -s \
     "http://unix/v1/amendment/list?project=$PROJECT&status=pending" | jq '.'
```

### Show

```bash
curl --unix-socket /tmp/hades-system.sock -s \
     "http://unix/v1/amendment/<id>" | jq '.'
```

### Acknowledge (ack)

Requires: amendment id (+ optional reason).

```bash
curl --unix-socket /tmp/hades-system.sock \
     -X POST \
     -d '{"reason":"..."}' \
     "http://unix/v1/amendment/<id>/ack"
```

Tessera-anchored `AmendmentAcknowledged` event. Dependent amendments blocked until parent ack'd.

### Deny

Requires: amendment id + reason (REQUIRED for denial).

```bash
curl --unix-socket /tmp/hades-system.sock \
     -X POST \
     -d '{"reason":"..."}' \
     "http://unix/v1/amendment/<id>/deny"
```

Tessera-anchored `AmendmentDenied` event. Downstream dependents auto-transition to `blocked_by_denial`.

## Invariants

- Reason text MUST NOT contain Claude/Anthropic/AI attribution (inv-hades-004; daemon regex-rejects)
- Denial reason is REQUIRED (load-bearing for audit chain readability)

## Cross-references

- the release design + the release design doctrine-amendment lifecycle
- inv-hades-072 amendment audit chain anchor
- /hades:amendment-list, /hades:amendment-show, /hades:amendment-ack, /hades:amendment-deny slash command handlers
