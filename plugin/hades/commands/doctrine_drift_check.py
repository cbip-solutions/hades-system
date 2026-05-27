# SPDX-License-Identifier: MIT
"""/hades:doctrine-drift-check handler — Detect doctrine drift via caronte code-graph query."""

from __future__ import annotations

_PROMPT = """# /hades:doctrine-drift-check — Doctrine drift detection via caronte code-graph query

You are detecting doctrine drift in the active HADES project. **Drift** = code/spec/docs that reference doctrine values inconsistent with current `.zen-swarm.toml` +  doctrine schema. Wraps caronte code-graph query + cross-references current doctrine config.

## 1. Identify active project + current doctrine

```bash
PROJECT="{project}"

# Read current doctrine config from daemon
DOCTRINE_CONFIG=$(curl --unix-socket /tmp/zen-swarm.sock -s \\
                       "http://unix/v1/doctrine/show?project=$PROJECT" \\
                       | jq -r '.doctrine_config')
```

## 2. Query caronte code-graph for doctrine references

For each known doctrine key, query mcpgateway (caronte is in-process; no separate MCP entry):

```bash
for KEY in $DOCTRINE_KEYS; do
  curl --unix-socket /tmp/zen-swarm.sock \\
       -X POST \\
       -H "Content-Type: application/json" \\
       -d '{{"tool":"mcp_zen-swarm_caronte_query","query":"references_to:doctrine.'"$KEY"'"}}' \\
       http://unix/v1/mcpgateway
done
```

This uses the caronte code-graph to find every code/spec/doc location referencing each doctrine key.

## 3. Cross-reference vs current config

For each (key, reference) pair:
- Extract the value asserted at the reference site (e.g., comment `max_kg_tokens=10000` in spec)
- Compare against `$DOCTRINE_CONFIG[$KEY]` from daemon
- If mismatch → drift detected

## 4. Render drift report

```
# Doctrine drift check — project: {project}

## Total references checked: <N>
## Drifts found: <K>

### HIGH
1. spec/2026-04-29-zen-swarm-design.md:842 references `max_kg_tokens=15000`
   - Current daemon config: `max_kg_tokens=25000` (max-scope doctrine)
   - Likely outdated since  ship; recommend doc update

### MEDIUM
2. tests/integration/augment_e2e_test.go:127 hardcodes `impact_threshold=20`
   - Current daemon config: `impact_threshold=10` (max-scope doctrine)

### LOW
3....

## No drift
- <K_clean> references match current config
```

## 5. Drift remediation

For each HIGH/MEDIUM drift:
- Suggest edit (file:line + new value)
- Operator can dispatch fix subagent: `/execute-plan` with a doc-revision plan OR manual edit

## 6. Periodic drift (recommended cadence)

This check should run periodically:
- Pre-merge gate (every PR in `make verify-doctrine-drift` Q16 layer C)
- Morning brief (`zen day` includes drift summary)
- Manual operator query (this slash) when investigating discrepancy

## 7. Audit chain anchor

This check emits `DoctrineDriftCheckCompleted` event:

```
Audit chain: zen://audit/<aggregate_event_id>
```

## Cross-references

-  doctrine schema (canonical source of truth)
-  §3.1 mcpgateway (caronte in-process; tool name mcp_zen-swarm_caronte_query)
- spec §4.2 slash command flow
- spec §11.3 cross-plan canon checklist (drift detection mechanism)
"""


def doctrine_drift_check_handler(raw_args: str) -> str | None:
    """/hades:doctrine-drift-check handler. raw_args: optional project name."""
                                                                                                        
    project = (
        raw_args.strip()
        or "$(curl --unix-socket /tmp/zen-swarm.sock -s http://unix/v1/project/active)"
    )
    return _PROMPT.format(project=project)
