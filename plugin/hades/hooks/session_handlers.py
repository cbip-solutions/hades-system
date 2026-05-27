# SPDX-License-Identifier: MIT
# plugin/zen-swarm/hooks/session_handlers.py
"""Hermes on_session_start + on_session_end callbacks for zen-swarm."""

from __future__ import annotations

import logging
import re
from pathlib import Path
from typing import Any

from ._common import invoke_event_poster

logger = logging.getLogger(__name__)

# Match a top-level `## TL;DR` heading (anchored at line start; rejects `### TL;DR`
# h3+ and mid-line occurrences). Tolerates any same-line trailing content after
# `TL;DR` (e.g. `## TL;DR — Where we are`, `## TL;DR (2026-05-11)`, `## TL;DR: ...`).
# Body captured up to the next `##`-prefixed line at line start, or end of file.
_TLDR_PATTERN = re.compile(
    r"(?:^|\n)##\s+TL;DR\b[^\n]*\n(?P<body>.*?)(?=\n##\s+|\Z)",
    re.IGNORECASE | re.DOTALL,
)
# Cap on emitted TL;DR text to avoid context bloat.
_TLDR_MAX_CHARS = 2000


def extract_tldr(handoff_path: Path) -> str:
    """Extract the TL;DR section body from .hades/session.md.

    Returns the trimmed body text capped at _TLDR_MAX_CHARS. Returns ""
    if the file is missing, unreadable, or contains no TL;DR section.
    """
    try:
        body = handoff_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        logger.debug("zen-swarm: .hades/session.md read failed at %s: %s", handoff_path, exc)
        return ""
    match = _TLDR_PATTERN.search(body)
    if not match:
        return ""
    text = match.group("body").strip()
    # Guard against empty TL;DR body capturing the next section's content.
    # When the original TL;DR has no body (immediately followed by another ## header),
    # the regex's lazy `.*?` may consume the blank line and start of the next section
    # before the lookahead succeeds. Detect this by checking whether the stripped body
    # starts with a top-level `## ` marker (exactly two hashes + space, not three+).
    # Note: `###` content is valid TL;DR sub-content; only `## ` signals a captured
    # adjacent section rather than real TL;DR body.
    if re.match(r"^##\s", text):
        return ""
    if len(text) > _TLDR_MAX_CHARS:
        text = text[:_TLDR_MAX_CHARS] + "\n\n…(truncated for context economy)"
    return text


def _build_session_resume_text(cwd: str, tldr: str) -> str:
    """Compose the operator-facing session-resume blurb."""
    return (
        "## zen-swarm session resume — .hades/session.md TL;DR\n\n"
        f"{tldr}\n\n"
        f"_Auto-loaded by zen-swarm Hermes plugin (on_session_start hook). "
        f"Project root: {cwd}._"
    )


def on_session_start(
    session_id: str = "",
    cwd: str = "",
    source: str = "",
    **kwargs: Any,
) -> str | None:
    """Hermes hook callback for `on_session_start`.

    Per Hermes plugins.py:78, registered for the canonical VALID_HOOKS name.

    Args (per Hermes on_session_start hook signature, with **kwargs for
    forward compatibility):
        session_id: current Hermes session ID
        cwd: working directory at session start
        source: trigger label (startup/resume/clear/compact)
        **kwargs: forward-compatible extras

    Returns:
        A markdown string with .hades/session.md TL;DR if present (forward-compat
        for Hermes versions that surface on_session_start returns as
        context); None otherwise.
    """
    payload = {
        "session_id": session_id,
        "cwd": cwd,
        "source": source,
        "hook_event_name": "on_session_start",
    }
    _ = invoke_event_poster("on_session_start", payload)

    if not cwd:
        return None
    handoff_path = Path(cwd) / ".hades/session.md"
    if not handoff_path.is_file():
        return None
    tldr = extract_tldr(handoff_path)
    if not tldr:
        return None
    return _build_session_resume_text(cwd, tldr)


def on_session_end(
    session_id: str = "",
    completed: bool = True,
    interrupted: bool = False,
    **kwargs: Any,
) -> None:
    """Hermes hook callback for `on_session_end`.

    Observer hook (return value ignored by Hermes). Emits an event with
    session_id + completion state for daemon-side session tracking.
    """
    payload = {
        "session_id": session_id,
        "completed": bool(completed),
        "interrupted": bool(interrupted),
        "hook_event_name": "on_session_end",
    }
    _ = invoke_event_poster("on_session_end", payload)
    return
