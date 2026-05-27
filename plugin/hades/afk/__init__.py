# SPDX-License-Identifier: MIT
"""zen-swarm AFK richness module — the release design release track."""

from __future__ import annotations

from .types import (
    AFKPlatform,
    KGOfflineCache,
    MobileSummaryCard,
    OfflineCacheEntry,
    VoiceFlow,
    VoiceFlowMode,
)

# Audit event canonical names (anchored to the the release design chain via daemon
# dispatcher). Subscribed by ``internal/audit/chain`` event registry;
# emitted by D-6 ``audit.py``.
AUDIT_VOICE_QUERY_DISPATCHED = "afk.voice_query_dispatched"
AUDIT_OFFLINE_CACHE_HIT = "afk.offline_cache_hit"
AUDIT_MOBILE_EXPANSION_REQUESTED = "afk.mobile_expansion_requested"

__version__ = "0.12.0"


# D-4 attaches LRU + audit-emission methods to KGOfflineCache via the
# ``kg_offline_cache`` binding module; D-5 replaces the privacy filter
# helper in place. Importing here ensures the bound methods are present
# whenever the AFK package is imported (production loader, test suite,
# or third-party consumer).
from . import kg_offline_cache as _kg_offline_cache  # noqa: F401, E402

__all__ = [
    "AFKPlatform",
    "AUDIT_MOBILE_EXPANSION_REQUESTED",
    "AUDIT_OFFLINE_CACHE_HIT",
    "AUDIT_VOICE_QUERY_DISPATCHED",
    "KGOfflineCache",
    "MobileSummaryCard",
    "OfflineCacheEntry",
    "VoiceFlow",
    "VoiceFlowMode",
    "__version__",
]
