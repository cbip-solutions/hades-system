# SPDX-License-Identifier: MIT
"""Tests for hades.commands.status_core — shared fetch+classify logic."""

from __future__ import annotations

import asyncio

import httpx

from hades.commands import status_core


def test_query_daemon_fans_out_all_endpoints():
    """status_core.query_daemon returns one entry per endpoint, parsing 2xx JSON."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"ok": True, "path": request.url.path})

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="http://localhost"
    )
    results = asyncio.run(status_core.query_daemon(client))
    assert set(results.keys()) == set(status_core.ENDPOINTS)
    assert all(v is not None for v in results.values())


def test_classify_field_state():
    assert status_core.classify_field_state({"x": 1}) == "ok"
    assert status_core.classify_field_state(None) == "degraded"


def test_detect_structured_error_envelope_found():
    """Returns envelope when any response has all four required keys."""
    responses = {
        "/v1/health": {
            "code": "daemon.not-running",
            "title": "not running",
            "body": "the daemon is down",
            "recovery_hint": "start it",
        },
        "/v1/cascade/state": None,
    }
    envelope = status_core.detect_structured_error_envelope(responses)
    assert envelope is not None
    assert envelope["code"] == "daemon.not-running"
    assert envelope["title"] == "not running"


def test_detect_structured_error_envelope_not_found():
    """Returns None when no response carries the four-key envelope."""
    responses = {
        "/v1/health": {"status": "ok", "version": "0.17.0"},
        "/v1/cascade/state": None,
    }
    assert status_core.detect_structured_error_envelope(responses) is None


def test_build_client_returns_async_client(monkeypatch):
    """build_client() returns an httpx.AsyncClient (UDS transport wired)."""
                                                                    
    monkeypatch.setenv("ZEN_SWARM_UDS", "/tmp/does-not-exist-status-core-test.sock")
    client = status_core.build_client()
    assert isinstance(client, httpx.AsyncClient)


def test_endpoints_tuple_has_seven_entries():
    """ENDPOINTS must list exactly the 7 daemon paths."""
    assert len(status_core.ENDPOINTS) == 7
    assert "/v1/health" in status_core.ENDPOINTS
    assert "/v1/cwd" in status_core.ENDPOINTS


def test_query_daemon_degraded_on_non_2xx():
    """Non-2xx responses without envelope shape → None (degraded)."""

    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/health":
            return httpx.Response(503, json={"error": "down"})
        return httpx.Response(200, json={"ok": True})

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="http://localhost"
    )
    results = asyncio.run(status_core.query_daemon(client))
    assert results["/v1/health"] is None
                                     
    assert results["/v1/cwd"] is not None


def test_query_daemon_returns_envelope_on_structured_error():
    """Non-2xx response WITH four-key envelope shape → body returned (not None)."""
    envelope_body = {
        "code": "daemon.not-running",
        "title": "t",
        "body": "b",
        "recovery_hint": "r",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, json=envelope_body)

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="http://localhost"
    )
    results = asyncio.run(status_core.query_daemon(client))
                                                                                   
    assert results["/v1/health"] is not None
    assert results["/v1/health"]["code"] == "daemon.not-running"


def test_public_api_surface():
    """All expected public names are present on the status_core module."""
    expected = {
        "DEFAULT_UDS_PATH",
        "ENDPOINTS",
        "ENDPOINT_TIMEOUT_S",
        "build_client",
        "query_daemon",
        "classify_field_state",
        "detect_structured_error_envelope",
    }
    import inspect

    actual = {name for name, _ in inspect.getmembers(status_core)}
    missing = expected - actual
    assert not missing, f"status_core is missing public names: {missing!r}"
