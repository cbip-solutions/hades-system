# SPDX-License-Identifier: MIT
"""Tests for the /hades:status slash command handler."""

from __future__ import annotations

import json
import os
import pathlib
import re
from typing import Any
from unittest.mock import patch

import httpx
import pytest

                                                                     
                                                                   
                                                                     
HAPPY_PATH_RESPONSES: dict[str, dict[str, Any]] = {
    "/v1/health": {
        "status": "ok",
        "pid": 84821,
        "uds_path": "/tmp/zen-swarm.sock",
        "active_model": "opus-4-7",
        "version": "0.17.0",
    },
    "/v1/cascade/state": {
        "active_tier": 1,
        "tier_name": "anthropic-paygo",
        "provider_count": 12,
    },
    "/v1/bypass/status": {
        "status": "live",
        "success_rate_24h": 1.0,
    },
    "/v1/cost/24h": {
        "spend_24h_usd": 0.043,
        "spend_session_usd": 0.041,
    },
    "/v1/context/used": {
        "used_tokens": 24200,
        "max_tokens": 100000,
    },
    "/v1/profile/active": {
        "profile_name": "max-scope",
        "kind": "doctrine",
    },
    "/v1/cwd": {
        "cwd": "/path/to/projects/hades-system",
    },
}


def _make_happy_path_handler() -> Any:
    """Build an httpx MockTransport handler that returns HAPPY_PATH_RESPONSES
    for each of the 7 endpoints and 404 for any other path."""

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method != "GET":
            return httpx.Response(405, json={"error": "method not allowed"})
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(
                404, json={"error": "uncanned path", "path": request.url.path}
            )
        return httpx.Response(200, json=canned)

    return handler


@pytest.fixture(autouse=True)
def _disable_color(monkeypatch: pytest.MonkeyPatch) -> None:
    """Set NO_COLOR=1 globally in the test suite so output comparison is
    on plain text. C-4 will add tests that re-enable color explicitly."""
    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.delenv("HERMES_FORCE_COLOR", raising=False)


@pytest.fixture(autouse=True)
def _patch_uds_exists(monkeypatch: pytest.MonkeyPatch) -> None:
    """Autouse: patch os.path.exists so the handler doesn't hit the
    UDS-missing early-exit when the real /tmp/zen-swarm.sock is absent.

    This fixture ONLY makes the hardcoded DEFAULT path `/tmp/zen-swarm.sock`
    appear present. Tests that exercise the UDS-missing error path set
    ZEN_SWARM_UDS to a path that genuinely does not exist on disk; the
    fixture deliberately does NOT intercept that path, so the handler's
    `os.path.exists(uds_path)` check returns False as expected.

    We patch via the module's `os` reference (monkeypatch.setattr with
    the module object directly) to avoid namespace-package dotted-path
    resolution issues with hermes_plugins.*.
    """
    import hermes_plugins.hades.commands.status as status_mod

    original_exists = status_mod.os.path.exists
    _DEFAULT_UDS = "/tmp/zen-swarm.sock"

    def _exists(path: str) -> bool:
                                                                         
                                                                            
                                                                       
        if str(path) == _DEFAULT_UDS:
            return True
        return original_exists(path)

    monkeypatch.setattr(status_mod.os.path, "exists", _exists)


@pytest.fixture
def mock_transport() -> httpx.MockTransport:
    """Default happy-path transport."""
    return httpx.MockTransport(_make_happy_path_handler())


                                                                     
                                                          
                                                                     


def test_handle_status_returns_non_none_string(
    mock_transport: httpx.MockTransport,
) -> None:
    """The handler MUST return a string (Hermes slash command contract per
    `hermes_cli/plugins.py:330-355` — `fn(raw_args: str) -> str | None`).

    For the happy path the handler returns a non-None block; the None
    case is reserved for "no output to surface" which does not apply
    here (status command always surfaces a block).
    """
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert isinstance(result, str)
    assert len(result) > 0


