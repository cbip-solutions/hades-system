# SPDX-License-Identifier: MIT
"""Typed contracts for the AFK richness module."""

from __future__ import annotations

import dataclasses
import time
from collections import OrderedDict
from collections.abc import Awaitable, Callable
from enum import Enum
from typing import Any


class AFKPlatform(str, Enum):
    """The six AFK platforms per design contract

    Values match Hermes' multi-platform gateway platform-id taxonomy
    (verified Spike-4 stage cross-platform parity).
    """

    TELEGRAM = "telegram"
    SLACK = "slack"
    WHATSAPP = "whatsapp"
    SIGNAL = "signal"
    EMAIL = "email"
    VOICE = "voice"


class VoiceFlowMode(str, Enum):
    """Voice query dispatch mode per design contract

    SYNC: estimated latency < 10s; operator hears response inline.
    ASYNC: estimated latency >= 10s; operator hears "results ready in
    inbox" notification + HADES design inbox notification with
    ``severity=info-immediate``.
    """

    SYNC = "sync"
    ASYNC = "async"


# Type alias for the canonical (key, value) top-field entry shape on a
# MobileSummaryCard. Accepted at construction time as either a tuple or a
# 2-element list (the latter surfaces naturally from JSON round-trips);
# normalized to a tuple-of-tuples via ``__post_init__``.
_TopFieldInput = list[list[str]] | list[tuple[str, str]] | tuple[tuple[str, str], ...]


@dataclasses.dataclass(frozen=True, slots=True)
class MobileSummaryCard:
    """Short citation render for AFK mobile platforms (Telegram/Slack/etc).

    per design contract: top 3 fields shown by default. Operator
    may request full payload via ``/expand <citation-id>`` slash command
    (D-2 implements the round-trip through daemon
    ``GET /v1/audit/event/<id>``).

    Invariants:

    - ``top_fields`` contains at most 3 (key, value) tuples — caller
      consequences drop extras silently in source citation envelope;
      this guard catches programming errors at construction time.
    - ``cache_state`` is one of ``{"fresh", "stale", "offline"}``. In the
      canonical HADES design stage ``citation.Envelope`` schema this
      surfaces via ``platform_renders["mobile"]["cache_state"]`` (no
      top-level field on the envelope itself). The ``MobileSummaryCard``
      projects that hint into this typed field for downstream renderers.
    """

    citation_id: str
    title: str
    top_fields: tuple[tuple[str, str], ...]
    audit_event_id: str
    project_id: str
    cache_state: str

    def __init__(
        self,
        *,
        citation_id: str,
        title: str,
        top_fields: _TopFieldInput,
        audit_event_id: str,
        project_id: str,
        cache_state: str,
    ) -> None:
        # frozen dataclass workaround: bypass __setattr__ for normalization.
        normalized: list[tuple[str, str]] = []
        for field in top_fields:
            if isinstance(field, (list, tuple)) and len(field) == 2:
                normalized.append((str(field[0]), str(field[1])))
            else:
                raise ValueError(
                    f"top_fields entries must be (key, value) pairs; got {field!r}"
                )
        if len(normalized) > 3:
            raise ValueError(
                f"top_fields must contain at most 3 entries; got {len(normalized)}"
            )
        if cache_state not in ("fresh", "stale", "offline"):
            raise ValueError(
                f"cache_state must be one of fresh|stale|offline; got {cache_state!r}"
            )
        object.__setattr__(self, "citation_id", citation_id)
        object.__setattr__(self, "title", title)
        object.__setattr__(self, "top_fields", tuple(normalized))
        object.__setattr__(self, "audit_event_id", audit_event_id)
        object.__setattr__(self, "project_id", project_id)
        object.__setattr__(self, "cache_state", cache_state)

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> MobileSummaryCard:
        """Build from a JSON-decoded dict (tolerates list-of-lists top_fields)."""
        return cls(
            citation_id=payload["citation_id"],
            title=payload["title"],
            top_fields=payload["top_fields"],
            audit_event_id=payload["audit_event_id"],
            project_id=payload["project_id"],
            cache_state=payload["cache_state"],
        )


