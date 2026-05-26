# SPDX-License-Identifier: MIT
"""Tests for ``hermes_plugins.hades.afk.voice_flow``."""

from __future__ import annotations

from typing import Any
from unittest.mock import AsyncMock

import httpx
import pytest
from hermes_plugins.hades.afk import (
    AUDIT_VOICE_QUERY_DISPATCHED,
    VoiceFlow,
    VoiceFlowMode,
)
from hermes_plugins.hades.afk.voice_flow import (
    SYNC_THRESHOLD_MS,
    dispatch_voice_query,
    estimate_latency_ms,
)


def test_estimate_latency_base_query() -> None:
    assert estimate_latency_ms("impact of dispatcher.Forward") == 2000


def test_estimate_latency_cross_project_baseline() -> None:
    assert (
        estimate_latency_ms(
            "impact of dispatcher.Forward",
            cross_project=True,
            project_count=1,
        )
        == 8000
    )


def test_estimate_latency_cross_project_multi() -> None:
                                                                             
    assert (
        estimate_latency_ms(
            "impact across A B C",
            cross_project=True,
            project_count=3,
        )
        == 11000
    )


def test_estimate_latency_community_keyword_adds_3000() -> None:
                                               
    assert estimate_latency_ms("show community summary for auth") == 5000


def test_estimate_latency_blast_radius_keyword_adds_2500() -> None:
    assert estimate_latency_ms("blast radius for orchestrator.Plan") == 4500


def test_estimate_latency_impact_pre_merge_adds_2500() -> None:
    assert estimate_latency_ms("impact-pre-merge for branch foo") == 4500


def test_estimate_latency_long_query_adds_1000() -> None:
    long_query = "x" * 600
    assert estimate_latency_ms(long_query) == 3000


def test_estimate_latency_combined_factors() -> None:
                                                                  
    long_q = "blast radius community across A B " + "x" * 500
                                                
    assert estimate_latency_ms(long_q, cross_project=True, project_count=2) == 16000


def test_estimate_latency_negative_project_count_clamped_to_baseline() -> None:
    """project_count <= 0 must not produce negative latency offsets."""
    assert (
        estimate_latency_ms("x", cross_project=True, project_count=0) == 8000
    )                  


def test_sync_threshold_constant_matches_spec() -> None:
    assert SYNC_THRESHOLD_MS == 10_000


@pytest.mark.asyncio
async def test_dispatch_voice_sync_path_under_threshold(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
    audit_event_capture: list[dict[str, Any]],
) -> None:
    async def _emit(**kwargs: Any) -> None:
        audit_event_capture.append({"name": AUDIT_VOICE_QUERY_DISPATCHED, **kwargs})

    audit_emitter = AsyncMock(side_effect=_emit)
    inbox_poster = AsyncMock()
    flow = await dispatch_voice_query(
        query="impact of dispatcher.Forward",
        operator_id="testuser",
        project_id="zen-swarm-sha256",
        explicit_override=None,
        cross_project=False,
        project_count=1,
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
        inbox_poster=inbox_poster,
    )
    assert isinstance(flow, VoiceFlow)
    assert flow.mode == VoiceFlowMode.SYNC
    assert flow.estimated_latency_ms == 2000
    assert flow.notification_dispatched is False
    assert flow.explicit_override is False
    assert audit_emitter.await_count == 1
    assert audit_event_capture[0]["mode"] == "sync"
    inbox_poster.assert_not_awaited()


@pytest.mark.asyncio
async def test_dispatch_voice_async_path_over_threshold(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
    audit_event_capture: list[dict[str, Any]],
) -> None:
    async def _emit(**kwargs: Any) -> None:
        audit_event_capture.append({"name": AUDIT_VOICE_QUERY_DISPATCHED, **kwargs})

    audit_emitter = AsyncMock(side_effect=_emit)
    inbox_poster = AsyncMock(return_value={"id": 99, "ack": "queued"})
    flow = await dispatch_voice_query(
        query="cross-project federated impact analysis with community summary",
        operator_id="testuser",
        project_id="zen-swarm-sha256",
        explicit_override=None,
        cross_project=True,
        project_count=3,                                         
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
        inbox_poster=inbox_poster,
    )
    assert flow.mode == VoiceFlowMode.ASYNC
    assert flow.estimated_latency_ms >= 10000
    assert flow.notification_dispatched is True
    assert audit_event_capture[0]["mode"] == "async"
    assert audit_event_capture[0]["notification_dispatched"] is True
    inbox_poster.assert_awaited_once()
    assert inbox_poster.await_args is not None
    inbox_call_kwargs = inbox_poster.await_args.kwargs
    assert inbox_call_kwargs["severity"] == "info-immediate"
    assert inbox_call_kwargs["event_type"] == "afk.voice_query_async_started"
    assert inbox_call_kwargs["payload"]["voice_query"].startswith("cross-project")
    assert inbox_call_kwargs["payload"]["operator_id"] == "testuser"
    assert inbox_call_kwargs["project_id"] == "zen-swarm-sha256"


@pytest.mark.asyncio
async def test_dispatch_voice_explicit_sync_override_ignores_estimate(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
) -> None:
    audit_emitter = AsyncMock()
    inbox_poster = AsyncMock()
    flow = await dispatch_voice_query(
        query="cross-project federated impact analysis with community summary",
        operator_id="testuser",
        project_id="zen-swarm-sha256",
        explicit_override=VoiceFlowMode.SYNC,
        cross_project=True,
        project_count=5,
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
        inbox_poster=inbox_poster,
    )
    assert flow.mode == VoiceFlowMode.SYNC
    assert flow.explicit_override is True
    assert flow.notification_dispatched is False
    inbox_poster.assert_not_awaited()
    audit_emitter.assert_awaited_once()


@pytest.mark.asyncio
async def test_dispatch_voice_explicit_async_override_ignores_estimate(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
) -> None:
    audit_emitter = AsyncMock()
    inbox_poster = AsyncMock(return_value={"id": 100, "ack": "queued"})
    flow = await dispatch_voice_query(
        query="impact of dispatcher.Forward",                        
        operator_id="testuser",
        project_id="zen-swarm-sha256",
        explicit_override=VoiceFlowMode.ASYNC,
        cross_project=False,
        project_count=1,
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
        inbox_poster=inbox_poster,
    )
    assert flow.mode == VoiceFlowMode.ASYNC
    assert flow.explicit_override is True
    assert flow.notification_dispatched is True
    inbox_poster.assert_awaited_once()


@pytest.mark.asyncio
async def test_dispatch_voice_at_exact_threshold_routes_async(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
) -> None:
    """estimate == SYNC_THRESHOLD_MS should route async (>= comparison)."""
    audit_emitter = AsyncMock()
    inbox_poster = AsyncMock(return_value={"id": 101, "ack": "queued"})
                                                                                 
                                                                               
                                             
                                                                                
                                                                        
    flow = await dispatch_voice_query(
        query="community across A B",
        operator_id="testuser",
        project_id="zen-swarm-sha256",
        explicit_override=None,
        cross_project=True,
        project_count=2,
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
        inbox_poster=inbox_poster,
    )
    assert flow.estimated_latency_ms >= 10_000
    assert flow.mode == VoiceFlowMode.ASYNC
