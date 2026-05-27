# SPDX-License-Identifier: MIT
                                          
"""Slack ``chat.postMessage`` renderer for citation envelopes."""

from __future__ import annotations

from typing import Any

from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Envelope,
    Platform,
    RenderResult,
)

SLACK_MAX_BLOCKS = 50
_HEADER_BLOCKS_RESERVED = 1
_OVERFLOW_BLOCK_RESERVED = 1
_BLOCKS_PER_CITATION = 2                         
_PER_CITATION_PAYLOAD_CAP = 240


def escape_mrkdwn(text: str) -> str:
    """Slack mrkdwn escape: replace ``<``, ``>``, ``&`` with HTML entities."""
    return text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


class SlackCitationRenderer(Renderer):
    """Renders an `AugmentationResult` to a Slack chat.postMessage payload."""

    PLATFORM = Platform.SLACK

    def __init__(
        self,
        use_url_for_audit: bool = False,
        *,
        daemon_url: str | None = None,
    ) -> None:
        """``use_url_for_audit``: emit ``zen://audit/<id>`` as ``button.url``.

        When False (default), audit button uses ``action_id`` only — Slack
        routes back to the plugin which dispatches ``/audit`` slash command.
        When True, the button has a ``url`` field; Slack opens via OS
        handler (daemon registered ``zen://`` scheme).

        ``daemon_url`` is forwarded to the base ``Renderer`` so the
        instance-level audit endpoint (``self._audit_endpoint``) honors
        an operator-configured daemon TCP URL when supplied (Fix-cycle
        I-1).
        """
        super().__init__(daemon_url=daemon_url)
        self._use_url_for_audit = use_url_for_audit

    def render(self, result: AugmentationResult) -> RenderResult:
        if not result.citations:
            return RenderResult(
                platform=Platform.SLACK,
                output={
                    "text": "No citations.",
                    "blocks": [
                        {
                            "type": "section",
                            "text": {
                                "type": "mrkdwn",
                                "text": "_(no citations)_",
                            },
                        }
                    ],
                    "attachments": [],
                },
                metadata=self._build_wrapper_meta(result),
                audit_event_ids=[],
            )

                                                        
        max_citations = (
            SLACK_MAX_BLOCKS - _HEADER_BLOCKS_RESERVED - _OVERFLOW_BLOCK_RESERVED
        ) // _BLOCKS_PER_CITATION
        truncated = len(result.citations) > max_citations
        included = result.citations[:max_citations]

        blocks: list[dict[str, Any]] = [self._build_header(len(result.citations))]
        audit_ids: list[str] = []
        for citation in included:
            blocks.append(self._build_section(citation))
            blocks.append(self._build_actions(citation))
            audit_ids.append(
                self.audit_anchor(
                    citation,
                    doctrine=result.doctrine,
                    rendered_at=result.emitted_at,
                )
            )

        if truncated:
            overflow_count = len(result.citations) - max_citations
            blocks.append(self._build_overflow_note(overflow_count))

                                                                       
                                                                   
                                                                      
                                                                     
                                                                     
        fallback_lines = [
            f"[{i}] {escape_mrkdwn(c.payload[:120])} (confidence {c.confidence:.2f})"
            for i, c in enumerate(included, start=1)
        ]
        if truncated:
            fallback_lines.append(
                f"... ({len(result.citations) - max_citations} more citations omitted)"
            )

        return RenderResult(
            platform=Platform.SLACK,
            output={
                "text": "Citations from HADES augmentation pipeline",
                "blocks": blocks,
                "attachments": [
                    {
                        "color": "#3aa3e3",
                        "fallback": "\n".join(fallback_lines),
                    }
                ],
            },
            metadata=self._build_wrapper_meta(result),
            audit_event_ids=audit_ids,
        )

    @staticmethod
    def _build_header(total_count: int) -> dict[str, Any]:
        return {
            "type": "header",
            "text": {
                "type": "plain_text",
                "text": f"Citations ({total_count})",
            },
        }

    def _build_section(self, citation: Envelope) -> dict[str, Any]:
        payload = escape_mrkdwn(citation.payload[:_PER_CITATION_PAYLOAD_CAP])
        source = escape_mrkdwn(citation.source.value)
        lane = escape_mrkdwn(citation.retrieval_lane.value)
        project = escape_mrkdwn(citation.project_id)
        text = (
            f"*{payload}*\n"
            f"_Source: `{source}` | "
            f"Lane: `{lane}` | "
            f"Project: `{project}` | "
            f"Confidence: `{citation.confidence:.2f}`_"
        )
        return {
            "type": "section",
            "block_id": f"citation_{citation.id}",
            "text": {"type": "mrkdwn", "text": text},
        }

    def _build_actions(self, citation: Envelope) -> dict[str, Any]:
        elements: list[dict[str, Any]] = [
            {
                "type": "button",
                "action_id": f"expand_{citation.id}",
                "text": {"type": "plain_text", "text": "Expand"},
                "value": citation.id,
            }
        ]
        audit_button: dict[str, Any] = {
            "type": "button",
            "action_id": f"audit_{citation.id}",
            "text": {"type": "plain_text", "text": "Audit"},
            "value": citation.audit_event_id,
        }
        if self._use_url_for_audit:
            audit_button["url"] = citation.audit_event_url()
        elements.append(audit_button)
        return {
            "type": "actions",
            "block_id": f"actions_{citation.id}",
            "elements": elements,
        }

    @staticmethod
    def _build_overflow_note(overflow_count: int) -> dict[str, Any]:
        return {
            "type": "context",
            "elements": [
                {
                    "type": "mrkdwn",
                    "text": (
                        f"_... and {overflow_count} more citations "
                        "(truncated to fit Slack 50-block ceiling)_"
                    ),
                }
            ],
        }
