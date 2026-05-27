# SPDX-License-Identifier: MIT
"""Dormant status-bar provider for the HADES Hermes plugin."""

from __future__ import annotations

import contextlib
from typing import Any

# Re-export status_core so tests can access status_core.ENDPOINTS via
# ``status_provider.status_core.ENDPOINTS`` without a separate import.
from hades.commands import status_core


def segments_from_responses(
    responses: dict[str, dict[str, Any] | None],
) -> list[str]:
    """Produce compact labelled segments from a daemon responses dict.

    Each segment is a short string suitable for a status-bar entry. The set of
    segments covers: daemon health, cascade tier, bypass state, 24h cost, and
    context usage percentage. Degraded endpoints render a marker (never raise).

    Args:
        responses: dict keyed by endpoint path (from ``status_core.ENDPOINTS``)
                   mapping to parsed JSON body or None if endpoint degraded.

    Returns:
        list of compact segment strings, e.g.
        ["daemon✓", "tier1", "bypass✓", "$0.04", "24%"]

    Pre-conditions:
        - responses may be partially or fully None-valued (degraded daemon).
        - Every key in ``status_core.ENDPOINTS`` should be present; missing
          keys are treated as degraded.

    Post-conditions:
        - Always returns a non-empty list.
        - Never raises.
        - No ANSI escape sequences in output.
    """
    segs: list[str] = []

    # --- daemon health ---------------------------------------------------------
    health = responses.get("/v1/health")
    if health is not None:
        segs.append("daemon✓")  # daemon✓
    else:
        segs.append("daemon✗")  # daemon✗

    # --- cascade tier ---------------------------------------------------------
    cascade = responses.get("/v1/cascade/state")
    if cascade is not None:
        tier = cascade.get("active_tier")
        if tier is not None:
            segs.append(f"tier{tier}")
        else:
            segs.append("tier?")
    else:
        segs.append("tier?")

    # --- bypass state ---------------------------------------------------------
    bypass = responses.get("/v1/bypass/status")
    if bypass is not None:
        status_val = bypass.get("status", "")
        if status_val == "live":
            segs.append("bypass✓")  # bypass✓
        else:
            segs.append("bypass⚠")  # bypass⚠
    else:
        segs.append("bypass⚠")  # bypass⚠

    # --- 24h cost -------------------------------------------------------------
    cost = responses.get("/v1/cost/24h")
    if cost is not None:
        spend = cost.get("spend_24h_usd")
        if spend is not None:
            segs.append(f"${spend:.2f}")
        else:
            segs.append("$-.--")
    else:
        segs.append("$-.--")

    # --- context usage % ------------------------------------------------------
    ctx_resp = responses.get("/v1/context/used")
    if ctx_resp is not None:
        used = ctx_resp.get("used_tokens")
        max_tok = ctx_resp.get("max_tokens")
        if used is not None and max_tok and max_tok > 0:
            pct = int(round(used / max_tok * 100))
            segs.append(f"{pct}%")
        else:
            segs.append("?%")
    else:
        segs.append("?%")

    return segs


async def status_segments() -> list[str]:
    """Full async helper: build client, query daemon, return compact segments.

    Intended to be registered as the status-bar provider callback when Hermes
    ships ``register_status_provider``. The callback is invoked by Hermes on
    its status-bar refresh cycle.

    Builds the httpx client via ``status_core.build_client()``, fans out the 7
    concurrent daemon GETs via ``status_core.query_daemon(client)``, and closes
    the client in a finally block before returning.

    Returns:
        list of compact segment strings (see ``segments_from_responses``).

    Pre-conditions:
        - The daemon may be absent or degraded; the function must not raise.

    Post-conditions:
        - Always returns a non-empty list.
        - Client is closed even if query_daemon raises.
        - Never raises (all exceptions are suppressed; a degraded segment list
          is returned instead).
    """
    client = status_core.build_client()
    try:
        responses = await status_core.query_daemon(client)
    except Exception:  # noqa: BLE001 — suppress all; never raise from a status callback
        responses = dict.fromkeys(status_core.ENDPOINTS, None)
    finally:
        with contextlib.suppress(Exception):
            await client.aclose()
    return segments_from_responses(responses)
