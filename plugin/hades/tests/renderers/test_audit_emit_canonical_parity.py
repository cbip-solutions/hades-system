# SPDX-License-Identifier: MIT
"""Canonical-parity test for Renderer.audit_anchor — POST /v1/audit/emit."""

from __future__ import annotations

import re
from datetime import datetime, timezone
from typing import Any
from unittest.mock import MagicMock, patch

from hermes_plugins.hades.renderers import (
    DEFAULT_AUDIT_ENDPOINT,
    DEFAULT_DAEMON_URL,
    Renderer,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RenderResult,
    RetrievalLane,
)


class _ProbeRenderer(Renderer):
    """Minimal concrete renderer for direct audit_anchor exercise."""

    PLATFORM = Platform.INK

    def render(self, result: AugmentationResult) -> RenderResult:
                                                          
        return RenderResult(platform=self.PLATFORM, output="")


def _envelope() -> Envelope:
    return Envelope(
        id="c-emitparity001",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-emit-parity",
        confidence=0.91,
        rrf_score=0.013,
        rrf_rank=0,
        project_id="zen-swarm",
        payload="MergeEngine.SelectWinner ... (internal/orchestrator/merge/winner.go:142)",
    )


                                                                             
                                                              
                                                                             


def test_default_audit_endpoint_is_v1_audit_emit() -> None:
    """The module-level constant points at the canonical daemon endpoint.

    Drift guard: pre-C-1 the constant read ``/v1/audit/anchor`` (never
    a registered daemon route). The daemon registers POST /v1/audit/emit
    at ``internal/daemon/server.go:737``; any value other than ``emit``
    here means every Python-rendered citation drops its audit anchor
    silently.
    """
    assert DEFAULT_DAEMON_URL == "http://localhost:4471"
    assert DEFAULT_AUDIT_ENDPOINT == "http://localhost:4471/v1/audit/emit"


def test_audit_anchor_posts_to_v1_audit_emit_path() -> None:
    """The httpx POST URL ends in ``/v1/audit/emit`` regardless of host."""
    r = _ProbeRenderer()
    citation = _envelope()

    captured: dict[str, Any] = {}
    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {"id": "evt-from-server", "accepted": True}

    def fake_post(url: str, json: dict[str, Any] | None = None) -> MagicMock:
        captured["url"] = url
        captured["body"] = json
        return mock_response

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = fake_post
        r.audit_anchor(citation, doctrine="default")

    assert captured["url"].endswith("/v1/audit/emit"), (
        f"audit_anchor must POST to canonical /v1/audit/emit; got {captured['url']!r}"
    )
    assert "/v1/audit/anchor" not in captured["url"], (
        "stale /v1/audit/anchor path resurfaced; daemon does not register that route"
    )


                                                                             
                                                                         
                                                                             


def test_audit_anchor_body_has_canonical_top_level_keys() -> None:
    """Top-level body has exactly ``project_id``, ``type``, ``payload`` keys."""
    r = _ProbeRenderer()
    citation = _envelope()

    captured: dict[str, Any] = {}
    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {"id": "evt-x"}

    def fake_post(url: str, json: dict[str, Any] | None = None) -> MagicMock:
        captured["body"] = json
        return mock_response

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = fake_post
        r.audit_anchor(citation, doctrine="capa-firewall")

    body = captured["body"]
    assert set(body.keys()) == {"project_id", "type", "payload"}, (
        f"top-level keys drift; got {sorted(body.keys())!r}"
    )
    assert body["project_id"] == "zen-swarm"
    assert body["type"] == "CitationRendered"
    assert "event_type" not in body, (
        "legacy 'event_type' top-level field resurfaced; canonical name is 'type'"
    )


def test_audit_anchor_payload_has_canonical_citation_rendered_fields() -> None:
    """``payload`` sub-dict mirrors ``citationadapter.EmitCitationRendered`` fields.

    The Go-side adapter populates the payload with ``citation_id``,
    ``platform``, ``audit_event_link``, ``rendered_at``, and
    ``doctrine``. This test pins the Python POST to the same set so
    daemon-side ``server_audit_query.go::extractDoctrineFromPayload``
    keeps returning the real doctrine (not the capa-firewall fail-closed
    fallback for unmarked rows).
    """
    r = _ProbeRenderer()
    citation = _envelope()

    captured: dict[str, Any] = {}
    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {"id": "evt-x"}

    def fake_post(url: str, json: dict[str, Any] | None = None) -> MagicMock:
        captured["body"] = json
        return mock_response

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = fake_post
        r.audit_anchor(citation, doctrine="max-scope")

    payload = captured["body"]["payload"]
    expected_keys = {
        "citation_id",
        "platform",
        "audit_event_link",
        "rendered_at",
        "doctrine",
    }
    assert set(payload.keys()) == expected_keys, (
        f"payload keys drift; got {sorted(payload.keys())!r}, "
        f"expected {sorted(expected_keys)!r}"
    )
    assert payload["citation_id"] == "c-emitparity001"
    assert payload["platform"] == "ink"
                                                                              
    assert payload["audit_event_link"] == "zen://audit/evt-emit-parity"
    assert "evt-emit-parity" not in payload.values() or (
        payload["audit_event_link"].startswith("zen://audit/")
    )
    assert payload["doctrine"] == "max-scope"
                                                                            
    assert re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", payload["rendered_at"]
    ), f"rendered_at not RFC 3339 with Z suffix: {payload['rendered_at']!r}"


