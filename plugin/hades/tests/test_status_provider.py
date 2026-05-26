# SPDX-License-Identifier: MIT
"""Tests for hades.commands.status_provider — dormant status-bar provider."""

from __future__ import annotations

from hades.commands import status_provider


def test_status_segments_compact_ok():
    """Happy-path: all 7 endpoints respond → compact labelled segments
    include daemon✓, tier marker, $cost, and context-pct."""
    responses = {
        "/v1/health": {
            "version": "1.0.0",
            "pid": 1,
            "uds_path": "/tmp/x.sock",
            "active_model": "opus",
        },
        "/v1/cascade/state": {
            "active_tier": 1,
            "tier_name": "anthropic-paygo",
            "provider_count": 12,
        },
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 1.0},
        "/v1/cost/24h": {"spend_24h_usd": 0.04, "spend_session_usd": 0.04},
        "/v1/context/used": {"used_tokens": 24000, "max_tokens": 100000},
        "/v1/profile/active": {"profile_name": "max-scope", "kind": "doctrine"},
        "/v1/cwd": {"cwd": "/x"},
    }
    segs = status_provider.segments_from_responses(responses)
    text = " · ".join(segs)
    assert (
        "daemon" in text and "tier" in text.lower() and "$0.04" in text and "24%" in text
    )


def test_status_segments_degraded_daemon():
    """All endpoints return None → degraded marker present, no exception raised."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    segs = status_provider.segments_from_responses(responses)
    assert any("daemon" in s for s in segs)                                         


def test_status_segments_returns_list():
    """segments_from_responses always returns a list (never raises)."""
    responses = {
        "/v1/health": {
            "version": "0.1.0",
            "pid": 42,
            "uds_path": "/tmp/z.sock",
            "active_model": "sonnet",
        },
        "/v1/cascade/state": {
            "active_tier": 2,
            "tier_name": "gemini",
            "provider_count": 5,
        },
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 0.99},
        "/v1/cost/24h": {"spend_24h_usd": 0.00, "spend_session_usd": 0.00},
        "/v1/context/used": {"used_tokens": 1000, "max_tokens": 200000},
        "/v1/profile/active": {"profile_name": "default", "kind": "profile"},
        "/v1/cwd": {"cwd": "/home/user"},
    }
    segs = status_provider.segments_from_responses(responses)
    assert isinstance(segs, list)
    assert len(segs) > 0


def test_status_segments_partial_degraded():
    """Some endpoints degraded → degraded fields render a marker, ok fields render normally."""
    responses = {
        "/v1/health": {
            "version": "1.0.0",
            "pid": 1,
            "uds_path": "/tmp/x.sock",
            "active_model": "opus",
        },
        "/v1/cascade/state": None,            
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 1.0},
        "/v1/cost/24h": None,            
        "/v1/context/used": {"used_tokens": 50000, "max_tokens": 100000},
        "/v1/profile/active": {"profile_name": "max-scope", "kind": "doctrine"},
        "/v1/cwd": {"cwd": "/x"},
    }
    segs = status_provider.segments_from_responses(responses)
                                         
    assert isinstance(segs, list)
    assert len(segs) > 0


def test_status_core_endpoints_accessible():
    """status_provider.status_core.ENDPOINTS is accessible (as required by
    test_status_segments_degraded_daemon which references it directly)."""
    endpoints = status_provider.status_core.ENDPOINTS
    assert isinstance(endpoints, tuple)
    assert len(endpoints) == 7
    assert "/v1/health" in endpoints


def test_status_provider_module_docstring_mentions_dormant():
    """The module docstring must state the dormant/capability-gated nature."""
    import inspect

    doc = inspect.getdoc(status_provider) or ""
    assert "dormant" in doc.lower() or "capability-gated" in doc.lower(), (
        "status_provider module docstring must document its dormant/capability-gated nature"
    )


def test_status_segments_context_pct_calculation():
    """Context percentage is computed as used/max * 100, rounded to int."""
    responses = {
        "/v1/health": {
            "version": "1.0.0",
            "pid": 1,
            "uds_path": "/tmp/x.sock",
            "active_model": "opus",
        },
        "/v1/cascade/state": {
            "active_tier": 1,
            "tier_name": "anthropic-paygo",
            "provider_count": 12,
        },
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 1.0},
        "/v1/cost/24h": {"spend_24h_usd": 0.10, "spend_session_usd": 0.05},
        "/v1/context/used": {"used_tokens": 75000, "max_tokens": 100000},
        "/v1/profile/active": {"profile_name": "max-scope", "kind": "doctrine"},
        "/v1/cwd": {"cwd": "/x"},
    }
    segs = status_provider.segments_from_responses(responses)
    text = " · ".join(segs)
    assert "75%" in text


def test_status_segments_cost_formatting():
    """Cost renders as $X.XX with two decimal places."""
    responses = {
        "/v1/health": {
            "version": "1.0.0",
            "pid": 1,
            "uds_path": "/tmp/x.sock",
            "active_model": "opus",
        },
        "/v1/cascade/state": {
            "active_tier": 1,
            "tier_name": "anthropic-paygo",
            "provider_count": 12,
        },
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 1.0},
        "/v1/cost/24h": {"spend_24h_usd": 1.234, "spend_session_usd": 0.0},
        "/v1/context/used": {"used_tokens": 1000, "max_tokens": 100000},
        "/v1/profile/active": {"profile_name": "max-scope", "kind": "doctrine"},
        "/v1/cwd": {"cwd": "/x"},
    }
    segs = status_provider.segments_from_responses(responses)
    text = " · ".join(segs)
                                                                        
    assert "$1.23" in text


                                                                             
                                                                           
                                                                             


def test_segments_from_responses_cascade_missing_active_tier():
    """cascade present but 'active_tier' key absent → 'tier?' segment."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    responses["/v1/cascade/state"] = {"tier_name": "anthropic-paygo"}                  
    segs = status_provider.segments_from_responses(responses)
    assert "tier?" in segs


