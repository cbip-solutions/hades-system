# SPDX-License-Identifier: MIT
"""/hades:openspec-resume handler — Resume a paused propose/apply/archive phase.

Port from CC-format plugin/hades/commands/openspec-resume.md workflow logic.
"""

from __future__ import annotations

_PROMPT = """# HADES /openspec-resume — Resume {feature_name}

Query HADES daemon for the current phase of `{feature_name}`:

## 1. Query daemon

```bash
PHASE=$(curl --unix-socket /tmp/hades-system.sock -s \\
             "http://unix/v1/swarms?feature={feature_name}" \\
             | jq -r '.swarms[0].phase')
```

## 2. Branch by phase

### Phase = "proposing"

Re-enter doc-live mode:
<!-- the release design — pending endpoint registration: swarm conversation history (GET); the release design swarm substrate ships but conversation log endpoint awaits the release design -->
- Load conversation history from daemon: `curl --unix-socket /tmp/hades-system.sock -s "http://unix/v1/swarms/<id>/conversation"`
- Continue the wizard / live-edit loop where operator left off
- Render any file diffs since session pause

### Phase = "applying"

Stream SSE events:
<!-- the release design — pending endpoint registration: swarm SSE event stream awaits the release design 'hades migrate' -->
- `curl --unix-socket /tmp/hades-system.sock --no-buffer "http://unix/v1/swarms/<id>/events"`
- Surface latest attention items
- Show progress (tasks complete / tasks in flight / tasks blocked)

### Phase = "archiving"

Render the in-progress archive briefing:
- Same UI as `/openspec-archive` but pre-populated with prior decisions
- Continue from where operator left off

### Phase = "complete"

Show "Feature {feature_name} complete; nothing to resume." + show last commit SHA + suggest next feature.

## 3. Conversation state

the release design wires conversation state preservation across runtime restarts (daemon-side persistence).

## Cross-references

- spec §3 modo C híbrido
- the release design conversation continuity
- /hades:openspec-propose (precedent if not yet started)
- /hades:openspec-apply (precedent for apply phase)
- /hades:openspec-archive (precedent for archive phase)
"""

_PROMPT_NO_FEATURE = """HADES: feature name required for openspec-resume
  /hades:openspec-resume requires a feature name to identify which swarm phase to resume.
  → Invoke /hades:openspec-resume <feature-name> or start fresh with /hades:openspec-propose <feature-name>
"""


def openspec_resume_handler(raw_args: str) -> str | None:
    """/hades:openspec-resume handler. raw_args: feature_name (required)."""
    feature_name = raw_args.strip()
    if not feature_name:
        return _PROMPT_NO_FEATURE
    return _PROMPT.format(feature_name=feature_name)
