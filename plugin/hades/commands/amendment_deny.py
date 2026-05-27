# SPDX-License-Identifier: MIT
"""/hades:amendment-deny handler — Deny pending doctrine amendment (audit-logged)."""

from __future__ import annotations

_PROMPT = """# /hades:amendment-deny — Deny HADES doctrine amendment

Deny amendment **{amendment_id}**. Reason: **{reason}**.

## 1. Validate reason non-empty

The reason arg is required. Denial is a doctrinal NO; the rationale is load-bearing for future agents reading audit chain.

## 2. POST to daemon

```bash
# pending endpoint registration: amendment deny (POST) anchored via invariant
curl --unix-socket /tmp/zen-swarm.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"reason":"{reason}"}}' \\
     "http://unix/v1/amendment/{amendment_id}/deny" \\
     | jq '.'
```

Expected response:
- 200 OK with `{{"status": "denied", "denied_at": "...", "audit_event_id": "..."}}`
- 409 Conflict if amendment is no longer `pending`

## 3. Confirm denial

```
Amendment {amendment_id} DENIED.
Reason: {reason}
Audit event: zen://audit/<audit_event_id>
Denied at: <denied_at>
```

## 4. Cross-impact

If denied amendment had downstream dependents:
- Daemon automatically transitions dependent amendments to `blocked_by_denial` status
- `/amendment-list` will surface them; operator can re-propose with adjusted scope or deny chain

## 5. NEVER add Claude attribution to audit log entry

Same as /amendment-ack — operator's reason MUST NOT contain Claude/Anthropic/AI attribution. Daemon regex-rejects.

## Cross-references

- the release design + the release design amendment lifecycle
- invariant amendment audit chain anchor
- /amendment-list, /amendment-show, /amendment-ack
"""

_PROMPT_NO_ID = """# /hades:amendment-deny — Deny amendment

HADES: amendment id and reason are required
  No amendment id was provided to /hades:amendment-deny.
  → Provide both: /hades:amendment-deny <amendment-id> <reason>. Run /hades:amendment-list to see pending ids.
"""

_PROMPT_NO_REASON = """# /hades:amendment-deny — Deny amendment

HADES: reason is required for denial
  Denial rationale is load-bearing for future agents reading audit chain.
  → Provide a reason: /hades:amendment-deny <amendment-id> <reason>
"""


def amendment_deny_handler(raw_args: str) -> str | None:
    """/hades:amendment-deny handler. raw_args: '<amendment-id> <reason>'."""
    parts = raw_args.strip().split(maxsplit=1)
    if not parts or not parts[0]:
        return _PROMPT_NO_ID
    amendment_id = parts[0]
    if len(parts) < 2 or not parts[1].strip():
        return _PROMPT_NO_REASON
    reason = parts[1].strip()
    return _PROMPT.format(amendment_id=amendment_id, reason=reason)
