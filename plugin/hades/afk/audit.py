# SPDX-License-Identifier: MIT
"""Canonical audit event emitters for the AFK module."""

from __future__ import annotations

import logging
from typing import Any

import httpx

from . import (
    AUDIT_MOBILE_EXPANSION_REQUESTED,
    AUDIT_OFFLINE_CACHE_HIT,
    AUDIT_VOICE_QUERY_DISPATCHED,
)

_log = logging.getLogger(__name__)

                                                                     
                                                                       
                                             
AUDIT_EMIT_PATH = "/v1/audit/emit"


async def _emit(
    *,
    daemon_url: str,
    client: httpx.AsyncClient,
    event_type: str,
    project_id: str,
    payload: dict[str, Any],
) -> None:
    """POST an audit event to the daemon's chain endpoint.

    Raises ``RuntimeError`` on non-2xx — audit chain integrity is
    load-bearing per invariant (Tessera anchor chain unbroken).
    Operators see the upstream error in the daemon log + the AFK
    module's caller logs the failed dispatch context.
    """
    url = f"{daemon_url}{AUDIT_EMIT_PATH}"
    body = {
        "project_id": project_id,
        "type": event_type,
        "payload": payload,
    }
    response = await client.post(url, json=body)
    if response.status_code >= 300:
        msg = (
            f"audit chain emit failed for {event_type}: "
            f"daemon {url} returned {response.status_code}: {response.text[:200]}"
        )
        _log.error(msg)
        raise RuntimeError(msg)


async def emit_mobile_expansion_requested(
    *,
    daemon_url: str,
    client: httpx.AsyncClient,
    citation_id: str,
    operator_id: str,
    platform: str,
    ts_unix_ms: int,
    project_id: str = "",
) -> None:
    """Emit ``AUDIT_MOBILE_EXPANSION_REQUESTED`` to the  chain.

    Invoked by ``mobile_summary.expand()`` after the operator's
    ``/expand <citation-id>`` slash command resolves the full envelope.
    The audit payload anchors which citation was expanded by which
    operator on which platform — useful for cross-project observability
    + privacy-violation forensics.

    The ``project_id`` argument defaults to empty string because the
    mobile-expansion path is operator-scoped, not project-scoped (a
    single expand call may surface citations from multiple projects
    when the operator is on a max-scope session). Callers may pass an
    explicit project_id when the operator session is project-scoped
    (capa-firewall doctrine).
    """
    await _emit(
        daemon_url=daemon_url,
        client=client,
        event_type=AUDIT_MOBILE_EXPANSION_REQUESTED,
        project_id=project_id,
        payload={
            "citation_id": citation_id,
            "operator_id": operator_id,
            "platform": platform,
            "ts_unix_ms": ts_unix_ms,
        },
    )


async def emit_voice_query_dispatched(
    *,
    daemon_url: str,
    client: httpx.AsyncClient,
    query: str,
    operator_id: str,
    project_id: str,
    mode: str,
    estimated_latency_ms: int,
    explicit_override: bool,
    notification_dispatched: bool,
    ts_unix_ms: int,
) -> None:
    """Emit ``AUDIT_VOICE_QUERY_DISPATCHED`` to the  chain.

    Invoked by ``voice_flow.dispatch_voice_query()`` on every voice
    query dispatch. Captures sync vs async decision + estimate +
    override flag + notification dispatch — full provenance for
    reconstruction of operator intent vs system decision.
    """
    await _emit(
        daemon_url=daemon_url,
        client=client,
        event_type=AUDIT_VOICE_QUERY_DISPATCHED,
        project_id=project_id,
        payload={
            "query": query,
            "operator_id": operator_id,
            "mode": mode,
            "estimated_latency_ms": estimated_latency_ms,
            "explicit_override": explicit_override,
            "notification_dispatched": notification_dispatched,
            "ts_unix_ms": ts_unix_ms,
        },
    )


async def emit_offline_cache_hit(
    *,
    daemon_url: str,
    client: httpx.AsyncClient,
    query_hash: str,
    citation_id: str,
    project_id: str,
    cache_doctrine: str,
    cache_size: int,
    ts_unix_ms: int,
) -> None:
    """Emit ``AUDIT_OFFLINE_CACHE_HIT`` to the  chain.

    Invoked by ``KGOfflineCache.get()`` on every hit (post privacy
    filter — D-5 rejects cross-project hits silently to avoid leak via
    audit). Captures cache state observability for doctor checks +
    chain-anchored proof that the offline cache served the operator's
    query (provenance vs daemon-side fresh query).
    """
    await _emit(
        daemon_url=daemon_url,
        client=client,
        event_type=AUDIT_OFFLINE_CACHE_HIT,
        project_id=project_id,
        payload={
            "query_hash": query_hash,
            "citation_id": citation_id,
            "cache_doctrine": cache_doctrine,
            "cache_size": cache_size,
            "ts_unix_ms": ts_unix_ms,
        },
    )


async def post_inbox_notification(
    *,
    daemon_url: str,
    client: httpx.AsyncClient,
    project_id: str,
    severity: str,
    event_type: str,
    payload: dict[str, Any],
) -> dict[str, Any]:
    """POST a  inbox notification (consumed by
    ``voice_flow.dispatch_voice_query`` as the inbox_poster).

    Drift note: the daemon-side inbox-write path uses the audit-emit
    endpoint plus the  ``inbox`` table is populated by the daemon
    itself when the corresponding audit event type matches the inbox
    routing config (spec §7.6). From Python's perspective the
    "post inbox notification" call is therefore a structured audit
    emit with ``payload.severity`` + ``payload.event_type`` carried
    inside the payload — the daemon's audit subscriber materializes the
    inbox row.

    Returns a dict-shaped acknowledgement compatible with the plan-file
    inline contract ``{"id": int, "ack": str}``; consumers may ignore
    the value (voice_flow currently does).
    """
    body = {
        "project_id": project_id,
        "type": event_type,
        "payload": {
            "severity": severity,
            **payload,
        },
    }
    url = f"{daemon_url}{AUDIT_EMIT_PATH}"
    response = await client.post(url, json=body)
    if response.status_code >= 300:
        msg = (
            f"inbox notification post failed for {event_type}: "
            f"daemon {url} returned {response.status_code}: {response.text[:200]}"
        )
        _log.error(msg)
        raise RuntimeError(msg)
    parsed = response.json()
    if isinstance(parsed, dict):
        return parsed
    return {"raw": parsed}
