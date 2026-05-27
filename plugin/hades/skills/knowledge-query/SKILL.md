---
name: knowledge-query
description: |
  HADES cross-project knowledge query: federated aggregator query with
  the release design privacy filter and RRF k=60 fusion. Use when operator invokes
  /hades:knowledge-query <pattern> or needs to find items across projects.
license: Proprietary
agentskills_version: 1.0
keywords:
  - knowledge-query
  - federated-query
  - aggregator
  - privacy-filter
  - RRF
  - hades
---

# HADES — knowledge-query skill (cross-project knowledge query)

This skill provides federated cross-project knowledge query through the release design D's
`aggregator.Query()` + the release design privacy filter. Triggered by `/hades:knowledge-query`.

## When to use

- Operator invokes `/hades:knowledge-query <pattern> [scope]`
- When searching for items across multiple HADES projects
- Before promoting a knowledge item to global (to verify its value)

## Workflow

### 1. Identify scope

Scope options: `self`, `max-scope`, `default`, `all`.
Default: doctrine-config determined.

### 2. POST to daemon

```bash
curl --unix-socket /tmp/hades-system.sock \
     -X POST \
     -d '{"pattern":"<pattern>","scope":"<scope>","realtime":false}' \
     http://unix/v1/knowledge/query
```

### 3. Privacy filter at retrieval boundary

Per inv-hades-163:
- capa-firewall: self-only
- max-scope ↔ default: bidirectional
- max-scope or default → capa-firewall: filtered

Response includes `privacy_filtered_count` for transparency.

### 4. Token budget

Per doctrine.augmentation.max_kg_tokens:
- max-scope: 25000
- default: 10000
- capa-firewall: 0 (disabled)

## Cross-references

- spec §9.1 the release design D aggregator.Query
- spec §3.4 doctrine.knowledge.cross_project
- inv-hades-163 augmentation cross-project privacy boundary
- inv-hades-167 augmentation budget gate
- /hades:knowledge-query slash command handler
