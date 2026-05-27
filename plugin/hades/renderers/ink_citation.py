# SPDX-License-Identifier: MIT
                                        
"""Hermes Ink TUI renderer for citation envelopes."""

from __future__ import annotations

from typing import Any

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)

_PAYLOAD_MAX_CHARS = 200
_LOW_CONFIDENCE_THRESHOLD = 0.5
_HIGH_CONFIDENCE_THRESHOLD = 0.85


class InkCitationRenderer(Renderer):
    """Renders an `AugmentationResult` as a Hermes Ink component tree."""

    PLATFORM = Platform.INK

    def render(self, result: AugmentationResult) -> RenderResult:
        if not result.citations:
            return RenderResult(
                platform=Platform.INK,
                output=self._empty_box(),
                metadata=self._build_wrapper_meta(result, include_cache_key=True),
                audit_event_ids=[],
            )

        children: list[dict[str, Any]] = []
        audit_ids: list[str] = []
        for idx, citation in enumerate(result.citations, start=1):
            children.append(self._build_citation_ref(citation, idx))
            children.append(self._build_citation_card(citation))
            audit_ids.append(
                self.audit_anchor(
                    citation,
                    doctrine=result.doctrine,
                    rendered_at=result.emitted_at,
                )
            )

        return RenderResult(
            platform=Platform.INK,
            output={
                "type": "Box",
                "props": {"flexDirection": "column", "marginTop": 1},
                "children": children,
            },
            metadata=self._build_wrapper_meta(result, include_cache_key=True),
            audit_event_ids=audit_ids,
        )

    @staticmethod
    def _empty_box() -> dict[str, Any]:
        """Placeholder Box for the zero-citations case."""
        return {
            "type": "Box",
            "props": {"flexDirection": "column", "marginTop": 1},
            "children": [
                {
                    "type": "Text",
                    "props": {"dimColor": True},
                    "children": ["(no citations)"],
                }
            ],
        }

    def _build_citation_ref(self, citation: Envelope, index: int) -> dict[str, Any]:
        """Footnote-style collapsed CitationRef component."""
        return {
            "type": "CitationRef",
            "props": {
                "citationId": citation.id,
                "index": index,
                "payload": self._truncate(citation.payload),
                "confidence": citation.confidence,
                "source": citation.source.value,
                "retrievalLane": citation.retrieval_lane.value,
                "onClick": f"expand_citation:{citation.id}",
                "style": self._confidence_style(citation.confidence),
            },
        }

    def _build_citation_card(self, citation: Envelope) -> dict[str, Any]:
        """Expandable detail-panel CitationCard component."""
        ink_hints = citation.platform_renders.get("ink", {})
        return {
            "type": "CitationCard",
            "props": {
                "citationId": citation.id,
                "payload": self._truncate(citation.payload),
                "source": citation.source.value,
                "retrievalLane": citation.retrieval_lane.value,
                "projectId": citation.project_id,
                "auditUrl": citation.audit_event_url(),
                "initiallyCollapsed": True,
                "confidence": citation.confidence,
                "rrfRank": citation.rrf_rank,
                "rrfScore": citation.rrf_score,
                "type": citation.type.value,
                "inkHints": dict(ink_hints),
            },
        }

    @staticmethod
    def _truncate(payload: str) -> str:
        """Cap payload to terminal-friendly length; append ellipsis on overflow."""
        if len(payload) <= _PAYLOAD_MAX_CHARS:
            return payload
        return payload[:_PAYLOAD_MAX_CHARS] + "…"

    @staticmethod
    def _confidence_style(confidence: float) -> str:
        """Map confidence to visual tier: low / normal / high."""
        if confidence < _LOW_CONFIDENCE_THRESHOLD:
            return "low-confidence"
        if confidence > _HIGH_CONFIDENCE_THRESHOLD:
            return "high-confidence"
        return "normal"
