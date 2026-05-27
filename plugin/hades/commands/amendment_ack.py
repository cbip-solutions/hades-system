# SPDX-License-Identifier: MIT
"""/hades:amendment-ack handler — Acknowledge + apply pending doctrine amendment."""

from __future__ import annotations

_PROMPT = """# /hades:amendment-ack — Acknowledge + apply HADES doctrine amendment

Apply amendment **{amendment_id}**. This is **audit-logged** chain (Tessera-anchored event `AmendmentAcknowledged`).

## 1. Confirm intent

If operator did not provide `--reason`, prompt for confirmation:

```
You are about to ACK amendment {amendment_id}.
This is a doctrinal change anchored in the audit chain.
Reason (recommended, can be empty): _
```

Wait for operator response before proceeding.

## 2. POST to daemon

```bash
REASON="{reason}"

# pending endpoint registration: amendment ack (POST) anchored via invariant
curl --unix-socket /tmp/zen-swarm.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"reason":"'"$REASON"'"}}' \\
     "http://unix/v1/amendment/{amendment_id}/ack" \\
     | jq '.'
```

Expected response:
- 200 OK with `{{"status": "acknowledged", "applied_at": "...", "audit_event_id": "..."}}`
- 409 Conflict if amendment is no longer `pending`
- 422 Unprocessable if amendment has `dependent_on` amendments still pending

## 3. Confirm application

After 200 OK:

```
Amendment {amendment_id} acknowledged and applied.
Audit event: zen://audit/<audit_event_id>
Applied at: <applied_at>
Affected invariants: <inv-zen-XXX list>
```

If 422 with dependent amendments:

```
Amendment {amendment_id} blocked by pending dependent amendments.
Run /amendment-show <dep-id> to investigate, or ack dependents first.
```

## 4. NEVER add Claude attribution to audit log entry

The reason text becomes part of audit chain. Operator's reason MUST NOT contain Claude/Anthropic/AI attribution. Daemon's audit handler regex-rejects.

## Cross-references

-  +  amendment lifecycle
- invariant amendment audit chain anchor
- /amendment-list, /amendment-show, /amendment-deny
"""

_PROMPT_NO_ID = """# /hades:amendment-ack — Acknowledge + apply amendment

HADES: amendment id is required
  No amendment id was provided to /hades:amendment-ack.
  → Run /hades:amendment-list to see pending amendment IDs, then retry with a valid id.
"""


def amendment_ack_handler(raw_args: str) -> str | None:
    """/hades:amendment-ack handler. raw_args: '<amendment-id> [reason]'."""
    parts = raw_args.strip().split(maxsplit=1)
    if not parts or not parts[0]:
        return _PROMPT_NO_ID
    amendment_id = parts[0]
    reason = parts[1] if len(parts) > 1 else "operator ack via /amendment-ack slash"
    return _PROMPT.format(amendment_id=amendment_id, reason=reason)
