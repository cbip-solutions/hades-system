---
name: doctrine
description: |
  HADES doctrine control: show active doctrine or apply runtime override
  (audit-logged via HADES design Tessera chain). Use when operator invokes /hades:doctrine
  or when needing to understand/change the active doctrine for a session.
license: Proprietary
agentskills_version: 1.0
keywords:
  - doctrine
  - max-scope
  - capa-firewall
  - runtime-override
  - audit-chain
  - hades
---

# HADES — doctrine skill (doctrine control)

This skill provides doctrine management for HADES sessions. The `/hades:doctrine`
slash command handler invokes it; orchestrators can also `skill_load("hades:doctrine")`
directly.

## When to use

- Operator invokes `/hades:doctrine [name]`
- When identifying which doctrine applies to the current project
- Before applying doctrine-sensitive operations (augmentation, cross-project queries)

## Workflow

### Show mode (no argument)

```bash
curl --unix-socket /tmp/hades-system.sock -s \
     "http://unix/v1/doctrine/show?project=$PROJECT&session=$SESSION" \
     | jq '.'
```

Format result as operator-friendly summary.

### Override mode (name argument)

Validate name is in `{max-scope, default, capa-firewall}`.

```bash
curl --unix-socket /tmp/hades-system.sock \
     -X POST \
     -H "Content-Type: application/json" \
     -d '{"project":"...","session":"...","doctrine":"<name>","reason":"..."}' \
     http://unix/v1/doctrine/override
```

Override is **audit-logged via HADES design chain** (Tessera-anchored `DoctrineOverridden` event).
Per invariant: can only TIGHTEN beyond project ceiling, never loosen.

## Doctrine values

| Doctrine | Augmentation | KG tokens | Cross-project |
|----------|-------------|-----------|---------------|
| max-scope | enabled | 25000 | max-scope ↔ all |
| default | enabled | 10000 | default ↔ max-scope |
| capa-firewall | disabled | 0 | self-only |

## Cross-references

- spec §3.4 Doctrine schema extensions
- invariant doctrine ceiling enforcement (HADES design)
- invariant amendment audit chain anchor
- /hades:doctrine slash command handler
