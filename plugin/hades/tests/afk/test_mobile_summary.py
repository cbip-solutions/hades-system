# SPDX-License-Identifier: MIT
"""Tests for ``hermes_plugins.hades.afk.mobile_summary``."""

from __future__ import annotations

from typing import Any
from unittest.mock import AsyncMock

import httpx
import pytest
from hermes_plugins.hades.afk import (
    AUDIT_MOBILE_EXPANSION_REQUESTED,
    AFKPlatform,
    MobileSummaryCard,
)
from hermes_plugins.hades.afk.mobile_summary import (
    expand,
    render_short,
    voice_short_phrase,
)


def test_render_short_drops_fields_beyond_first_three(
    citation_envelope_factory: Any,
) -> None:
    envelope = citation_envelope_factory(top_field_count=4)
    card = render_short(envelope)
    assert isinstance(card, MobileSummaryCard)
    assert len(card.top_fields) == 3
                                                    
    assert ("complexity", "high") not in card.top_fields


def test_render_short_preserves_envelope_metadata(
    citation_envelope_factory: Any,
) -> None:
    envelope = citation_envelope_factory(
        citation_id="c-007",
        cache_state="stale",
        project_id="zen-swarm",
    )
    card = render_short(envelope)
    assert card.citation_id == "c-007"
    assert card.cache_state == "stale"
    assert card.project_id == "zen-swarm"
    assert card.audit_event_id == "evt-c-007"
    assert card.title == "file/c-007.go"


def test_render_short_preserves_top_field_order(
    citation_envelope_factory: Any,
) -> None:
    envelope = citation_envelope_factory(top_field_count=3)
    card = render_short(envelope)
    keys = [k for k, _ in card.top_fields]
    assert keys == ["blast_radius", "top_callers", "community"]


def test_render_short_rejects_envelope_missing_required_keys() -> None:
    bad = {
        "citation_id": "c-x",
        "title": "x",
                                                                        
    }
    with pytest.raises(ValueError, match="envelope missing required keys"):
        render_short(bad)


def test_voice_short_phrase_renders_tts_friendly_text(
    citation_envelope_factory: Any,
) -> None:
    envelope = citation_envelope_factory(citation_id="c-009")
    phrase = voice_short_phrase(envelope)
                                                                           
    assert "audit event evt-c-009" in phrase.lower()
    assert "blast radius" in phrase.lower()
    assert "12 callers" in phrase
                                           
    assert "top callers" in phrase.lower()


@pytest.mark.asyncio
async def test_expand_calls_daemon_audit_endpoint_and_emits_audit_event(
    daemon_url: str,
    mock_daemon: httpx.AsyncClient,
    audit_event_capture: list[dict[str, Any]],
) -> None:
    async def _emitter(**kwargs: Any) -> None:
        audit_event_capture.append({"name": AUDIT_MOBILE_EXPANSION_REQUESTED, **kwargs})

    audit_emitter = AsyncMock(side_effect=_emitter)
    full = await expand(
        citation_id="evt-1234abcd",
        operator_id="testuser",
        platform=AFKPlatform.TELEGRAM,
        daemon_url=daemon_url,
        client=mock_daemon,
        audit_emitter=audit_emitter,
    )
    assert full["audit_event_id"] == "evt-1234abcd"
    assert "callers" in full["full_payload"]
    assert audit_emitter.await_count == 1
    assert audit_event_capture[0]["citation_id"] == "evt-1234abcd"
    assert audit_event_capture[0]["operator_id"] == "testuser"
    assert audit_event_capture[0]["platform"] == "telegram"
    assert audit_event_capture[0]["ts_unix_ms"] >= 0


@pytest.mark.asyncio
async def test_expand_propagates_daemon_404_as_value_error(
    daemon_url: str,
) -> None:
                                        
    transport = httpx.MockTransport(
        lambda request: httpx.Response(404, json={"error": "not_found"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(ValueError, match="audit event evt-missing not found"):
            await expand(
                citation_id="evt-missing",
                operator_id="testuser",
                platform=AFKPlatform.TELEGRAM,
                daemon_url=daemon_url,
                client=client,
                audit_emitter=AsyncMock(),
            )


@pytest.mark.asyncio
async def test_expand_propagates_5xx_as_runtime_error(
    daemon_url: str,
) -> None:
    transport = httpx.MockTransport(
        lambda request: httpx.Response(503, json={"error": "daemon_offline"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(RuntimeError, match="returned 503"):
            await expand(
                citation_id="evt-down",
                operator_id="testuser",
                platform=AFKPlatform.TELEGRAM,
                daemon_url=daemon_url,
                client=client,
                audit_emitter=AsyncMock(),
            )


@pytest.mark.asyncio
async def test_expand_4xx_other_than_404_propagates(
    daemon_url: str,
) -> None:
    """A 4xx that is not 404 should not silently succeed."""
    transport = httpx.MockTransport(
        lambda request: httpx.Response(401, json={"error": "unauthorized"})
    )
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(httpx.HTTPStatusError):
            await expand(
                citation_id="evt-x",
                operator_id="testuser",
                platform=AFKPlatform.SLACK,
                daemon_url=daemon_url,
                client=client,
                audit_emitter=AsyncMock(),
            )


@pytest.mark.asyncio
async def test_expand_does_not_emit_audit_on_failure(
    daemon_url: str,
) -> None:
    """Audit event must NOT be emitted when expansion fails (no half-state)."""
    transport = httpx.MockTransport(
        lambda request: httpx.Response(404, json={"error": "not_found"})
    )
    audit_emitter = AsyncMock()
    async with httpx.AsyncClient(transport=transport) as client:
        with pytest.raises(ValueError):
            await expand(
                citation_id="evt-missing",
                operator_id="testuser",
                platform=AFKPlatform.TELEGRAM,
                daemon_url=daemon_url,
                client=client,
                audit_emitter=audit_emitter,
            )
    audit_emitter.assert_not_awaited()
