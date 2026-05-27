# SPDX-License-Identifier: MIT
"""zen-swarm AFK richness module —  """

from __future__ import annotations

from .types import (
    AFKPlatform,
    KGOfflineCache,
    MobileSummaryCard,
    OfflineCacheEntry,
    VoiceFlow,
    VoiceFlowMode,
)

                                                                      
                                                                     
                              
AUDIT_VOICE_QUERY_DISPATCHED = "afk.voice_query_dispatched"
AUDIT_OFFLINE_CACHE_HIT = "afk.offline_cache_hit"
AUDIT_MOBILE_EXPANSION_REQUESTED = "afk.mobile_expansion_requested"

__version__ = "0.12.0"


                                                                     
                                                                      
                                                                       
                                                                      
                           
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
