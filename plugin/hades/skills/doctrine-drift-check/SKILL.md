---
name: doctrine-drift-check
description: |
  HADES doctrine drift detection: query caronte code-graph for all code/spec/doc
  references to doctrine keys; compare against current Plan 8 config; report
  mismatches by severity. Use when operator invokes /hades:doctrine-drift-check.
license: Proprietary
agentskills_version: 1.0
keywords:
  - doctrine-drift
  - caronte
  - drift-detection
  - plan-8
  - cross-reference
  - hades
---

# HADES — doctrine-drift-check skill (doctrine drift detection)

This skill detects doctrine drift by querying caronte code-graph for all references to
doctrine keys and cross-referencing with the current Plan 8 doctrine config.
Triggered by `/hades:doctrine-drift-check [project]`.

## When to use

- Operator invokes `/hades:doctrine-drift-check`
- Pre-merge gate (part of `make verify-doctrine-drift`)
- Morning brief (`zen day` includes drift summary)
- When investigating discrepancy between code/spec and active doctrine

## Workflow

### 1. Get current doctrine config

```bash
curl --unix-socket /tmp/zen-swarm.sock -s \
     "http://unix/v1/doctrine/show?project=$PROJECT" | jq '.doctrine_config'
```

### 2. Query caronte code-graph for each doctrine key

```bash
curl --unix-socket /tmp/zen-swarm.sock \
     -X POST \
     -d '{"tool":"mcp_zen-swarm_caronte_query","query":"references_to:doctrine.<key>"}' \
     http://unix/v1/mcpgateway
```

### 3. Cross-reference values

For each (key, reference) pair: compare asserted value at reference site vs
daemon config. Mismatch → drift detected.

### 4. Three-severity output

- HIGH: doctrine ceiling references (most critical)
- MEDIUM: threshold/config references in tests (may cause future failures)
- LOW: comment/doc references (awareness only)

### 5. Audit anchor

Emits `DoctrineDriftCheckCompleted` event anchored in Plan 9 audit chain.

## Cross-references

- Plan 8 doctrine schema (canonical source of truth)
- Plan 11 §3.1 mcpgateway (caronte in-process; tool name mcp_zen-swarm_caronte_query)
- /hades:doctrine-drift-check slash command handler
