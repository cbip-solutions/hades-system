# SPDX-License-Identifier: MIT
"""Privacy filter tests — capa-firewall doctrine isolation per invariant."""

from __future__ import annotations

from unittest.mock import AsyncMock

import pytest
from hermes_plugins.hades.afk import (
    KGOfflineCache,
    OfflineCacheEntry,
)


def _entry(query_hash: str, project_id: str) -> OfflineCacheEntry:
    return OfflineCacheEntry(
        citation_id=f"c-{query_hash}",
        query_hash=query_hash,
        project_id=project_id,
        envelope_payload={"title": f"q-{query_hash}"},
        community_summary="auth boundary",
        ingested_at_unix_ms=1746998400000,
    )


@pytest.mark.asyncio
async def test_max_scope_no_privacy_filter_returns_cross_project() -> None:
    cache = KGOfflineCache(doctrine="max-scope", active_project_id="zen-swarm")
    cache.put(_entry("h1", project_id="other-project-sha256"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is not None                                      
    assert audit_emitter.await_count == 1


@pytest.mark.asyncio
async def test_default_no_privacy_filter_returns_cross_project() -> None:
    cache = KGOfflineCache(doctrine="default", active_project_id="zen-swarm")
    cache.put(_entry("h1", project_id="other-project-sha256"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is not None                                    
    assert audit_emitter.await_count == 1


@pytest.mark.asyncio
async def test_capa_firewall_matching_project_returns_entry() -> None:
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    cache.put(_entry("h1", project_id="zen-swarm-sha256"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is not None
    assert audit_emitter.await_count == 1


@pytest.mark.asyncio
async def test_capa_firewall_cross_project_returns_none_no_audit() -> None:
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
                                                                           
                                                                      
    # lets a leak reach the cache. The AFK filter MUST block it.
    cache.put(_entry("h1", project_id="other-project-sha256"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is None                   
    assert cache.stats()["miss_count"] == 1
    assert cache.stats()["hit_count"] == 0
    audit_emitter.assert_not_awaited()                           


@pytest.mark.asyncio
async def test_capa_firewall_active_project_id_none_rejects_all() -> None:
                                                                      
    # MUST not surface ANY data. Operator must explicitly set the project
                                 
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id=None)
    cache.put(_entry("h1", project_id="zen-swarm-sha256"))
    audit_emitter = AsyncMock()
    got = await cache.get(
        "h1", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
    )
    assert got is None
    audit_emitter.assert_not_awaited()


@pytest.mark.asyncio
async def test_capa_firewall_filter_does_not_emit_per_attempted_query() -> None:
    """Ensures audit event count is zero across many cross-project attempts."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    for i in range(15):
        cache.put(_entry(f"h{i}", project_id=f"other-project-{i}-sha256"))
    audit_emitter = AsyncMock()
    for i in range(15):
        await cache.get(
            f"h{i}", project_id="zen-swarm-sha256", audit_emitter=audit_emitter
        )
    audit_emitter.assert_not_awaited()
                                    
    assert cache.stats()["miss_count"] == 15
    assert cache.stats()["hit_count"] == 0


@pytest.mark.asyncio
async def test_capa_firewall_filter_uses_cache_active_project_not_caller() -> None:
    """The privacy filter compares ``entry.project_id`` against the
    cache's ``active_project_id`` at construction time, NOT the caller's
    ``project_id`` argument. The caller argument flows to the audit
    payload for traceability but does not influence the privacy decision.

    Rationale: the caller is the per-query operator session; the cache's
    active project is the session-binding identity established when the
    cache was constructed. Trusting the caller alone would let a buggy
    caller (or compromised session) widen the filter mid-session.
    """
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    cache.put(_entry("h1", project_id="zen-swarm-sha256"))
    audit_emitter = AsyncMock()
                                                                     
                                                                      
                                                              
    got = await cache.get(
        "h1",
        project_id="caller-claimed-project",                     
        audit_emitter=audit_emitter,
    )
    assert got is not None
    audit_emitter.assert_awaited_once()
