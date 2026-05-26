# SPDX-License-Identifier: MIT
                                        
"""Web HTML5 + SVG renderer for citation envelopes (Plan 12 Phase A Task A-8)."""

from __future__ import annotations

from typing import Any

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)
from markupsafe import Markup, escape

_LOW_CONFIDENCE_THRESHOLD = 0.5
_HIGH_CONFIDENCE_THRESHOLD = 0.85
_SVG_CALLER_NODE_WIDTH = 80
_SVG_CALLER_NODE_HEIGHT = 24
_SVG_CALLER_NODE_GAP = 16
_SVG_CALLER_MAX_NODES = 5
_SVG_BLAST_MAX_RADIUS = 32
_PER_CITATION_PAYLOAD_CAP = 600

                                                                           
                                                                    
                                                     
                                                                
                                                   
_BLAST_LOW_THRESHOLD = 0.33
_BLAST_MID_THRESHOLD = 0.66
_BLAST_COLOR_LOW = "#28a745"
_BLAST_COLOR_MID = "#f6a623"
_BLAST_COLOR_HIGH = "#d73a49"


class WebCitationRenderer(Renderer):
    """Renders an `AugmentationResult` to an HTML5 + inline SVG fragment."""

    PLATFORM = Platform.WEB

    def render(self, result: AugmentationResult) -> RenderResult:
        if not result.citations:
            section = (
                '<section class="hades-citations" data-doctrine="'
                + str(escape(result.doctrine))
                + '"><p class="empty">No citations available.</p></section>'
            )
            return RenderResult(
                platform=Platform.WEB,
                output=section,
                metadata=self._build_wrapper_meta(result),
                audit_event_ids=[],
            )

        articles: list[str] = []
        audit_ids: list[str] = []
        for idx, citation in enumerate(result.citations, start=1):
            articles.append(self._build_article(citation, idx))
            audit_ids.append(
                self.audit_anchor(
                    citation,
                    doctrine=result.doctrine,
                    rendered_at=result.emitted_at,
                )
            )

        section = (
            '<section class="hades-citations" data-doctrine="'
            + str(escape(result.doctrine))
            + '" data-request-id="'
            + str(escape(result.request_id))
            + '">\n'
            + "\n".join(articles)
            + "\n</section>"
        )

        return RenderResult(
            platform=Platform.WEB,
            output=section,
            metadata=self._build_wrapper_meta(result),
            audit_event_ids=audit_ids,
        )

    def _build_article(self, citation: Envelope, index: int) -> str:
        payload_e = escape(citation.payload[:_PER_CITATION_PAYLOAD_CAP])
        source_e = escape(citation.source.value)
        lane_e = escape(citation.retrieval_lane.value)
        project_e = escape(citation.project_id)
        audit_id_e = escape(citation.audit_event_id)
                                                                         
                                                                       
                                                                     
                                                                
        audit_url_e = escape(citation.audit_event_url())
        citation_id_e = escape(citation.id)
        confidence_class = self._confidence_class(citation.confidence)
        confidence_str = f"{citation.confidence:.2f}"

                                                                  
        aside_parts: list[str] = []
        web_hints = citation.platform_renders.get("web", {})
        callers = web_hints.get("callers")
        if isinstance(callers, list) and callers:
            aside_parts.append(self._build_caller_chain_svg(callers))
        blast_score_raw = web_hints.get("blast_radius_score")
        blast_max_raw = web_hints.get("blast_radius_max", 100)
        if (
            isinstance(blast_score_raw, (int, float))
            and isinstance(blast_max_raw, (int, float))
            and blast_max_raw > 0
        ):
            aside_parts.append(
                self._build_blast_radius_svg(int(blast_score_raw), int(blast_max_raw))
            )

        aside_html = (
            f'<aside class="visualizations">{"".join(aside_parts)}</aside>'
            if aside_parts
            else ""
        )

        article = Markup("""
<article class="citation" data-citation-id="{citation_id}" data-audit-event-id="{audit_id}" data-index="{index}">
  <header>
    <span class="citation-index">[{index}]</span>
    <span class="citation-confidence-chip {confidence_class}" data-confidence="{confidence_value}">{confidence_str}</span>
  </header>
  <p class="citation-payload">{payload}</p>
  <footer class="citation-meta">
    <span class="meta-source">Source: <code>{source}</code></span>
    <span class="meta-lane">Lane: <code>{lane}</code></span>
    <span class="meta-project">Project: <code>{project}</code></span>
    <a class="meta-audit-link" href="{audit_url}">Audit chain &rarr;</a>
  </footer>
  {aside}
</article>
""").format(
            citation_id=citation_id_e,
            audit_id=audit_id_e,
            audit_url=audit_url_e,
            index=index,
            payload=payload_e,
            confidence_class=confidence_class,
            confidence_value=confidence_str,
            confidence_str=confidence_str,
            source=source_e,
            lane=lane_e,
            project=project_e,
            aside=Markup(aside_html),
        )
        return str(article)

    @staticmethod
    def _confidence_class(confidence: float) -> str:
        if confidence < _LOW_CONFIDENCE_THRESHOLD:
            return "confidence-low"
        if confidence > _HIGH_CONFIDENCE_THRESHOLD:
            return "confidence-high"
        return "confidence-normal"

    @staticmethod
    def _build_caller_chain_svg(callers: list[Any]) -> str:
        """Horizontal node-edge graph of caller chain (first 5 callers)."""
        capped = callers[:_SVG_CALLER_MAX_NODES]
        n = len(capped)
        width = n * _SVG_CALLER_NODE_WIDTH + (n - 1) * _SVG_CALLER_NODE_GAP + 20
        height = _SVG_CALLER_NODE_HEIGHT + 20

        parts: list[str] = [
            f'<svg class="caller-chain" width="{width}" height="{height}" '
            f'viewBox="0 0 {width} {height}" xmlns="http://www.w3.org/2000/svg">'
        ]

        for idx, caller in enumerate(capped):
            label = str(escape(str(caller)))
            x = 10 + idx * (_SVG_CALLER_NODE_WIDTH + _SVG_CALLER_NODE_GAP)
            y = 10
            parts.append(
                f'<rect x="{x}" y="{y}" width="{_SVG_CALLER_NODE_WIDTH}" '
                f'height="{_SVG_CALLER_NODE_HEIGHT}" rx="3" ry="3" '
                f'fill="#f6f8fa" stroke="#0366d6" stroke-width="1" />'
            )
            text_x = x + _SVG_CALLER_NODE_WIDTH // 2
            text_y = y + _SVG_CALLER_NODE_HEIGHT // 2 + 4
            parts.append(
                f'<text x="{text_x}" y="{text_y}" font-family="monospace" '
                f'font-size="10" text-anchor="middle" fill="#24292e">{label}</text>'
            )
            if idx < n - 1:
                edge_x1 = x + _SVG_CALLER_NODE_WIDTH
                edge_x2 = edge_x1 + _SVG_CALLER_NODE_GAP
                edge_y = y + _SVG_CALLER_NODE_HEIGHT // 2
                parts.append(
                    f'<line x1="{edge_x1}" y1="{edge_y}" x2="{edge_x2}" '
                    f'y2="{edge_y}" stroke="#586069" stroke-width="1" '
                    f'marker-end="url(#arrow)" />'
                )

        parts.append(
            '<defs><marker id="arrow" viewBox="0 0 10 10" refX="8" refY="5" '
            'markerWidth="6" markerHeight="6" orient="auto">'
            '<path d="M0,0 L10,5 L0,10 z" fill="#586069" /></marker></defs>'
        )
        parts.append("</svg>")
        return "".join(parts)

    @staticmethod
    def _build_blast_radius_svg(score: int, max_score: int) -> str:
        """Proportional bubble (sqrt-scale circle area) for blast-radius score.

        Color tier (constants are module-level so they survive a refactor
        without breaking visual regression — M-6 fix-cycle):
        - ``ratio < _BLAST_LOW_THRESHOLD``        → green ``_BLAST_COLOR_LOW``
        - ``ratio < _BLAST_MID_THRESHOLD``        → orange ``_BLAST_COLOR_MID``
        - ``ratio ≥ _BLAST_MID_THRESHOLD``        → red ``_BLAST_COLOR_HIGH``

        ``ratio`` = ``score / max_score`` (NOT the sqrt-scaled radius
        ratio used for area proportionality; the color tier reflects the
        operator-visible severity, not the geometric scaling).
        """
        ratio = (score / max_score) ** 0.5 if max_score > 0 else 0.0
        radius = max(4, int(_SVG_BLAST_MAX_RADIUS * ratio))
        diameter = _SVG_BLAST_MAX_RADIUS * 2 + 8
        cx = diameter // 2
        cy = diameter // 2
        severity = score / max_score if max_score > 0 else 0.0
        if severity < _BLAST_LOW_THRESHOLD:
            color = _BLAST_COLOR_LOW
        elif severity < _BLAST_MID_THRESHOLD:
            color = _BLAST_COLOR_MID
        else:
            color = _BLAST_COLOR_HIGH
        return (
            f'<svg class="blast-radius" width="{diameter}" height="{diameter}" '
            f'viewBox="0 0 {diameter} {diameter}" '
            f'xmlns="http://www.w3.org/2000/svg" '
            f'data-blast-radius="{score}" data-blast-max="{max_score}">'
            f'<circle cx="{cx}" cy="{cy}" r="{radius}" fill="{color}" '
            f'fill-opacity="0.6" stroke="{color}" stroke-width="1.5" />'
            f'<text x="{cx}" y="{cy + 4}" font-family="monospace" '
            f'font-size="11" text-anchor="middle" fill="#24292e">{score}</text>'
            f"</svg>"
        )
