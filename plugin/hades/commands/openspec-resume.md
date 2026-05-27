---
name: openspec:resume
description: Resume a paused propose/apply/archive phase
arguments:
  - name: feature_name
    type: string
    required: true
---

# Resume: {{feature_name}}

Query daemon for the current phase of `{{feature_name}}`:

- If phase = "proposing": re-enter doc-live mode; load conversation
  history from daemon and continue the wizard / live-edit loop.
- If phase = "applying": stream SSE events; surface latest attention items.
- If phase = "archiving": render the in-progress archive briefing.

HADES design wires conversation state preservation across runtime restarts.
