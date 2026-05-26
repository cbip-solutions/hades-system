# SPDX-License-Identifier: MIT
"""Type round-trip + module export tests for ``hermes_plugins.hades.afk``.

Coverage targets:
- Every public dataclass round-trips through dict (asdict + from_dict).
- Every enum has the expected member set; no extras, no missing.
- Module ``__init__.py`` exports the canonical surface (types + audit constants).
"""

from __future__ import annotations

import dataclasses

import pytest
from hermes_plugins.hades.afk import (
    AUDIT_MOBILE_EXPANSION_REQUESTED,
    AUDIT_OFFLINE_CACHE_HIT,
    AUDIT_VOICE_QUERY_DISPATCHED,
    AFKPlatform,
    KGOfflineCache,
    MobileSummaryCard,
    OfflineCacheEntry,
    VoiceFlow,
    VoiceFlowMode,
    __version__,
)


def test_version_anchor_matches_plan_12_release() -> None:
    assert __version__ == "0.12.0"


def test_audit_event_constants_are_canonical_strings() -> None:
    assert AUDIT_VOICE_QUERY_DISPATCHED == "afk.voice_query_dispatched"
    assert AUDIT_OFFLINE_CACHE_HIT == "afk.offline_cache_hit"
    assert AUDIT_MOBILE_EXPANSION_REQUESTED == "afk.mobile_expansion_requested"


def test_afk_platform_enum_members() -> None:
    expected = {"telegram", "slack", "whatsapp", "signal", "email", "voice"}
    assert {p.value for p in AFKPlatform} == expected


def test_voice_flow_mode_enum_members() -> None:
    assert {m.value for m in VoiceFlowMode} == {"sync", "async"}


def test_mobile_summary_card_round_trip() -> None:
    card = MobileSummaryCard(
        citation_id="c-001",
        title="cmd/zen/cli/audit_event.go",
        top_fields=[
            ("blast_radius", "12 callers"),
            ("top_callers", "dispatcher.Forward, orchestrator.Plan, hra.Decide"),
            ("community", "daemon-bootstrap"),
        ],
        audit_event_id="evt-1234abcd",
        project_id="zen-swarm",
        cache_state="fresh",
    )
    payload = dataclasses.asdict(card)
    assert payload["citation_id"] == "c-001"
                                                                      
                                                                       
                                                                        
                      
    rebuilt = MobileSummaryCard.from_dict(payload)
    assert rebuilt == card


def test_mobile_summary_card_accepts_list_of_lists_from_json_round_trip() -> None:
    """JSON round-trip surfaces tuples as lists; constructor must normalize."""
    card = MobileSummaryCard(
        citation_id="c-001",
        title="x",
        top_fields=[
            ["blast_radius", "12 callers"],
            ["community", "daemon-bootstrap"],
        ],
        audit_event_id="evt-1234abcd",
        project_id="zen-swarm",
        cache_state="fresh",
    )
    assert card.top_fields == (
        ("blast_radius", "12 callers"),
        ("community", "daemon-bootstrap"),
    )


def test_mobile_summary_card_rejects_more_than_three_top_fields() -> None:
    with pytest.raises(ValueError, match="at most 3"):
        MobileSummaryCard(
            citation_id="c-002",
            title="x",
            top_fields=[
                ("a", "1"),
                ("b", "2"),
                ("c", "3"),
                ("d", "4"),
            ],
            audit_event_id="evt-x",
            project_id="zen-swarm",
            cache_state="fresh",
        )


def test_mobile_summary_card_rejects_malformed_top_field_entry() -> None:
    with pytest.raises(ValueError, match="top_fields entries must be"):
        MobileSummaryCard(
            citation_id="c-003",
            title="x",
            top_fields=[("only-one-element",)],  # type: ignore[arg-type]
            audit_event_id="evt-x",
            project_id="zen-swarm",
            cache_state="fresh",
        )


def test_mobile_summary_card_rejects_unknown_cache_state() -> None:
    with pytest.raises(ValueError, match="cache_state"):
        MobileSummaryCard(
            citation_id="c-004",
            title="x",
            top_fields=[("a", "1")],
            audit_event_id="evt-x",
            project_id="zen-swarm",
            cache_state="bogus",                           
        )


def test_voice_flow_round_trip() -> None:
    flow = VoiceFlow(
        query="impact of changing dispatcher.Forward",
        estimated_latency_ms=8500,
        mode=VoiceFlowMode.SYNC,
        explicit_override=False,
        notification_dispatched=False,
    )
    payload = dataclasses.asdict(flow)
    rebuilt = VoiceFlow.from_dict(payload)
    assert rebuilt == flow


def test_voice_flow_from_dict_accepts_string_mode() -> None:
    flow = VoiceFlow.from_dict(
        {
            "query": "x",
            "estimated_latency_ms": 12000,
            "mode": "async",
            "explicit_override": False,
            "notification_dispatched": True,
        }
    )
    assert flow.mode is VoiceFlowMode.ASYNC


def test_voice_flow_rejects_negative_latency() -> None:
    with pytest.raises(ValueError, match="estimated_latency_ms must be >= 0"):
        VoiceFlow(
            query="x",
            estimated_latency_ms=-1,
            mode=VoiceFlowMode.SYNC,
            explicit_override=False,
            notification_dispatched=False,
        )


def test_voice_flow_rejects_inconsistent_notification_flag() -> None:
    with pytest.raises(ValueError, match="notification_dispatched=True"):
        VoiceFlow(
            query="x",
            estimated_latency_ms=2000,
            mode=VoiceFlowMode.SYNC,
            explicit_override=False,
            notification_dispatched=True,                                
        )


def test_offline_cache_entry_round_trip() -> None:
    entry = OfflineCacheEntry(
        citation_id="c-003",
        query_hash="abc123",
        project_id="zen-swarm",
        envelope_payload={"top_fields": [["blast_radius", "5"]], "title": "t"},
        community_summary="auth boundary",
        ingested_at_unix_ms=1746998400000,
    )
    payload = dataclasses.asdict(entry)
    rebuilt = OfflineCacheEntry.from_dict(payload)
    assert rebuilt == entry


def test_kg_offline_cache_default_capacity_is_doctrine_default() -> None:
    cache = KGOfflineCache()                    
    assert cache.capacity == 50                                          
    assert cache.doctrine == "default"
    assert cache.privacy_filter_enabled is False


def test_kg_offline_cache_max_scope_capacity() -> None:
    cache = KGOfflineCache(doctrine="max-scope")
    assert cache.capacity == 100
    assert cache.privacy_filter_enabled is False


def test_kg_offline_cache_capa_firewall_capacity_and_privacy_filter() -> None:
    cache = KGOfflineCache(doctrine="capa-firewall", active_project_id="zen-swarm")
    assert cache.capacity == 20
    assert cache.privacy_filter_enabled is True
    assert cache.active_project_id == "zen-swarm"


def test_kg_offline_cache_unknown_doctrine_raises() -> None:
    with pytest.raises(ValueError, match="unknown doctrine"):
        KGOfflineCache(doctrine="not-a-real-doctrine")
