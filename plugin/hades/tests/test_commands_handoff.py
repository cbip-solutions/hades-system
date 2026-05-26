# SPDX-License-Identifier: MIT
"""Tests for the /zen-swarm:handoff slash command handler."""

from __future__ import annotations

from unittest.mock import patch

from hermes_plugins.hades.commands.handoff import (
    _build_commit_msg,
    _git_brief,
    _read_prior_handoff,
    handle_handoff,
)


def test_handle_handoff_returns_string():
    result = handle_handoff("")
    assert result is None or isinstance(result, str)


def test_handle_handoff_includes_template(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    result = handle_handoff("")
    assert result is not None
                                                        
    assert "## TL;DR" in result
    assert "## Repo state" in result
    assert "## Active plan status" in result


def test_handle_handoff_includes_commit_message():
    result = handle_handoff("")
    assert result is not None
    assert "docs(handoff)" in result


def test_handle_handoff_uses_existing_handoff_as_basis(tmp_path, monkeypatch):
    """If a HANDOFF.md exists in cwd, the proposed update should reference
    its prior content (e.g., quote the prior TL;DR section so operator can
    see what's being replaced)."""
    handoff = tmp_path / "HANDOFF.md"
    handoff.write_text("# HANDOFF\n\n## TL;DR\n\nPrior session state marker.\n")
    monkeypatch.chdir(tmp_path)
    result = handle_handoff("")
    assert result is not None
                                                                         
    assert "HANDOFF" in result or "prior" in result.lower()


def test_handle_handoff_not_placeholder():
    import inspect

    from hermes_plugins.hades.commands import handoff as mod

    src = inspect.getsource(mod.handle_handoff)
    assert "placeholder" not in src.lower(), (
        "handle_handoff still appears to be a placeholder"
    )


def test_handle_handoff_args_passthrough(tmp_path, monkeypatch):
    """Operator may pass a context note as raw_args; handler should include
    it in the TL;DR proposal."""
    monkeypatch.chdir(tmp_path)
    result = handle_handoff("phase H' shipping")
    assert result is not None
                                                    
    assert "phase H'" in result.lower() or "shipping" in result.lower()


                                                                                


def test_git_brief_no_git_dir(tmp_path):
    """_git_brief returns empty strings for non-git directory."""
    info = _git_brief(tmp_path)
    assert info["branch"] == ""
    assert info["last_commit"] == ""
    assert info["dirty"] == "no"


def test_git_brief_with_real_git(tmp_path):
    """_git_brief reads branch/commit from a real git repo (subprocess path)."""
    import subprocess

    subprocess.run(["git", "init", str(tmp_path)], check=True, capture_output=True)
    subprocess.run(
        ["git", "-C", str(tmp_path), "config", "user.email", "test@x.com"],
        check=True,
        capture_output=True,
    )
    subprocess.run(
        ["git", "-C", str(tmp_path), "config", "user.name", "T"],
        check=True,
        capture_output=True,
    )
                                                                    
    (tmp_path / "f.txt").write_text("x")
    subprocess.run(
        ["git", "-C", str(tmp_path), "add", "f.txt"],
        check=True,
        capture_output=True,
    )
    subprocess.run(
        ["git", "-C", str(tmp_path), "commit", "-m", "init"],
        check=True,
        capture_output=True,
    )
    info = _git_brief(tmp_path)
                                                                         
    assert info["branch"] != "" or info["last_commit"] != ""
    assert info["dirty"] == "no"


def test_git_brief_dirty(tmp_path):
    """_git_brief sets dirty=yes when there are uncommitted changes."""
    import subprocess

    subprocess.run(["git", "init", str(tmp_path)], check=True, capture_output=True)
    subprocess.run(
        ["git", "-C", str(tmp_path), "config", "user.email", "test@x.com"],
        check=True,
        capture_output=True,
    )
    subprocess.run(
        ["git", "-C", str(tmp_path), "config", "user.name", "T"],
        check=True,
        capture_output=True,
    )
    (tmp_path / "f.txt").write_text("x")
    subprocess.run(
        ["git", "-C", str(tmp_path), "add", "f.txt"],
        check=True,
        capture_output=True,
    )
    subprocess.run(
        ["git", "-C", str(tmp_path), "commit", "-m", "init"],
        check=True,
        capture_output=True,
    )
                   
    (tmp_path / "f.txt").write_text("changed")
    info = _git_brief(tmp_path)
    assert info["dirty"] == "yes"


def test_git_brief_subprocess_error(tmp_path):
    """_git_brief handles SubprocessError gracefully — best-effort, no raise."""
                                                                       
    (tmp_path / ".git").mkdir()
    with patch("subprocess.run", side_effect=OSError("no git")):
        info = _git_brief(tmp_path)
                                   
    assert info["branch"] == ""
    assert info["last_commit"] == ""
    assert info["dirty"] == "no"


def test_read_prior_handoff_missing(tmp_path):
    """_read_prior_handoff returns empty string when file absent."""
    assert _read_prior_handoff(tmp_path) == ""


def test_read_prior_handoff_oserror(tmp_path):
    """_read_prior_handoff returns empty string on OSError."""
                                                                            
                                                                                 
    fake = tmp_path / "HANDOFF.md"
    fake.write_text("content")
    with patch("pathlib.Path.read_text", side_effect=OSError("io error")):
        result = _read_prior_handoff(tmp_path)
    assert result == ""


def test_build_commit_msg_empty_seed():
    msg = _build_commit_msg("")
    assert msg == "docs(handoff): refresh state snapshot"


def test_build_commit_msg_short_seed():
    msg = _build_commit_msg("phase H' done")
    assert "docs(handoff): refresh post" in msg
    assert "phase H' done" in msg


def test_build_commit_msg_long_seed_truncation():
    long = "x" * 80
    msg = _build_commit_msg(long)
                                      
    assert msg.endswith("...")
                                            
    assert len(msg) < 120


def test_build_commit_msg_whitespace_only_seed():
    """Whitespace-only seed collapses to empty after strip — defensive
    guard prevents producing 'docs(handoff): refresh post ' with trailing
    space (H'-8 NIT-2 backlog item)."""
    for ws in ("   ", "\t\t", "\n\n", " \t\n "):
        msg = _build_commit_msg(ws)
        assert msg == "docs(handoff): refresh state snapshot", (
            f"whitespace-only input {ws!r} should fall back to default"
        )
