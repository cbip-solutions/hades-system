# SPDX-License-Identifier: MIT
"""VoiceCitationRenderer tests: TTS-ready text emission + shell-injection defence.

Output: plain text consumable by Hermes voice mode (pipes through configured
TTS provider). Per spec §1 Q9 example (post-Plan-19: "Citing caronte query,
Tessera event 1234abc, confidence 94 percent"; source name from citation.source.value).
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
from hermes_plugins.hades.renderers.voice_citation import (
    SHELL_METACHARS,
    VoiceCitationRenderer,
    confidence_to_words,
    pronounce_event_id,
    sanitize_for_tts,
)


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    with patch.object(VoiceCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        yield


                                                                             
         
                                                                             


def test_voice_renderer_platform_is_voice() -> None:
    r = VoiceCitationRenderer()
    assert r.PLATFORM == Platform.VOICE


def test_sanitize_for_tts_strips_shell_metachars() -> None:
    raw = "hello$world`cmd`;rm -rf /\nnewline\rcr|pipe&amp<>backslash\\trail"
    out = sanitize_for_tts(raw)
    for ch in SHELL_METACHARS:
        assert ch not in out, f"metachar {ch!r} survived in {out!r}"


def test_sanitize_for_tts_preserves_normal_punctuation() -> None:
    raw = "Hello, world. Is this OK? Yes!"
    out = sanitize_for_tts(raw)
    assert "Hello" in out
    assert "world" in out
    assert "," in out
    assert "." in out


def test_sanitize_for_tts_strips_markdown_formatting() -> None:
    """Markdown formatting chars (`*_~[]()#{}=+`) replaced with space."""
    raw = "**bold** *italic* _under_ ~strike~ [link](url) #hash {brace}"
    out = sanitize_for_tts(raw)
    for ch in "*_~[]()#{}=+":
        assert ch not in out, f"markdown char {ch!r} survived"


def test_confidence_to_words_handles_zero_low_mid_high_full() -> None:
    assert "zero percent" in confidence_to_words(0.0).lower()
    assert "five percent" in confidence_to_words(0.05).lower()
    assert "fifty percent" in confidence_to_words(0.50).lower()
    assert "fifty-five percent" in confidence_to_words(0.55).lower()
    assert "ninety-four percent" in confidence_to_words(0.94).lower()
    assert "one hundred percent" in confidence_to_words(1.0).lower()


def test_confidence_to_words_clamps_invalid_input() -> None:
    """Out-of-range inputs clamp to [0, 100] without raising."""
    assert "one hundred percent" in confidence_to_words(1.5).lower()
    assert "zero percent" in confidence_to_words(-0.1).lower()


def test_pronounce_event_id_spells_hex_letters() -> None:
    """Event ID 'evt-1234abcd' → 'event 1234 A B C D' or equivalent."""
    out = pronounce_event_id("evt-1234abcd")
    assert "1234" in out
                                                                         
    assert "A B C D" in out
                                         
    assert "event" in out.lower()


def test_pronounce_event_id_no_evt_prefix_uses_event_id_label() -> None:
    """Event ID without `evt-` prefix → 'event id <body>' or equivalent."""
    out = pronounce_event_id("xyz123")
    assert "X Y Z" in out
    assert "123" in out


                                                                             
           
                                                                             


def test_voice_render_empty_augmentation_result(
    empty_augmentation_result: AugmentationResult,
) -> None:
    r = VoiceCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.VOICE
    assert "no citations" in result.output.lower()
    assert result.audit_event_ids == []


def test_voice_render_single_citation_pattern(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Single citation per spec §1 Q9 example pattern."""
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
    r = VoiceCitationRenderer()
    text = r.render(single).output
    assert (
        "caronte" in text.lower()
    )                                                            
    assert "ninety-four percent" in text.lower()
                                        
    assert "1234" in text


