# SPDX-License-Identifier: MIT
"""Coverage-gap tests: targeted micro-tests to drive uncovered branches.

These exist solely to reach 100% coverage on the renderers module per
spec §6.6 (≥85% with security-critical ≥90%; max-scope target 100%).

Each test corresponds to a specific uncovered branch surfaced by:
    pytest --cov=plugin/zen-swarm/renderers --cov-report=term-missing
"""

from __future__ import annotations

from datetime import UTC, datetime
from unittest.mock import MagicMock, patch

import pytest
from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RenderResult,
    RetrievalLane,
)
from hermes_plugins.hades.renderers.voice_citation import pronounce_event_id

                                                                             
                                                                                    
 
                                                                      
                                                      
                                                                               
                                                                             


class _NoopRenderer(Renderer):
    PLATFORM = Platform.INK

    def render(self, result: AugmentationResult) -> RenderResult:
        return RenderResult(platform=self.PLATFORM, output="")


def _citation_for_audit() -> Envelope:
    return Envelope(
        id="c-audit01234567",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-audit-test",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload",
    )


def test_audit_anchor_returns_event_id_on_success() -> None:
    """Successful POST returns the ``id`` field from the AuditEventOut response."""
    r = _NoopRenderer()
    citation = _citation_for_audit()

    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {
        "id": "evt-from-server",
        "accepted": True,
        "emitted_at": 1715856000,
    }
    with patch("httpx.Client") as mock_client_cls:
        client_instance = mock_client_cls.return_value.__enter__.return_value
        client_instance.post.return_value = mock_response
        out = r.audit_anchor(
            citation,
            doctrine="default",
            audit_endpoint="http://x:1/v1/audit/emit",
        )
    assert out == "evt-from-server"


def test_audit_anchor_returns_empty_when_response_not_dict() -> None:
    """If daemon returns non-dict JSON (defensive), audit_anchor returns ''."""
    r = _NoopRenderer()
    citation = _citation_for_audit()

    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = ["not", "a", "dict"]
    with patch("httpx.Client") as mock_client_cls:
        client_instance = mock_client_cls.return_value.__enter__.return_value
        client_instance.post.return_value = mock_response
        out = r.audit_anchor(
            citation,
            doctrine="default",
            audit_endpoint="http://x:1/v1/audit/emit",
        )
    assert out == ""


                                                                             
                                                                  
                                                                             


def test_renderer_subclass_without_render_skips_platform_check() -> None:
    """Partial subclass (no concrete render) is still abstract → no PLATFORM check."""

    class _PartialBase(Renderer):
                                                                          
        pass

                                                               
    assert issubclass(_PartialBase, Renderer)


def test_renderer_concrete_subclass_inheriting_platform_does_not_raise() -> None:
    """Concrete subclass inheriting PLATFORM from a parent does NOT need to redeclare it."""

    class _MidConcreteRenderer(Renderer):
        PLATFORM = Platform.WEB

        def render(self, result: AugmentationResult) -> RenderResult:
            return RenderResult(platform=self.PLATFORM, output="")

    class _LeafRenderer(_MidConcreteRenderer):
                                                                            
        def render(self, result: AugmentationResult) -> RenderResult:
            return RenderResult(platform=self.PLATFORM, output="leaf")

    leaf = _LeafRenderer()
    assert leaf.PLATFORM == Platform.WEB
    result = leaf.render(
        AugmentationResult(
            request_id="r",
            session_id="s",
            doctrine="default",
            project_id="p",
            citations=[],
            emitted_at=datetime.now(UTC),
            kg_token_count=0,
            cache_key_hash="sha256:0",
            audit_event_id="evt-1",
        )
    )
    assert result.output == "leaf"


                                                                             
                                                   
                                                                             


def test_pronounce_event_id_handles_embedded_separators() -> None:
    """Event ID with hyphens/underscores: separators trigger chunk-flush path."""
    out = pronounce_event_id("evt-abc-123-xyz")
                                                               
    assert "A B C" in out
    assert "123" in out
    assert "X Y Z" in out


def test_pronounce_event_id_handles_all_letters_no_evt_prefix() -> None:
    """No evt- prefix, no digits: still produces spelled letters."""
    out = pronounce_event_id("abc")
    assert "A B C" in out
    assert "event id" in out.lower()


def test_pronounce_event_id_handles_alternating_letters_digits() -> None:
    """Alternating letters and digits → separate chunks."""
    out = pronounce_event_id("evt-a1b2c3")
    assert "A" in out
    assert "1" in out
    assert "B" in out
    assert "2" in out


                                                                             
                                                                 
                                                                             
 
                                                                         
                                                                       
# members do not exist (the enum is a closed set), so the `if not self.X`
                                                                         
                                                                         
                                                                       
                                                                    
                                                                           
                                       
 
                                                                           
                                                                          
                                                                


                                                                             
                                                                              
                                                   
                                                                             


@pytest.mark.parametrize("platform", list(Platform))
def test_render_result_constructible_for_every_platform(platform: Platform) -> None:
    rr = RenderResult(platform=platform, output="x")
    assert rr.platform == platform
    assert rr.metadata == {}
    assert rr.audit_event_ids == []
