# SPDX-License-Identifier: MIT
"""/hades:amendment-list handler — List pending doctrine-amendment proposals."""

from __future__ import annotations

_PROMPT = """# /hades:amendment-list — Pending doctrine amendments

List pending HADES doctrine-amendment proposals awaiting operator decision (ack/deny).

## 1. Query daemon

```bash
# pending endpoint registration: /v1/project/active resolves the active project alias
PROJECT=$(curl --unix-socket /tmp/hades-system.sock -s http://unix/v1/project/active)

# pending endpoint registration: amendment lifecycle (list/show/ack/deny) per spec §7.2
curl --unix-socket /tmp/hades-system.sock -s \\
     "http://unix/v1/amendment/list?project=$PROJECT&status=pending" \\
     | jq '.'
```

Expected response:

```json
{
  "amendments": [
    {
      "id": "amend-2026-05-10-1234",
      "type": "doctrine_change",
      "proposed_by": "agent",
      "proposed_at": "2026-05-10T14:23:11Z",
      "scope": "max_kg_tokens",
      "from": 25000,
      "to": 30000,
      "reason": "...",
      "evidence": [],
      "audit_event_id": "evt-abc123"
    }
  ]
}
```

## 2. Render summary

For each amendment:
```
[<id>] <scope>: <from> → <to>
  Proposed by: <proposed_by> at <proposed_at>
  Reason: <reason>
  Audit event: hades://audit/<audit_event_id>
```

## 3. Operator next step

Operator can:
- `/amendment-show <id>` — full detail
- `/amendment-ack <id>` — approve + apply
- `/amendment-deny <id>` — reject + close

## Cross-references

- the release design + the release design doctrine-amendment lifecycle
- spec §7.2 (slash commands)
- the release design inv-hades-072 amendment audit chain anchor
"""


def amendment_list_handler(raw_args: str) -> str | None:
    """/hades:amendment-list handler. raw_args: optional project name."""
    return _PROMPT
