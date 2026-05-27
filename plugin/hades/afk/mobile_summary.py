# SPDX-License-Identifier: MIT
"""Mobile summary card rendering + ``/expand`` round-trip for AFK platforms."""

from __future__ import annotations

import time
from collections.abc import Awaitable, Callable
from typing import Any

import httpx

from .types import AFKPlatform, MobileSummaryCard

_REQUIRED_ENVELOPE_KEYS = (
    "citation_id",
    "title",
    "top_fields",
    "audit_event_id",
    "project_id",
    "cache_state",
)


def render_short(envelope: dict[str, Any]) -> MobileSummaryCard:
    """Render a citation envelope as a short ``MobileSummaryCard``.

    Drops ``top_fields`` beyond the first 3 (``MobileSummaryCard``'s
    invariant). Preserves ``cache_state`` + ``audit_event_id`` +
    ``project_id`` verbatim — these fields drive release track renderer
    dispatch and the release design audit chain anchor.

    Args:
        envelope: The citation envelope JSON (the release design substrate shape) —
            see ``internal/citation/envelope.go`` for the canonical Go
            type. Python consumes via daemon HTTP round-trip.

    Returns:
        A ``MobileSummaryCard`` with at most 3 ``top_fields``. Any extra
        fields in ``envelope["top_fields"]`` are silently dropped
        (citation envelope's structured fields are pre-ranked by Plan
        11 — first 3 = highest signal per spec §7.4).

    Raises:
        ValueError: if the envelope is malformed (missing required keys).
    """
    missing = [k for k in _REQUIRED_ENVELOPE_KEYS if k not in envelope]
    if missing:
        raise ValueError(f"envelope missing required keys: {missing}")
    top_fields = envelope["top_fields"][:3]  # drop fields beyond first 3
    return MobileSummaryCard(
        citation_id=envelope["citation_id"],
        title=envelope["title"],
        top_fields=top_fields,
        audit_event_id=envelope["audit_event_id"],
        project_id=envelope["project_id"],
        cache_state=envelope["cache_state"],
    )


def voice_short_phrase(envelope: dict[str, Any]) -> str:
    """Render the citation envelope's top finding as a TTS-friendly phrase.

    Per spec §1 Q9=C+ voice format: verbal description, no special
    chars, explicit audit-event-id reference for traceability ("Citing
    audit event evt-1234abcd, blast radius 12 callers, community
    daemon-bootstrap.").

    Used by D-3 ``voice_flow.py`` during sync inline response.
    """
    card = render_short(envelope)
    parts = [f"Citing audit event {card.audit_event_id}"]
    for key, value in card.top_fields:
        spoken_key = key.replace("_", " ")
        parts.append(f"{spoken_key} {value}")
    return ", ".join(parts) + "."


async def expand(
    *,
    citation_id: str,
    operator_id: str,
    platform: AFKPlatform,
    daemon_url: str,
    client: httpx.AsyncClient,
    audit_emitter: Callable[..., Awaitable[None]],
) -> dict[str, Any]:
    """Resolve a citation envelope's full payload via daemon HTTP round-trip.

    Implements the ``/expand <citation-id>`` slash command flow per spec
    §1 Q6=B. Operator on AFK platform issues ``/expand evt-1234abcd``;
    Hermes' slash command parser (release track registers the command; this
    function executes it) invokes this coroutine. The daemon's
    ``GET /v1/audit/event/<id>`` endpoint (the release design substrate shipped per
    ``internal/daemon/handlers/audit_event.go``) returns the full
    envelope JSON; the AFK module emits an
    ``AUDIT_MOBILE_EXPANSION_REQUESTED`` audit event for the release design chain
    anchoring before returning to the platform renderer.

    Args:
        citation_id: The the release design citation envelope ID (e.g.
            ``"evt-1234abcd"``).
        operator_id: The session operator's id (audit chain attribution).
        platform: The active AFK platform (audit event payload).
        daemon_url: Daemon HTTP base URL (e.g. ``"http://localhost:4471"``).
        client: An ``httpx.AsyncClient`` (test injects ``MockTransport``;
            production constructs via
            ``plugin/hades-system/transports/hades_system_transport.py``).
        audit_emitter: Callable that emits the
            ``AUDIT_MOBILE_EXPANSION_REQUESTED`` event to the daemon's
            audit chain (D-6 ships
            ``audit.emit_mobile_expansion_requested`` as the canonical
            impl). The emitter is awaited AFTER a successful resolve so
            failures do not surface as audit-chain noise.

    Returns:
        The full envelope dict as returned by the daemon.

    Raises:
        ValueError: if the daemon returns 404 (citation_id unknown).
        RuntimeError: if the daemon returns 5xx (daemon offline /
            internal error).
        httpx.HTTPStatusError: for other non-2xx (401/403/...).
    """
    url = f"{daemon_url}/v1/audit/event/{citation_id}"
    response = await client.get(url)
    if response.status_code == 404:
        raise ValueError(f"audit event {citation_id} not found")
    if response.status_code >= 500:
        raise RuntimeError(
            f"daemon {url} returned {response.status_code}: {response.text[:200]}"
        )
    response.raise_for_status()
    body = response.json()
    envelope: dict[str, Any] = body["envelope"]
    # Emit audit event AFTER successful resolve — D-6 audit.py wires the
    # canonical implementation. Timestamp emitted as unix milliseconds
    # (UTC); matches the release design chain convention for cross-platform AFK
    # telemetry.
    await audit_emitter(
        citation_id=citation_id,
        operator_id=operator_id,
        platform=platform.value,
        ts_unix_ms=int(time.time() * 1000),
    )
    return envelope
