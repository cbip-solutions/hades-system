# SPDX-License-Identifier: MIT
"""End-to-end integration test for the Telegram AFK flow."""

from __future__ import annotations

import json
from typing import Any

import httpx
import pytest
from hermes_plugins.hades.afk import (
    AUDIT_MOBILE_EXPANSION_REQUESTED,
    AUDIT_OFFLINE_CACHE_HIT,
    AFKPlatform,
    KGOfflineCache,
    MobileSummaryCard,
)
from hermes_plugins.hades.afk.aggregator_consumer import (
    hydrate_from_aggregator_query,
)
from hermes_plugins.hades.afk.audit import (
    AUDIT_EMIT_PATH,
    emit_mobile_expansion_requested,
    emit_offline_cache_hit,
)
from hermes_plugins.hades.afk.mobile_summary import expand, render_short


@pytest.mark.asyncio
async def test_telegram_e2e_short_render_expand_cache_hit() -> None:
    """End-to-end: Telegram operator → short → /expand → cache populate → hit."""
    captured_audit: list[dict[str, Any]] = []

    full_envelope = {
        "envelope": {
            "citation_id": "evt-1234abcd",
            "title": "cmd/zen/cli/audit_event.go",
            "top_fields": [
                ["blast_radius", "12 callers"],
                ["top_callers", "dispatcher.Forward, orchestrator.Plan, hra.Decide"],
                ["community", "daemon-bootstrap"],
            ],
            "full_payload": {
                "callers": ["dispatcher.Forward", "orchestrator.Plan", "hra.Decide"],
                "callees": ["audit.AnchorTessera", "store.WriteEvent"],
                "blast_radius_score": 12,
                "community_label": "daemon-bootstrap",
                "tessera_anchor": "0xdeadbeef",
            },
            "audit_event_id": "evt-1234abcd",
            "project_id": "zen-swarm-sha256",
            "cache_state": "fresh",
        }
    }

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/audit/event/evt-1234abcd" and request.method == "GET":
            return httpx.Response(200, json=full_envelope)
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            captured_audit.append(json.loads(request.content))
            return httpx.Response(202, json={"id": f"chain-{len(captured_audit)}"})
        return httpx.Response(404, json={"error": "uncanned", "path": request.url.path})

    daemon_url = "http://localhost:4471"

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
                                                                     
                                             
        initial_envelope = full_envelope["envelope"]
        card = render_short(initial_envelope)
        assert isinstance(card, MobileSummaryCard)
        assert card.citation_id == "evt-1234abcd"
        assert len(card.top_fields) == 3
        assert card.cache_state == "fresh"

                                                                        
        async def _emitter(**kwargs: Any) -> None:
            await emit_mobile_expansion_requested(
                daemon_url=daemon_url,
                client=client,
                **kwargs,
            )

        full = await expand(
            citation_id="evt-1234abcd",
            operator_id="testuser",
            platform=AFKPlatform.TELEGRAM,
            daemon_url=daemon_url,
            client=client,
            audit_emitter=_emitter,
        )
        assert "callers" in full["full_payload"]
        assert len(captured_audit) == 1
        assert captured_audit[0]["type"] == AUDIT_MOBILE_EXPANSION_REQUESTED
        assert captured_audit[0]["payload"]["citation_id"] == "evt-1234abcd"
        assert captured_audit[0]["payload"]["platform"] == "telegram"
        assert captured_audit[0]["payload"]["operator_id"] == "testuser"

                                                                      
                                                       
        cache = KGOfflineCache(doctrine="default")
        aggregator_response = {
            "results": [
                {
                    "citation_id": "evt-1234abcd",
                    "query_hash": "h_blast_radius_dispatcher",
                    "project_id": "zen-swarm-sha256",
                    "envelope_payload": initial_envelope,
                    "community_summary": "daemon-bootstrap",
                    "ingested_at_unix_ms": 1746998400000,
                },
            ]
        }
        populated = hydrate_from_aggregator_query(cache, aggregator_response)
        assert populated == 1

                                                            
        async def _hit_emitter(**kwargs: Any) -> None:
            await emit_offline_cache_hit(
                daemon_url=daemon_url,
                client=client,
                **kwargs,
            )

        hit = await cache.get(
            "h_blast_radius_dispatcher",
            project_id="zen-swarm-sha256",
            audit_emitter=_hit_emitter,
        )
        assert hit is not None
        assert hit.citation_id == "evt-1234abcd"

                                                       
        assert len(captured_audit) == 2
        assert captured_audit[1]["type"] == AUDIT_OFFLINE_CACHE_HIT
        assert captured_audit[1]["payload"]["query_hash"] == "h_blast_radius_dispatcher"
        assert captured_audit[1]["payload"]["cache_doctrine"] == "default"

                                      
        snap = cache.stats()
        assert snap["hit_count"] == 1
        assert snap["miss_count"] == 0
        assert snap["size"] == 1