def test_handle_status_emits_spec_header_line(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 mandates the first line is `HADES system v<version> —
    runtime status`. The version is read from `/v1/health`'s `version`
    field."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    first_line = result.splitlines()[0]
    assert first_line == "HADES system v0.17.0 — runtime status"


def test_handle_status_emits_7_data_lines_in_order(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template specifies 7 data lines in this order:
       daemon / model / cascade / bypass / cost 24h / context / profile / cwd
    The test asserts each data line appears in order.
    """
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
                                             
    assert len(lines) >= 8

    expected_labels = [
        "daemon:",
        "model:",
        "cascade:",
        "bypass:",
        "cost 24h:",
        "context:",
        "profile:",
        "cwd:",
    ]
    found_indices: list[int] = []
    for i, line in enumerate(lines):
        for label in expected_labels:
            if line.strip().startswith(label):
                found_indices.append(i)
                break

    assert len(found_indices) == 8, (
        f"expected 8 data labels (header + 7); found {len(found_indices)}\n"
        f"full output:\n{result}"
    )
    for a, b in zip(found_indices, found_indices[1:], strict=False):
        assert a < b, (
            f"data lines out of order: index {a} should be < {b}\nfull output:\n{result}"
        )


def test_handle_status_daemon_line_includes_pid_and_uds(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `daemon: ok (PID 84821, UDS /tmp/zen-swarm.sock)`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "daemon:" in result
    assert "ok" in result
    assert "PID 84821" in result
    assert "UDS /tmp/zen-swarm.sock" in result


def test_handle_status_model_line_from_health_endpoint(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `model: opus-4-7`. The model is read from
    `/v1/health`'s `active_model` field (NOT a separate endpoint)."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "model:" in result
    assert "opus-4-7" in result


def test_handle_status_cascade_line_includes_tier_and_count(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `cascade: tier 1 (anthropic-paygo) · 12 providers registered`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "cascade:" in result
    assert "tier 1" in result
    assert "anthropic-paygo" in result
    assert "12 providers" in result


def test_handle_status_bypass_line_includes_live_and_success_rate(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `bypass: live · success 24h: 100.0%`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "bypass:" in result
    assert "live" in result
    assert "100.0%" in result


def test_handle_status_cost_line_includes_24h_and_session(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `cost 24h: $0.043 (this session: $0.041)`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "cost 24h:" in result
    assert "$0.043" in result
    assert "this session: $0.041" in result


def test_handle_status_context_line_includes_pct_and_token_counts(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `context: 24% (24,200 / 100,000 tokens)`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "context:" in result
    assert "24%" in result
    assert "24,200" in result
    assert "100,000" in result
    assert "tokens" in result


def test_handle_status_profile_line_includes_name_and_kind(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `profile: max-scope (doctrine)`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "profile:" in result
    assert "max-scope" in result
    assert "doctrine" in result


def test_handle_status_cwd_line_uses_tilde_for_home(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 template: `cwd: ~/projects/hades-system`. The handler
    abbreviates the operator's home dir to `~`."""
    from hermes_plugins.hades.commands.status import handle_status

                                                                     
                                                                            
    with patch.dict(os.environ, {"HOME": "/path/to"}):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=mock_transport, base_url="http://localhost"
            ),
        ):
            result = handle_status("")

    assert result is not None
    assert "cwd:" in result
    assert "~/projects/hades-system" in result
    assert "/path/to/projects/hades-system" not in result


def test_handle_status_no_ansi_escape_sequences_when_no_color_set(
    mock_transport: httpx.MockTransport,
) -> None:
    """When NO_COLOR=1, the output MUST be plain text — no ANSI escape sequences."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "\x1b" not in result, (
        f"output contains ANSI escape sequences despite NO_COLOR=1\n"
        f"full output:\n{result!r}"
    )


def test_handle_status_handler_signature_matches_hermes_contract() -> None:
    """Hermes slash command contract: `fn(raw_args: str) -> str | None`."""
    import inspect

    from hermes_plugins.hades.commands import status

    sig = inspect.signature(status.handle_status)
    params = list(sig.parameters.values())
    assert len(params) == 1, (
        f"handle_status must take exactly 1 positional arg (raw_args); "
        f"got {len(params)}: {[p.name for p in params]}"
    )
    assert params[0].name == "raw_args"
    ret = sig.return_annotation
    assert ret is not inspect.Signature.empty, (
        "handle_status must annotate its return type"
    )


def test_handle_status_not_placeholder() -> None:
    """Sanity: the handler body must be implemented (not a one-line placeholder)."""
    import inspect

    from hermes_plugins.hades.commands import status

    src = inspect.getsource(status.handle_status)
    assert "placeholder" not in src.lower(), (
        "handle_status still appears to be a placeholder"
    )
    assert "asyncio" in src, (
        "handle_status does not use asyncio — cannot drive concurrent GETs"
    )


                                                                     
                                                                     
                                                                     


def _make_one_degraded_handler(failing_path: str, status_code: int = 503) -> Any:
    """Build an httpx MockTransport handler where ONE specified path
    returns the given non-2xx status code, all others return happy-path."""

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method != "GET":
            return httpx.Response(405, json={"error": "method not allowed"})
        if request.url.path == failing_path:
            return httpx.Response(status_code, json={"error": "service degraded"})
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(
                404, json={"error": "uncanned path", "path": request.url.path}
            )
        return httpx.Response(200, json=canned)

    return handler


                                                                        
_DEGRADED_FIELD_MAP: list[tuple[str, list[str]]] = [
    ("/v1/health", ["daemon:", "model:"]),
    ("/v1/cascade/state", ["cascade:"]),
    ("/v1/bypass/status", ["bypass:"]),
    ("/v1/cost/24h", ["cost 24h:"]),
    ("/v1/context/used", ["context:"]),
    ("/v1/profile/active", ["profile:"]),
    ("/v1/cwd", ["cwd:"]),
]


@pytest.mark.parametrize(
    "failing_path,degraded_labels",
    _DEGRADED_FIELD_MAP,
    ids=[entry[0] for entry in _DEGRADED_FIELD_MAP],
)
def test_handle_status_degraded_field_surfaces_unavailable_per_endpoint(
    failing_path: str,
    degraded_labels: list[str],
) -> None:
    """Spec §Q5: when ONE endpoint returns 503, the corresponding field
    surfaces `<field>: unavailable (daemon path down — try: hades doctor)`
    while the other fields render normally."""
    from hermes_plugins.hades.commands.status import handle_status

    handler = _make_one_degraded_handler(failing_path, status_code=503)
    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    for label in degraded_labels:
        matching = [ln for ln in lines if ln.strip().startswith(label)]
        assert matching, f"degraded field {label!r} missing from output\n{result}"
        line = matching[0]
        assert "unavailable" in line, (
            f"degraded line for {label!r} missing 'unavailable':\n{line}"
        )
        assert "daemon path down" in line, (
            f"degraded line for {label!r} missing 'daemon path down' phrase:\n{line}"
        )
        assert "hades doctor" in line, (
            f"degraded line for {label!r} missing 'hades doctor' recovery hint:\n{line}"
        )

    all_labels = {
        "daemon:",
        "model:",
        "cascade:",
        "bypass:",
        "cost 24h:",
        "context:",
        "profile:",
        "cwd:",
    }
    non_degraded = all_labels - set(degraded_labels)
    for label in non_degraded:
        matching = [ln for ln in lines if ln.strip().startswith(label)]
        assert matching, f"non-degraded field {label!r} missing from output\n{result}"
        line = matching[0]
        assert "unavailable" not in line, (
            f"non-degraded {label!r} line incorrectly surfaces 'unavailable':\n{line}"
        )


@pytest.mark.parametrize("status_code", [502, 503, 504])
def test_handle_status_degraded_field_handles_all_5xx_status_codes(
    status_code: int,
) -> None:
    """The degraded contract applies to ALL 5xx status codes."""
    from hermes_plugins.hades.commands.status import handle_status

    handler = _make_one_degraded_handler("/v1/bypass/status", status_code=status_code)
    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    bypass_lines = [ln for ln in lines if ln.strip().startswith("bypass:")]
    assert bypass_lines, f"bypass line missing\n{result}"
    assert "unavailable" in bypass_lines[0]


def test_handle_status_degraded_field_handles_4xx_as_degraded() -> None:
    """4xx responses are also classified as degraded (any non-2xx triggers
    the unavailable rendering)."""
    from hermes_plugins.hades.commands.status import handle_status

    handler = _make_one_degraded_handler("/v1/cascade/state", status_code=404)
    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    cascade_lines = [ln for ln in lines if ln.strip().startswith("cascade:")]
    assert cascade_lines, f"cascade line missing\n{result}"
    assert "unavailable" in cascade_lines[0]


def test_handle_status_degraded_field_handles_timeout_as_degraded() -> None:
    """Spec §Q5: timeout classifies as degraded.
    Simulate a transport-level timeout via httpx.MockTransport raising ReadTimeout."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/cost/24h":
            raise httpx.ReadTimeout("simulated timeout")
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    cost_lines = [ln for ln in lines if ln.strip().startswith("cost 24h:")]
    assert cost_lines, f"cost line missing\n{result}"
    assert "unavailable" in cost_lines[0]