def test_segments_from_responses_bypass_non_live_status():
    """bypass present but status != 'live' → 'bypass⚠' segment."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    responses["/v1/bypass/status"] = {"status": "degraded"}
    segs = status_provider.segments_from_responses(responses)
    assert "bypass⚠" in segs


def test_segments_from_responses_cost_missing_spend_key():
    """cost present but 'spend_24h_usd' key absent → '$-.--' segment."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    responses["/v1/cost/24h"] = {"spend_session_usd": 0.0}                    
    segs = status_provider.segments_from_responses(responses)
    assert "$-.--" in segs


def test_segments_from_responses_context_missing_token_keys():
    """context present but token keys absent → '?%' segment."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    responses["/v1/context/used"] = {"note": "no token data"}                             
    segs = status_provider.segments_from_responses(responses)
    assert "?%" in segs


def test_segments_from_responses_context_max_tokens_zero():
    """context present, max_tokens=0 (division guard) → '?%' segment."""
    responses = dict.fromkeys(status_provider.status_core.ENDPOINTS, None)
    responses["/v1/context/used"] = {"used_tokens": 1000, "max_tokens": 0}
    segs = status_provider.segments_from_responses(responses)
    assert "?%" in segs


                                                                             
                                                
                                                                             


def test_status_segments_happy_path(monkeypatch: object) -> None:
    """status_segments() calls build_client + query_daemon, returns segments.

    Monkeypatches status_core.build_client (sync factory returning a fake
    async client) and status_core.query_daemon (returns a known responses
    dict). Asserts the returned segments match segments_from_responses output.
    """
    import asyncio

    known_responses = {
        "/v1/health": {
            "version": "1.0.0",
            "pid": 1,
            "uds_path": "/tmp/x.sock",
            "active_model": "opus",
        },
        "/v1/cascade/state": {
            "active_tier": 2,
            "tier_name": "gemini",
            "provider_count": 5,
        },
        "/v1/bypass/status": {"status": "live", "success_rate_24h": 1.0},
        "/v1/cost/24h": {"spend_24h_usd": 0.07, "spend_session_usd": 0.0},
        "/v1/context/used": {"used_tokens": 30000, "max_tokens": 100000},
        "/v1/profile/active": {"profile_name": "max-scope", "kind": "doctrine"},
        "/v1/cwd": {"cwd": "/x"},
    }

                                                            
    class FakeClient:
        def __init__(self) -> None:
            self.closed = False

        async def aclose(self) -> None:
            self.closed = True

    fake_client = FakeClient()

                                                   
    monkeypatch.setattr(  # type: ignore[attr-defined]
        status_provider.status_core, "build_client", lambda: fake_client
    )

    async def fake_query(_client: object) -> dict:  # type: ignore[type-arg]
        return known_responses

    monkeypatch.setattr(  # type: ignore[attr-defined]
        status_provider.status_core, "query_daemon", fake_query
    )

    segs = asyncio.run(status_provider.status_segments())

                                                         
    expected = status_provider.segments_from_responses(known_responses)
    assert segs == expected

                                   
    assert fake_client.closed


def test_status_segments_query_raises_returns_degraded_never_raises(
    monkeypatch: object,
) -> None:
    """When query_daemon raises, status_segments returns degraded segments and never raises.

    This gate verifies the post-condition: "Client is closed even if query_daemon raises."
    And: "Never raises (all exceptions are suppressed)."
    """
    import asyncio

    class FakeClient:
        def __init__(self) -> None:
            self.closed = False

        async def aclose(self) -> None:
            self.closed = True

    fake_client = FakeClient()

    monkeypatch.setattr(  # type: ignore[attr-defined]
        status_provider.status_core, "build_client", lambda: fake_client
    )

    async def fake_query_raises(_client: object) -> dict:  # type: ignore[type-arg]
        raise RuntimeError("daemon unreachable")

    monkeypatch.setattr(  # type: ignore[attr-defined]
        status_provider.status_core, "query_daemon", fake_query_raises
    )

                                      
    segs = asyncio.run(status_provider.status_segments())

                                                                     
                                                     
    assert isinstance(segs, list)
    assert len(segs) > 0
    assert any("daemon✗" in s for s in segs)

                                                          
    assert fake_client.closed
