# SPDX-License-Identifier: MIT
"""Privacy-filter swap point for ``KGOfflineCache``."""

from __future__ import annotations

from .types import KGOfflineCache, OfflineCacheEntry


def _privacy_filter_passes(
    cache: KGOfflineCache,
    entry: OfflineCacheEntry,
    project_id: str,
) -> bool:
    """Privacy filter — capa-firewall isolation per invariant.

    Defense-in-depth layer 4 per design contract: storage / query /
    serialization / **AFK cache** / audit. Even if a cross-project entry
    somehow lands in the cache (defense in depth — a bug in layers 1-3
    should not propagate to the AFK surface), this filter blocks return.

    Behaviour:

    - ``privacy_filter_enabled=False`` (max-scope, default): pass-through.
    - ``privacy_filter_enabled=True`` + ``active_project_id=None``:
      REJECT all (defensive default — ambiguous active project under
      capa-firewall MUST not surface any data).
    - ``privacy_filter_enabled=True`` + ``entry.project_id ==
      active_project_id``: pass-through.
    - ``privacy_filter_enabled=True`` + cross-project: REJECT.

    Note: the privacy decision compares the entry against the cache's
    ``active_project_id`` (established at construction time) — NOT the
    caller's per-query ``project_id`` argument. The caller argument
    flows to the audit payload for traceability but does not influence
    the privacy decision. This prevents a buggy or compromised caller
    from widening the filter mid-session.

    Returns True when the entry is allowed to be returned; False
    otherwise. Caller (``KGOfflineCache.get``) treats False as a miss
    WITHOUT emitting an audit event (no leak of cross-project hit-
    existence via the audit chain — even the structure of the violation
    must not surface, only the count via ``miss_count``).
    """
    # ``project_id`` (caller-claimed) deliberately unused for the
    # privacy decision per the doctrine above; retained in the signature
    # so the audit-emit path can keep both values close together.
    _ = project_id
    if not cache.privacy_filter_enabled:
        return True
    if cache.active_project_id is None:
        return False  # defensive default
    return entry.project_id == cache.active_project_id
