# SPDX-License-Identifier: MIT
"""/hades:amendment-show handler — Show full detail of a pending doctrine-amendment."""

from __future__ import annotations

_PROMPT = """# /hades:amendment-show — Full amendment detail for HADES project

Show full detail for amendment **{amendment_id}**.

## 1. Query daemon

```bash
# pending endpoint registration: amendment detail (GET) per spec §7.2
curl --unix-socket /tmp/zen-swarm.sock -s \\
     "http://unix/v1/amendment/{amendment_id}" \\
     | jq '.'
```

Expected response includes:
- `id` — amendment id
- `type` — `doctrine_change` | `rule_addition` | `rule_removal` | `threshold_adjustment`
- `proposed_by` — `agent` (subagent name) or `operator` or `automated_drift_detection`
- `proposed_at` — ISO 8601 timestamp
- `scope` — doctrine key path (e.g., `augmentation.max_kg_tokens`)
- `from` / `to` — current value + proposed value
- `reason` — natural-language rationale
- `evidence` — list of supporting events / commits / metrics
- `audit_event_id` — the release design Tessera-anchored event linking
- `status` — `pending` | `acknowledged` | `denied`
- `dependent_on` — list of prior amendments this one depends on (if any)
- `affects_invariants` — list of inv-zen-XXX impacted (if any)

## 2. Render full detail

```
# Amendment {amendment_id}

## Scope
<scope>: <from> → <to>

## Proposed
By <proposed_by> at <proposed_at>

## Type
<type>

## Reason
<reason>

## Evidence
1. <evidence_1>
...

## Audit chain link
zen://audit/<audit_event_id>

## Affects invariants
<inv-zen-XXX list>

## Dependent on prior amendments
<dependent_on list>

## Status
<status>
```

## 3. Recommend next step

If status is `pending`:
- Operator can `/amendment-ack {amendment_id}` to approve OR `/amendment-deny {amendment_id}` to reject

If status is `acknowledged` or `denied`:
- Show closing context (when, by whom, reason)

## Cross-references

- the release design + the release design doctrine-amendment lifecycle
- /amendment-list, /amendment-ack, /amendment-deny
"""

_PROMPT_NO_ID = """# /hades:amendment-show — Full amendment detail

HADES: amendment id is required
  No amendment id was provided to /hades:amendment-show.
  → Run /hades:amendment-list to see pending amendment IDs, then retry with a valid id.
"""


def amendment_show_handler(raw_args: str) -> str | None:
    """/hades:amendment-show handler. raw_args: amendment id (required)."""
    amendment_id = raw_args.strip()
    if not amendment_id:
        return _PROMPT_NO_ID
    return _PROMPT.format(amendment_id=amendment_id)