@dataclasses.dataclass(frozen=True, slots=True)
class VoiceFlow:
    """Metadata for a voice query in flight (sync vs async dispatch).

    per design contract: ``estimated_latency_ms < 10000`` → ``SYNC``;
    otherwise ``ASYNC`` + HADES design inbox notification. Operator may force
    per-query via ``explicit_override`` (D-3 honours the request mode
    regardless of estimate when the flag is set).

    Invariants:

    - ``estimated_latency_ms >= 0`` (caller bug if negative).
    - ``notification_dispatched=True`` only when ``mode=ASYNC`` (D-3
      enforces).
    """

    query: str
    estimated_latency_ms: int
    mode: VoiceFlowMode
    explicit_override: bool
    notification_dispatched: bool

    def __post_init__(self) -> None:
        if self.estimated_latency_ms < 0:
            raise ValueError(
                f"estimated_latency_ms must be >= 0; got {self.estimated_latency_ms}"
            )
        if self.notification_dispatched and self.mode != VoiceFlowMode.ASYNC:
            raise ValueError("notification_dispatched=True is only valid when mode=ASYNC")

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> VoiceFlow:
        """Build from a JSON-decoded dict; accepts mode as string or enum."""
        mode_raw = payload["mode"]
        mode = VoiceFlowMode(mode_raw) if isinstance(mode_raw, str) else mode_raw
        return cls(
            query=payload["query"],
            estimated_latency_ms=payload["estimated_latency_ms"],
            mode=mode,
            explicit_override=payload["explicit_override"],
            notification_dispatched=payload["notification_dispatched"],
        )


@dataclasses.dataclass(frozen=True, slots=True)
class OfflineCacheEntry:
    """Per-row payload hydrated from HADES design D ``aggregator.Query()`` results.

    The KG offline cache stores the last N (doctrine-sized) entries
    indexed by ``query_hash``; on cache hit, the entry's
    ``envelope_payload`` is used to render the ``MobileSummaryCard`` with
    ``cache_state="offline"``.

    Invariants:

    - ``project_id`` is the canonical HADES design sha256 hex — privacy filter compares this field exactly against the active operator
      session's project_id under capa-firewall.
    - ``ingested_at_unix_ms`` is the daemon-side ingestion timestamp
      (UTC unix milliseconds) — used for LRU eviction tiebreaks when
      access counts are equal.
    """

    citation_id: str
    query_hash: str
    project_id: str
    envelope_payload: dict[str, Any]
    community_summary: str
    ingested_at_unix_ms: int

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> OfflineCacheEntry:
        return cls(
            citation_id=payload["citation_id"],
            query_hash=payload["query_hash"],
            project_id=payload["project_id"],
            envelope_payload=payload["envelope_payload"],
            community_summary=payload["community_summary"],
            ingested_at_unix_ms=payload["ingested_at_unix_ms"],
        )


# Doctrine-tunable cache capacities per design contract
_DOCTRINE_CAPACITIES: dict[str, int] = {
    "max-scope": 100,
    "default": 50,
    "capa-firewall": 20,
}


