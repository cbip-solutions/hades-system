# SPDX-License-Identifier: MIT
"""Tests for ``hermes_plugins.hades.afk.audit`` + ``aggregator_consumer``."""

from __future__ import annotations

import json
import logging
from typing import Any

import httpx
import pytest
from hermes_plugins.hades.afk import (
    AUDIT_MOBILE_EXPANSION_REQUESTED,
    AUDIT_OFFLINE_CACHE_HIT,
    AUDIT_VOICE_QUERY_DISPATCHED,
    KGOfflineCache,
)
from hermes_plugins.hades.afk.aggregator_consumer import (
    hydrate_from_aggregator_query,
)
from hermes_plugins.hades.afk.audit import (
    AUDIT_EMIT_PATH,
    emit_mobile_expansion_requested,
    emit_offline_cache_hit,
    emit_voice_query_dispatched,
    post_inbox_notification,
)


@pytest.mark.asyncio
async def test_emit_mobile_expansion_requested_posts_canonical_event() -> None:
    captured: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            captured.append(json.loads(request.content))
            return httpx.Response(202, json={"id": "evt-001"})
        return httpx.Response(404, json={"error": "uncanned"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
        await emit_mobile_expansion_requested(
            daemon_url="http://localhost:4471",
            client=client,
            citation_id="evt-1234",
            operator_id="testuser",
            platform="telegram",
            ts_unix_ms=1746998400000,
        )
    assert len(captured) == 1
    body = captured[0]
    assert body["type"] == AUDIT_MOBILE_EXPANSION_REQUESTED
    assert body["payload"]["citation_id"] == "evt-1234"
    assert body["payload"]["operator_id"] == "testuser"
    assert body["payload"]["platform"] == "telegram"
    assert body["payload"]["ts_unix_ms"] == 1746998400000


@pytest.mark.asyncio
async def test_emit_voice_query_dispatched_posts_canonical_event() -> None:
    captured: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        captured.append(json.loads(request.content))
        return httpx.Response(202, json={"id": "evt-002"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
        await emit_voice_query_dispatched(
            daemon_url="http://localhost:4471",
            client=client,
            query="impact dispatcher.Forward",
            operator_id="testuser",
            project_id="zen-swarm-sha256",
            mode="async",
            estimated_latency_ms=14000,
            explicit_override=False,
            notification_dispatched=True,
            ts_unix_ms=1746998400000,
        )
    body = captured[0]
    assert body["type"] == AUDIT_VOICE_QUERY_DISPATCHED
    assert body["project_id"] == "zen-swarm-sha256"
    assert body["payload"]["mode"] == "async"
    assert body["payload"]["notification_dispatched"] is True
    assert body["payload"]["estimated_latency_ms"] == 14000


@pytest.mark.asyncio
async def test_emit_offline_cache_hit_posts_canonical_event() -> None:
    captured: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        captured.append(json.loads(request.content))
        return httpx.Response(202, json={"id": "evt-003"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
        await emit_offline_cache_hit(
            daemon_url="http://localhost:4471",
            client=client,
            query_hash="abc123",
            citation_id="c-001",
            project_id="zen-swarm-sha256",
            cache_doctrine="default",
            cache_size=42,
            ts_unix_ms=1746998400000,
        )
    body = captured[0]
    assert body["type"] == AUDIT_OFFLINE_CACHE_HIT
    assert body["project_id"] == "zen-swarm-sha256"
    assert body["payload"]["cache_doctrine"] == "default"
    assert body["payload"]["cache_size"] == 42


@pytest.mark.asyncio
async def test_audit_emitter_raises_on_5xx() -> None:
    transport = httpx.MockTransport(
        lambda request: httpx.Response(503, json={"error": "chain_offline"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(RuntimeError, match="audit chain emit failed"):
            await emit_mobile_expansion_requested(
                daemon_url="http://localhost:4471",
                client=client,
                citation_id="x",
                operator_id="testuser",
                platform="telegram",
                ts_unix_ms=1746998400000,
            )


@pytest.mark.asyncio
async def test_audit_emitter_raises_on_4xx() -> None:
    transport = httpx.MockTransport(
        lambda request: httpx.Response(400, json={"error": "bad_payload"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(RuntimeError, match="audit chain emit failed"):
            await emit_offline_cache_hit(
                daemon_url="http://localhost:4471",
                client=client,
                query_hash="h",
                citation_id="c",
                project_id="p",
                cache_doctrine="default",
                cache_size=1,
                ts_unix_ms=0,
            )


@pytest.mark.asyncio
async def test_audit_emitter_accepts_200_ok() -> None:
    """  ships 202; future endpoints may return 200 — accept both."""
    transport = httpx.MockTransport(
        lambda request: httpx.Response(200, json={"id": "evt-ok"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        await emit_voice_query_dispatched(
            daemon_url="http://localhost:4471",
            client=client,
            query="q",
            operator_id="o",
            project_id="p",
            mode="sync",
            estimated_latency_ms=1,
            explicit_override=False,
            notification_dispatched=False,
            ts_unix_ms=0,
        )


def test_hydrate_populates_cache_from_aggregator_response() -> None:
    cache = KGOfflineCache(doctrine="default")
    daemon_response = {
        "results": [
            {
                "citation_id": f"c-{i}",
                "query_hash": f"h{i}",
                "project_id": "zen-swarm-sha256",
                "envelope_payload": {"title": f"q-{i}"},
                "community_summary": "auth",
                "ingested_at_unix_ms": 1746998400000 + i,
            }
            for i in range(10)
        ]
    }
    populated = hydrate_from_aggregator_query(cache, daemon_response)
    assert populated == 10
    assert cache.stats()["size"] == 10


def test_hydrate_warns_when_batch_exceeds_capacity(
    caplog: pytest.LogCaptureFixture,
) -> None:
    cache = KGOfflineCache(
        doctrine="capa-firewall", active_project_id="zen-swarm-sha256"
    )               
    daemon_response = {
        "results": [
            {
                "citation_id": f"c-{i}",
                "query_hash": f"h{i}",
                "project_id": "zen-swarm-sha256",
                "envelope_payload": {"title": f"q-{i}"},
                "community_summary": "auth",
                "ingested_at_unix_ms": 1746998400000 + i,
            }
            for i in range(50)                                  
        ]
    }
    with caplog.at_level(
        logging.WARNING, logger="hermes_plugins.hades.afk.aggregator_consumer"
    ):
        populated = hydrate_from_aggregator_query(cache, daemon_response)
    assert populated == 50
    assert cache.stats()["size"] == 20                                    
    assert any("exceeds doctrine capacity" in r.message for r in caplog.records)


def test_hydrate_empty_response_is_noop() -> None:
    cache = KGOfflineCache(doctrine="default")
    populated = hydrate_from_aggregator_query(cache, {"results": []})
    assert populated == 0
    assert cache.stats()["size"] == 0


def test_hydrate_no_results_key_treated_as_empty() -> None:
    """Missing 'results' key surfaces no rows; this is the empty-aggregator
    response shape (e.g. when the daemon returns ``{}`` for a hit-rate
    debug ping)."""
    cache = KGOfflineCache(doctrine="default")
    populated = hydrate_from_aggregator_query(cache, {})
    assert populated == 0


def test_hydrate_malformed_row_raises() -> None:
    cache = KGOfflineCache(doctrine="default")
    bad_response = {"results": [{"citation_id": "c-1"}]}                         
    with pytest.raises(KeyError):
        hydrate_from_aggregator_query(cache, bad_response)


@pytest.mark.asyncio
async def test_post_inbox_notification_raises_on_5xx() -> None:
    """Inbox notification post must raise on non-2xx to surface
    daemon outage to the caller (matches the audit-emit failure mode
    so the voice flow's inbox-post error path is observable)."""
    transport = httpx.MockTransport(
        lambda request: httpx.Response(503, json={"error": "daemon_offline"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(RuntimeError, match="inbox notification post failed"):
            await post_inbox_notification(
                daemon_url="http://localhost:4471",
                client=client,
                project_id="zen-swarm-sha256",
                severity="info-immediate",
                event_type="afk.voice_query_async_started",
                payload={"voice_query": "q"},
            )


@pytest.mark.asyncio
async def test_post_inbox_notification_handles_non_dict_response() -> None:
    """Defensive: if the daemon returns a non-dict body (legacy or
    misconfigured response), the helper wraps it in a {raw:...} dict
    so callers see a stable shape."""

    def _handler(_: httpx.Request) -> httpx.Response:
                                                                       
                 
        return httpx.Response(202, json=["non-dict-ack"])

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
        result = await post_inbox_notification(
            daemon_url="http://localhost:4471",
            client=client,
            project_id="zen-swarm-sha256",
            severity="info-immediate",
            event_type="afk.voice_query_async_started",
            payload={"voice_query": "q"},
        )
        assert result == {"raw": ["non-dict-ack"]}


@pytest.mark.asyncio
async def test_post_inbox_notification_dict_response_returned_as_is() -> None:
    """The plan-file inline contract is ``{"id": int, "ack": str}``."""

    def _handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(202, json={"id": 42, "ack": "queued"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
        result = await post_inbox_notification(
            daemon_url="http://localhost:4471",
            client=client,
            project_id="zen-swarm-sha256",
            severity="info-immediate",
            event_type="afk.voice_query_async_started",
            payload={"voice_query": "q"},
        )
        assert result == {"id": 42, "ack": "queued"}