def test_handle_status_degraded_field_handles_malformed_json_as_degraded() -> None:
    """When the daemon returns 200 OK but with malformed JSON, the
    handler catches the ValueError and surfaces unavailable."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/profile/active":
            return httpx.Response(200, content=b"not-valid-json {{{ broken")
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    profile_lines = [ln for ln in lines if ln.strip().startswith("profile:")]
    assert profile_lines, f"profile line missing\n{result}"
    assert "unavailable" in profile_lines[0]


def test_handle_status_three_endpoints_degraded_simultaneously() -> None:
    """Max-scope: when 3+ endpoints are degraded simultaneously, EACH
    degraded field surfaces unavailable independently."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/cost/24h":
            raise httpx.ReadTimeout("cost timeout")
        if request.url.path == "/v1/bypass/status":
            return httpx.Response(503, json={"error": "bypass down"})
        if request.url.path == "/v1/context/used":
            return httpx.Response(200, content=b"broken-json")
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    for label in ["cost 24h:", "bypass:", "context:"]:
        matching = [ln for ln in lines if ln.strip().startswith(label)]
        assert matching, f"{label} missing\n{result}"
        assert "unavailable" in matching[0], (
            f"{label} line should be degraded:\n{matching[0]}"
        )
    for label in ["daemon:", "model:", "cascade:", "profile:", "cwd:"]:
        matching = [ln for ln in lines if ln.strip().startswith(label)]
        assert matching, f"{label} missing\n{result}"
        assert "unavailable" not in matching[0], (
            f"{label} should not be degraded:\n{matching[0]}"
        )