class KGOfflineCache:
    """Doctrine-tunable LRU cache for AFK offline KG queries.

    per design contract:

    - ``max-scope``: 100 entries cached
    - ``default``: 50 entries cached
    - ``capa-firewall``: 20 entries cached + privacy filter (no
      cross-project content)

    Implementation notes:

    - LRU backing via ``collections.OrderedDict`` (``move_to_end`` on
      access).
    - Privacy filter only enabled under capa-firewall doctrine (D-5
      replaces the helper ``_privacy_filter_passes`` in
      ``kg_offline_cache.py`` in place — same signature, capa-firewall
      isolation body).
    - Hydration happens via daemon ``/v1/knowledge/query``; D-6
      ``aggregator_consumer.hydrate_from_aggregator_query`` wires the
      population path.

    Single-declaration anchor per master plan §"Cross-stage type
    discipline": method bodies live HERE. ``kg_offline_cache.py``
    exposes only the ``_privacy_filter_passes`` swap point (D-4 ships
    permissive baseline; D-5 replaces with capa-firewall isolation).
    """

    def __init__(
        self,
        doctrine: str = "default",
        active_project_id: str | None = None,
    ) -> None:
        if doctrine not in _DOCTRINE_CAPACITIES:
            raise ValueError(
                f"unknown doctrine {doctrine!r}; expected one of "
                f"{sorted(_DOCTRINE_CAPACITIES.keys())}"
            )
        self.doctrine = doctrine
        self.capacity = _DOCTRINE_CAPACITIES[doctrine]
        self.privacy_filter_enabled = doctrine == "capa-firewall"
        self.active_project_id = active_project_id
        # LRU storage; key = query_hash, value = OfflineCacheEntry.
        # OrderedDict preserves insertion order; move_to_end on access
        # makes oldest-by-access prefix the eviction target.
        self._entries: OrderedDict[str, OfflineCacheEntry] = OrderedDict()
        # Hit + miss counters (used by audit chain emission via D-6).
        self._hit_count: int = 0
        self._miss_count: int = 0

    def put(self, entry: OfflineCacheEntry) -> None:
        """Insert or update an entry; evict oldest if over capacity (LRU).

        If ``entry.query_hash`` already present, the entry is replaced
        and moved to the end of the LRU order (counts as access).
        Otherwise inserted at end. Eviction triggers when size exceeds
        doctrine capacity — ``popitem(last=False)`` removes the oldest.
        """
        if entry.query_hash in self._entries:
            self._entries[entry.query_hash] = entry
            self._entries.move_to_end(entry.query_hash)
            return
        self._entries[entry.query_hash] = entry
        if len(self._entries) > self.capacity:
            self._entries.popitem(last=False)  # evict oldest

    async def get(
        self,
        query_hash: str,
        *,
        project_id: str,
        audit_emitter: Callable[..., Awaitable[None]],
    ) -> OfflineCacheEntry | None:
        """Return entry by ``query_hash``; update LRU; emit audit on hit.

        The privacy filter (capa-firewall isolation) is consulted via
        the module-level ``_privacy_filter_passes`` helper before LRU
        update + audit emit. D-5 replaces the helper's body with the
        capa-firewall isolation check; D-4 ships the LRU + audit
        emission with a permissive baseline filter.

        Args:
            query_hash: Stable hash of the query (canonical HADES design D shape).
            project_id: Active operator session's project_id (privacy
                filter input; D-5 rejects when
                ``entry.project_id != project_id`` under capa-firewall).
            audit_emitter: Callable invoked on hit; D-6 wires the
                canonical implementation
                (``audit.emit_offline_cache_hit``).

        Returns:
            The ``OfflineCacheEntry`` if present (and privacy-cleared
            when applicable), else ``None``.
        """
        from . import kg_offline_cache as _impl  # local: avoid circular

        entry = self._entries.get(query_hash)
        if entry is None:
            self._miss_count += 1
            return None

        # Privacy filter hook — D-5 wraps with capa-firewall isolation.
        # D-4 baseline: permissive (returns True). Helper lives in
        # ``kg_offline_cache.py`` so D-5 can replace the body in place
        # without re-touching this declaration.
        if not _impl._privacy_filter_passes(self, entry, project_id):
            # Treated as a miss for audit purposes (no leak of even
            # hit-existence across project boundaries per design contract
            # defense-in-depth layer 4).
            self._miss_count += 1
            return None

        # LRU access update — move to end so eviction prefers older entries.
        self._entries.move_to_end(query_hash)
        self._hit_count += 1

        # Emit audit event — D-6 wires audit.emit_offline_cache_hit.
        # cache_size is sampled AFTER the access for consistency with
        # the stats() snapshot.
        await audit_emitter(
            query_hash=query_hash,
            citation_id=entry.citation_id,
            project_id=project_id,
            cache_doctrine=self.doctrine,
            cache_size=len(self._entries),
            ts_unix_ms=int(time.time() * 1000),
        )
        return entry

    def stats(self) -> dict[str, Any]:
        """Return observable cache statistics for doctor + TUI surfaces."""
        return {
            "hit_count": self._hit_count,
            "miss_count": self._miss_count,
            "size": len(self._entries),
            "capacity": self.capacity,
            "doctrine": self.doctrine,
            "privacy_filter_enabled": self.privacy_filter_enabled,
        }

    def clear(self) -> None:
        """Reset cache state — used at Hermes session boundary."""
        self._entries.clear()
        self._hit_count = 0
        self._miss_count = 0

    @property
    def hit_rate(self) -> float:
        """Compute hit / (hit+miss); zero on no-access."""
        total = self._hit_count + self._miss_count
        if total == 0:
            return 0.0
        return self._hit_count / total
