# SPDX-License-Identifier: MIT
"""Type stub tests: Envelope (per-citation) + AugmentationResult (wrapper)
dataclass round-trip against Go JSON wire (cross-language source-of-truth:
`internal/citation/types.go` + `internal/augment/types.go`).
"""

from __future__ import annotations

import json
from datetime import datetime, timezone

import pytest
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RetrievalLane,
)

                                                                             
                                               
                                                                             


def test_citation_type_enum_values():
    """All CitationType values match   Go enum."""
    assert CitationType.KG_NODE.value == "kg_node"
    assert CitationType.KG_EDGE.value == "kg_edge"
    assert CitationType.FILE_SLICE.value == "file_slice"
    assert CitationType.COMMIT_REF.value == "commit_ref"
    assert CitationType.COMMUNITY_SUMMARY.value == "community_summary"
    assert CitationType.AUDIT_EVENT.value == "audit_event"
    assert CitationType.CUSTOM.value == "custom"


def test_citation_source_enum_values():
    """CitationSource enum matches   Go enum."""
                                                                                       
    assert CitationSource.CARONTE_QUERY.value == "caronte_query"
    assert CitationSource.CARONTE_CONTEXT.value == "caronte_context"
    assert CitationSource.AGGREGATOR_FTS.value == "aggregator_fts"
    assert CitationSource.AGGREGATOR_VEC.value == "aggregator_vec"
    assert CitationSource.TEMPORAL.value == "temporal"
    assert CitationSource.MANUAL_OVERRIDE.value == "manual_override"
                                                                                
    assert CitationSource.GITNEXUS_QUERY.value == "gitnexus_query"
    assert CitationSource.GITNEXUS_CONTEXT.value == "gitnexus_context"


def test_retrieval_lane_enum_values():
    """RetrievalLane enum maps to spec §1 Q2=C 5-lane RRF taxonomy."""
    assert RetrievalLane.SEMANTIC.value == "semantic"
    assert RetrievalLane.LEXICAL.value == "lexical"
    assert RetrievalLane.GRAPH.value == "graph"
    assert RetrievalLane.RERANK.value == "rerank"
    assert RetrievalLane.TEMPORAL.value == "temporal"


def test_platform_enum_values():
    """Platform enum: 6 platform renderers + markdown fallback."""
    assert Platform.INK.value == "ink"
    assert Platform.TELEGRAM.value == "telegram"
    assert Platform.SLACK.value == "slack"
    assert Platform.EMAIL.value == "email"
    assert Platform.VOICE.value == "voice"
    assert Platform.WEB.value == "web"
    assert Platform.MARKDOWN_FALLBACK.value == "markdown_fallback"


                                                                             
                                               
                                                                             


def test_envelope_dataclass_round_trip():
    """Envelope.to_dict() / from_dict() preserves all fields (per-citation envelope)."""
    e = Envelope(
        id="c-test01234567",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-test",
    )
    d = e.to_dict()
    e2 = Envelope.from_dict(d)
    assert e2 == e


def test_envelope_to_json_matches_go_wire():
    """Envelope.to_json() emits keys matching Go envelope.go json tags.

    : primary source value is ``caronte_query``; backward-compat
    alias ``GITNEXUS_QUERY`` still round-trips (tested separately in
    test_citation_source_enum_values).
    """
    e = Envelope(
        id="c-jsonwire0001",
        type=CitationType.KG_NODE,
        source=CitationSource.CARONTE_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-test",
    )
    raw = e.to_json()
    parsed = json.loads(raw)
                                                                      
                                               
    expected_keys = {
        "id",
        "type",
        "source",
        "retrieval_lane",
        "audit_event_id",
        "confidence",
        "rrf_score",
        "rrf_rank",
        "project_id",
        "payload",
    }
    assert set(parsed.keys()) == expected_keys
    assert parsed["type"] == "kg_node"
    assert parsed["source"] == "caronte_query"
    assert parsed["retrieval_lane"] == "semantic"


def test_envelope_from_json_round_trip():
    """JSON round-trip preserves equality (per-citation level)."""
    e = Envelope(
        id="c-roundtrip01",
        type=CitationType.FILE_SLICE,
        source=CitationSource.AGGREGATOR_FTS,
        retrieval_lane=RetrievalLane.LEXICAL,
        audit_event_id="evt-rt",
        confidence=0.5,
        rrf_score=0.012,
        rrf_rank=3,
        project_id="some-project",
        payload="some payload content",
    )
    raw = e.to_json()
    parsed = Envelope.from_json(raw)
    assert parsed == e


def test_envelope_with_expiration_and_platform_renders_round_trip():
    """Optional expiration + platform_renders round-trip preserved."""
    exp = datetime(2026, 12, 31, 23, 59, 59, tzinfo=timezone.utc)
    pr = {"web": {"callers": ["a.go:1", "b.go:2"]}, "ink": {"clickable": True}}
    e = Envelope(
        id="c-richexp0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-rich",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload",
        expiration=exp,
        platform_renders=pr,
    )
    raw = e.to_json()
    parsed = Envelope.from_json(raw)
    assert parsed == e
                                          
    d = json.loads(raw)
    assert "expiration" in d
    assert "platform_renders" in d