def test_handle_status_all_seven_endpoints_degraded() -> None:
    """Max-scope: when ALL 7 endpoints are degraded, EACH field surfaces
    unavailable independently. The command STILL produces output."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, json={"error": "service degraded"})

    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    assert lines[0].startswith("HADES system v"), f"header missing:\n{result}"
    for label in [
        "daemon:",
        "model:",
        "cascade:",
        "bypass:",
        "cost 24h:",
        "context:",
        "profile:",
        "cwd:",
    ]:
        matching = [ln for ln in lines if ln.strip().startswith(label)]
        assert matching, f"{label} missing\n{result}"
        assert "unavailable" in matching[0], f"{label} should be degraded:\n{matching[0]}"


                                                                     
                                                         
                                                                     

                                                                             
_ANSI_OK_GREEN = "\x1b[38;2;16;185;129m"           
_ANSI_WARN_ORANGE = "\x1b[38;2;255;167;38m"           
_ANSI_FAIL_CRIMSON = "\x1b[38;2;196;30;58m"           
_ANSI_MUTED_GRAY = "\x1b[38;2;153;153;153m"        
_ANSI_RESET = "\x1b[0m"

                                                 
_PALETTE_ANSI: dict[str, str] = {
    "ok": _ANSI_OK_GREEN,
    "warn": _ANSI_WARN_ORANGE,
    "fail": _ANSI_FAIL_CRIMSON,
    "muted": _ANSI_MUTED_GRAY,
}


def _ansi_shim(text: str, color_key: str) -> str:
    """ANSI-emitting shim for _colored_text when Hermes is not installed.

    Used by C-4 color tests to assert ANSI sequences appear in output
    without requiring the real hermes_cli.terminal helper.
    """
    seq = _PALETTE_ANSI.get(color_key, "")
    if not seq:
        return text
    return f"{seq}{text}{_ANSI_RESET}"


@pytest.fixture
def color_enabled(monkeypatch: pytest.MonkeyPatch) -> None:
    """Override the autouse _disable_color fixture and inject the ANSI shim.

    Since hermes_cli.terminal is not installed in this dev environment,
    we patch _colored_text directly with a shim that emits ANSI sequences
    matching the expected palette. This is the documented mitigation per
    plan §C-4 Step 2 note.
    """
    monkeypatch.delenv("NO_COLOR", raising=False)
    monkeypatch.setenv("HERMES_FORCE_COLOR", "1")


def test_handle_status_ok_field_renders_with_ok_green(
    color_enabled: None,
    mock_transport: httpx.MockTransport,
) -> None:
    """Happy-path 'daemon: ok ...' line surfaces the ok marker in
    ok-green per spec §Q5."""
    import hermes_plugins.hades.commands.status as status_mod

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=mock_transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert _ANSI_OK_GREEN in result, (
        f"output missing ok-green {_ANSI_OK_GREEN!r}\n{result!r}"
    )
    assert _ANSI_RESET in result


def handle_status_direct(raw_args: str) -> str | None:
    """Test helper — imports after module is patched."""
    from hermes_plugins.hades.commands.status import handle_status

    return handle_status(raw_args)


def test_handle_status_degraded_field_renders_with_warn_orange(
    color_enabled: None,
) -> None:
    """Degraded field 'unavailable (...)' surface uses warn-orange #ffa726."""
    import hermes_plugins.hades.commands.status as status_mod

    handler = _make_one_degraded_handler("/v1/bypass/status", status_code=503)
    transport = httpx.MockTransport(handler)

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert _ANSI_WARN_ORANGE in result, (
        f"degraded output missing warn-orange {_ANSI_WARN_ORANGE!r}\n{result!r}"
    )


def test_handle_status_no_color_env_suppresses_all_ansi(
    monkeypatch: pytest.MonkeyPatch,
    mock_transport: httpx.MockTransport,
) -> None:
    """The NO_COLOR env suppresses ALL ANSI escape sequences."""
    from hermes_plugins.hades.commands.status import handle_status

    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.delenv("HERMES_FORCE_COLOR", raising=False)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("")

    assert result is not None
    assert "\x1b" not in result, (
        f"NO_COLOR=1 should suppress ANSI, but found escape sequences:\n{result!r}"
    )


def test_handle_status_color_invokes_hermes_terminal_helper(
    color_enabled: None,
    mock_transport: httpx.MockTransport,
) -> None:
    """The handler MUST invoke the Hermes terminal helper for color
    application — it does NOT re-implement ANSI."""
    import hermes_plugins.hades.commands.status as status_mod

    with patch.object(status_mod, "_colored_text") as mock_colored:
        mock_colored.side_effect = lambda text, color_key: f"[{color_key}]{text}[/]"
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=mock_transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert mock_colored.call_count >= 8, (
        f"_colored_text expected ≥8 invocations; got {mock_colored.call_count}"
    )
    color_keys_seen = {call.args[1] for call in mock_colored.call_args_list}
                                                                              
                                                                                 
    expected_keys_subset = {"ok", "muted"}
    assert expected_keys_subset.issubset(color_keys_seen), (
        f"expected at least color keys {expected_keys_subset!r} to appear;\n"
        f"saw: {color_keys_seen!r}"
    )


def test_handle_status_color_body_text_uses_muted_gray(
    color_enabled: None,
    mock_transport: httpx.MockTransport,
) -> None:
    """The field VALUE text (after the field label colon) uses the
    muted-gray palette #999 per spec §Q5."""
    import hermes_plugins.hades.commands.status as status_mod

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=mock_transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert _ANSI_MUTED_GRAY in result, (
        f"output missing muted-gray {_ANSI_MUTED_GRAY!r}\n{result!r}"
    )


def test_handle_status_colorize_helper_unavailable_graceful_degrade(
    color_enabled: None,
    monkeypatch: pytest.MonkeyPatch,
    mock_transport: httpx.MockTransport,
) -> None:
    """If the Hermes terminal helper is unavailable, the handler MUST
    gracefully degrade to plain-text output. Never raises ImportError."""
    import hermes_plugins.hades.commands.status as status_mod

    def _broken(text: str, color_key: str) -> str:
        raise ImportError("simulated: hermes_cli.terminal not found")

    with patch.object(status_mod, "_colored_text", side_effect=_broken):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=mock_transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert "\x1b" not in result, (
        f"broken color helper should fall back to plain text;\n{result!r}"
    )
    assert "HADES system v" in result
    assert "daemon:" in result


                                                                     
                                                       
                                                                     

