# SPDX-License-Identifier: MIT
                                                 
"""Tests for session_handlers (on_session_start + on_session_end)."""

from __future__ import annotations

import os
from pathlib import Path
from unittest.mock import patch

from hermes_plugins.hades.hooks.session_handlers import (
    extract_tldr,
    on_session_end,
    on_session_start,
)


def test_extract_tldr_finds_section(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "# zen-swarm — HANDOFF\n\n## TL;DR\n\n"
        "Phase H' Hermes redirect shipping.\n\n"
        "## Repo state\n\nBranch main, last commit feat(plugin)...\n"
    )
    tldr = extract_tldr(handoff)
    assert "Phase H' Hermes redirect shipping" in tldr
    assert "## Repo state" not in tldr


def test_extract_tldr_returns_empty_for_missing_file():
    assert extract_tldr(Path("/nonexistent/HANDOFF.md")) == ""


def test_extract_tldr_returns_empty_when_no_tldr_section(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("# header\n\n## Other section\n\nbody\n")
    assert extract_tldr(handoff) == ""


def test_extract_tldr_caps_length(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR\n\n" + ("x" * 5000) + "\n")
    out = extract_tldr(handoff)
    assert len(out) < 5000                      


def test_on_session_start_dry_run_no_exception():
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        result = on_session_start(
            session_id="sess-1",
            cwd="/tmp/no-handoff",
            source="startup",
        )
                                                                          
                                      
    assert result is None or isinstance(result, str)


def test_on_session_start_includes_tldr_when_handoff_present(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR\n\nResume marker present.\n")
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        result = on_session_start(
            session_id="sess-2",
            cwd=str(tmp_path),
            source="resume",
        )
                                                                          
                                                                       
                                                                          
                                   
    assert result is not None
    assert "Resume marker present" in result
    assert "zen-swarm session resume" in result                     


def test_on_session_start_handles_unreadable_handoff(tmp_path, monkeypatch):
    """If HANDOFF.md exists but read fails, callback must not raise."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR\nx\n")
    handoff.chmod(0o000)
    try:
        with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
                              
            on_session_start(
                session_id="sess-3",
                cwd=str(tmp_path),
                source="startup",
            )
    finally:
                                            
        handoff.chmod(0o644)


def test_on_session_end_no_exception():
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        result = on_session_end(
            session_id="sess-end-1",
            completed=True,
        )
    assert result is None


def test_extract_tldr_accepts_em_dash_suffix(tmp_path):
    """Real-world HANDOFF.md format: ## TL;DR — Where we are (date)."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "## TL;DR — Where we are (2026-05-11)\n\n"
        "Phase H' shipping.\n\n"
        "## Repo state\n\nrest\n"
    )
    out = extract_tldr(handoff)
    assert "Phase H' shipping" in out
    assert "## Repo state" not in out


def test_extract_tldr_accepts_colon_suffix(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR: snapshot\n\nbody-text\n")
    assert "body-text" in extract_tldr(handoff)


def test_extract_tldr_accepts_parenthetical_suffix(tmp_path):
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR (2026-05-11)\n\nparen-body\n")
    assert "paren-body" in extract_tldr(handoff)


def test_extract_tldr_ignores_h3_or_deeper(tmp_path):
    """Anchored regex rejects ### TL;DR (h3) and deeper headings."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "# top\n\n## Actual section\n\nactual body\n\n"
        "### TL;DR\n\nh3 body must NOT be extracted as h2 TL;DR\n"
    )
    assert extract_tldr(handoff) == ""


def test_extract_tldr_returns_empty_for_empty_body(tmp_path):
    """Empty TL;DR section followed by another ## must return empty string,
    not capture the next section's content."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR\n\n\n## Next\nactual section body\n")
    assert extract_tldr(handoff) == ""


def test_extract_tldr_case_insensitive(tmp_path):
    """re.IGNORECASE flag — `## tl;dr` (lowercase) also matches."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## tl;dr\n\nlowercase body\n## next\nx\n")
    assert "lowercase body" in extract_tldr(handoff)


def test_on_session_start_returns_none_when_cwd_empty():
    """Documented contract: cwd absent → return None."""
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        result = on_session_start(session_id="sess-x", cwd="", source="startup")
    assert result is None


def test_on_session_start_payload_includes_source(monkeypatch):
    """Payload composition contract: session_id/cwd/source/hook_event_name."""
    from hermes_plugins.hades.hooks import session_handlers

    captured = []
    monkeypatch.setattr(
        session_handlers,
        "invoke_event_poster",
        lambda name, payload: captured.append((name, payload)) or 0,
    )
    session_handlers.on_session_start(session_id="s1", cwd="/tmp", source="resume")
    assert len(captured) == 1
    name, payload = captured[0]
    assert name == "on_session_start"
    assert payload["session_id"] == "s1"
    assert payload["cwd"] == "/tmp"
    assert payload["source"] == "resume"
    assert payload["hook_event_name"] == "on_session_start"


def test_on_session_end_payload_includes_completion_flags(monkeypatch):
    """on_session_end payload: session_id/completed/interrupted/hook_event_name."""
    from hermes_plugins.hades.hooks import session_handlers

    captured = []
    monkeypatch.setattr(
        session_handlers,
        "invoke_event_poster",
        lambda name, payload: captured.append((name, payload)) or 0,
    )
    session_handlers.on_session_end(session_id="s2", completed=False, interrupted=True)
    assert len(captured) == 1
    name, payload = captured[0]
    assert name == "on_session_end"
    assert payload["session_id"] == "s2"
    assert payload["completed"] is False
    assert payload["interrupted"] is True
    assert payload["hook_event_name"] == "on_session_end"


def test_extract_tldr_body_with_h3_subheadings_not_empty(tmp_path):
    """Real HANDOFF.md shape: ## TL;DR — heading followed by ### sub-sections.
    The empty-body guard must not falsely treat h3 content as a captured next
    section (h3 starts with `###`, not `## `)."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "## TL;DR — Where we are (2026-05-11 end-of-session)\n\n"
        "### This session's work\n\n"
        "12 commits + 7 PRs landed.\n\n"
        "## Repo state\n\nBranch main.\n"
    )
    out = extract_tldr(handoff)
    assert "### This session's work" in out
    assert "12 commits" in out
    assert "## Repo state" not in out
