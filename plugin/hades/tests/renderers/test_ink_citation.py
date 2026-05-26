# SPDX-License-Identifier: MIT
"""InkCitationRenderer tests: Hermes Ink TUI component-tree emission."""

from __future__ import annotations

from datetime import datetime, timezone
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers.ink_citation import InkCitationRenderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RetrievalLane,
)


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    """Avoid network calls in audit_anchor across all tests in this module."""
    with patch.object(InkCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        yield


def test_ink_renderer_platform_is_ink() -> None:
    r = InkCitationRenderer()
    assert r.PLATFORM == Platform.INK


def test_ink_render_empty_augmentation_result(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Zero citations → Box with single 'no citations' Text child."""
    r = InkCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.INK
    out = result.output
    assert isinstance(out, dict)
    assert out["type"] == "Box"
    children = out["children"]
    assert len(children) == 1
    text_node = children[0]
    assert text_node["type"] == "Text"
    assert text_node["props"]["dimColor"] is True
    assert text_node["children"] == ["(no citations)"]
    assert result.audit_event_ids == []


def test_ink_render_single_citation_emits_ref_plus_card(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """One citation → 2 children (CitationRef + CitationCard pair)."""
    truncated = AugmentationResult(
        request_id=sample_augmentation_result.request_id,
        session_id=sample_augmentation_result.session_id,
        doctrine=sample_augmentation_result.doctrine,
        project_id=sample_augmentation_result.project_id,
        citations=sample_augmentation_result.citations[:1],
        emitted_at=sample_augmentation_result.emitted_at,
        kg_token_count=sample_augmentation_result.kg_token_count,
        cache_key_hash=sample_augmentation_result.cache_key_hash,
        audit_event_id=sample_augmentation_result.audit_event_id,
        static_context=sample_augmentation_result.static_context,
        volatile_context=sample_augmentation_result.volatile_context,
    )
    r = InkCitationRenderer()
    result = r.render(truncated)
    out = result.output
    assert out["type"] == "Box"
    children = out["children"]
    assert len(children) == 2
    ref, card = children
    assert ref["type"] == "CitationRef"
    assert ref["props"]["citationId"] == "c-1234abcd0123"
    assert ref["props"]["index"] == 1
    assert ref["props"]["confidence"] == 0.94
    assert ref["props"]["onClick"] == "expand_citation:c-1234abcd0123"
    assert "MergeEngine" in ref["props"]["payload"]
    assert ref["props"]["source"] == "caronte_query"                  
    assert ref["props"]["retrievalLane"] == "semantic"
    assert ref["props"]["style"] == "high-confidence"

    assert card["type"] == "CitationCard"
    assert card["props"]["citationId"] == "c-1234abcd0123"
    assert card["props"]["initiallyCollapsed"] is True
    assert "MergeEngine" in card["props"]["payload"]
    assert card["props"]["source"] == "caronte_query"                  
    assert card["props"]["projectId"] == "zen-swarm"
    assert card["props"]["auditUrl"] == "zen://audit/evt-1234abcd"


def test_ink_render_multiple_citations_alternates_ref_and_card(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Two citations → 4 children (CitationRef, CitationCard, CitationRef, CitationCard)."""
    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    children = result.output["children"]
    assert len(children) == 4
    assert children[0]["type"] == "CitationRef"
    assert children[0]["props"]["index"] == 1
    assert children[1]["type"] == "CitationCard"
    assert children[2]["type"] == "CitationRef"
    assert children[2]["props"]["index"] == 2
    assert children[3]["type"] == "CitationCard"


def test_ink_render_audit_event_ids_count_matches_citations(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert len(result.audit_event_ids) == len(sample_augmentation_result.citations)
    assert all(eid == "evt-mocked" for eid in result.audit_event_ids)


def test_ink_render_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine
    assert result.metadata["citation_count"] == 2
    assert result.metadata["kg_token_count"] == 512


def test_ink_render_truncates_long_payload_with_ellipsis(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Payloads > 200 chars: card payload truncated + ellipsis appended."""
    long_payload = "x" * 500
    long_citation = Envelope(
        id="c-longxxxx0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-long",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload=long_payload,
    )
    truncated = AugmentationResult(
        request_id=sample_augmentation_result.request_id,
        session_id=sample_augmentation_result.session_id,
        doctrine=sample_augmentation_result.doctrine,
        project_id=sample_augmentation_result.project_id,
        citations=[long_citation],
        emitted_at=sample_augmentation_result.emitted_at,
        kg_token_count=sample_augmentation_result.kg_token_count,
        cache_key_hash=sample_augmentation_result.cache_key_hash,
        audit_event_id=sample_augmentation_result.audit_event_id,
        static_context=sample_augmentation_result.static_context,
        volatile_context=sample_augmentation_result.volatile_context,
    )
    r = InkCitationRenderer()
    result = r.render(truncated)
    card = result.output["children"][1]
    assert card["props"]["payload"].endswith("…")
    assert len(card["props"]["payload"]) <= 201                         


def test_ink_render_low_confidence_gets_low_confidence_style() -> None:
    """Confidence < 0.5 → CitationRef.style == 'low-confidence'."""
    c = Envelope(
        id="c-lowconf0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-low",
        confidence=0.3,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload-low",
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[c],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug",
    )
    r = InkCitationRenderer()
    result = r.render(wrapper)
    ref = result.output["children"][0]
    assert ref["props"]["style"] == "low-confidence"
    assert ref["props"]["confidence"] == 0.3


def test_ink_render_mid_confidence_normal_style() -> None:
    c = Envelope(
        id="c-midconf0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-mid",
        confidence=0.7,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload-mid",
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[c],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug",
    )
    r = InkCitationRenderer()
    result = r.render(wrapper)
    assert result.output["children"][0]["props"]["style"] == "normal"


def test_ink_render_handles_zero_confidence() -> None:
    """Confidence of exactly 0.0 → low-confidence; never raises."""
    c = Envelope(
        id="c-zero01234567",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-zero",
        confidence=0.0,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload-zero",
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[c],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug",
    )
    r = InkCitationRenderer()
    result = r.render(wrapper)
    ref = result.output["children"][0]
    assert ref["props"]["confidence"] == 0.0
    assert ref["props"]["style"] == "low-confidence"


def test_ink_render_box_has_margin_top_one(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Top-level Box props include marginTop: 1 for visual spacing."""
    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.output["props"]["marginTop"] == 1
    assert result.output["props"]["flexDirection"] == "column"


def test_ink_render_card_includes_retrieval_lane_and_rrf_rank(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Card props expose retrieval lane + RRF rank for operator inspection."""
    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    card = result.output["children"][1]
    assert card["props"]["retrievalLane"] == "semantic"
    assert card["props"]["rrfRank"] == 0


def test_ink_render_card_includes_platform_renders_when_present() -> None:
    """When citation has platform_renders.ink hints, card props.metadata reflects it."""
    c = Envelope(
        id="c-platformrnd",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-pr",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload-pr",
        platform_renders={"ink": {"preview": "1-line preview"}},
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[c],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug",
    )
    r = InkCitationRenderer()
    result = r.render(wrapper)
    card = result.output["children"][1]
                                                                    
    assert card["props"]["inkHints"] == {"preview": "1-line preview"}


def test_ink_render_no_zen_swarm_brand_string_in_component_tree(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Plan 18b Phase D regression guard: the Ink renderer carries NO
    `zen-swarm` substring in its rendered component tree.

    The Hermes Ink TUI renderer emits structured component-tree dicts
    (Box → CitationRef/CitationCard pairs); brand strings would only
    appear as VALUES inside props. This test serializes the entire
    output tree to JSON and asserts the absence of the legacy
    wordmark across ALL string values — recursive across nested
    children, props, and metadata.

    Test data (`project_id="zen-swarm"` in fixtures) is exempt
    because the renderer's `projectId` prop is a pass-through of
    the operator's project name (per inv-zen-031 boundary the
    renderer is purely a presentation layer). The check excludes
    `projectId` prop values from the brand-clean assertion.
    """
    import json

    r = InkCitationRenderer()
    result = r.render(sample_augmentation_result)
    tree_str = json.dumps(result.output)

                                                                       
                                                                     
                                                               
                                                                   
                                                    
    project_id_fixture = sample_augmentation_result.project_id
    stripped = tree_str.replace(f'"{project_id_fixture}"', '"<project_id>"')

    assert "zen-swarm" not in stripped, (
        f"Ink renderer output contains 'zen-swarm' substring outside "
        f"test-data projectId; full tree (project_id stripped): {stripped[:500]}"
    )


def test_ink_render_empty_branch_no_zen_swarm_brand_string(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Empty-result branch also brand-clean (separate emit path)."""
    import json

    r = InkCitationRenderer()
    result = r.render(empty_augmentation_result)
    tree_str = json.dumps(result.output)
    project_id_fixture = empty_augmentation_result.project_id
    stripped = tree_str.replace(f'"{project_id_fixture}"', '"<project_id>"')

    assert "zen-swarm" not in stripped, (
        f"Ink empty-branch output contains 'zen-swarm' substring; tree: {stripped[:500]}"
    )
