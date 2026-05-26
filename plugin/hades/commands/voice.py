# SPDX-License-Identifier: MIT
"""/hades:voice handler — Voice memo input flow (sync <10s vs async) — Q6=B AFK."""

from __future__ import annotations

_PROMPT = """# /hades:voice — Voice memo input flow

You are entering voice-input mode for HADES. Per spec §1 Q6=B + §7.4 AFK UX: voice queries are sync if estimated <10s response time; async (with notification) if longer. Operator can force `--mode=sync` or `--mode=async`.

## 1. Verify Hermes voice mode

```bash
HERMES_VERSION=$(hermes --version 2>/dev/null | head -1)
# Voice shipped v0.11.0+ per spec §11.1 + spec §7.4
```

If Hermes version < 0.11.0, abort with: "ERROR: voice mode requires Hermes ≥0.11.0."

## 2. Voice STT input

If operator has not provided a text query, invoke Hermes voice mode:

```
Listening... (operator speaks query; voice STT transcribes via Hermes voice substrate)
```

After STT, capture transcribed text as `$QUERY`.

If operator provided `--query`, skip STT and use the text directly.

## 3. Estimate response time

```bash
QUERY="{query}"

# pending endpoint registration: voice estimate (POST) gates sync/async path selection
ESTIMATE=$(curl --unix-socket /tmp/zen-swarm.sock -s \\
                -X POST \\
                -H "Content-Type: application/json" \\
                -d '{{"query":"'"$QUERY"'"}}' \\
                http://unix/v1/voice/estimate)
```

Response: `{{"estimated_seconds": <number>, "complexity": "low|medium|high", "lanes_used": []}}`.

## 4. Mode selection (auto: sync if <10s, async otherwise)

```bash
ESTIMATED_SECONDS=$(echo "$ESTIMATE" | jq -r '.estimated_seconds')

if [ "$ESTIMATED_SECONDS" -lt 10 ]; then
  DECISION="sync"
else
  DECISION="async"
fi
```

## 5. Sync flow (estimated <10s)

```bash
RESPONSE=$(curl --unix-socket /tmp/zen-swarm.sock \\
                -X POST \\
                -H "Content-Type: application/json" \\
                -d '{{"query":"'"$QUERY"'","mode":"voice_sync"}}' \\
                http://unix/v1/augment)
```

Render via voice TTS renderer (Phase A `plugin/hades/renderers/voice_citation.py`).

## 6. Async flow (estimated ≥10s)

```bash
# pending endpoint registration: augment/dispatch posts a fire-and-forget job
JOB_ID=$(curl --unix-socket /tmp/zen-swarm.sock \\
              -X POST \\
              -H "Content-Type: application/json" \\
              -d '{{"query":"'"$QUERY"'","mode":"voice_async"}}' \\
              http://unix/v1/augment/dispatch \\
              | jq -r '.job_id')
```

Result will be in Plan 7 inbox + push notification per Hermes routing.

When operator returns:
```
/audit-impact <job_event_id>     # full result with KG context
/full <citation_id>              # expand individual citation
```

## 7. Offline cache subset (Q6=B mobile-friendly)

If offline, respond from KG offline cache subset:

```bash
# pending endpoint registration: knowledge offline cache HTTP surface for mobile clients
curl --unix-socket /tmp/zen-swarm.sock -s \\
     "http://unix/v1/knowledge/cache?query=$QUERY"
```

Cache holds last 50 queries + community summaries (doctrine-tunable size per spec §1 Q6=B).

## 8. Privacy filter

Per inv-zen-163, voice TTS NEVER reads aloud capa-firewall sensitive content per spec §6.4 (privacy filter never leaks).

## Cross-references

- spec §1 Q6=B AFK comprehensive
- spec §7.4 AFK UX
- spec §7.6 notification routing
- Phase A renderers/voice_citation.py
- Phase D AFK richness substrate (offline cache; sync/async flow logic)
- /full (companion command for citation expansion)
"""


def voice_handler(raw_args: str) -> str | None:
    """/hades:voice handler. raw_args: optional text query."""
    query = raw_args.strip() or "(voice STT — operator speaks)"
    return _PROMPT.format(query=query)
