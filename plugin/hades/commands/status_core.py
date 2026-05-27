# SPDX-License-Identifier: MIT
"""Shared fetch+classify core for HADES daemon status queries."""

from __future__ import annotations

import asyncio
import os
from typing import Any

import httpx

# Default UDS path. Borderline-stays per spec §Q3 (operator scripts
# reference this path; rename deferred to the release design+N borderline migration).
DEFAULT_UDS_PATH: str = "/tmp/hades-system.sock"

# Per-endpoint timeout in seconds. 3s is conservative — each endpoint is
# a local UDS call and returns near-instantly under normal conditions;
# 3s catches a degraded daemon (load spike, GC pause) without making
# the operator wait indefinitely.
ENDPOINT_TIMEOUT_S: float = 3.0

# Endpoint paths per spec §Q5. Order matters: it drives the line
# ordering in the rendered block (per inv-hades-221 stable schema).
ENDPOINTS: tuple[str, ...] = (
    "/v1/health",
    "/v1/cascade/state",
    "/v1/bypass/status",
    "/v1/cost/24h",
    "/v1/context/used",
    "/v1/profile/active",
    "/v1/cwd",
)


def build_client() -> httpx.AsyncClient:
    """Construct an httpx.AsyncClient bound to the daemon's UDS path.

    The base URL is ``http://localhost`` and the transport uses
    ``httpx.AsyncHTTPTransport(uds=...)`` to route over the UDS. The path
    can be overridden via the ``HADES_SYSTEM_UDS`` env var (operator
    convention — same env consumed by ``bin/hades-event-poster``).

    NOTE: tests patch this factory to inject ``httpx.MockTransport`` so
    the handler can be exercised without a real daemon. Production
    path uses the real UDS transport.
    """
    uds_path = os.environ.get("HADES_SYSTEM_UDS") or DEFAULT_UDS_PATH
    transport = httpx.AsyncHTTPTransport(uds=uds_path)
    return httpx.AsyncClient(
        transport=transport,
        base_url="http://localhost",
        timeout=ENDPOINT_TIMEOUT_S,
    )


async def query_daemon(
    client: httpx.AsyncClient,
) -> dict[str, dict[str, Any] | None]:
    """Fan out 7 concurrent GETs against the daemon. Returns a dict
    keyed by endpoint path with the parsed JSON body OR None if the
    endpoint returned a non-2xx status / raised a transport error.

    For structured-error detection: if a non-2xx response contains the
    four-key envelope shape {code, title, body, recovery_hint}, the
    body is returned (not None) so the top-level handler can detect and
    dispatch the three-line error block.

    release track ships the happy-path semantics (every endpoint OK).
    release track extends with degraded-mode classification: None marks
    a degraded field which downstream rendering surfaces as
    ``<field>: unavailable (...)`` per spec §Q5.
    """

    async def _fetch_one(path: str) -> dict[str, Any] | None:
        try:
            resp = await client.get(path)
            try:
                body = resp.json()
            except ValueError:
                return None
            if not isinstance(body, dict):
                return None
            # If 2xx, return as success.
            if resp.status_code == 200:
                return body
            # Non-2xx: if body has the structured-error envelope shape,
            # return it so the top-level handler can detect + dispatch.
            if all(k in body for k in ("code", "title", "body", "recovery_hint")):
                return body
            # Non-2xx without envelope = degraded for this field.
            return None
        except (httpx.HTTPError, ValueError):
            # ValueError covers JSON decode failures; httpx.HTTPError
            # covers all httpx transport-level failures (timeouts,
            # connection errors, etc.).
            return None

    results = await asyncio.gather(
        *(_fetch_one(path) for path in ENDPOINTS),
        return_exceptions=False,
    )
    return dict(zip(ENDPOINTS, results, strict=True))


def classify_field_state(response: dict[str, Any] | None) -> str:
    """Return 'ok' if response is non-None, 'degraded' otherwise.

    Schema-v1 state classifier per spec §Q5 + inv-hades-221.
    """
    return "ok" if response is not None else "degraded"


def detect_structured_error_envelope(
    responses: dict[str, dict[str, Any] | None],
) -> dict[str, str] | None:
    """Inspect responses for the daemon's structured-error envelope
    shape ``{"code": "...", "title": "...", "body": "...",
    "recovery_hint": "..."}``. The daemon may return this envelope on
    ANY endpoint when it is in a degraded boot state — usually
    ``/v1/health`` first.

    Returns the envelope dict if detected; None otherwise.
    """
    for path in ENDPOINTS:
        resp = responses.get(path)
        if resp is None:
            continue
        # Envelope detection: presence of all four required keys.
        if all(k in resp for k in ("code", "title", "body", "recovery_hint")):
            return {
                "code": str(resp["code"]),
                "title": str(resp["title"]),
                "body": str(resp["body"]),
                "recovery_hint": str(resp["recovery_hint"]),
            }
    return None
