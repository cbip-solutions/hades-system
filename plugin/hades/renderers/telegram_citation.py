# SPDX-License-Identifier: MIT
# plugin/hades/renderers/telegram_citation.py
"""Telegram Bot API renderer for citation envelopes (the release design release track Task A-4)."""

from __future__ import annotations

import re
from typing import Any

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)

TELEGRAM_MAX_MESSAGE_CHARS = 4096

# MarkdownV2 reserved chars per Telegram Bot API spec. Backslash escapes
# everything in the class — backslash itself is also escaped (otherwise the
# escaped output would contain unbalanced sequences).
_MARKDOWN_V2_SPECIAL_RE = re.compile(r"([_*\[\]()~`>#+\-=|{}.!\\])")
# Cap excerpt portion per citation so a single citation cannot blow the
# 4096-char budget on its own.
_PER_CITATION_PAYLOAD_CAP = 240


def escape_markdown_v2(text: str) -> str:
    """Escape MarkdownV2 reserved characters per Telegram Bot API spec.

    Reserved chars: ``_*[]()~`>#+-=|{}.!\\``. Each occurrence is prefixed
    with a single backslash so the rendered message preserves the raw text
    verbatim instead of interpreting it as Markdown.
    """
    return _MARKDOWN_V2_SPECIAL_RE.sub(r"\\\1", text)


class TelegramCitationRenderer(Renderer):
    """Renders an `AugmentationResult` to a list of Telegram sendMessage payloads."""

    PLATFORM = Platform.TELEGRAM

    def __init__(
        self,
        active_project: str | None = None,
        *,
        daemon_url: str | None = None,
    ) -> None:
        """``active_project``: when set + doctrine == 'capa-firewall', drop foreign citations.

        ``daemon_url`` is forwarded to the base ``Renderer`` so the
        instance-level audit endpoint (``self._audit_endpoint``) honors
        an operator-configured daemon TCP URL when supplied (Fix-cycle
        I-1).
        """
        super().__init__(daemon_url=daemon_url)
        self._active_project = active_project

    def render(self, result: AugmentationResult) -> RenderResult:
        citations = self._filter_for_doctrine(result)

        if not citations:
            return RenderResult(
                platform=Platform.TELEGRAM,
                output=[
                    {
                        "text": "_\\(no citations\\)_",
                        "parse_mode": "MarkdownV2",
                    }
                ],
                metadata=self._build_wrapper_meta(result),
                audit_event_ids=[],
            )

        chunks: list[dict[str, Any]] = []
        current_text_parts: list[str] = []
        current_keyboard: list[list[dict[str, Any]]] = []
        current_chars = 0
        audit_ids: list[str] = []

        for idx, citation in enumerate(citations, start=1):
            footnote = self._build_footnote(citation, idx)
            row = self._build_keyboard_row(citation, idx)
            footnote_chars = len(footnote)
            # +2 for the "\n\n" separator between citations in the same
            # message body.
            if (
                current_chars + footnote_chars + 2 > TELEGRAM_MAX_MESSAGE_CHARS
                and current_text_parts
            ):
                chunks.append(self._finalize_chunk(current_text_parts, current_keyboard))
                current_text_parts = []
                current_keyboard = []
                current_chars = 0
            current_text_parts.append(footnote)
            current_keyboard.append(row)
            current_chars += footnote_chars + 2
            audit_ids.append(
                self.audit_anchor(
                    citation,
                    doctrine=result.doctrine,
                    rendered_at=result.emitted_at,
                )
            )

        if current_text_parts:
            chunks.append(self._finalize_chunk(current_text_parts, current_keyboard))

        return RenderResult(
            platform=Platform.TELEGRAM,
            output=chunks,
            metadata=self._build_wrapper_meta(result),
            audit_event_ids=audit_ids,
        )

    def _filter_for_doctrine(self, result: AugmentationResult) -> list[Envelope]:
        """capa-firewall + active_project set → drop cross-project citations."""
        if result.doctrine == "capa-firewall" and self._active_project is not None:
            return [c for c in result.citations if c.project_id == self._active_project]
        return list(result.citations)

    def _build_footnote(self, citation: Envelope, index: int) -> str:
        """One footnote line in MarkdownV2 form."""
        payload_e = escape_markdown_v2(citation.payload[:_PER_CITATION_PAYLOAD_CAP])
        confidence_str = escape_markdown_v2(f"{citation.confidence:.2f}")
        source = escape_markdown_v2(citation.source.value)
        lane = escape_markdown_v2(citation.retrieval_lane.value)
        project = escape_markdown_v2(citation.project_id)
        return (
            f"\\[{index}\\] *{payload_e}* \\(confidence {confidence_str}\\)\n"
            f"    Source: {source} \\| Lane: {lane} \\| Project: {project}"
        )

    def _build_keyboard_row(self, citation: Envelope, index: int) -> list[dict[str, Any]]:
        """Inline keyboard row: [Expand, Audit] buttons for the citation.

        ``callback_data`` keeps to opaque short tokens within Telegram's
        1-64 byte limit:
        - ``<citation_id>exp`` for expand (max 21 chars given 18-char ID + 3 suffix)
        - ``<citation_id>aud`` for audit
        Hermes' Telegram adapter resolves these on click (POSTs the matching
        slash command via plugin handler).
        """
        return [
            {
                "text": f"Expand [{index}]",
                "callback_data": f"{citation.id}exp",
            },
            {
                "text": "Audit",
                "callback_data": f"{citation.id}aud",
            },
        ]

    @staticmethod
    def _finalize_chunk(
        text_parts: list[str], keyboard: list[list[dict[str, Any]]]
    ) -> dict[str, Any]:
        return {
            "text": "\n\n".join(text_parts),
            "parse_mode": "MarkdownV2",
            "reply_markup": {"inline_keyboard": keyboard},
        }
