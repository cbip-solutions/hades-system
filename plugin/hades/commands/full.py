# SPDX-License-Identifier: MIT
"""/hades:full handler — Citation expansion (Q6=B AFK comprehensive, mobile format)."""

from __future__ import annotations

_PROMPT = """# /hades:full — Citation expansion for {citation_id}

You are expanding citation **{citation_id}** from a mobile-format summary card to full content in HADES. Per spec §1 Q6=B + §7.4 AFK UX: by default, mobile responses are short summary cards; operator runs `/hades:full <id>` to fetch the complete result.

## 1. Resolve citation envelope

```bash
# pending endpoint registration: citation envelope retrieval (GET); substrate ships envelope type, daemon endpoint awaits hades migrate
curl --unix-socket /tmp/hades-system.sock -s \\
     "http://unix/v1/citation/{citation_id}" \\
     | jq '.'
```

Expected response: full citation envelope per spec §1 Q9 structure:
```json
{{
  "citation_id": "{citation_id}",
  "source_tool": "...",
  "source_query": "...",
  "result_summary": "...",
  "audit_event_link": "hades://audit/...",
  "confidence": 0.94,
  "platform_renderings": {{
    "ink": {{}},
    "telegram": {{}},
    "slack": {{}},
    "email_html": "...",
    "voice_tts": "...",
    "web_html": "..."
  }},
  "raw_kg_context": {{
    "callers": [],
    "callees": [],
    "community": [],
    "raw_search_hits": []
  }}
}}
```

## 2. Identify operator's active platform

```bash
PLATFORM="$(echo "$HERMES_PLATFORM_CONTEXT" | jq -r '.active_platform')"
```

Possible values per spec §3.1 Layer 5: `ink_desktop`, `telegram`, `slack`, `whatsapp`, `signal`, `email`, `voice`, `web`.

## 3. Render full citation per active platform

Use release track renderer for the active platform (per spec §3.2):
- `ink_desktop` → `plugin/hades/renderers/ink_citation.py` `InkCitationRenderer.render(envelope)`
- `telegram` → `plugin/hades/renderers/telegram_citation.py` `TelegramCitationRenderer.render(envelope)`
- `slack` → `plugin/hades/renderers/slack_citation.py` `SlackCitationRenderer.render(envelope)`
- `email` → `plugin/hades/renderers/email_citation.py` `EmailCitationRenderer.render(envelope)`
- `voice` → `plugin/hades/renderers/voice_citation.py` `VoiceCitationRenderer.render(envelope)`
- `web` → `plugin/hades/renderers/web_citation.py` `WebCitationRenderer.render(envelope)`

## 4. Fallback when active platform unknown

If `HERMES_PLATFORM_CONTEXT` env var is unset or platform is novel:

```bash
# Use universal markdown fallback when platform context is unset.
# pending endpoint registration: explicit renderer dispatch (POST); per-renderer call is shipped substrate but daemon side awaits hades migrate
curl --unix-socket /tmp/hades-system.sock -s \\
     -X POST \\
     -H "Content-Type: application/json" \\
     -d '{{"renderer":"markdown_fallback"}}' \\
     "http://unix/v1/citation/{citation_id}/render"
```

Returns universal markdown footnote-rendering (spec §1 Q9 §3.1 Layer 2).

## 5. Display + offer further deep-link

After rendering, surface follow-up options:

```
[Full citation rendered above]

Further actions:
- /audit-impact <event_id> — show audit chain context
- /knowledge-query "<related_query>" — find related items
- Reply with question — continue augmented thread
```

## 6. Privacy filter

Per inv-hades-163, if citation crosses doctrine boundary, daemon may return 403 Forbidden.

## Cross-references

- spec §1 Q6=B AFK comprehensive
- spec §1 Q9 citation envelope + platform renderings
- spec §7.4 AFK UX
- release track renderers (consumers; release track is UX entry)
- /voice (companion command for voice flow)
"""

_PROMPT_NO_ID = """# /hades:full — Citation expansion

HADES: citation id is required
  No citation id was provided to /hades:full.
  → Provide a citation id: /hades:full <citation-id>  (e.g., c1). Run /hades:knowledge-query or /hades:audit-impact to get ids.
"""


def full_handler(raw_args: str) -> str | None:
    """/hades:full handler. raw_args: citation id (required)."""
    citation_id = raw_args.strip()
    if not citation_id:
        return _PROMPT_NO_ID
    return _PROMPT.format(citation_id=citation_id)