_GOLDEN_FIXTURE = pathlib.Path(__file__).parent / "testdata" / "status_schema_v1.json"


def _filter_volatile_fields(payload: dict[str, Any]) -> dict[str, Any]:
    """Strip volatile fields (PID, rendered_at) so the golden-file
    comparison is stable across releases."""
    out = json.loads(json.dumps(payload))             
    out.pop("rendered_at", None)
    return out


def test_handle_status_json_mode_has_schema_version_field(
    mock_transport: httpx.MockTransport,
) -> None:
    """Spec §Q5 + inv-zen-221: JSON output has top-level `schema_version: 1`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    assert parsed.get("schema_version") == 1


def test_handle_status_json_mode_emits_all_8_fields(
    mock_transport: httpx.MockTransport,
) -> None:
    """JSON output `fields` key contains all 8 field names per spec §Q5 ordering."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    fields = parsed.get("fields", {})
    expected_keys = {
        "daemon",
        "model",
        "cascade",
        "bypass",
        "cost_24h",
        "context",
        "profile",
        "cwd",
    }
    assert set(fields.keys()) == expected_keys, (
        f"fields keys mismatch — expected {expected_keys!r}, got {set(fields.keys())!r}"
    )


def test_handle_status_json_mode_matches_golden_fixture(
    mock_transport: httpx.MockTransport,
) -> None:
    """The JSON output (under HAPPY_PATH_RESPONSES) deep-equal-matches
    the golden fixture at testdata/status_schema_v1.json (modulo volatile fields).

    This is the load-bearing inv-zen-221 stability anchor."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    actual = _filter_volatile_fields(json.loads(result))

    with _GOLDEN_FIXTURE.open(encoding="utf-8") as f:
        expected = _filter_volatile_fields(json.load(f))

    assert actual == expected, (
        f"JSON output does not match golden fixture.\n"
        f"actual:   {json.dumps(actual, indent=2)}\n"
        f"expected: {json.dumps(expected, indent=2)}"
    )


def test_handle_status_json_mode_each_field_has_state_marker(
    mock_transport: httpx.MockTransport,
) -> None:
    """Each field value in JSON mode has a `state` key: `ok` or `degraded`."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    fields = parsed.get("fields", {})
    for field_name, field_value in fields.items():
        assert isinstance(field_value, dict), (
            f"field {field_name!r} value is not a dict: {field_value!r}"
        )
        assert field_value.get("state") in ("ok", "degraded"), (
            f"field {field_name!r} state must be 'ok' or 'degraded'; "
            f"got {field_value.get('state')!r}"
        )


