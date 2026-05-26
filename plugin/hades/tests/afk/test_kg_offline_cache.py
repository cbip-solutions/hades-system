# SPDX-License-Identifier: MIT
"""Tests for ``hermes_plugins.hades.afk.kg_offline_cache`` (D-4 scope)."""

from __future__ import annotations

from typing import Any
from unittest.mock import AsyncMock

import pytest
from hermes_plugins.hades.afk import (
    AUDIT_OFFLINE_CACHE_HIT,
    KGOfflineCache,
    OfflineCacheEntry,
)


def _entry(query_hash: str, project_id: str = "zen-swarm-sha256") -> OfflineCacheEntry:
    return OfflineCacheEntry(
        citation_id=f"c-{query_hash}",
        query_hash=query_hash,
        project_id=project_id,
        envelope_payload={"title": f"q-{query_hash}"},
        community_summary="auth boundary",
        ingested_at_unix_ms=1746998400000,
    )


@pytest.mark.asyncio
async def test_put_then_get_returns_entry() -> None:
    cache = KGOfflineCache(doctrine="default")
    cache.put(_entry("h1"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is not None
    assert got.citation_id == "c-h1"


@pytest.mark.asyncio
async def test_get_miss_returns_none_increments_miss() -> None:
    cache = KGOfflineCache(doctrine="default")
    audit_emitter = AsyncMock()
    got = await cache.get(
        "nope", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is None
    assert cache.stats()["miss_count"] == 1
    assert cache.stats()["hit_count"] == 0
    audit_emitter.assert_not_awaited()


@pytest.mark.asyncio
async def test_get_hit_emits_audit_event() -> None:
    cache = KGOfflineCache(doctrine="default")
    cache.put(_entry("h2"))
    captured: list[dict[str, Any]] = []

    async def _emit(**kwargs: Any) -> None:
        captured.append({"name": AUDIT_OFFLINE_CACHE_HIT, **kwargs})

    audit_emitter = AsyncMock(side_effect=_emit)
    got = await cache.get(
        "h2", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is not None
    assert audit_emitter.await_count == 1
    assert captured[0]["query_hash"] == "h2"
    assert captured[0]["citation_id"] == "c-h2"
    assert captured[0]["project_id"] == "zen-swarm-sha256"
    assert captured[0]["cache_doctrine"] == "default"
    assert captured[0]["cache_size"] == 1
    assert captured[0]["ts_unix_ms"] >= 0


@pytest.mark.asyncio
async def test_lru_eviction_when_over_capacity_default() -> None:
    cache = KGOfflineCache(doctrine="default")               
    audit_emitter = AsyncMock()
                                                                   
    for i in range(51):
        cache.put(_entry(f"h{i}"))
    assert cache.stats()["size"] == 50
    h0 = await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    h1 = await cache.get("h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    assert h0 is None           
    assert h1 is not None                 


@pytest.mark.asyncio
async def test_lru_access_order_updates_on_get() -> None:
    cache = KGOfflineCache(doctrine="default")               
    audit_emitter = AsyncMock()
    for i in range(50):
        cache.put(_entry(f"h{i}"))
                                       
    await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
                                                                 
    cache.put(_entry("h_new"))
    assert (
        await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is not None
    assert (
        await cache.get("h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is None


@pytest.mark.asyncio
async def test_lru_eviction_max_scope_capacity_100() -> None:
    cache = KGOfflineCache(doctrine="max-scope")                
    for i in range(101):
        cache.put(_entry(f"h{i}"))
    assert cache.stats()["size"] == 100
    audit_emitter = AsyncMock()
    assert (
        await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is None
    assert (
        await cache.get("h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is not None


@pytest.mark.asyncio
async def test_lru_eviction_capa_firewall_capacity_20() -> None:
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    for i in range(21):
        cache.put(_entry(f"h{i}"))
    assert cache.stats()["size"] == 20
    audit_emitter = AsyncMock()
                                                                     
                                                                       
                                                                         
    assert (
        await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is None
    assert (
        await cache.get("h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
    ) is not None


def test_put_existing_query_hash_updates_in_place() -> None:
    cache = KGOfflineCache(doctrine="default")
    cache.put(_entry("h1"))
    cache.put(_entry("h1"))                                     
    assert cache.stats()["size"] == 1


@pytest.mark.asyncio
async def test_put_existing_moves_to_end_of_lru() -> None:
    """Re-putting an existing hash refreshes its LRU position."""
    cache = KGOfflineCache(doctrine="default")               
    for i in range(50):
        cache.put(_entry(f"h{i}"))
                                                             
    cache.put(_entry("h0"))
    cache.put(_entry("h_new"))                               
    audit_emitter = AsyncMock()
                                                
    assert (
        await cache.get("h0", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
        is not None
    )
                                                                
    assert (
        await cache.get("h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter)
        is None
    )


def test_clear_resets_counters_and_entries() -> None:
    cache = KGOfflineCache(doctrine="default")
    cache.put(_entry("h1"))
    cache.put(_entry("h2"))
    assert cache.stats()["size"] == 2
    cache.clear()
    assert cache.stats()["size"] == 0
    assert cache.stats()["hit_count"] == 0
    assert cache.stats()["miss_count"] == 0


@pytest.mark.asyncio
async def test_hit_rate_property() -> None:
    cache = KGOfflineCache(doctrine="default")
    audit_emitter = AsyncMock()
    assert cache.hit_rate == 0.0
    cache.put(_entry("h1"))
    await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )       
    await cache.get(
        "h2", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )        
    await cache.get(
        "h3", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )        
                                         
    assert cache.hit_rate == pytest.approx(1 / 3)


def test_stats_reports_full_snapshot() -> None:
    cache = KGOfflineCache(doctrine="max-scope")
    cache.put(_entry("h1"))
    snap = cache.stats()
    assert snap == {
        "hit_count": 0,
        "miss_count": 0,
        "size": 1,
        "capacity": 100,
        "doctrine": "max-scope",
        "privacy_filter_enabled": False,
    }
