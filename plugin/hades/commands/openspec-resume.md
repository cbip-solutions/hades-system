---
name: openspec:resume
description: Resume a paused propose/apply/archive stage
arguments:
  - name: feature_name
    type: string
    required: true
---

# Resume: {{feature_name}}

Query daemon for the current stage of `{{feature_name}}`:

- If stage = "proposing": re-enter doc-live mode; load conversation
  history from daemon and continue the wizard / live-edit loop.
- If stage = "applying": stream SSE events; surface latest attention items.
- If stage = "archiving": render the in-progress archive briefing.

HADES design wires conversation state preservation across runtime restarts.