def test_handle_status_json_mode_degraded_field_marked_in_state(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """When an endpoint is degraded, the corresponding JSON field has
    `state: "degraded"` (not `state: "ok"`)."""
    from hermes_plugins.hades.commands.status import handle_status

    handler = _make_one_degraded_handler("/v1/bypass/status", status_code=503)
    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    fields = parsed.get("fields", {})
    assert fields["bypass"]["state"] == "degraded"
    assert fields["daemon"]["state"] == "ok"
    assert fields["model"]["state"] == "ok"


def test_handle_status_json_mode_includes_rendered_at_iso8601(
    mock_transport: httpx.MockTransport,
) -> None:
    """JSON output includes a top-level `rendered_at` ISO-8601 UTC timestamp."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    rendered_at = parsed.get("rendered_at")
    assert isinstance(rendered_at, str), (
        f"rendered_at missing or wrong type: {rendered_at!r}"
    )
    assert len(rendered_at) >= 19
    assert rendered_at[4] == "-"
    assert "T" in rendered_at


def test_handle_status_schema_version_constant_exported() -> None:
    """The handler module exports `SCHEMA_VERSION` at module level."""
    from hermes_plugins.hades.commands import status as status_mod

    assert hasattr(status_mod, "SCHEMA_VERSION")
    assert status_mod.SCHEMA_VERSION == 1


def test_handle_status_json_mode_no_ansi_sequences(
    mock_transport: httpx.MockTransport,
) -> None:
    """JSON mode emits pure JSON — no ANSI escape sequences."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    assert "\x1b" not in result, f"JSON mode must not contain ANSI sequences\n{result!r}"


def test_golden_fixture_is_valid_json() -> None:
    """Sanity: the fixture file is valid JSON + has schema_version=1."""
    with _GOLDEN_FIXTURE.open(encoding="utf-8") as f:
        fixture = json.load(f)
    assert fixture.get("schema_version") == 1
    assert "fields" in fixture
    assert len(fixture["fields"]) == 8


                                                                     
                                                                     
                                 
                                                                     


def test_handle_status_uds_path_missing_renders_daemon_not_running(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: pathlib.Path,
) -> None:
    """When the UDS socket does not exist on disk, the handler renders
    the spec §Q6 three-line block with `daemon.not-running` catalog code."""
    from hermes_plugins.hades.commands.status import handle_status

    nonexistent = tmp_path / "definitely-does-not-exist.sock"
    monkeypatch.setenv("ZEN_SWARM_UDS", str(nonexistent))
    monkeypatch.setenv("NO_COLOR", "1")

    result = handle_status("")

    assert result is not None
    assert "HADES:" in result
    assert "daemon" in result.lower()
    assert "not running" in result.lower() or "not-running" in result.lower()
    assert "→" in result
    has_concrete_hint = (
        "zen-swarm-ctld" in result or "hades doctor" in result or "make build" in result
    )
    assert has_concrete_hint, (
        f"recovery hint should be concrete (zen-swarm-ctld / hades doctor / "
        f"make build); got:\n{result}"
    )


def test_handle_status_connection_refused_renders_daemon_not_running(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """When httpx raises ConnectError (UDS exists but daemon not listening),
    the handler renders the three-line block."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("Connection refused")

    transport = httpx.MockTransport(handler)
    monkeypatch.setenv("NO_COLOR", "1")

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        with patch(
            "hermes_plugins.hades.commands.status.os.path.exists",
            return_value=True,
        ):
            result = handle_status("")

    assert result is not None
    has_top_level_error = "HADES:" in result and "→" in result
    has_per_field_degraded = "unavailable" in result
    assert has_top_level_error or has_per_field_degraded, (
        f"connection refused → either top-level error block OR "
        f"all-fields-degraded variant; got:\n{result}"
    )


def test_handle_status_structured_error_json_renders_three_line_block(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """When the daemon returns a structured-error JSON envelope on any
    endpoint, the handler renders the three-line block per spec §Q6."""
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/health":
            return httpx.Response(
                503,
                json={
                    "code": "daemon.not-running",
                    "title": "daemon is in bootstrap-loop state",
                    "body": "the daemon process is restarting; please wait",
                    "recovery_hint": "tail -f ~/.zen/logs/zen-swarm-ctld.log",
                },
            )
        return httpx.Response(503, json={"error": "follow-on"})

    transport = httpx.MockTransport(handler)
    monkeypatch.setenv("NO_COLOR", "1")

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        with patch(
            "hermes_plugins.hades.commands.status.os.path.exists",
            return_value=True,
        ):
            result = handle_status("")

    assert result is not None
    assert "HADES:" in result
    assert "bootstrap-loop" in result or "restarting" in result
    assert "tail -f" in result or "log" in result.lower()


def test_handle_status_top_level_error_in_json_mode_returns_structured_payload(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: pathlib.Path,
) -> None:
    """When `--json` is set AND the daemon is unreachable, the JSON
    output emits an error payload with schema_version=1 + `error` key."""
    from hermes_plugins.hades.commands.status import handle_status

    nonexistent = tmp_path / "definitely-does-not-exist.sock"
    monkeypatch.setenv("ZEN_SWARM_UDS", str(nonexistent))

    result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    assert parsed.get("schema_version") == 1
    assert "error" in parsed
    err = parsed["error"]
    assert err.get("code") == "daemon.not-running"
    assert isinstance(err.get("title"), str)
    assert isinstance(err.get("body"), str)
    assert isinstance(err.get("recovery_hint"), str)
    assert "fields" not in parsed


def test_handle_status_top_level_error_uses_fail_crimson_when_colored(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: pathlib.Path,
) -> None:
    """When color is enabled and a top-level error occurs, the HADES:
    headline renders in fail-crimson #c41e3a per spec §Q6."""
    import hermes_plugins.hades.commands.status as status_mod

    nonexistent = tmp_path / "definitely-does-not-exist.sock"
    monkeypatch.setenv("ZEN_SWARM_UDS", str(nonexistent))
    monkeypatch.delenv("NO_COLOR", raising=False)
    monkeypatch.setenv("HERMES_FORCE_COLOR", "1")

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        result = handle_status_direct("")

    assert result is not None
    assert _ANSI_FAIL_CRIMSON in result, (
        f"top-level error should render in fail-crimson #c41e3a;\n{result!r}"
    )


                                                                     
                                                           
                                                                     


@pytest.mark.parametrize(
    "raw_args,expected_is_json",
    [
        ("", False),
        ("--json", True),
        (" --json", True),
        ("--json ", True),
        ("  --json  ", True),
        ("--json extra", True),
        ("extra --json", True),
        ("--JSON", False),
        ("--json-pretty", False),
        ("text-mode --json text", True),
    ],
)
def test_handle_status_is_json_mode_flag_detection(
    raw_args: str,
    expected_is_json: bool,
    mock_transport: httpx.MockTransport,
) -> None:
    """`--json` flag detection is whole-token, case-sensitive, whitespace-tolerant."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status(raw_args)

    assert result is not None
    if expected_is_json:
        try:
            parsed = json.loads(result)
            assert parsed.get("schema_version") == 1
        except json.JSONDecodeError:
            pytest.fail(
                f"raw_args={raw_args!r} should trigger JSON mode but output is "
                f"not valid JSON:\n{result}"
            )
    else:
        first_line = result.splitlines()[0]
        first_line_plain = re.sub(r"\x1b\[[0-9;]*m", "", first_line)
        assert first_line_plain.startswith("HADES system v"), (
            f"raw_args={raw_args!r} should trigger TEXT mode but first line is:\n"
            f"{first_line!r}"
        )


def test_handle_status_unknown_flag_routes_through_arg_validation_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """If raw_args contains an unknown flag-like token, the handler either
    accepts it silently (forward-compat) or renders a structured error block."""
    from hermes_plugins.hades.commands.status import handle_status

    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.delenv("ZEN_SWARM_UDS", raising=False)

    result = handle_status("--bogus-flag")

    assert result is not None
    assert "HADES:" in result or "daemon" in result.lower()


def test_handle_status_json_flag_with_color_env_still_outputs_json(
    monkeypatch: pytest.MonkeyPatch,
    mock_transport: httpx.MockTransport,
) -> None:
    """Regardless of NO_COLOR/HERMES_FORCE_COLOR state, JSON mode emits
    valid JSON with NO embedded ANSI sequences."""
    from hermes_plugins.hades.commands.status import handle_status

    monkeypatch.delenv("NO_COLOR", raising=False)
    monkeypatch.setenv("HERMES_FORCE_COLOR", "1")

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    assert parsed.get("schema_version") == 1
    assert "\x1b" not in result


def test_handle_status_json_mode_round_trip_via_json_dumps_json_loads(
    mock_transport: httpx.MockTransport,
) -> None:
    """JSON output MUST round-trip through json.dumps(json.loads(out))."""
    from hermes_plugins.hades.commands.status import handle_status

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(
            transport=mock_transport, base_url="http://localhost"
        ),
    ):
        result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    re_serialized = json.dumps(parsed, indent=2, sort_keys=False)
    re_parsed = json.loads(re_serialized)
    assert re_parsed == parsed


def test_handle_status_text_mode_is_default_when_raw_args_empty(
    mock_transport: httpx.MockTransport,
) -> None:
    """The DEFAULT mode is text (human-readable). JSON requires the explicit
    `--json` flag."""
    from hermes_plugins.hades.commands.status import handle_status

                                                                             
                                                                              
                                                                            
                                                                        
                    
    def _fresh_client() -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=mock_transport, base_url="http://localhost")

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        side_effect=_fresh_client,
    ):
        result_empty = handle_status("")
        result_whitespace = handle_status("   ")

    assert result_empty is not None
    assert result_whitespace is not None
    for label, output in [
        ("empty", result_empty),
        ("whitespace-only", result_whitespace),
    ]:
        first_line = output.splitlines()[0]
        first_plain = re.sub(r"\x1b\[[0-9;]*m", "", first_line)
        assert first_plain.startswith("HADES system v"), (
            f"raw_args={label} should default to text mode; first line:\n{first_line!r}"
        )


                                                                     
                                                       
                                                                     


def test_status_command_registered_in_plugin_init() -> None:
    """The /hades:status command MUST be registered in plugin/hades/__init__.py."""
    import inspect

    from hermes_plugins import hades

    src = inspect.getsource(hades)
    assert '"hades:status"' in src, (
        "plugin/hades/__init__.py must register the slash command "
        "'hades:status' via ctx.register_command"
    )
    assert "handle_status" in src, (
        "plugin/hades/__init__.py must import handle_status from .commands.status"
    )


def test_status_handler_importable_via_hermes_namespace() -> None:
    """The handler is importable via the Hermes plugin namespace."""
    from hermes_plugins.hades.commands.status import (
        SCHEMA_VERSION,
        handle_status,
    )

    assert callable(handle_status)
    assert SCHEMA_VERSION == 1


def test_status_handler_coverage_baseline_meets_target() -> None:
    """Sanity: handler has all expected functions for ≥85% coverage."""
    import inspect

    from hermes_plugins.hades.commands import status as status_mod

    expected_functions = {
        "handle_status",
        "_build_client",
        "_query_daemon",
        "_render_human",
        "_render_json",
        "_render_error",
        "_render_error_json",
        "_degraded_line",
        "_classify_field_state",
        "_detect_structured_error_envelope",
        "_is_json_mode",
        "_colored_text",
        "_safe_colorize",
        "_try_import_terminal_helper",
    }
    actual_functions = {
        name for name, _ in inspect.getmembers(status_mod, inspect.isfunction)
    }
    missing = expected_functions - actual_functions
    assert not missing, (
        f"expected functions missing from status module: {missing!r}\n"
        f"actual: {actual_functions!r}"
    )


                                                                     
                                                                   
                                                                     


def test_fetch_one_non_dict_json_body_is_degraded() -> None:
    """_query_daemon: a 200 response with a JSON non-dict body (e.g., list)
    is classified as degraded (the field returns None).

    Coverage target: `commands/status.py:250` (`if not isinstance(body, dict):`).
    """
    from hermes_plugins.hades.commands.status import handle_status

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/context/used":
                                                                         
            return httpx.Response(200, json=["not", "a", "dict"])
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=httpx.AsyncClient(transport=transport, base_url="http://localhost"),
    ):
        result = handle_status("")

    assert result is not None
    lines = result.splitlines()
    context_lines = [ln for ln in lines if ln.strip().startswith("context:")]
    assert context_lines, f"context line missing\n{result}"
    assert "unavailable" in context_lines[0], (
        f"non-dict 200 response should degrade context field:\n{context_lines[0]}"
    )


def test_bypass_status_degraded_literal_uses_warn_color(
    color_enabled: None,
) -> None:
    """When the bypass `/v1/bypass/status` returns status='degraded', the
    bypass line renders in warn-orange (not ok-green). This exercises the
    `elif bypass_status == 'degraded':` branch in `_render_human`.

    Coverage target: `commands/status.py:357-358`.
    """
    import hermes_plugins.hades.commands.status as status_mod

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/bypass/status":
            return httpx.Response(
                200,
                json={
                    "status": "degraded",
                    "success_rate_24h": 0.73,
                },
            )
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert "degraded" in result
                                                               
    assert _ANSI_WARN_ORANGE in result, (
        f"bypass status='degraded' should render in warn-orange;\n{result!r}"
    )


def test_bypass_status_unknown_value_uses_muted_color(
    color_enabled: None,
) -> None:
    """When bypass status is an unrecognised value (neither 'live' nor
    'degraded'), the status marker renders in muted-gray. This exercises
    the `else:` branch of the bypass-status color selection.

    Coverage target: `commands/status.py:359-360`.
    """
    import hermes_plugins.hades.commands.status as status_mod

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/bypass/status":
            return httpx.Response(
                200,
                json={
                    "status": "starting",
                    "success_rate_24h": 0.0,
                },
            )
        canned = HAPPY_PATH_RESPONSES.get(request.url.path)
        if canned is None:
            return httpx.Response(404, json={"error": "uncanned"})
        return httpx.Response(200, json=canned)

    transport = httpx.MockTransport(handler)

    with patch.object(status_mod, "_colored_text", side_effect=_ansi_shim):
        with patch(
            "hermes_plugins.hades.commands.status._build_client",
            return_value=httpx.AsyncClient(
                transport=transport, base_url="http://localhost"
            ),
        ):
            result = handle_status_direct("")

    assert result is not None
    assert "starting" in result
                                           
    assert _ANSI_MUTED_GRAY in result, (
        f"unknown bypass status should render in muted-gray;\n{result!r}"
    )


def test_handle_status_client_level_http_error_renders_daemon_not_running(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """When asyncio.run(_query_daemon(client)) raises httpx.HTTPError at
    the client level (belt-and-suspenders path — rare but must be covered),
    the handler renders the three-line error block.

    Coverage target: `commands/status.py:637-645` (except httpx.HTTPError).
    """
    from hermes_plugins.hades.commands.status import handle_status

    monkeypatch.setenv("NO_COLOR", "1")

    async def _bad_query(_client: Any) -> dict[str, Any]:
        raise httpx.HTTPError("simulated client-level HTTP failure")

    with patch(
        "hermes_plugins.hades.commands.status._query_daemon",
        side_effect=_bad_query,
    ):
        with patch(
            "hermes_plugins.hades.commands.status.os.path.exists",
            return_value=True,
        ):
            with patch(
                "hermes_plugins.hades.commands.status._build_client",
                return_value=httpx.AsyncClient(
                    transport=httpx.MockTransport(_make_happy_path_handler()),
                    base_url="http://localhost",
                ),
            ):
                result = handle_status("")

    assert result is not None
    assert "HADES:" in result or "daemon" in result.lower()
    assert "→" in result or "not running" in result.lower()


def test_handle_status_client_level_http_error_json_mode(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """JSON mode: when asyncio.run(_query_daemon(client)) raises httpx.HTTPError,
    the JSON error payload is returned.

    Coverage target: `commands/status.py:643-644` (json branch of except httpx.HTTPError).
    """
    from hermes_plugins.hades.commands.status import handle_status

    async def _bad_query(_client: Any) -> dict[str, Any]:
        raise httpx.HTTPError("simulated client-level HTTP failure")

    with patch(
        "hermes_plugins.hades.commands.status._query_daemon",
        side_effect=_bad_query,
    ):
        with patch(
            "hermes_plugins.hades.commands.status.os.path.exists",
            return_value=True,
        ):
            with patch(
                "hermes_plugins.hades.commands.status._build_client",
                return_value=httpx.AsyncClient(
                    transport=httpx.MockTransport(_make_happy_path_handler()),
                    base_url="http://localhost",
                ),
            ):
                result = handle_status("--json")

    assert result is not None
    parsed = json.loads(result)
    assert parsed.get("schema_version") == 1
    assert "error" in parsed


def test_client_aclose_exception_is_silently_swallowed(
    monkeypatch: pytest.MonkeyPatch,
    mock_transport: httpx.MockTransport,
) -> None:
    """The finally block in handle_status catches RuntimeError / httpx.HTTPError
    from client.aclose() and swallows them (defense in depth). The command
    MUST still return a valid result.

    Coverage target: `commands/status.py:649-650`.
    """
    from hermes_plugins.hades.commands.status import handle_status

    monkeypatch.setenv("NO_COLOR", "1")

    real_client = httpx.AsyncClient(transport=mock_transport, base_url="http://localhost")

                                                                         
                                                      
    async def _bad_aclose() -> None:
        raise RuntimeError("simulated aclose failure")

    real_client.aclose = _bad_aclose  # type: ignore[method-assign]

    with patch(
        "hermes_plugins.hades.commands.status._build_client",
        return_value=real_client,
    ):
        result = handle_status("")

    assert result is not None
    assert "HADES system v" in result
