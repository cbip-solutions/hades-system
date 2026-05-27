# SPDX-License-Identifier: MIT
"""WebCitationRenderer tests: HTML5 + inline SVG fragment emission.

Output: HTML5 fragment (no <html>/<body> wrapper — web UI embeds inside its
layout shell). Per-citation <article> with header/excerpt/footer/optional
aside (SVG visualizations).
"""

from __future__ import annotations

from datetime import datetime, timezone
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RetrievalLane,
)
from hermes_plugins.hades.renderers.web_citation import WebCitationRenderer


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    with patch.object(WebCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        yield


def test_web_renderer_platform_is_web() -> None:
    r = WebCitationRenderer()
    assert r.PLATFORM == Platform.WEB


def test_web_render_empty_augmentation_result_returns_empty_section(
    empty_augmentation_result: AugmentationResult,
) -> None:
    r = WebCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.WEB
    html = result.output
    assert "<section" in html
    assert "No citations" in html


def test_web_render_single_citation_emits_article(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Single citation → <article> with payload, source, audit link, confidence."""
    single = AugmentationResult(
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
    r = WebCitationRenderer()
    html = r.render(single).output
    assert "<article" in html
    assert "MergeEngine.SelectWinner" in html
    assert "caronte" in html                                          
    assert 'href="zen://audit/evt-1234abcd"' in html
    assert "0.94" in html


def test_web_render_emits_caller_chain_svg_via_platform_renders() -> None:
    """citation.platform_renders['web']['callers'] → inline <svg> caller-chain graph."""
    citation = Envelope(
        id="c-webcaller01",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-callerchain",
        platform_renders={
            "web": {"callers": ["dispatcher.go:67", "main.go:42", "init.go:18"]}
        },
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[citation],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-callerchain",
    )
    r = WebCitationRenderer()
    html = r.render(wrapper).output
    assert "<svg" in html
    assert "dispatcher.go" in html
    assert "main.go" in html
    assert "init.go" in html


def test_web_render_emits_blast_radius_svg_when_score_present() -> None:
    """citation.platform_renders['web']['blast_radius_score'] → inline <svg> circle."""
    citation = Envelope(
        id="c-webblast001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-blast",
        platform_renders={"web": {"blast_radius_score": 25, "blast_radius_max": 100}},
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[citation],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-blast",
    )
    r = WebCitationRenderer()
    html = r.render(wrapper).output
    assert "<svg" in html
    assert "<circle" in html
    assert 'data-blast-radius="25"' in html


def test_web_render_xss_in_payload_escaped() -> None:
    """Payload with <script> → HTML-escaped to &lt;script&gt;."""
    malicious = Envelope(
        id="c-webxss0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="<script>alert(1)</script>",
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[malicious],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-xss",
    )
    r = WebCitationRenderer()
    html = r.render(wrapper).output
    assert "<script>alert(1)</script>" not in html
    assert "&lt;script&gt;" in html


def test_web_render_confidence_chip_class_varies_by_tier() -> None:
    """Confidence chip CSS class corresponds to tier (low/normal/high)."""
    cases = [
        (0.3, "confidence-low"),
        (0.7, "confidence-normal"),
        (0.95, "confidence-high"),
    ]
    for conf, expected_class in cases:
        citation = Envelope(
            id="c-webconf0001",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=conf,
            rrf_score=0.0150,
            rrf_rank=0,
            project_id="p",
            payload="payload-conf",
        )
        wrapper = AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="p",
            citations=[citation],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-aug-conf",
        )
        r = WebCitationRenderer()
        html = r.render(wrapper).output
        assert expected_class in html, f"confidence={conf}: missing {expected_class}"


def test_web_render_emits_data_attributes_for_js_hooks(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """data-citation-id + data-audit-event-id present for futuro web UI hooks."""
    r = WebCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert 'data-citation-id="c-1234abcd0123"' in html
    assert 'data-audit-event-id="evt-1234abcd"' in html


def test_web_render_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = WebCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["citation_count"] == len(sample_augmentation_result.citations)


def test_web_render_section_carries_data_doctrine(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Top-level <section> has data-doctrine attribute for stylesheet variants."""
    r = WebCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert 'data-doctrine="default"' in html


def test_web_render_blast_radius_svg_color_changes_with_score() -> None:
    """Low score → green; mid → orange; high → red."""
    cases = [
        (10, "#28a745"),                
        (50, "#f6a623"),       
        (90, "#d73a49"),        
    ]
    for score, color in cases:
        citation = Envelope(
            id="c-webblastclr",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="p",
            payload="payload-blastclr",
            platform_renders={
                "web": {"blast_radius_score": score, "blast_radius_max": 100}
            },
        )
        wrapper = AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="p",
            citations=[citation],
            emitted_at=datetime.now(timezone.utc),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-aug",
        )
        r = WebCitationRenderer()
        html = r.render(wrapper).output
        assert color in html, f"score={score}: expected color {color}"


def test_web_render_caps_caller_chain_at_first_five() -> None:
    """Caller chain visualization caps at 5 callers (visualization clarity)."""
    callers = [f"caller_{i}.go:{i}" for i in range(10)]
    citation = Envelope(
        id="c-webcallerca",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload-callerca",
        platform_renders={"web": {"callers": callers}},
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[citation],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-callerca",
    )
    r = WebCitationRenderer()
    html = r.render(wrapper).output
    for i in range(5):
        assert f"caller_{i}.go" in html
                                                           
    assert "caller_5.go" not in html
    assert "caller_6.go" not in html


def test_web_render_section_uses_hades_citations_css_class(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """ : top-level <section> CSS class rebranded from
    `zen-citations` to `hades-citations`.

    This is the document-fragment stylesheet hook the future +
    web UI targets (`<style>.hades-citations {... }</style>`). The
    brand-pass rebrands the class name so downstream consumers
    converge on the HADES wordmark.
    """
    r = WebCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert 'class="hades-citations"' in html
    assert 'class="zen-citations"' not in html


def test_web_render_empty_section_uses_hades_citations_css_class(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """ : empty-result fallback <section> also uses
    `hades-citations` class.

    The empty-state branch is a separate emit path; both branches MUST
    converge on the same CSS class for stylesheet consistency.
    """
    r = WebCitationRenderer()
    html = r.render(empty_augmentation_result).output
    assert 'class="hades-citations"' in html
    assert 'class="zen-citations"' not in html
