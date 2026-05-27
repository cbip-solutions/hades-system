# SPDX-License-Identifier: MIT
"""Slash command handler for /hades:handoff.

Composes a proposed .hades/session.md update + proposed commit message for the
operator to review and apply. Does NOT autonomously write or commit (the
write+commit decision is operator-driven; the handler emits guidance).

Hermes slash command contract: fn(raw_args: str) -> str | None.
"""

from __future__ import annotations

import logging
import os
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

logger = logging.getLogger(__name__)


def _git_brief(cwd: Path) -> dict[str, str]:
    """Return a dict with branch/last_commit/dirty fields (best-effort)."""
    info = {"branch": "", "last_commit": "", "dirty": "no"}
    if not (cwd / ".git").exists():
        return info
    try:
        out = subprocess.run(
            ["git", "-C", str(cwd), "branch", "--show-current"],
            capture_output=True,
            text=True,
            timeout=2.0,
            check=False,
        )
        if out.returncode == 0:
            info["branch"] = out.stdout.strip()
    except (subprocess.SubprocessError, OSError):
        pass
    try:
        out = subprocess.run(
            ["git", "-C", str(cwd), "log", "--oneline", "-1"],
            capture_output=True,
            text=True,
            timeout=2.0,
            check=False,
        )
        if out.returncode == 0:
            info["last_commit"] = out.stdout.strip()
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
        if out.returncode == 0 and out.stdout.strip():
            info["dirty"] = "yes"
    except (subprocess.SubprocessError, OSError):
        pass
    return info


def _read_prior_handoff(cwd: Path) -> str:
    path = cwd / ".hades/session.md"
    if not path.is_file():
        return ""
    try:
        return path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return ""


def _build_handoff_template(
    tldr_seed: str,
    git: dict[str, str],
) -> str:
    """Compose the proposed .hades/session.md content.

    Caller (handle_handoff) handles the prior-.hades/session.md presence
    branching at the wrapper-message level; the template body is the
    same regardless. See feedback in H'-8 NIT-1 backlog.
    """
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    tldr_text = tldr_seed if tldr_seed else "<1-3 sentences synthesizing the session>"
    branch = git.get("branch") or "<branch>"
    last_commit = git.get("last_commit") or '<hash> "<subject>"'
    dirty = git.get("dirty", "no")
    return f"""# HADES — Session handoff

_Last updated: {ts}_

## TL;DR

{tldr_text}

## Repo state

- Branch: `{branch}`
- Last commit: `{last_commit}`
- Uncommitted: {dirty}
- Recent tags: <fill in: `git tag --list 'v*' --sort=-creatordate | head -3`>

## Active plan status

<release item release track — <status>; or "no active plan">

## Pending dispatches

<Background subagents launched but not yet collected; non-recoverable across
session ends. List with explicit "NON-RECOVERABLE; re-dispatch in next session".>

## Pending operator actions

<Items awaiting operator (e.g., 'operator must run `bin/hades bypass extract-config`
interactively to enable /v1/messages').>

## Suggested first-message

<Single-letter approval (`procede` / `y`) OR concrete next step>

## See also

- `design records`
- `docs/METHODOLOGY.md`
- `~/.claude/projects/-path-to-projects-hades-system/memory/MEMORY.md`
"""


def _build_commit_msg(tldr_seed: str) -> str:
    if not tldr_seed:
        return "docs(handoff): refresh state snapshot"
    # Truncate to a conventional-commit-friendly subject (~70 chars max).
    seed = re.sub(r"\s+", " ", tldr_seed).strip()
    # Whitespace-only inputs collapse to empty here even when tldr_seed
    # was truthy (e.g. "   "). Guard against producing a subject with a
    # trailing space. See feedback in H'-8 NIT-2 backlog.
    if not seed:
        return "docs(handoff): refresh state snapshot"
    if len(seed) > 60:
        seed = seed[:57] + "..."
    return f"docs(handoff): refresh post {seed}"


def handle_handoff(raw_args: str) -> str | None:
    """Handler for /hades:handoff slash command.

    Args:
        raw_args: trailing text — optionally a TL;DR seed phrase (1-2 words
            of context, e.g. "phase H' shipping").

    Returns:
        Markdown block with proposed .hades/session.md content + proposed commit
        message + apply instructions. Operator reviews and applies in their
        terminal or via Hermes Bash tool.
    """
    cwd = Path(os.getcwd())
    git = _git_brief(cwd)
    prior = _read_prior_handoff(cwd)
    prior_present = bool(prior)
    tldr_seed = (raw_args or "").strip()

    template = _build_handoff_template(tldr_seed, git)
    commit_msg = _build_commit_msg(tldr_seed)

    out = ["## Proposed .hades/session.md update", ""]
    if prior_present:
        out.append(
            "_(replaces existing .hades/session.md — preserve any sections not in this proposal)_"
        )
    else:
        out.append("_(no prior .hades/session.md in cwd — creating from scratch)_")
    out.append("")
    out.append("```markdown")
    out.append(template)
    out.append("```")
    out.append("")
    out.append("## Proposed commit")
    out.append("")
    out.append("```bash")
    out.append(
        "# Save the markdown above to .hades/session.md (replace existing or create new)."
    )
    out.append("# Then:")
    out.append("git add .hades/session.md")
    out.append(f"git commit -m {commit_msg!r}")
    out.append("```")
    out.append("")
    out.append("## Optional push")
    out.append("")
    out.append(
        f"`git push origin {git.get('branch') or '<branch>'}` (operator approval; "
        "tag safety gate: NEVER push tags here)"
    )
    out.append("")
    out.append("---")
    out.append("")
    out.append(
        "Doctrine: conventional commit subject, NO AI-attribution "
        "(inv-hades-004 gated by pre_tool_call callback)."
    )

    return "\n".join(out)
