# SPDX-License-Identifier: MIT
"""EmailCitationRenderer tests: HTML email rendering with XSS-safe templating.

Output: single HTML5 string (full document) suitable for SMTP send. Hermes'
email adapter sets subject/recipient/from; the renderer owns body only.
"""

from __future__ import annotations

from datetime import datetime, timezone
from html.parser import HTMLParser
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers.email_citation import EmailCitationRenderer
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
    with patch.object(EmailCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        yield


def test_email_renderer_platform_is_email() -> None:
    r = EmailCitationRenderer()
    assert r.PLATFORM == Platform.EMAIL


def test_email_render_empty_augmentation_result_returns_html_doc(
    empty_augmentation_result: AugmentationResult,
) -> None:
    r = EmailCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.EMAIL
    html = result.output
    assert isinstance(html, str)
    assert html.startswith("<!DOCTYPE html>")
    assert "(no citations)" in html


def test_email_render_single_citation_emits_styled_block(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Single citation → table block with payload, source, audit link."""
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
    r = EmailCitationRenderer()
    result = r.render(single)
    html = result.output
                      
    assert "MergeEngine.SelectWinner" in html
                                         
    assert "caronte" in html
                      
    assert "0.94" in html
                
    assert 'href="zen://audit/evt-1234abcd"' in html
                                                                    
    assert 'style="' in html


def test_email_render_xss_in_payload_is_escaped() -> None:
    """Payload with <script> → HTML-escaped to &lt;script&gt; (XSS prevention)."""
    malicious = Envelope(
        id="c-xssaaaaaaa12",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-bad",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="<svg/onload=alert(1)>",
        payload=(
            "<script>alert('xss')</script>"
            "<img src=x onerror=alert(1)>"
            '<a href="javascript:alert(1)">x</a>'
        ),
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="zen-swarm",
        citations=[malicious],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-xss",
    )
    r = EmailCitationRenderer()
    html = r.render(wrapper).output
                                              
    assert "<script>alert('xss')</script>" not in html
                              
    assert "&lt;script&gt;" in html
    assert "&lt;img" in html
                             
    assert "&lt;svg" in html
                                                
    assert 'href="javascript:' not in html


def test_email_render_uses_table_layout(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = EmailCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert "<table" in html
    assert "</table>" in html


def test_email_render_max_width_600px(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = EmailCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert "max-width:600px" in html or "max-width: 600px" in html


def test_email_render_fallback_audit_url_when_configured(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """`web_fallback_audit_url=True` substitutes zen:// with https://<base>.

    Plan 18b Phase D: default `audit_web_base_url` rebranded to
    `https://hades.local`; this test asserts the operator-overridden
    form via the explicit constructor arg. The default-form coverage
    moved to `test_email_render_default_audit_web_base_url_is_hades`
    (added in D-2).
    """
    r = EmailCitationRenderer(
        web_fallback_audit_url=True,
        audit_web_base_url="https://hades.local",
    )
    html = r.render(sample_augmentation_result).output
    assert 'href="https://hades.local/audit/evt-1234abcd"' in html
    assert "https://hades.local/audit/evt-1234abcd" in html


def test_email_render_audit_web_base_url_trailing_slash_stripped() -> None:
    """Trailing slash on audit_web_base_url should not double up.

    Plan 18b Phase D: rebranded literal to `https://hades.local`.
    """
    citation = Envelope(
        id="c-baseurl0001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-base",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload",
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
    r = EmailCitationRenderer(
        web_fallback_audit_url=True,
        audit_web_base_url="https://hades.local/",
    )
    html = r.render(wrapper).output
    assert "https://hades.local//audit" not in html
    assert 'href="https://hades.local/audit/evt-base"' in html


def test_email_render_html_is_well_formed(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Output HTML is parseable (all tags closed; no unmatched ends)."""

    class _Counter(HTMLParser):
        def __init__(self) -> None:
            super().__init__()
            self.depth = 0
            self.errors = 0

        def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
            if tag.lower() not in {"br", "img", "hr", "meta", "link", "input"}:
                self.depth += 1

        def handle_endtag(self, tag: str) -> None:
            self.depth -= 1
            if self.depth < 0:
                self.errors += 1

    r = EmailCitationRenderer()
    html = r.render(sample_augmentation_result).output
    parser = _Counter()
    parser.feed(html)
    parser.close()
    assert parser.errors == 0
    assert parser.depth == 0


def test_email_render_includes_envelope_metadata_footer(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Footer notes doctrine + kg_token_count + cache_key + request_id."""
    r = EmailCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert sample_augmentation_result.doctrine in html
    assert str(sample_augmentation_result.kg_token_count) in html
    assert sample_augmentation_result.cache_key_hash in html
    assert sample_augmentation_result.request_id in html


def test_email_render_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = EmailCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine
    assert result.metadata["citation_count"] == 2


def test_email_render_audit_event_ids_count_matches_citations(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = EmailCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert len(result.audit_event_ids) == 2


def test_email_render_default_audit_web_base_url_is_hades(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Plan 18b Phase D: default `audit_web_base_url` rebranded from
    `https://zen-swarm.local` to `https://hades.local`.

    This test asserts the BARE constructor (no explicit
    `audit_web_base_url=` arg) produces the post-rebrand default. The
    operator-overridden form coverage lives in
    `test_email_render_fallback_audit_url_when_configured` and
    `test_email_render_audit_web_base_url_trailing_slash_stripped`.
    """
    r = EmailCitationRenderer(web_fallback_audit_url=True)
    html = r.render(sample_augmentation_result).output
    assert 'href="https://hades.local/audit/evt-1234abcd"' in html
    assert "https://zen-swarm.local" not in html


def test_email_render_html_title_is_hades_branded(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Plan 18b Phase D: `<title>` element wordmark rebranded from
    `zen-swarm citations` to `HADES citations`.

    This is the load-bearing brand-pass assertion for the HTML email
    <title> element — the surface a webmail client renders in the tab
    / window-title / preview-pane snippet. The wordmark MUST be
    HADES post-rebrand per spec §Q2 banner identity.
    """
    r = EmailCitationRenderer()
    html = r.render(sample_augmentation_result).output
    assert "<title>HADES citations" in html
    assert "zen-swarm citations" not in html
