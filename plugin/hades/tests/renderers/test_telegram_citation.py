# SPDX-License-Identifier: MIT
"""TelegramCitationRenderer tests: MarkdownV2 + inline keyboard payload emission.

Renderer produces a list of Telegram sendMessage payloads (chunked at 4096
chars). Capa-firewall doctrine + ``active_project`` constructor arg filters
cross-project citations as privacy boundary integration.
"""

from __future__ import annotations

from datetime import datetime, timezone
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers.telegram_citation import (
    TELEGRAM_MAX_MESSAGE_CHARS,
    TelegramCitationRenderer,
    escape_markdown_v2,
)
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
    with patch.object(
        TelegramCitationRenderer, "audit_anchor", return_value="evt-mocked"
    ):
        yield


                                                                             
                          
                                                                             


def test_telegram_renderer_platform_is_telegram() -> None:
    r = TelegramCitationRenderer()
    assert r.PLATFORM == Platform.TELEGRAM


def test_escape_markdown_v2_escapes_each_reserved_char() -> None:
    """All reserved chars _*[]()~`>#+-=|{}.!\\ are escaped individually."""
    for ch in "_*[]()~`>#+-=|{}.!\\":
        out = escape_markdown_v2(ch)
        assert out == f"\\{ch}"


def test_escape_markdown_v2_chained_payload_escapes_all() -> None:
    raw = "Hello *world*! [link](url) ~strike~ `code`"
    escaped = escape_markdown_v2(raw)
    assert "\\*world\\*" in escaped
    assert "\\!" in escaped
    assert "\\[link\\]" in escaped
    assert "\\(url\\)" in escaped
    assert "\\~strike\\~" in escaped
    assert "\\`code\\`" in escaped


def test_escape_markdown_v2_escapes_dot_and_dash() -> None:
    """Dots and dashes (zen://audit URLs, version strings) must escape."""
    out = escape_markdown_v2("v1.0.0 - hello")
    assert "v1\\.0\\.0" in out
    assert "\\-" in out


                                                                             
           
                                                                             


