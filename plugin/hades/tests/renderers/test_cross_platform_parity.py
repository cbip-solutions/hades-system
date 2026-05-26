# SPDX-License-Identifier: MIT
"""Cross-platform parity: same envelope produces equivalent citations across renderers."""

from __future__ import annotations

import json
from typing import Any
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.email_citation import EmailCitationRenderer
from hermes_plugins.hades.renderers.ink_citation import InkCitationRenderer
from hermes_plugins.hades.renderers.slack_citation import SlackCitationRenderer
from hermes_plugins.hades.renderers.telegram_citation import (
    TelegramCitationRenderer,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Platform,
)
from hermes_plugins.hades.renderers.voice_citation import VoiceCitationRenderer
from hermes_plugins.hades.renderers.web_citation import WebCitationRenderer


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    """Avoid network in audit_anchor across all renderers in this module."""
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        yield


def _all_renderers() -> list[Renderer]:
    """All 6 platform renderers; markdown fallback is registry-internal."""
    return [
        InkCitationRenderer(),
        TelegramCitationRenderer(),
        SlackCitationRenderer(),
        EmailCitationRenderer(),
        VoiceCitationRenderer(),
        WebCitationRenderer(),
    ]


def _output_contains(output: Any, needle: str) -> bool:
    """Search renderer output (str/dict/list) for `needle`."""
    if isinstance(output, str):
        return needle in output
    if isinstance(output, (dict, list)):
        return needle in json.dumps(output, sort_keys=True)
    return False


                                                                             
                                
                                                                             


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_audit_event_ids_count_matches_citations(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Each renderer emits one audit event per citation (parity invariant)."""
    result = renderer.render(sample_augmentation_result)
    assert len(result.audit_event_ids) == len(sample_augmentation_result.citations), (
        f"{renderer.PLATFORM.value}: audit_event_ids count mismatch"
    )


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_produce_non_empty_output(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Non-empty envelope MUST produce non-empty output (regression)."""
    result = renderer.render(sample_augmentation_result)
    assert result.output, (
        f"{renderer.PLATFORM.value}: empty output for non-empty envelope"
    )


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_metadata_includes_request_id(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """metadata MUST carry request_id across all platforms."""
    result = renderer.render(sample_augmentation_result)
    assert "request_id" in result.metadata, (
        f"{renderer.PLATFORM.value}: request_id missing"
    )
    assert result.metadata["request_id"] == sample_augmentation_result.request_id


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_handle_empty_augmentation_result_without_error(
    renderer: Renderer,
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Empty wrapper MUST NOT raise (does not throw)."""
                                                                                  
                                                                     
                                                                      
                 
    result = renderer.render(empty_augmentation_result)
    assert result is not None
    assert result.platform == renderer.PLATFORM


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_reference_citation_or_audit_in_output(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Each renderer's output references citation_id OR audit_event_id.

    Voice renderer is exempt from literal-string match (TTS pronounces
    event_id letter-by-letter — 'evt-1234abcd' becomes spoken form). Voice
    still includes the numeric portion of the event_id verbatim.
    """
    result = renderer.render(sample_augmentation_result)
    citation = sample_augmentation_result.citations[0]
    if renderer.PLATFORM == Platform.VOICE:
                                                                      
                                                             
        text = (
            result.output if isinstance(result.output, str) else json.dumps(result.output)
        )
        assert "1234" in text, f"{renderer.PLATFORM.value}: numeric ID portion missing"
        return
    has_id = _output_contains(result.output, citation.id) or _output_contains(
        result.output, citation.audit_event_id
    )
    assert has_id, (
        f"{renderer.PLATFORM.value}: neither citation.id nor audit_event_id "
        "present in output"
    )


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_preserve_doctrine_in_metadata(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """doctrine field MUST be preserved through render (operator visibility)."""
    result = renderer.render(sample_augmentation_result)
    assert "doctrine" in result.metadata, (
        f"{renderer.PLATFORM.value}: doctrine missing from metadata"
    )
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine


@pytest.mark.parametrize("renderer", _all_renderers())
def test_all_renderers_audit_anchor_invocation_count(
    renderer: Renderer,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Every renderer calls audit_anchor exactly once per citation."""
    with patch.object(
        type(renderer), "audit_anchor", return_value="evt-mocked"
    ) as mock_anchor:
        renderer.render(sample_augmentation_result)
        assert mock_anchor.call_count == len(sample_augmentation_result.citations), (
            f"{renderer.PLATFORM.value}: audit_anchor invocation count mismatch"
        )
