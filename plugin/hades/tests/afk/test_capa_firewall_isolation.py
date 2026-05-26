# SPDX-License-Identifier: MIT
"""Adversarial tests for capa-firewall offline cache isolation."""

from __future__ import annotations

from typing import Any
from unittest.mock import AsyncMock

import pytest
from hermes_plugins.hades.afk import (
    KGOfflineCache,
    OfflineCacheEntry,
)

pytestmark = pytest.mark.adversarial


def _entry(query_hash: str, project_id: str) -> OfflineCacheEntry:
    return OfflineCacheEntry(
        citation_id=f"c-{query_hash}",
        query_hash=query_hash,
        project_id=project_id,
        envelope_payload={"title": f"q-{query_hash}", "secret": "do-not-leak"},
        community_summary="cross-project secret",
        ingested_at_unix_ms=1746998400000,
    )


@pytest.mark.asyncio
async def test_adversarial_bulk_cross_project_no_leak() -> None:
    """200 cross-project entries; 200 reads; ZERO returned; ZERO audit emits."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="active-sha256")
    captured_audit: list[Any] = []

    async def _emit(**kwargs: Any) -> None:
        captured_audit.append(kwargs)

    audit_emitter = AsyncMock(side_effect=_emit)

                                                                   
                                                                     
                  
    for i in range(200):
        cache.put(_entry(f"h{i}", project_id=f"other-{i}-sha256"))

                                                              
    for i in range(200):
        got = await cache.get(
            f"h{i}",
            project_id="active-sha256",
            audit_emitter=audit_emitter,
        )
        assert got is None, f"leak detected on h{i}"

                                                                
    assert audit_emitter.await_count == 0
    assert len(captured_audit) == 0

                                        
    snap = cache.stats()
    assert snap["miss_count"] == 200
    assert snap["hit_count"] == 0


@pytest.mark.asyncio
async def test_adversarial_identical_query_hash_different_projects() -> None:
    """Same query_hash from two project_ids; only the matching project succeeds."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="active-sha256")
    audit_emitter = AsyncMock()

    cache.put(_entry("shared_hash", project_id="other-sha256"))
                                                                      
    got = await cache.get(
        "shared_hash",
        project_id="active-sha256",
        audit_emitter=audit_emitter,
    )
    assert got is None
    audit_emitter.assert_not_awaited()

                                                             
    cache.put(_entry("shared_hash", project_id="active-sha256"))
    got = await cache.get(
        "shared_hash",
        project_id="active-sha256",
        audit_emitter=audit_emitter,
    )
    assert got is not None
    assert got.project_id == "active-sha256"
    audit_emitter.assert_awaited_once()


@pytest.mark.asyncio
async def test_adversarial_project_id_substring_attack() -> None:
    """Substring/suffix attack must NOT match (exact comparison only)."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm-sha256")
    audit_emitter = AsyncMock()

                                                                      
                              
    cache.put(_entry("h1", project_id="zen-swarm-sha256-evil-suffix"))
    got = await cache.get(
        "h1",
        project_id="zen-swarm-sha256",
        audit_emitter=audit_emitter,
    )
    assert got is None
    audit_emitter.assert_not_awaited()

                                                                        
                                                                 
    cache2 = KGOfflineCache(
        doctrine="capa-firewall",
        active_project_id="zen-swarm-sha256-evil-suffix",
    )
    cache2.put(_entry("h2", project_id="zen-swarm-sha256"))
    got = await cache2.get(
        "h2",
        project_id="zen-swarm-sha256-evil-suffix",
        audit_emitter=audit_emitter,
    )
    assert got is None
    audit_emitter.assert_not_awaited()


@pytest.mark.asyncio
async def test_adversarial_none_active_project_rejects_all() -> None:
    """``active_project_id=None`` must reject ALL reads (defensive default)."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id=None)
    audit_emitter = AsyncMock()

    for i in range(50):
        cache.put(_entry(f"h{i}", project_id=f"any-project-{i}"))

    for i in range(50):
        got = await cache.get(
            f"h{i}",
            project_id="any-project-x",
            audit_emitter=audit_emitter,
        )
        assert got is None

    audit_emitter.assert_not_awaited()


@pytest.mark.asyncio
async def test_adversarial_audit_silent_under_full_attack_load() -> None:
    """Sustained cross-project attack: audit chain must remain ZERO emissions.

    Verifies the defense-in-depth principle: even the count of attempted
    cross-project reads must not surface via the audit chain (only via
    miss_count in the local stats snapshot — which is operator-scoped,
    not cross-project).
    """
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="active-sha256")
    captured: list[dict[str, Any]] = []

    async def _emit(**kwargs: Any) -> None:
        captured.append(kwargs)

    audit_emitter = AsyncMock(side_effect=_emit)

                                                                             
    for i in range(500):
        cache.put(_entry(f"h{i}", project_id=f"adv-{i}-sha256"))

                                                                                   
    for i in range(1000):
        await cache.get(
            f"h{i}",
            project_id="active-sha256",
            audit_emitter=audit_emitter,
        )

    assert audit_emitter.await_count == 0
    assert len(captured) == 0
    assert cache.stats()["hit_count"] == 0


@pytest.mark.asyncio
async def test_adversarial_doctrine_downgrade_per_instance_isolation() -> None:
    """Constructing a default-doctrine cache pointing at the same
    project_id does NOT inherit entries from a prior capa-firewall
    cache. Per-instance state isolation means doctrine downgrade
    requires re-construction (no in-memory shared singleton)."""
    capa_cache = KGOfflineCache(
        doctrine="capa-firewall", active_project_id="active-sha256"
    )
    capa_cache.put(_entry("h1", project_id="active-sha256"))

                                                     
    default_cache = KGOfflineCache(doctrine="default")
    audit_emitter = AsyncMock()
    got = await default_cache.get(
        "h1",
        project_id="active-sha256",
        audit_emitter=audit_emitter,
    )
                                                         
    assert got is None
    assert default_cache.stats()["size"] == 0
    audit_emitter.assert_not_awaited()


@pytest.mark.asyncio
async def test_adversarial_clear_does_not_leak_after_reuse() -> None:
    """After ``clear()``, the cache must be a fresh storage; no residual
    entries surface across the boundary. A bug that retained a soft
    reference would let prior session entries reappear after the next
    Hermes session boundary clears."""
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="active-sha256")
    for i in range(20):
        cache.put(_entry(f"h{i}", project_id="active-sha256"))
    cache.clear()
    audit_emitter = AsyncMock()
    for i in range(20):
        got = await cache.get(
            f"h{i}",
            project_id="active-sha256",
            audit_emitter=audit_emitter,
        )
        assert got is None
    assert cache.stats()["size"] == 0
    audit_emitter.assert_not_awaited()
