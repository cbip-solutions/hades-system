# SPDX-License-Identifier: MIT
"""Voice query dispatch — sync vs async with notification fallback."""

from __future__ import annotations

import time
from collections.abc import Awaitable, Callable
from typing import Any

import httpx

from .types import VoiceFlow, VoiceFlowMode

# Threshold for sync vs async classification (milliseconds).
# Per spec §1 Q6=B: "sync if estimated <10s; async with notification beyond".
SYNC_THRESHOLD_MS = 10_000

# Estimator rule constants — sourced from spec §1 Q8 aggressive
# performance budgets. Empirical calibration (release track spike + the release design D
# production) tunes these post-ship via doctrine amendment lifecycle
# (the release design + the release design) per §1 Q8 footnote.
_BASE_RRF_MS = 2_000
_CROSS_PROJECT_BASELINE_MS = 8_000
_PER_EXTRA_PROJECT_MS = 1_500
_COMMUNITY_KEYWORD_MS = 3_000
_BLAST_RADIUS_KEYWORD_MS = 2_500
_LONG_QUERY_MS = 1_000
_LONG_QUERY_THRESHOLD_CHARS = 500

_COMMUNITY_KEYWORDS: tuple[str, ...] = ("community", "cluster", "summary")
_BLAST_RADIUS_KEYWORDS: tuple[str, ...] = (
    "blast radius",
    "blast-radius",
    "impact-pre-merge",
)


def estimate_latency_ms(
    query: str,
    *,
    cross_project: bool = False,
    project_count: int = 1,
) -> int:
    """Estimate query latency in milliseconds via rule-based heuristics.

    Per spec §1 Q8: empirical calibration via release track spike + production
    measurement; this function captures the calibrated baseline at
    plan-write time. Doctrine amendment lifecycle re-tunes constants as
    production data accumulates.

    Args:
        query: The voice query text (operator's spoken intent).
        cross_project: True when the operator asked across project
            boundaries (release track ``/voice`` slash parser sets this when
            ``--cross-project`` flag present OR query mentions multiple
            project ids).
        project_count: Number of projects spanned (used only when
            ``cross_project`` is True; ignored otherwise). Values ≤ 1
            collapse to the cross-project baseline floor (no extra
            per-project cost).

    Returns:
        Latency estimate in milliseconds (always >= 0).
    """
    if cross_project:
        latency = _CROSS_PROJECT_BASELINE_MS
        if project_count > 1:
            latency += _PER_EXTRA_PROJECT_MS * (project_count - 1)
    else:
        latency = _BASE_RRF_MS

    query_lower = query.lower()
    if any(kw in query_lower for kw in _COMMUNITY_KEYWORDS):
        latency += _COMMUNITY_KEYWORD_MS
    if any(kw in query_lower for kw in _BLAST_RADIUS_KEYWORDS):
        latency += _BLAST_RADIUS_KEYWORD_MS
    if len(query) > _LONG_QUERY_THRESHOLD_CHARS:
        latency += _LONG_QUERY_MS

    return latency


async def dispatch_voice_query(
    *,
    query: str,
    operator_id: str,
    project_id: str,
    explicit_override: VoiceFlowMode | None,
    cross_project: bool,
    project_count: int,
    daemon_url: str,
    client: httpx.AsyncClient,
    audit_emitter: Callable[..., Awaitable[None]],
    inbox_poster: Callable[..., Awaitable[dict[str, Any]]],
) -> VoiceFlow:
    """Dispatch a voice query via sync or async path per Q6=B decision tree.

    Args:
        query: The voice query text.
        operator_id: Session operator id (audit + inbox attribution).
        project_id: Active project's sha256 hex (the release design inbox project_id).
        explicit_override: If not None, forces the dispatch mode
            regardless of estimate. Honours the operator's ``--sync`` /
            ``--async`` flag.
        cross_project: True if query spans projects.
        project_count: Number of projects when ``cross_project=True``.
        daemon_url: Daemon HTTP base URL.
        client: ``httpx.AsyncClient`` (test injects mock; production
            constructs via
            ``plugin/hades-system/transports/hades_system_transport.py``).
        audit_emitter: Emits ``AUDIT_VOICE_QUERY_DISPATCHED`` to the release design
            chain.
        inbox_poster: Posts to the release design inbox via daemon
            ``/v1/notifications/inbox``.

    Returns:
        A ``VoiceFlow`` capturing the dispatch decision (mode, estimate,
        override flag, notification flag). Caller renders the
        appropriate voice phrase from this metadata + the query result
        (sync) or "results ready in inbox" phrase (async).
    """
    estimate = estimate_latency_ms(
        query, cross_project=cross_project, project_count=project_count
    )

    if explicit_override is not None:
        mode = explicit_override
        is_explicit = True
    elif estimate < SYNC_THRESHOLD_MS:
        mode = VoiceFlowMode.SYNC
        is_explicit = False
    else:
        mode = VoiceFlowMode.ASYNC
        is_explicit = False

    notification_dispatched = False
    if mode == VoiceFlowMode.ASYNC:
        # Post the release design inbox notification ("results ready in inbox") with
        # severity=info-immediate. The daemon-side query continuation
        # will post the completion notification (the release design amendment);
        # Python's responsibility ends at dispatch + initial notification.
        # The inbox_poster is dependency-injected so D-6 / production
        # wires through to httpx; the daemon_url is forwarded for the
        # canonical impl to compose the final URL.
        await inbox_poster(
            daemon_url=daemon_url,
            client=client,
            project_id=project_id,
            severity="info-immediate",
            event_type="afk.voice_query_async_started",
            payload={
                "voice_query": query,
                "expected_completion_ms": estimate,
                "operator_id": operator_id,
            },
        )
        notification_dispatched = True

    # Audit event emission per the release design chain anchor convention.
    await audit_emitter(
        daemon_url=daemon_url,
        client=client,
        query=query,
        operator_id=operator_id,
        project_id=project_id,
        mode=mode.value,
        estimated_latency_ms=estimate,
        explicit_override=is_explicit,
        notification_dispatched=notification_dispatched,
        ts_unix_ms=int(time.time() * 1000),
    )

    return VoiceFlow(
        query=query,
        estimated_latency_ms=estimate,
        mode=mode,
        explicit_override=is_explicit,
        notification_dispatched=notification_dispatched,
    )
