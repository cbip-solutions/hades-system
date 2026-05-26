# SPDX-License-Identifier: MIT
                                             
"""Tests for the shared hook helpers in plugin/zen-swarm/hooks/_common.py."""

from __future__ import annotations

import json
import os
from pathlib import Path
from unittest.mock import patch

from hermes_plugins.hades.hooks import _common  # noqa: E402


def test_poster_bin_path_resolves_to_plugin_root():
    """_POSTER_BIN should resolve relative to plugin root (parent of hooks/)."""
    plugin_root = Path(_common.__file__).resolve().parent.parent
    expected_parent = plugin_root / "bin"
    assert _common._POSTER_BIN.parent == expected_parent
    assert _common._POSTER_BIN.name == "zen-event-poster"


def test_invoke_event_poster_dry_run_returns_zero():
    """ZEN_HOOK_DRY_RUN=1 short-circuits subprocess; returns 0 without invoking poster."""
    payload = {"session_id": "x", "cwd": "/tmp/p", "hook_event_name": "on_session_start"}
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        rc = _common.invoke_event_poster("on_session_start", payload)
    assert rc == 0


def test_invoke_event_poster_missing_binary_returns_one():
    """If bin/zen-event-poster is absent, helper returns 1 (warning, not block)."""
    payload = {"session_id": "x", "cwd": "/tmp/p"}
                                                 
    with patch.object(_common, "_POSTER_BIN", Path("/nonexistent/zen-event-poster")):
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("ZEN_HOOK_DRY_RUN", None)
            rc = _common.invoke_event_poster("on_session_start", payload)
    assert rc == 1


def test_invoke_event_poster_serializes_json_correctly():
    """Helper must pass JSON-encoded payload to the subprocess on stdin."""
    captured = {}

    def fake_run(cmd, input, timeout, check, capture_output):
        captured["cmd"] = cmd
        captured["input"] = input

        class FakeResult:
            returncode = 0
            stderr = b""

        return FakeResult()

    payload = {"session_id": "abc", "tool_name": "Bash"}
    with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
        with patch("hermes_plugins.hades.hooks._common.Path.exists", return_value=True):
            with patch(
                "hermes_plugins.hades.hooks._common.subprocess.run",
                side_effect=fake_run,
            ):
                os.environ.pop("ZEN_HOOK_DRY_RUN", None)
                rc = _common.invoke_event_poster("pre_tool_call", payload)
    assert rc == 0
    assert captured["cmd"][1] == "pre_tool_call"
    assert json.loads(captured["input"]) == payload


def test_invoke_event_poster_handles_timeout():
    """subprocess.TimeoutExpired → return 1 (warn), never raise."""
    import subprocess

    def fake_run(*args, **kwargs):
        raise subprocess.TimeoutExpired(cmd="zen-event-poster", timeout=1.0)

    payload = {"session_id": "x"}
    with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
        with patch("hermes_plugins.hades.hooks._common.Path.exists", return_value=True):
            with patch(
                "hermes_plugins.hades.hooks._common.subprocess.run",
                side_effect=fake_run,
            ):
                os.environ.pop("ZEN_HOOK_DRY_RUN", None)
                rc = _common.invoke_event_poster("on_session_start", payload)
    assert rc == 1


def test_invoke_event_poster_handles_oserror():
    """OSError (e.g., permission denied) → return 1 (warn)."""

    def fake_run(*args, **kwargs):
        raise OSError("permission denied")

    payload = {"session_id": "x"}
    with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
        with patch("hermes_plugins.hades.hooks._common.Path.exists", return_value=True):
            with patch(
                "hermes_plugins.hades.hooks._common.subprocess.run",
                side_effect=fake_run,
            ):
                os.environ.pop("ZEN_HOOK_DRY_RUN", None)
                rc = _common.invoke_event_poster("on_session_start", payload)
    assert rc == 1


def test_pre_llm_call_emits_event_returns_none_by_default():
    """Plan 11 will return {context: ...}; Phase H' baseline returns None
    (event-emit only, no injection)."""
    from hermes_plugins.hades.hooks.llm_handlers import pre_llm_call

    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        result = pre_llm_call(
            session_id="sess-1",
            cwd="/tmp/p",
            messages=[{"role": "user", "content": "hello"}],
        )
                                     
    assert result is None


def test_safe_str_none_returns_empty():
    """safe_str(None) returns '' (not 'None')."""
    assert _common.safe_str(None) == ""


def test_safe_str_short_string_passthrough():
    """Strings shorter than max_len pass through unchanged."""
    assert _common.safe_str("hello", 10) == "hello"


def test_safe_str_exact_length_passthrough():
    """Strings at exactly max_len pass through (no truncation; len(s) > max_len is the trigger)."""
    s = "a" * 200
    assert _common.safe_str(s, 200) == s