def test_audit_anchor_response_reads_id_field() -> None:
    """Renderer reads ``id`` from the AuditEventOut response (NOT ``event_id``).

    Daemon-side ``AuditEmit`` handler emits ``{"id": uuid, "accepted": true,
    "emitted_at": unix}``. Pre-C-1 the renderer read ``event_id`` and
    silently returned the empty string for every successful anchor.
    """
    r = _ProbeRenderer()
    citation = _envelope()

    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {
        "id": "evt-canonical-12345",
        "accepted": True,
        "emitted_at": 1715856000,
    }
    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.return_value = mock_response
        out = r.audit_anchor(citation, doctrine="default")

    assert out == "evt-canonical-12345", (
        f"audit_anchor must return AuditEventOut.id ('id' field), got {out!r}"
    )


def test_audit_anchor_rendered_at_uses_now_when_not_supplied() -> None:
    """Default ``rendered_at`` is server-side (Python) now() in UTC.

    Matches the Go-side adapter's behaviour: when the renderer does not
    stamp a render time, the adapter substitutes ``time.Now().Unix()``.
    Python mirrors this so the audit chain has a real timestamp on every
    anchor (no empty-string sentinel that the daemon would reject).
    """
    r = _ProbeRenderer()
    citation = _envelope()

    before = datetime.now(timezone.utc).replace(microsecond=0)
    captured: dict[str, Any] = {}
    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {"id": "evt-x"}

    def fake_post(url: str, json: dict[str, Any] | None = None) -> MagicMock:
        captured["body"] = json
        return mock_response

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = fake_post
        r.audit_anchor(citation, doctrine="default")
    after = datetime.now(timezone.utc).replace(microsecond=0)

    parsed = datetime.strptime(
        captured["body"]["payload"]["rendered_at"], "%Y-%m-%dT%H:%M:%SZ"
    ).replace(tzinfo=timezone.utc)
    assert before <= parsed <= after, (
        f"rendered_at outside expected window: {parsed} not in [{before},{after}]"
    )


def test_audit_anchor_rendered_at_honors_explicit_value() -> None:
    """Explicit ``rendered_at`` override is preserved byte-for-byte (Z suffix).

    Renderers MAY pass a deterministic timestamp (e.g., the
    ``AugmentationResult.emitted_at``) so the audit row aligns with the
    operator-visible render time, not the wall-clock at chain-emit.
    """
    r = _ProbeRenderer()
    citation = _envelope()

    fixed = datetime(2026, 5, 16, 12, 0, 0, tzinfo=timezone.utc)
    captured: dict[str, Any] = {}
    mock_response = MagicMock()
    mock_response.raise_for_status.return_value = None
    mock_response.json.return_value = {"id": "evt-x"}

    def fake_post(url: str, json: dict[str, Any] | None = None) -> MagicMock:
        captured["body"] = json
        return mock_response

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = fake_post
        r.audit_anchor(citation, doctrine="default", rendered_at=fixed)

    assert captured["body"]["payload"]["rendered_at"] == "2026-05-16T12:00:00Z"


                                                                             
                                                                             
                                                                             


def test_python_render_pipeline_never_references_anchor_path() -> None:
    """No file under ``plugin/zen-swarm/renderers/`` references the legacy
    ``/v1/audit/anchor`` path (which the daemon never registered).

    Tightens beyond ``test_no_legacy_7345_references_in_renderers_module``:
    that one only checks one module; this scans the renderers package.
    Excludes test files (which reference the legacy path in comments to
    document the C-1 migration). Imports are restricted to stdlib.
    """
    import os

    plugin_root = os.path.dirname(  # noqa: PTH120 — keep stdlib-only
        os.path.dirname(  # noqa: PTH120
            os.path.dirname(os.path.abspath(__file__))  # noqa: PTH100, PTH120
        )
    )
    renderers_dir = os.path.join(plugin_root, "renderers")  # noqa: PTH118
    leaks: list[str] = []
    for entry in os.listdir(renderers_dir):
        if not entry.endswith(".py"):
            continue
        path = os.path.join(renderers_dir, entry)  # noqa: PTH118
        with open(path, encoding="utf-8") as fh:  # noqa: PTH123
            content = fh.read()
        if "/v1/audit/anchor" in content:
            leaks.append(entry)
    assert not leaks, (
        f"legacy /v1/audit/anchor path still referenced in: {leaks!r}; "
        "the daemon registers /v1/audit/emit (internal/daemon/server.go:737)"
    )
