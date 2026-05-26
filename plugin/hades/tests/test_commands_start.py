# SPDX-License-Identifier: MIT
"""Tests for the /zen-swarm:start slash command handler."""

from __future__ import annotations

import os
from pathlib import Path
from unittest.mock import patch

from hermes_plugins.hades.commands.start import handle_start


def test_handle_start_returns_string():
    """Slash command handlers must return str | None per Hermes contract
    (plugins.py:330-355)."""
    result = handle_start("")
    assert result is None or isinstance(result, str)


def test_handle_start_includes_handoff_section_when_present(tmp_path, monkeypatch):
    """When invoked in a zen-swarm project with HANDOFF.md, output should
    surface the TL;DR section."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "# HANDOFF\n\n## TL;DR\n\n"
        "Phase H' Hermes redirect in flight.\n\n"
        "## Repo state\n\nBranch main.\n"
    )
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
    assert "Phase H' Hermes redirect" in result


def test_handle_start_includes_handoff_with_em_dash_suffix(tmp_path, monkeypatch):
    """Real HANDOFF.md format: '## TL;DR — Where we are (2026-05-11)'.
    Must extract body — regression-test for H'-4 broken regex pattern."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "# HANDOFF\n\n## TL;DR — Where we are (2026-05-11 end-of-session)\n\n"
        "Real-world TL;DR body with em-dash heading suffix.\n\n"
        "## Repo state\n\nbranch main\n"
    )
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
    assert "Real-world TL;DR body" in result


def test_handle_start_handles_missing_handoff(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
                                                                           
    assert "HANDOFF" in result or "session resume" in result.lower()


def test_handle_start_handles_args_text_passthrough(tmp_path, monkeypatch):
    """Handler signature accepts raw_args; even if operator passes text,
    handler must not crash."""
    monkeypatch.chdir(tmp_path)
    result = handle_start("some extra args")
    assert result is None or isinstance(result, str)


def test_handle_start_not_placeholder():
    """Sanity: the handler body must have been filled in (not a one-line
    placeholder return)."""
    import inspect

    from hermes_plugins.hades.commands import start as mod

    src = inspect.getsource(mod.handle_start)
                                                                                            
    assert "placeholder" not in src.lower(), (
        "handle_start still appears to be a placeholder"
    )


def test_extract_section_empty_body_returns_empty(tmp_path, monkeypatch):
    """Empty TL;DR section followed by another ## must not capture next section's body."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "## TL;DR\n\n\n## Active plan status\n\nshould not appear in TL;DR\n"
    )
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
                                                                           
                                                                           
                                                                                   
    assert "### TL;DR" not in result


def test_extract_section_truncates_at_max_chars(tmp_path, monkeypatch):
    """Body longer than _SECTION_MAX_CHARS (2000) gets truncated with '...' suffix."""
    handoff = tmp_path / "HANDOFF.md"
    long_body = "x" * 3000
    handoff.write_text(f"## TL;DR\n\n{long_body}\n\n## Next\nrest\n")
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
                                                                             
    assert "..." in result


def test_handle_start_all_four_sections_extracted(tmp_path, monkeypatch):
    """When HANDOFF.md has all 4 sections (TL;DR, Active plan, Pending operator
    actions, Suggested first-message), all 4 should appear in output."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text(
        "## TL;DR\n\ntldr body\n\n"
        "## Active plan status\n\nactive plan body\n\n"
        "## Pending operator actions\n\npending body\n\n"
        "## Suggested first-message\n\nsuggested body\n"
    )
    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
    assert "tldr body" in result
    assert "active plan body" in result
    assert "pending body" in result
    assert "suggested body" in result
                                                    
    assert "### TL;DR" in result
    assert "### Active plan" in result
    assert "### Pending operator actions" in result
    assert "### Suggested next" in result


def test_handle_start_git_section_in_real_repo(tmp_path, monkeypatch):
    """When invoked in a real git repo, the Repo state section should include
    last-commit / branch / uncommitted-changes lines."""
    import subprocess as _sp

                                   
    _sp.run(["git", "init", "-q", str(tmp_path)], check=True, timeout=5)
    _sp.run(
        ["git", "-C", str(tmp_path), "config", "user.email", "test@test.local"],
        check=True,
        timeout=5,
    )
    _sp.run(
        ["git", "-C", str(tmp_path), "config", "user.name", "Test"], check=True, timeout=5
    )
    _sp.run(
        ["git", "-C", str(tmp_path), "config", "commit.gpgsign", "false"],
        check=True,
        timeout=5,
    )
    (tmp_path / "file.txt").write_text("hello\n")
    _sp.run(["git", "-C", str(tmp_path), "add", "."], check=True, timeout=5)
    _sp.run(
        ["git", "-C", str(tmp_path), "commit", "-q", "-m", "test: initial"],
        check=True,
        timeout=5,
        env={
            **os.environ,
            "GIT_AUTHOR_NAME": "Test",
            "GIT_AUTHOR_EMAIL": "test@test.local",
            "GIT_COMMITTER_NAME": "Test",
            "GIT_COMMITTER_EMAIL": "test@test.local",
        },
    )

    monkeypatch.chdir(tmp_path)
    result = handle_start("")
    assert result is not None
    assert "### Repo state" in result
    assert "Last commit" in result
    assert "Branch" in result
    assert "Uncommitted changes" in result


def test_handle_start_handles_handoff_read_error(tmp_path, monkeypatch):
    """If HANDOFF.md exists but read raises OSError, handler must not crash."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("## TL;DR\n\nbody\n")
    monkeypatch.chdir(tmp_path)

                                      
    with patch.object(Path, "read_text", side_effect=OSError("permission denied")):
        result = handle_start("")
    assert result is not None
    assert isinstance(result, str)
                                                                      


def test_daemon_brief_returns_documented_status_string(tmp_path, monkeypatch):
    """_daemon_brief returns one of: 'running' / 'NOT running' / 'unknown' markdown lines."""
    from hermes_plugins.hades.commands.start import _daemon_brief

    monkeypatch.chdir(tmp_path)
    result = _daemon_brief()
                                                                
    assert result.startswith("- **Daemon**:")
                                                      
    assert any(suffix in result for suffix in ["running", "NOT running", "unknown"])


def test_git_brief_returns_empty_outside_repo(tmp_path, monkeypatch):
    """_git_brief returns '' when cwd is not a git repo (no .git directory)."""
    from hermes_plugins.hades.commands.start import _git_brief

                                    
    result = _git_brief(tmp_path)
    assert result == ""