def test_safe_str_truncates_long_strings_with_ellipsis_suffix():
    """Long strings are truncated to max_len + '...' (total length = max_len + 3)."""
    s = "a" * 250
    out = _common.safe_str(s, 200)
    assert out == "a" * 200 + "..."
    assert len(out) == 203                                                    


def test_safe_str_non_string_value_stringified():
    """Non-string values are converted via str()."""
    assert _common.safe_str(42) == "42"
    assert _common.safe_str([1, 2, 3]) == "[1, 2, 3]"


def test_summarize_args_non_dict_returns_empty():
    """Non-dict input returns {} (defensive against bad caller input)."""
    assert _common.summarize_args("not a dict") == {}
    assert _common.summarize_args(None) == {}
    assert _common.summarize_args([1, 2]) == {}


def test_summarize_args_top_level_strings_truncated():
    """Top-level string values are passed through safe_str truncation."""
    args = {"command": "x" * 250}
    out = _common.summarize_args(args)
    assert out["command"] == "x" * 200 + "..."


def test_summarize_args_short_strings_passthrough():
    """Top-level short string values pass through unchanged."""
    args = {"command": "git status", "cwd": "/tmp"}
    out = _common.summarize_args(args)
    assert out == {"command": "git status", "cwd": "/tmp"}


def test_summarize_args_nested_dict_marker():
    """Nested dict values are replaced with '[object]' marker."""
    args = {"env": {"K": "v", "M": "n"}, "command": "ls"}
    out = _common.summarize_args(args)
    assert out["env"] == "[object]"
    assert out["command"] == "ls"


def test_summarize_args_nested_list_marker():
    """Nested list values are replaced with '[array]' marker."""
    args = {"files": ["a.txt", "b.txt"], "tool": "Write"}
    out = _common.summarize_args(args)
    assert out["files"] == "[array]"
    assert out["tool"] == "Write"


def test_summarize_args_primitives_passthrough():
    """Non-string primitives (int, float, bool, None) pass through unchanged."""
    args = {"count": 5, "ratio": 1.5, "enabled": True, "marker": None}
    out = _common.summarize_args(args)
    assert out == {"count": 5, "ratio": 1.5, "enabled": True, "marker": None}


def test_invoke_event_poster_returncode_propagates_nonzero():
    """Helper returns binary's actual exit code, even when non-zero (1 or 2)."""

    def fake_run_returning(rc):
        def _fake(*args, **kwargs):
            class FakeResult:
                returncode = rc
                stderr = b""

            return FakeResult()

        return _fake

    payload = {"session_id": "x"}
    for rc in (1, 2, 127):
        with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
            with patch(
                "hermes_plugins.hades.hooks._common.Path.exists", return_value=True
            ):
                with patch(
                    "hermes_plugins.hades.hooks._common.subprocess.run",
                    side_effect=fake_run_returning(rc),
                ):
                    os.environ.pop("ZEN_HOOK_DRY_RUN", None)
                    out = _common.invoke_event_poster("pre_tool_call", payload)
        assert out == rc, f"returncode {rc} not propagated; got {out}"


def test_invoke_event_poster_surfaces_stderr_via_logger(caplog):
    """When subprocess returns non-empty stderr, the helper logs it as a warning."""
    import logging

    def fake_run(*args, **kwargs):
        class FakeResult:
            returncode = 0
            stderr = b"daemon unreachable\n"

        return FakeResult()

    payload = {"session_id": "x"}
    caplog.set_level(logging.WARNING, logger="hermes_plugins.hades.hooks._common")
    with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
        with patch("hermes_plugins.hades.hooks._common.Path.exists", return_value=True):
            with patch(
                "hermes_plugins.hades.hooks._common.subprocess.run",
                side_effect=fake_run,
            ):
                os.environ.pop("ZEN_HOOK_DRY_RUN", None)
                _common.invoke_event_poster("pre_tool_call", payload)
    assert any(
        "zen-swarm hook poster stderr: daemon unreachable" in rec.message
        for rec in caplog.records
    ), f"expected stderr-surfacing log; got {[r.message for r in caplog.records]}"


def test_invoke_event_poster_json_encode_failure_returns_one():
    """Non-JSON-serializable payload (e.g., contains a set) returns 1, never raises."""
    payload = {"session_id": "x", "bad": {1, 2, 3}}                                
    with patch.object(_common, "_POSTER_BIN", Path("/fake/zen-event-poster")):
        with patch("hermes_plugins.hades.hooks._common.Path.exists", return_value=True):
            os.environ.pop("ZEN_HOOK_DRY_RUN", None)
            rc = _common.invoke_event_poster("pre_tool_call", payload)
    assert rc == 1
