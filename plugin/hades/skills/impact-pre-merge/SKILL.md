---
name: impact-pre-merge
description: |
  HADES pre-merge blast radius analysis: wraps the release design augmentation
  pipeline (preflight mode) + caronte community detection (gonum k-core/SCC). Use when
  operator invokes /hades:impact-pre-merge <branch> or before merging large PRs.
license: Proprietary
agentskills_version: 1.0
keywords:
  - impact-pre-merge
  - blast-radius
  - augmentation
  - caronte
  - the release design
  - hades
---

# HADES — impact-pre-merge skill (pre-merge impact analysis)

This skill wraps the release design augmentation pipeline in preflight mode to analyze
the blast radius of a pending merge. Triggered by `/hades:impact-pre-merge <branch>`.

## When to use

- Operator invokes `/hades:impact-pre-merge <branch>`
- Before merging a feature branch with broad file changes
- As part of the release design MergeEngine winner selection flow

## Workflow

### 1. Get changed files

```bash
git diff --name-only main..<branch>
```

### 2. Per-file preflight augmentation

```bash
curl --unix-socket /tmp/zen-swarm.sock \
     -X POST \
     -d '{"mode":"preflight","file":"<file>","diff_context":"..."}' \
     http://unix/v1/augment
```

Returns: blast_radius_score (0..100), affected_callers, affected_callees,
community_size (caronte k-core/SCC), recent_churn_score (temporal lane 5), citations[].

### 3. Render three-tier report

- HIGH (blast radius >50): immediate attention required
- MEDIUM (25..50): reviewer depth increase recommended
- LOW (<25): surface for awareness

### 4. Doctrine integration

Impact timeout per doctrine: max-scope=2000ms, default=500ms, capa-firewall=5000ms.
On timeout → warn-proceed (constant).

## Cross-references

- spec §4.3 the release design orchestrator pre-flight extension
- spec §4.4 the release design MergeEngine winner extension
- invariant augmentation budget gate
- /hades:impact-pre-merge slash command handler
