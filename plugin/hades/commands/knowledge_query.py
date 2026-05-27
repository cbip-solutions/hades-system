# SPDX-License-Identifier: MIT
"""/hades:knowledge-query handler — Cross-project federated knowledge query."""

from __future__ import annotations

_PROMPT = """# /hades:knowledge-query — Cross-project knowledge query

You are running a federated cross-project knowledge query in HADES. Wraps the release design D `aggregator.Query()` + the release design `/v1/augment` privacy filter (spec §9.1).

## 1. Identify scope

```bash
SCOPE="{scope}"
# pending endpoint registration: /v1/project/active resolves active project alias
PROJECT=$(curl --unix-socket /tmp/hades-system.sock -s http://unix/v1/project/active)
```

Scope values:
- `self` — only the current project (mandatory in capa-firewall doctrine per the release design §3.4)
- `max-scope` — only max-scope-doctrine projects
- `default` — only default-doctrine projects
- `all` — all visible projects per current doctrine ceiling

## 2. POST query to daemon aggregator

```bash
# pending endpoint registration: knowledge query (POST) federates aggregator.Query() across projects
curl --unix-socket /tmp/hades-system.sock \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"pattern":"{pattern}","scope":"'"$SCOPE"'","realtime":false}}' \\
     http://unix/v1/knowledge/query
```

Daemon dispatches to the release design D `aggregator.Query()`:
- `realtime=false` → consume cached aggregator.db (the release design D Litestream-replicated)
- `realtime=true` → live federation: query each in-scope project's aggregator live

## 3. Privacy filter at retrieval boundary

Per the release design inv-hades-163 + the release design §3.4 doctrine.knowledge.cross_project:
- capa-firewall projects: visible only to other capa-firewall sessions (self-only)
- max-scope ↔ max-scope OR default ↔ default: bidirectional
- max-scope ↔ default: bidirectional
- max-scope or default → capa-firewall: filtered out (one-way isolation)

Daemon applies filter at retrieval boundary; response includes `privacy_filtered_count`.

## 4. Render results

```
# Knowledge query: "{pattern}"
Scope: <SCOPE> | Realtime: <true|false>

## Top results (RRF k=60 fusion of 5 lanes)

1. [project=<P1>, file=<F1>, score=<S1>]
   <result_summary>
   Audit: hades://audit/<event_id>

## Privacy filter
- Filtered <privacy_filtered_count> results due to capa-firewall doctrine
- Visible projects: <count> / <total>
- Inv anchor: inv-hades-163

## Audit chain anchor
hades://audit/<aggregate_event_id>
```

## 5. Token budget enforcement

Per spec §1 Q3 doctrine.augmentation.max_kg_tokens:
- max-scope: 25000
- default: 10000
- capa-firewall: 0 (augmentation disabled by default)

If response would exceed budget, daemon returns truncated result + `truncation_warning`.

## 6. Cache hit rate visibility

```
Cache: lane1=hit | lane2=miss | lane3=hit | lane4=miss | lane5=hit
```

## Cross-references

- spec §9.1 the release design D substrate consumption (aggregator.Query)
- spec §3.4 doctrine.knowledge.cross_project schema
- inv-hades-163 augmentation cross-project privacy boundary
- inv-hades-167 augmentation budget gate
- /knowledge-promote (companion command)
"""

_PROMPT_NO_PATTERN = """# /hades:knowledge-query — Cross-project knowledge query

HADES: query pattern is required
  No query pattern was provided to /hades:knowledge-query.
  → Provide a pattern: /hades:knowledge-query <pattern> [scope]  (e.g., "doctrine max_kg_tokens" max-scope)
"""


def knowledge_query_handler(raw_args: str) -> str | None:
    """/hades:knowledge-query handler. raw_args: '<pattern> [scope]'."""
    parts = raw_args.strip().split(maxsplit=1)
    if not parts or not parts[0]:
        return _PROMPT_NO_PATTERN
    pattern = parts[0]
    scope = parts[1] if len(parts) > 1 else "doctrine-config"
    return _PROMPT.format(pattern=pattern, scope=scope)