@pytest.mark.asyncio
async def test_e2e_short_render_consistent_across_platforms() -> None:
    """Cross-platform parity: same envelope renders to same MobileSummaryCard
    regardless of which AFK platform the operator is on. 
    platform renderer transforms the card to platform-specific markup;
    the AFK module only owns the field-set extraction."""
    envelope = {
        "citation_id": "evt-555",
        "title": "internal/dispatcher/dispatcher.go",
        "top_fields": [
            ["blast_radius", "8 callers"],
            ["top_callers", "orchestrator.Plan"],
            ["community", "daemon-bootstrap"],
        ],
        "audit_event_id": "evt-555",
        "project_id": "zen-swarm-sha256",
        "cache_state": "fresh",
    }
    card = render_short(envelope)
                                                                
                                                                   
                                                               
    assert card.citation_id == "evt-555"
    assert card.top_fields == (
        ("blast_radius", "8 callers"),
        ("top_callers", "orchestrator.Plan"),
        ("community", "daemon-bootstrap"),
    )


@pytest.mark.asyncio
async def test_e2e_capa_firewall_hydrate_then_read_filters_cross_project() -> None:
    """Defense-in-depth scenario: cache hydrates from an aggregator
    response that erroneously contains a cross-project row. The AFK cache layer 4 filter blocks
    the cross-project entry on read under capa-firewall.

    This test exercises D-5 (privacy filter) + D-6 (hydration) together
    end-to-end — the hydration writes both entries (it does not filter
    on write per spec §8.3 layer 4 design), but the read path returns
    only the matching-project entry.
    """
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    aggregator_response = {
        "results": [
            {
                "citation_id": "c-own",
                "query_hash": "h_own",
                "project_id": "zen-swarm-sha256",
                "envelope_payload": {"title": "own"},
                "community_summary": "own community",
                "ingested_at_unix_ms": 1,
            },
            {
                "citation_id": "c-leak",
                "query_hash": "h_leak",
                "project_id": "other-sha256",                 
                "envelope_payload": {"title": "secret"},
                "community_summary": "secret community",
                "ingested_at_unix_ms": 2,
            },
        ]
    }
    populated = hydrate_from_aggregator_query(cache, aggregator_response)
    assert populated == 2                                                

    captured_audit: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            captured_audit.append(json.loads(request.content))
            return httpx.Response(202, json={"id": "ok"})
        return httpx.Response(404)

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:

        async def _hit_emitter(**kwargs: Any) -> None:
            await emit_offline_cache_hit(
                daemon_url="http://localhost:4471",
                client=client,
                **kwargs,
            )

                                              
        own = await cache.get(
            "h_own",
            project_id="zen-swarm-sha256",
            audit_emitter=_hit_emitter,
        )
        assert own is not None
        assert own.citation_id == "c-own"

                                                                 
        leak = await cache.get(
            "h_leak",
            project_id="zen-swarm-sha256",
            audit_emitter=_hit_emitter,
        )
        assert leak is None

    assert len(captured_audit) == 1                                
    assert captured_audit[0]["payload"]["query_hash"] == "h_own"
    assert cache.stats()["hit_count"] == 1
    assert cache.stats()["miss_count"] == 1
