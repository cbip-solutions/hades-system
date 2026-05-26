# SPDX-License-Identifier: MIT
"""SlackCitationRenderer tests: Block Kit + mrkdwn payload emission.

Output: single dict matching Slack chat.postMessage payload (text + blocks +
attachments). Header + per-citation section + per-citation actions block.
"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers.slack_citation import (
    SLACK_MAX_BLOCKS,
    SlackCitationRenderer,
    escape_mrkdwn,
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
    with patch.object(SlackCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        yield


def test_slack_renderer_platform_is_slack() -> None:
    r = SlackCitationRenderer()
    assert r.PLATFORM == Platform.SLACK


def test_escape_mrkdwn_escapes_lt_gt_amp() -> None:
    out = escape_mrkdwn("Hello <world> & friends")
    assert "&lt;world&gt;" in out
    assert "&amp;" in out


def test_escape_mrkdwn_preserves_formatting_chars() -> None:
    """mrkdwn formatting chars (*, _, ~, `) NOT escaped — renderer controls them."""
    out = escape_mrkdwn("*bold* _italic_ ~strike~ `code`")
    assert "*bold*" in out
    assert "_italic_" in out
    assert "~strike~" in out
    assert "`code`" in out


def test_slack_render_empty_augmentation_result(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Zero citations → single section block with '(no citations)' note."""
    r = SlackCitationRenderer()
    result = r.render(empty_augmentation_result)
    assert result.platform == Platform.SLACK
    out = result.output
    assert isinstance(out, dict)
    blocks = out["blocks"]
    assert len(blocks) == 1
    assert blocks[0]["type"] == "section"
    assert "(no citations)" in blocks[0]["text"]["text"]


def test_slack_render_single_citation(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """One citation → header + 1 section + 1 actions = 3 blocks."""
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
    r = SlackCitationRenderer()
    result = r.render(single)
    blocks = result.output["blocks"]
    assert len(blocks) == 3
    assert blocks[0]["type"] == "header"
    assert "Citations" in blocks[0]["text"]["text"]

    section = blocks[1]
    assert section["type"] == "section"
    assert section["block_id"] == "citation_c-1234abcd0123"
    text = section["text"]["text"]
    assert "MergeEngine" in text
    assert "0.94" in text
    assert "caronte" in text.lower()                                          

    actions = blocks[2]
    assert actions["type"] == "actions"
    elements = actions["elements"]
    assert len(elements) == 2
    assert elements[0]["type"] == "button"
    assert elements[0]["action_id"] == "expand_c-1234abcd0123"
    assert elements[0]["text"]["text"] == "Expand"
    assert elements[1]["action_id"] == "audit_c-1234abcd0123"
    assert elements[1]["text"]["text"] == "Audit"


def test_slack_render_multiple_citations(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Two citations → header + 2 * (section + actions) = 5 blocks."""
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    blocks = result.output["blocks"]
    assert len(blocks) == 5


def test_slack_render_audit_button_url_links_zen_audit_when_configured(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """`use_url_for_audit=True` → audit button gets `url` field with zen://audit URL."""
    r = SlackCitationRenderer(use_url_for_audit=True)
    result = r.render(sample_augmentation_result)
    blocks = result.output["blocks"]
    actions = blocks[2]                            
    audit_button = actions["elements"][1]
    assert audit_button["url"] == "zen://audit/evt-1234abcd"


def test_slack_render_audit_button_has_no_url_by_default(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Default: no `url` field on audit button — Slack routes via action_id."""
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    actions = result.output["blocks"][2]
    audit_button = actions["elements"][1]
    assert "url" not in audit_button


def test_slack_render_attachments_legacy_fallback_present(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Legacy `attachments` field provides plain-text fallback for older clients."""
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    attachments = result.output["attachments"]
    assert len(attachments) == 1
    assert "fallback" in attachments[0]
    assert "MergeEngine" in attachments[0]["fallback"]
    assert "WorkforceQueue" in attachments[0]["fallback"]


def test_slack_render_truncates_at_block_ceiling() -> None:
    """Slack 50-block ceiling: many citations truncated with overflow note."""
    citations = [
        Envelope(
            id=f"c-slack{i:08d}",
            type=CitationType.KG_NODE,
            source=CitationSource.GITNEXUS_QUERY,
            retrieval_lane=RetrievalLane.SEMANTIC,
            audit_event_id=f"evt-{i:04d}",
            confidence=0.9,
            rrf_score=0.0150,
            rrf_rank=i,
            project_id="p",
            payload=f"T{i}: payload entry",
        )
        for i in range(50)
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
        audit_event_id="evt-aug-slack",
    )
    r = SlackCitationRenderer()
    result = r.render(wrapper)
    blocks = result.output["blocks"]
    assert len(blocks) <= SLACK_MAX_BLOCKS
    last = blocks[-1]
    assert last["type"] == "context"
                                                     
    text = json.dumps(last)
    assert "more" in text.lower() or "truncated" in text.lower()


def test_slack_render_metadata_carries_wrapper_provenance(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine
    assert result.metadata["citation_count"] == 2


def test_slack_render_section_includes_lane_and_project(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Section mrkdwn body surfaces lane + project for operator inspection."""
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    section_text = result.output["blocks"][1]["text"]["text"]
    assert "semantic" in section_text or "Semantic" in section_text
    assert "zen-swarm" in section_text


def test_slack_render_audit_event_ids_match_count(
    sample_augmentation_result: AugmentationResult,
) -> None:
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    assert len(result.audit_event_ids) == 2


def test_slack_render_text_field_is_hades_branded(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Plan 18b Phase D: Slack top-level `text` field rebranded from
    `Citations from zen-swarm augmentation pipeline` to
    `Citations from HADES augmentation pipeline`.

    The `text` field is Slack's notification preview / fallback text
    for clients that do not render Block Kit. It is the most visible
    brand surface in the Slack delivery channel.
    """
    r = SlackCitationRenderer()
    result = r.render(sample_augmentation_result)
    text = result.output["text"]
    assert "Citations from HADES augmentation pipeline" in text
    assert "zen-swarm" not in text
