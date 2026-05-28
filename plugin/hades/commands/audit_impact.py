# SPDX-License-Identifier: MIT
"""/hades:audit-impact handler — Show KG context for an audit event via augment pipeline."""

from __future__ import annotations

_PROMPT = """# /hades:audit-impact — KG context for audit event {event_id}

Resolve audit event **{event_id}** + show its full KG augmentation context (citations, affected symbols, community membership) in the HADES project. Wraps `hades://audit/<id>` URL handler (spec §4.2) + HADES design augmentation citation chain.

## 1. Resolve event via daemon

```bash
curl --unix-socket /tmp/hades-system.sock -s \\
     "http://unix/v1/audit/event/{event_id}" \\
     | jq '.'
```

Expected response:
- `event_id` — same as input
- `event_type` — e.g., `AugmentationCompleted`, `KGQueryDispatched`, `MergeWinnerSelected`
- `payload` — type-specific fields
- `citations[]` — list of citation envelopes per design contract
- `tessera_leaf_anchor` — HADES design tile-log leaf hash
- `prior_events[]` — chained prior events (HADES design audit chain prev-pointer)

## 2. Augment with KG context

For each citation source, query the HADES design augmentation pipeline (`/v1/augment` mode=audit_resolve):

```bash
curl --unix-socket /tmp/hades-system.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"mode":"audit_resolve","source_query":"<citation.source_query>"}}' \\
     http://unix/v1/augment
```

Returns full 5-lane RRF context (spec §4.1):
- Lane 1 KG semantic + caller/callee
- Lane 2 aggregator FTS5 BM25
- Lane 3 KG context (community + neighbors)
- Lane 4 cross-encoder reranker
- Lane 5 temporal scoring

## 3. Render full context

Format as operator briefing:

```
# Audit event {event_id}

## Type
<event_type>

## When + by what
<timestamp>, <emitting_component>

## Citations + KG context (5-lane RRF)
1. <citation_1.source_tool>: <citation_1.result_summary>
   - Confidence: <citation_1.confidence>
   - Audit chain link: <citation_1.audit_event_link>
   - Affected callers: <list>
   - Community: <name>

## Prior chained events (HADES design prev-pointer chain)
<event_id_1> → <event_id_2> → ... → <root>

## Tessera leaf anchor
<tessera_leaf_anchor>
```

## 4. Privacy filter

Per HADES design invariant (privacy boundary), if event was emitted in capa-firewall doctrine, only show events visible to operator's current doctrine + project.

```
Some context filtered by capa-firewall privacy boundary (invariant).
```

## 5. Audit chain navigation

Operator can deep-link to any chained event:
```
/audit-impact <prior_event_id>
```

## Cross-references

- spec §4.2 slash command flow
- spec §4.6 audit chain integration (event types table)
- spec §1 design choice citation envelope structure
- invariant privacy boundary
- invariant hades://audit URL handler auth check
"""

_PROMPT_NO_ID = """# /hades:audit-impact — KG context for audit event

HADES: audit event id is required
  No event id was provided to /hades:audit-impact.
  → Provide an event id: /hades:audit-impact <event-id>  (e.g., evt-abc123)
"""


def audit_impact_handler(raw_args: str) -> str | None:
    """/hades:audit-impact handler. raw_args: audit event id (required)."""
    event_id = raw_args.strip()
    if not event_id:
        return _PROMPT_NO_ID
    return _PROMPT.format(event_id=event_id)
