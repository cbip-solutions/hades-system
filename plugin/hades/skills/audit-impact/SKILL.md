---
name: audit-impact
description: |
  HADES audit event KG context: resolve a the release design Tessera-anchored audit
  event + show full 5-lane RRF augmentation context (citations, callers,
  callees, community). Use when operator invokes /hades:audit-impact <event-id>.
license: Proprietary
agentskills_version: 1.0
keywords:
  - audit-impact
  - audit-chain
  - KG-context
  - augmentation
  - citations
  - hades
---

# HADES — audit-impact skill (audit event KG context)

This skill resolves a the release design Tessera-anchored audit event and shows its full
knowledge graph augmentation context. Triggered by `/hades:audit-impact <event-id>`.

## When to use

- Operator invokes `/hades:audit-impact <event-id>`
- When investigating a failed augmentation or unexpected KG result
- When navigating the audit chain of a past operation

## Workflow

### 1. Resolve event via daemon

```bash
curl --unix-socket /tmp/hades-system.sock -s \
     "http://unix/v1/audit/event/<event_id>" | jq '.'
```

### 2. Augment with KG context

For each citation in the event, query `/v1/augment` in audit_resolve mode.

Returns full 5-lane RRF context:
- Lane 1: KG semantic + caller/callee
- Lane 2: aggregator FTS5 BM25
- Lane 3: KG community + neighbors
- Lane 4: cross-encoder reranker
- Lane 5: temporal scoring

### 3. Privacy filter

Per inv-hades-163: capa-firewall events filtered from non-capa-firewall sessions.

### 4. Navigation

Operator can recurse: `/hades:audit-impact <prior_event_id>` to navigate the chain.

## Cross-references

- spec §4.2 slash command flow
- spec §4.6 audit chain integration
- spec §1 Q9 citation envelope structure
- inv-hades-163 privacy boundary
- inv-hades-172 hades://audit URL handler auth check
- /hades:audit-impact slash command handler