def test_voice_render_caps_word_count_for_long_payloads() -> None:
    """Long payloads truncated to fit ~30-second TTS budget."""
    long_payload = " ".join(["word"] * 200)
    citation = Envelope(
        id="c-voicelong01",
        type=CitationType.KG_NODE,
        source=CitationSource.CARONTE_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-long",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload=long_payload,
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
        audit_event_id="evt-aug-voice-long",
    )
    r = VoiceCitationRenderer()
    text = r.render(wrapper).output
    word_count = len(text.split())
    assert word_count <= 100                                           


def test_voice_render_shell_injection_neutralised() -> None:
    """Payload with shell metacharacters → sanitized in TTS output."""
    malicious = Envelope(
        id="c-voicebad01",
        type=CitationType.KG_NODE,
        source=CitationSource.CARONTE_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-bad",
        confidence=0.9,
        rrf_score=0.0150,
        rrf_rank=0,
        project_id="p",
        payload="evil`rm -rf /`title$(cmd) and $(echo pwned)",
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
        audit_event_id="evt-aug-voice-bad",
    )
    r = VoiceCitationRenderer()
    text = r.render(wrapper).output
    for ch in SHELL_METACHARS:
        assert ch not in text, f"shell metachar {ch!r} survived"


def test_voice_render_emits_audit_event_per_citation(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = VoiceCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert len(result.audit_event_ids) == len(sample_augmentation_result.citations)


def test_voice_render_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = VoiceCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine


def test_voice_render_caps_total_word_budget_for_many_citations() -> None:
    """Many citations collectively cap at ~total budget."""
    citations = [
        Envelope(
            id=f"c-vbudget{i:05d}",
            type=CitationType.KG_NODE,
            source=CitationSource.CARONTE_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id=f"evt-{i:04d}",
            confidence=0.9,
            rrf_score=0.01,
            rrf_rank=i,
            project_id="p",
            payload=" ".join(["word"] * 50),
        )
        for i in range(10)
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
        audit_event_id="evt-aug-vbudget",
    )
    r = VoiceCitationRenderer()
    text = r.render(wrapper).output
    word_count = len(text.split())
    assert word_count <= 100                      


def test_voice_render_no_zen_swarm_brand_string_in_tts_output(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Plan 18b Phase D regression guard: the voice TTS renderer
    carries NO `zen-swarm` substring in its rendered text.

    The voice renderer emits TTS-ready plain text shaped by the spec
    §1 Q9 sentence pattern (`Citing <source>: <payload>, event <id>,
    confidence <pct> percent.`). No hardcoded wordmark literals
    appear; the source comes from `citation.source.value` (e.g.,
    `caronte_query` → `caronte query` after underscore-strip).

    Test fixture `project_id="zen-swarm"` is NOT rendered into the
    TTS output — voice renderer drops project_id per the §1 Q9
    template (source + payload + event-id + confidence only). So the
    assertion is direct: no `zen-swarm` substring anywhere in the
    output.
    """
    r = VoiceCitationRenderer()
    text = r.render(sample_augmentation_result).output

    assert "zen-swarm" not in text, (
        f"Voice TTS output contains 'zen-swarm' substring; output: {text!r}"
    )
                                                           
                                                               
    assert "zen swarm" not in text.lower(), (
        f"Voice TTS output contains 'zen swarm' (underscore-stripped) "
        f"substring; output: {text!r}"
    )


def test_voice_render_empty_branch_no_zen_swarm_brand_string(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Empty-result branch also brand-clean (separate emit path:
    single sentence `"No citations available."`)."""
    r = VoiceCitationRenderer()
    text = r.render(empty_augmentation_result).output

    assert "zen-swarm" not in text, (
        f"Voice empty-branch TTS output contains 'zen-swarm' substring; output: {text!r}"
    )
    assert "zen swarm" not in text.lower(), (
        f"Voice empty-branch TTS output contains 'zen swarm' substring; output: {text!r}"
    )
