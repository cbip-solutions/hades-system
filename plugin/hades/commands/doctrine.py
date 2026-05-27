# SPDX-License-Identifier: MIT
"""/hades:doctrine handler — show active doctrine OR runtime override."""

from __future__ import annotations

_SHOW_PROMPT = """# /hades:doctrine — Show active doctrine

You are showing the currently-active doctrine for the current HADES project + session.

## 1. Identify active project + session

```bash
# pending endpoint registration: /v1/project/active resolves active project alias from .zen-swarm.toml
PROJECT=$(curl --unix-socket /tmp/zen-swarm.sock -s http://unix/v1/project/active 2>&1)
SESSION=$(echo "$HERMES_SESSION_ID")
```

## 2. Show mode

Show currently-active doctrine for project + session.

```bash
curl --unix-socket /tmp/zen-swarm.sock -s \\
     "http://unix/v1/doctrine/show?project=$PROJECT&session=$SESSION" \\
     | jq '.'
```

Expected response shape:

```json
{
  "project": "<project>",
  "session": "<session-id>",
  "active_doctrine": "max-scope",
  "ceiling_doctrine": "max-scope",
  "override_history": [
    {"timestamp": "...", "from": "default", "to": "max-scope", "reason": "..."}
  ],
  "doctrine_config": {
    "augmentation": {},
    "preflight": {},
    "codegraph": {},
    "knowledge": {}
  }
}
```

## 3. Display result

Format the daemon response as operator-friendly summary:

```
# Doctrine for project <project>
- Active: <active>
- Ceiling: <ceiling>
- Last override: <timestamp> by <operator> (<from> → <to>)
- Augmentation enabled: <true|false>
- Token budget: <max_kg_tokens>
```

## Cross-references

- spec §3.4 Doctrine schema extensions
- invariant doctrine ceiling enforcement (the release design)
- the release design audit chain (Tessera-anchored DoctrineOverridden event)
"""

_OVERRIDE_PROMPT = """# /hades:doctrine — Runtime doctrine override

You are overriding the active doctrine to **{name}** for the current HADES session.

## 1. Identify active project + session

```bash
# pending endpoint registration: /v1/project/active resolves active project alias from .zen-swarm.toml
PROJECT=$(curl --unix-socket /tmp/zen-swarm.sock -s http://unix/v1/project/active 2>&1)
SESSION=$(echo "$HERMES_SESSION_ID")
```

## 2. Override mode

Override doctrine to **{name}** for the current session.

### Validate doctrine name
Allowed values: `max-scope`, `default`, `capa-firewall`.

<!-- pending endpoint registration: doctrine override surface awaits spec §3.4 wiring -->
### POST to daemon /v1/doctrine/override

```bash
# pending endpoint registration: doctrine runtime override per session (audit-anchored via invariant)
curl --unix-socket /tmp/zen-swarm.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"project":"'"$PROJECT"'","session":"'"$SESSION"'","doctrine":"{name}","reason":"operator runtime override via /doctrine slash"}}' \\
     http://unix/v1/doctrine/override
```

The override is **audit-logged via the release design chain** — Tessera-anchored event `DoctrineOverridden` with payload (project, session, prior-doctrine, new-doctrine, reason, operator-identity-from-keychain). Per invariant (the release design amendment-anchored audit), every override appears in the Hermes Ink TUI F10 panel + `zen day` morning brief.

Per the release design doctrine ceiling enforcement (invariant): override can ONLY tighten beyond project ceiling, not loosen. If `{name}` is more permissive than project ceiling → daemon refuses with 409 Conflict + actionable error.

## 3. Display result

Format the daemon response as operator-friendly summary:

```
# Doctrine for project <project>
- Active: {name}
- Ceiling: <ceiling>
- Last override: <timestamp> by <operator> (<from> → {name})
- Augmentation enabled: <true|false>
- Token budget: <max_kg_tokens>
```

## Cross-references

- spec §3.4 Doctrine schema extensions
- invariant doctrine ceiling enforcement (the release design)
- the release design audit chain (Tessera-anchored DoctrineOverridden event)
"""

_VALID_DOCTRINES = {"max-scope", "default", "capa-firewall"}


def doctrine_handler(raw_args: str) -> str | None:
    """/hades:doctrine handler. raw_args: optional doctrine name for override."""
    name = raw_args.strip()
    if not name:
        return _SHOW_PROMPT
    if name not in _VALID_DOCTRINES:
        return (
            f"HADES: invalid doctrine name\n"
            f"  {name!r} is not a recognised doctrine.\n"
            f"  → Use one of: max-scope | default | capa-firewall"
        )
    return _OVERRIDE_PROMPT.format(name=name)
