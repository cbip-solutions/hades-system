# SPDX-License-Identifier: MIT
"""Hydrate the AFK offline cache from the daemon /v1/knowledge/query federated aggregator."""

from __future__ import annotations

import logging
from typing import Any

from .types import KGOfflineCache, OfflineCacheEntry

_log = logging.getLogger(__name__)


def hydrate_from_aggregator_query(
    cache: KGOfflineCache,
    daemon_response: dict[str, Any],
) -> int:
    """Populate the offline cache from Plan 9 D aggregator.Query() results.

    Args:
        cache: The ``KGOfflineCache`` instance (already constructed with
            active doctrine + project_id).
        daemon_response: The JSON body from daemon
            ``/v1/knowledge/query``. Shape:
            ``{"results": [{"citation_id": ..., "query_hash": ..., ...}, ...]}``.
            Missing ``"results"`` is treated as empty.

    Returns:
        The number of rows attempted (NOT the number stored — eviction
        may have removed some). Cache size after hydration is
        ``min(rows, capacity)``.

    Raises:
        KeyError: if a row is missing required keys (malformed daemon
            response).

    Side effects:
        Logs a WARNING when ``len(rows) > cache.capacity`` — surfaces
        cache-sizing mismatch to operators (e.g., capa-firewall cache at
        20 cannot hold a 50-row workload).
    """
    rows = daemon_response.get("results", [])
    if len(rows) > cache.capacity:
        _log.warning(
            "aggregator query batch (%d rows) exceeds doctrine capacity "
            "(%d); older entries will be evicted via LRU. Consider doctrine "
            "override or reducing query scope.",
            len(rows),
            cache.capacity,
        )

    populated = 0
    for row in rows:
        entry = OfflineCacheEntry(
            citation_id=row["citation_id"],
            query_hash=row["query_hash"],
            project_id=row["project_id"],
            envelope_payload=row["envelope_payload"],
            community_summary=row["community_summary"],
            ingested_at_unix_ms=row["ingested_at_unix_ms"],
        )
        cache.put(entry)
        populated += 1

    return populated
