# SPDX-License-Identifier: MIT
"""/hades:impact-pre-merge handler — Pre-merge blast radius analysis via caronte/augment."""

from __future__ import annotations

_PROMPT = """# /hades:impact-pre-merge — Pre-merge blast radius analysis

You are analyzing the blast radius of merging **{branch}** into the active HADES project's main branch. This wraps  augmentation pipeline (`/v1/augment` mode=preflight per spec §4.3) with pre-merge specifics.

## 1. Identify diff scope

```bash
git diff --name-only main..{branch}
```

Capture all changed files in `{branch}`.

## 2. Per-file impact analysis via augmentation pipeline

For each changed file, query daemon's augmentation pipeline in preflight mode:

```bash
for FILE in $CHANGED_FILES; do
  curl --unix-socket /tmp/zen-swarm.sock \\
       -X POST \\
       -H "Content-Type: application/json" \\
       -d '{{"mode":"preflight","file":"'"$FILE"'","diff_context":"'"$(git diff main..{branch} -- $FILE | head -200)"'"}}' \\
       http://unix/v1/augment
done
```

Each response includes:
- `blast_radius_score` — 0..100 estimate
- `affected_callers` — list of caller functions / files
- `affected_callees` — list of callee functions / files
- `community_size` — caronte community detection neighborhood (gonum k-core/SCC)
- `recent_churn_score` — temporal scoring (Lane 5 of 5-lane RRF)
- `citations[]` — supporting evidence with audit event IDs

## 3. Aggregate + render summary

Render operator-friendly report:

```
# Pre-merge impact for {branch}

## Files changed: <N>

## High-impact files (blast radius >50)
1. <file_1> (score: 78)
   - Affected callers: <list> (top 5)
   - Community: <community_name> (size 23)
   - Recent churn: high (12 commits last 30d)

## Medium-impact files (blast radius 25..50)
...

## Low-impact files (blast radius <25)
...

## Doctrine threshold check
Per  doctrine.preflight.impact_thresholds.high:
- Files exceeding threshold: <count>
- Recommended: reviewer depth INCREASE proportional to impact
```

## 4. Doctrine integration

Per spec §4.3 +  doctrine schema `[doctrine.preflight]`:
- `impact_timeout_ms` per-doctrine (max-scope=2000, default=500, capa-firewall=5000)
- `impact_thresholds.high/medium` per-doctrine cutoffs
- `on_timeout = "warn-proceed"` (constant)

If aggregate run exceeds `impact_timeout_ms`, daemon returns partial result + warning: "Pre-flight timeout; partial result".

## 5. Audit chain anchor

Each preflight call emits `AugmentationStarted` + `AugmentationCompleted` events (spec §4.6).

```
Audit chain: zen://audit/<aggregate_event_id>
```

## 6.  integration (MergeEngine)

Per spec §4.4,  winner selection extends with:
```
winner = max(test_pass) + max(reviewer_agreement) + min(unintended_blast_radius)
```

## Cross-references

- spec §4.3  orchestrator pre-flight extension
- spec §4.4  MergeEngine winner extension
- spec §3.4 doctrine.preflight schema
- invariant augmentation budget gated budget MCP
"""

_PROMPT_NO_BRANCH = """# /hades:impact-pre-merge — Pre-merge blast radius analysis

HADES: branch name is required
  No branch name was provided to /hades:impact-pre-merge.
  → Provide a branch: /hades:impact-pre-merge <branch-name>  (e.g., feature/my-feature)
"""


def impact_pre_merge_handler(raw_args: str) -> str | None:
    """/hades:impact-pre-merge handler. raw_args: branch name (required)."""
    branch = raw_args.strip()
    if not branch:
        return _PROMPT_NO_BRANCH
    return _PROMPT.format(branch=branch)
