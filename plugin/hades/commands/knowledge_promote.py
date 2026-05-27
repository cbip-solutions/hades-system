# SPDX-License-Identifier: MIT
"""/hades:knowledge-promote handler — Promote knowledge item to global (audit-logged)."""

from __future__ import annotations

_PROMPT = """# /hades:knowledge-promote — Promote to HADES global memory

Promote knowledge item **{item_id}** to global cross-project memory. Reason: **{reason}**.

## 1. Validate reason non-empty

The reason arg is required. Promotion is a doctrinal YES — moving content from project-local scope to globally-visible memory. The rationale is load-bearing for future agents reading audit chain.

## 2. Pre-flight visibility check

```bash
# Verify knowledge item exists + operator can see it.
# pending endpoint registration: knowledge item GET; aggregator.Get() awaits hades migrate
curl --unix-socket /tmp/hades-system.sock -s \\
     "http://unix/v1/knowledge/{item_id}" \\
     | jq '.'
```

If 404 or privacy-filtered → operator does not have visibility; abort with actionable error.

## 3. POST promote to daemon

```bash
# pending endpoint registration: knowledge promote (POST) anchors aggregator.Promote() via audit chain
curl --unix-socket /tmp/hades-system.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"reason":"{reason}"}}' \\
     "http://unix/v1/knowledge/{item_id}/promote"
```

Daemon dispatches HADES design D `aggregator.Promote()`:
- Adds item to `global_pins` table (HADES design D)
- Anchors event in HADES design audit chain (Tessera leaf with operator identity from keychain)
- Item now visible in cross-project queries from all in-scope projects per current doctrine

Expected response:
- 200 OK with `{{"status": "promoted", "promoted_at": "...", "audit_event_id": "..."}}`
- 409 Conflict if item already promoted
- 422 Unprocessable if item is in capa-firewall project (cannot promote without explicit project doctrine override)

## 4. Confirm + show audit deep-link

```
Knowledge item {item_id} promoted to global.
Reason: {reason}
Audit event: hades://audit/<audit_event_id>
Promoted at: <promoted_at>
Visible to: <list of projects per doctrine>
```

## 5. Reverse path: unpromote

If operator changes mind:
```
hades knowledge unpromote {item_id} --reason "..."
```

CLI subcommand (no slash equivalent; demoting is rarer + less time-critical). Anchored in audit chain identically.

## 6. NEVER add Claude attribution to audit log entry

The reason text becomes part of audit chain. Operator's reason MUST NOT contain Claude/Anthropic/AI attribution. Daemon's audit handler regex-rejects (HADES design substrate hook).

## Cross-references

- spec §9.1 HADES design D aggregator.Promote
- spec §4.6 audit chain integration (event types)
- invariant privacy boundary (promotion crosses boundary; explicit operator action required)
- /knowledge-query (companion command)
"""

_PROMPT_NO_ID = """# /hades:knowledge-promote — Promote to HADES global memory

HADES: knowledge item id is required
  No item id was provided to /hades:knowledge-promote.
  → Provide both: /hades:knowledge-promote <item-id> <reason>
"""

_PROMPT_NO_REASON = """# /hades:knowledge-promote — Promote to HADES global memory

HADES: reason is required for promotion
  Promotion rationale is load-bearing for future agents reading audit chain.
  → Provide a reason: /hades:knowledge-promote <item-id> <reason>
"""


def knowledge_promote_handler(raw_args: str) -> str | None:
    """/hades:knowledge-promote handler. raw_args: '<item-id> <reason>'."""
    parts = raw_args.strip().split(maxsplit=1)
    if not parts or not parts[0]:
        return _PROMPT_NO_ID
    item_id = parts[0]
    if len(parts) < 2 or not parts[1].strip():
        return _PROMPT_NO_REASON
    reason = parts[1].strip()
    return _PROMPT.format(item_id=item_id, reason=reason)
