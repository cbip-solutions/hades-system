# SPDX-License-Identifier: MIT
"""Slash command handler for /hades:start."""

from __future__ import annotations

import logging
import os
import re
import subprocess
from pathlib import Path

logger = logging.getLogger(__name__)

_SECTION_MAX_CHARS = 2000


def _extract_section(handoff_body: str, section_name: str) -> str:
    """Extract `## <section_name>` section from HANDOFF.md body.

    Pattern anchored at line start (rejects `### <name>` h3+ headings) and
    tolerant of same-line suffix content after the section name (e.g.
    `## TL;DR — Where we are`). Empty-body guard prevents capturing the
    next section's content when the current section has no body.

    Regression anchor: H'-4 surfaced the same broken pattern in
    session_handlers.py; H'-7 applies the corrected pattern proactively
    here so the slash-command path handles real-world HANDOFF.md correctly.

    **Constraint**: section names must not be prefixes of other section names in
    the same document. The `\\b` word boundary after the name allows
    `_extract_section(body, "Pending")` to match `## Pending operator actions`
    unintentionally. Current call sites use four unique non-prefix names.
    """
    pattern = re.compile(
        rf"(?:^|\n)##\s+{re.escape(section_name)}\b[^\n]*\n(?P<body>.*?)(?=\n##\s+|\Z)",
        re.IGNORECASE | re.DOTALL,
    )
    m = pattern.search(handoff_body)
    if not m:
        return ""
    text = m.group("body").strip()
                                                                        
                                                                         
    if re.match(r"^##\s", text):
        return ""
    if len(text) > _SECTION_MAX_CHARS:
        text = text[:_SECTION_MAX_CHARS] + "..."
    return text


def _git_brief(cwd: Path) -> str:
    """Run a brief git status/log/tag probe; returns markdown lines or empty
    string if git unavailable / not a repo."""
    if not (cwd / ".git").exists():
        return ""
    lines: list[str] = []
    try:
        out = subprocess.run(
            ["git", "-C", str(cwd), "log", "--oneline", "-1"],
            capture_output=True,
            text=True,
            timeout=2.0,
            check=False,
        )
        if out.returncode == 0 and out.stdout.strip():
            lines.append(f"- **Last commit**: `{out.stdout.strip()}`")
    except (subprocess.SubprocessError, OSError):
        pass
    try:
        out = subprocess.run(
            ["git", "-C", str(cwd), "branch", "--show-current"],
            capture_output=True,
            text=True,
            timeout=2.0,
            check=False,
        )
        if out.returncode == 0 and out.stdout.strip():
            lines.append(f"- **Branch**: `{out.stdout.strip()}`")
    except (subprocess.SubprocessError, OSError):
        pass
    try:
        out = subprocess.run(
            ["git", "-C", str(cwd), "status", "--porcelain"],
            capture_output=True,
            text=True,
            timeout=2.0,
            check=False,
        )
        if out.returncode == 0:
            dirty = bool(out.stdout.strip())
            lines.append(f"- **Uncommitted changes**: {'yes' if dirty else 'no'}")
    except (subprocess.SubprocessError, OSError):
        pass
    return "\n".join(lines)


def _daemon_brief() -> str:
    """Check daemon liveness; returns a one-line status."""
    try:
        out = subprocess.run(
            ["pgrep", "-f", "zen-swarm-ctld"],
            capture_output=True,
            text=True,
            timeout=1.0,
            check=False,
        )
        return (
            "- **Daemon**: running"
            if out.returncode == 0
            else "- **Daemon**: NOT running"
        )
    except (subprocess.SubprocessError, OSError):
        return "- **Daemon**: unknown (pgrep unavailable)"


def handle_start(raw_args: str) -> str | None:
    """Handler for /hades:start slash command.

    Args:
        raw_args: trailing text after the command name (typically empty).

    Returns:
        Markdown session-resume summary, or a fallback note if HANDOFF.md
        absent. Never raises.
    """
    cwd = Path(os.getcwd())
    handoff_path = cwd / "HANDOFF.md"
    out_lines: list[str] = ["## HADES session resume", ""]

    if handoff_path.is_file():
        try:
            body = handoff_path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            logger.warning("HADES: HANDOFF.md read failed: %s", exc)
            body = ""
        if body:
            tldr = _extract_section(body, "TL;DR")
            if tldr:
                out_lines.append("### TL;DR")
                out_lines.append("")
                out_lines.append(tldr)
                out_lines.append("")
            active = _extract_section(body, "Active plan status")
            if active:
                out_lines.append("### Active plan")
                out_lines.append("")
                out_lines.append(active)
                out_lines.append("")
            pending_op = _extract_section(body, "Pending operator actions")
            if pending_op:
                out_lines.append("### Pending operator actions")
                out_lines.append("")
                out_lines.append(pending_op)
                out_lines.append("")
            suggested = _extract_section(body, "Suggested first-message")
            if suggested:
                out_lines.append("### Suggested next")
                out_lines.append("")
                out_lines.append(suggested)
                out_lines.append("")
    else:
        out_lines.append("_HANDOFF.md not present in cwd._")
        out_lines.append("")
        out_lines.append("Run `/hades:handoff` in a future session to enable resume.")
        out_lines.append("")

    git_section = _git_brief(cwd)
    if git_section:
        out_lines.append("### Repo state")
        out_lines.append("")
        out_lines.append(git_section)
        out_lines.append("")

    daemon_line = _daemon_brief()
    if daemon_line:
        out_lines.append("### Runtime")
        out_lines.append("")
        out_lines.append(daemon_line)
        out_lines.append("")

    out_lines.append("---")
    out_lines.append("")
    out_lines.append(
        "Doctrine reminders apply: max-scope, no defer, no tech debt, "
        "no AI-attribution in commits, tag safety gate."
    )
    out_lines.append("")
    out_lines.append("¿procedo con la siguiente acción sugerida, o cambiamos prioridad?")

    return "\n".join(out_lines)