def test_envelope_validation_rejects_invalid_confidence():
    """Confidence must be in [0.0, 1.0]; ValueError otherwise."""
    with pytest.raises(ValueError, match="confidence"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=1.5,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_negative_confidence():
    with pytest.raises(ValueError, match="confidence"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=-0.01,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_empty_id():
    """Envelope.id must be non-empty."""
    with pytest.raises(ValueError, match="id"):
        Envelope(
            id="",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_empty_payload():
    """Envelope.payload must be non-empty."""
    with pytest.raises(ValueError, match="payload"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="",
        )


def test_envelope_validation_rejects_empty_audit_event_id():
    with pytest.raises(ValueError, match="audit_event_id"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_negative_rrf_score():
    with pytest.raises(ValueError, match="rrf_score"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=-0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_rrf_rank_below_minus_one():
    with pytest.raises(ValueError, match="rrf_rank"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=-2,
            project_id="p",
            payload="payload",
        )


def test_envelope_validation_rejects_empty_project_id():
    with pytest.raises(ValueError, match="project_id"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="",
            payload="payload",
        )


def test_envelope_validation_rejects_naive_expiration():
    """Expiration must be UTC-aware when set."""
    naive = datetime(2026, 12, 31, 23, 59, 59)         
    with pytest.raises(ValueError, match="expiration"):
        Envelope(
            id="c-test01234567",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload",
            expiration=naive,
        )


def test_envelope_audit_event_url():
    """audit_event_url returns canonical zen://audit/<id> deep-link."""
    e = Envelope(
        id="c-test01234567",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-abc123",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload",
    )
    assert e.audit_event_url() == "zen://audit/evt-abc123"


def test_envelope_from_dict_raises_value_error_on_missing_field():
    """from_dict raises ValueError on missing required key."""
    bad = {
        "id": "c-test01234567",
        "type": "kg_node",
                        
    }
    with pytest.raises(ValueError, match="from_dict"):
        Envelope.from_dict(bad)


def test_envelope_from_json_raises_value_error_on_invalid_json():
    """from_json raises ValueError on invalid JSON."""
    with pytest.raises(ValueError, match="invalid JSON"):
        Envelope.from_json("{not json")


def test_envelope_from_json_raises_value_error_on_non_object():
    with pytest.raises(ValueError, match="expected object"):
        Envelope.from_json("[1, 2, 3]")


                                                                             
                                                    
                                                                             


def test_augmentation_result_to_json_matches_go_wire(sample_augmentation_result):
    """AugmentationResult.to_json() emits keys matching Go AugmentationResult MarshalJSON."""
    raw = sample_augmentation_result.to_json()
    parsed = json.loads(raw)
    expected_keys = {
        "request_id",
        "session_id",
        "doctrine",
        "project_id",
        "emitted_at",
        "citations",
        "kg_token_count",
        "cache_key_hash",
        "audit_event_id",
        "static_context",
        "volatile_context",
    }
    assert set(parsed.keys()) == expected_keys
    citation_keys = {
        "id",
        "type",
        "source",
        "retrieval_lane",
        "audit_event_id",
        "confidence",
        "rrf_score",
        "rrf_rank",
        "project_id",
        "payload",
    }
    for c in parsed["citations"]:
        assert set(c.keys()) == citation_keys


def test_augmentation_result_from_json_round_trip(sample_augmentation_result):
    """JSON round-trip preserves equality (wrapper level)."""
    raw = sample_augmentation_result.to_json()
    parsed = AugmentationResult.from_json(raw)
    assert parsed == sample_augmentation_result


def test_augmentation_result_validation_rejects_invalid_doctrine():
    """Doctrine must be in {max-scope, default, capa-firewall}."""
    with pytest.raises(ValueError, match="doctrine"):
        AugmentationResult(
            request_id="req-1",
            session_id="sess-1",
            doctrine="invalid",
            project_id="p",
            citations=[],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )


def test_augmentation_result_validation_rejects_empty_request_id():
    with pytest.raises(ValueError, match="request_id"):
        AugmentationResult(
            request_id="",
            session_id="sess-1",
            doctrine="default",
            project_id="p",
            citations=[],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )


def test_augmentation_result_validation_rejects_empty_project_id():
    with pytest.raises(ValueError, match="project_id"):
        AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="",
            citations=[],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )


def test_augmentation_result_validation_rejects_negative_token_count():
    with pytest.raises(ValueError, match="kg_token_count"):
        AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="p",
            citations=[],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=-1,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )


def test_augmentation_result_validation_rejects_naive_emitted_at():
    naive = datetime(2026, 5, 10, 12, 0, 0)
    with pytest.raises(ValueError, match="emitted_at"):
        AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="p",
            citations=[],
            emitted_at=naive,
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )


def test_augmentation_result_from_json_raises_on_invalid_json():
    with pytest.raises(ValueError, match="invalid JSON"):
        AugmentationResult.from_json("{not json")


def test_augmentation_result_from_json_raises_on_non_object():
    with pytest.raises(ValueError, match="expected object"):
        AugmentationResult.from_json("[1, 2, 3]")


def test_augmentation_result_from_dict_raises_on_missing_field():
    with pytest.raises(ValueError, match="from_dict"):
        AugmentationResult.from_dict({"request_id": "r"})


def test_render_result_default_factory_independence():
    """RenderResult.metadata + audit_event_ids default factories are per-instance."""
    from hermes_plugins.hades.renderers.types import RenderResult

    a = RenderResult(platform=Platform.INK, output="x")
    b = RenderResult(platform=Platform.INK, output="y")
    a.metadata["foo"] = "bar"
    a.audit_event_ids.append("evt-1")
    assert b.metadata == {}
    assert b.audit_event_ids == []