def test_telegram_render_empty_augmentation_result(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Zero citations → single message with '(no citations)' note; no keyboard."""
    r = TelegramCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.TELEGRAM
    out = result.output
    assert isinstance(out, list)
    assert len(out) == 1
    msg = out[0]
    assert msg["text"] == "_\\(no citations\\)_"
    assert msg["parse_mode"] == "MarkdownV2"
    assert "reply_markup" not in msg
    assert result.audit_event_ids == []


def test_telegram_render_single_citation(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Single-citation wrapper → single message with footnote + inline keyboard."""
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
    r = TelegramCitationRenderer()
    result = r.render(truncated)
    assert len(result.output) == 1
    msg = result.output[0]
    assert msg["parse_mode"] == "MarkdownV2"
                                               
    assert "\\[1\\]" in msg["text"]
    assert "MergeEngine" in msg["text"]
                                                                              
    keyboard = msg["reply_markup"]["inline_keyboard"]
    assert len(keyboard) == 1
    buttons = keyboard[0]
    assert len(buttons) == 2
    assert buttons[0]["text"] == "Expand [1]"
    assert buttons[0]["callback_data"] == "c-1234abcd0123exp"
    assert buttons[1]["text"] == "Audit"
    assert buttons[1]["callback_data"] == "c-1234abcd0123aud"


def test_telegram_render_multi_citation_single_message(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Two citations fit a single message; inline keyboard has 2 rows (one per citation)."""
    r = TelegramCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert len(result.output) == 1
    msg = result.output[0]
    keyboard = msg["reply_markup"]["inline_keyboard"]
    assert len(keyboard) == 2
    assert keyboard[0][0]["callback_data"] == "c-1234abcd0123exp"
    assert keyboard[1][0]["callback_data"] == "c-5678efgh4567exp"


def test_telegram_chunks_large_wrapper_into_multiple_messages() -> None:
    """Many large citations → split across messages, each ≤4096 chars."""
    long_payload = "x" * 600
    citations = [
        Envelope(
            id=f"c-tg{i:010d}",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id=f"evt-{i:04d}",
            confidence=0.9,
            rrf_score=0.0150,
            rrf_rank=i,
            project_id="p",
            payload=f"T{i}: {long_payload}",
        )
        for i in range(20)
    ]
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=citations,
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-tg-many",
    )
    r = TelegramCitationRenderer()
    result = r.render(wrapper)
    assert len(result.output) > 1
    for msg in result.output:
        assert len(msg["text"]) <= TELEGRAM_MAX_MESSAGE_CHARS


def test_telegram_capa_firewall_filters_cross_project() -> None:
    """capa-firewall doctrine + active_project → drop cross-project citations."""
    citations = [
        Envelope(
            id="c-localaaaaaaaa",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.0150,
            rrf_rank=0,
            project_id="zen-swarm",
            payload="Local citation payload",
        ),
        Envelope(
            id="c-otherbbbbbbbb",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-2",
            confidence=0.9,
            rrf_score=0.0148,
            rrf_rank=1,
            project_id="other-project",
            payload="Cross-project citation payload",
        ),
    ]
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="capa-firewall",
        project_id="zen-swarm",
        citations=citations,
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-cf",
    )
    r = TelegramCitationRenderer(active_project="zen-swarm")
    result = r.render(wrapper)
    assert len(result.output) == 1
    text = result.output[0]["text"]
    assert "Local" in text
    assert "Cross-project" not in text
                                                      
    assert len(result.audit_event_ids) == 1


def test_telegram_capa_firewall_drops_all_returns_empty_note() -> None:
    """All citations cross-project + active_project set → '(no citations)' fallback."""
    cit = Envelope(
        id="c-othercccccccc",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-other",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="other-project",
        payload="Cross-project only",
    )
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="capa-firewall",
        project_id="zen-swarm",
        citations=[cit],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-cf2",
    )
    r = TelegramCitationRenderer(active_project="zen-swarm")
    result = r.render(wrapper)
    assert len(result.output) == 1
    msg = result.output[0]
    assert "(no citations)" in msg["text"].replace("\\", "")


def test_telegram_default_doctrine_does_not_filter_cross_project() -> None:
    """Default doctrine: cross-project citations preserved."""
    citations = [
        Envelope(
            id="c-localaaaaaaaa",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-1",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=0,
            project_id="zen-swarm",
            payload="Local",
        ),
        Envelope(
            id="c-otherbbbbbbbb",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id="evt-2",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=1,
            project_id="other-project",
            payload="OtherProj",
        ),
    ]
    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="zen-swarm",
        citations=citations,
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-dflt",
    )
    r = TelegramCitationRenderer(active_project="zen-swarm")
    result = r.render(wrapper)
    text = result.output[0]["text"]
    assert "Local" in text
    assert "OtherProj" in text


def test_telegram_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = TelegramCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine
    assert result.metadata["citation_count"] == 2


def test_telegram_render_no_zen_swarm_brand_string_in_messages(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """  regression guard: the Telegram renderer carries
    NO `zen-swarm` substring in its rendered message payloads.

    The Telegram renderer emits a list of `sendMessage` dicts; brand
    strings would appear inside `text` (MarkdownV2 body) or inside
    `inline_keyboard[][].text` (button labels). This test serializes
    all rendered message dicts to JSON and asserts the absence of the
    legacy wordmark across ALL string values.

    Test data (`project_id="zen-swarm"` in fixtures) is exempt — the
    renderer carries this value through to the footnote
    `Project: zen-swarm` body as operator-controlled data, not as
    brand. The check excludes the project_id fixture substring from
    the brand-clean assertion.
    """
    import json

    r = TelegramCitationRenderer()
    result = r.render(sample_augmentation_result)
    payloads_str = json.dumps(result.output)
    project_id_fixture = sample_augmentation_result.project_id
    stripped = payloads_str.replace(project_id_fixture, "<project_id>")
                                                                        
                                                 
    stripped = stripped.replace(project_id_fixture.replace("-", "\\\\-"), "<project_id>")

    assert "zen-swarm" not in stripped, (
        f"Telegram renderer output contains 'zen-swarm' substring "
        f"outside test-data project_id; payloads: {stripped[:500]}"
    )


def test_telegram_render_empty_branch_no_zen_swarm_brand_string(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Empty-result branch also brand-clean (separate emit path: single
    message with `_(no citations)_` body)."""
    import json

    r = TelegramCitationRenderer()
    result = r.render(empty_augmentation_result)
    payloads_str = json.dumps(result.output)

    assert "zen-swarm" not in payloads_str, (
        f"Telegram empty-branch output contains 'zen-swarm' substring; "
        f"payloads: {payloads_str[:500]}"
    )
