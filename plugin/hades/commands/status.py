# SPDX-License-Identifier: MIT
"""Slash command handler for /hades:status."""

from __future__ import annotations

import asyncio
import contextlib
import datetime
import json
import logging
import os
from typing import Any

import httpx

from hades.commands import status_core

logger = logging.getLogger(__name__)

                                                                       
                                                                      
                                                                      
SCHEMA_VERSION: int = 1

                                                                             
                                                                      
                                                                         
                                                                         
                            
                                                                             
_DEFAULT_UDS_PATH: str = status_core.DEFAULT_UDS_PATH
_ENDPOINT_TIMEOUT_S: float = status_core.ENDPOINT_TIMEOUT_S
_ENDPOINTS: tuple[str, ...] = status_core.ENDPOINTS

                                                                 
                                                
_DEGRADED_HINT = "unavailable (daemon path down — try: hades doctor)"

                                                                     
                                                                     
                                                                      
                                                              
                                    
_CODE_DAEMON_NOT_RUNNING = "daemon.not-running"

                                                                     
                                                                      
                                                                 
                                         
_LOCAL_DAEMON_NOT_RUNNING_ENVELOPE: dict[str, str] = {
    "code": _CODE_DAEMON_NOT_RUNNING,
    "title": "daemon not running",
    "body": (
        "The zen-swarm-ctld daemon is not listening on the expected UDS path. "
        "/hades:status cannot retrieve runtime state without the daemon."
    ),
    "recovery_hint": (
        "Start the daemon: `bin/zen-swarm-ctld` (foreground) or via the launchd "
        "agent. Run `hades doctor` for an end-to-end environment check."
    ),
}

                                                                  
                                                            
                                        
_PALETTE: dict[str, str] = {
    "ok": "#10b981",                                  
    "warn": "#ffa726",                                  
    "fail": "#c41e3a",                                           
    "muted": "#999999",                          
}

                                                                     
                                               
_PALETTE_ANSI: dict[str, str] = {
    "ok": "\x1b[38;2;16;185;129m",
    "warn": "\x1b[38;2;255;167;38m",
    "fail": "\x1b[38;2;196;30;58m",
    "muted": "\x1b[38;2;153;153;153m",
}
_ANSI_RESET = "\x1b[0m"


def _try_import_terminal_helper() -> Any:
    """Best-effort import of the Hermes terminal helper. Returns the
    helper's `colorize` callable, or None if Hermes is unavailable.

    The handler tolerates a missing helper by falling back to plain
    text — color is a polish concern, content delivery is the primary
    contract.
    """
    try:
        from hermes_cli.terminal import colorize  # type: ignore[import-not-found]

        return colorize
    except ImportError:
        return None


_HERMES_COLORIZE = _try_import_terminal_helper()


def _colored_text(text: str, color_key: str) -> str:
    """Apply the named color from the palette to the given text.

    Args:
        text: plain text to colorize.
        color_key: one of "ok", "warn", "fail", "muted".

    Returns:
        Text with ANSI color escape sequences applied. The approach:
        - If Hermes terminal helper is available: delegate to it (handles
          TTY detection + NO_COLOR env conventions).
        - Otherwise: apply ANSI directly but only when neither NO_COLOR
          nor the absence of HERMES_FORCE_COLOR (non-TTY) would suppress.

    The helper invocation may raise ImportError if the helper went
    missing between module load and call time; _safe_colorize wraps this.
    """
                                                            
    no_color = os.environ.get("NO_COLOR")
    if no_color is not None:
        return text

    if _HERMES_COLORIZE is not None:
        hex_color = _PALETTE.get(color_key)
        if hex_color is None:
            return text
        return _HERMES_COLORIZE(text, hex_color)  # type: ignore[no-any-return]

                                                                
    force_color = os.environ.get("HERMES_FORCE_COLOR")
    if not force_color:
        return text

    ansi_seq = _PALETTE_ANSI.get(color_key)
    if not ansi_seq:
        return text
    return f"{ansi_seq}{text}{_ANSI_RESET}"


def _safe_colorize(text: str, color_key: str) -> str:
    """Defense-in-depth wrapper around `_colored_text` that catches
    any helper-level exception and falls back to plain text. The
    handler invokes this from `_render_human` for every colorized
    region.
    """
    try:
        return _colored_text(text, color_key)
    except (ImportError, AttributeError, TypeError, ValueError):
                                                                     
                                                                  
        return text


def _build_client() -> httpx.AsyncClient:
    """Construct an httpx.AsyncClient bound to the daemon's UDS path.

    Delegates to ``status_core.build_client()``. Private alias preserved
    so existing tests that patch
    ``hermes_plugins.hades.commands.status._build_client`` continue to work.
    """
    return status_core.build_client()


async def _query_daemon(
    client: httpx.AsyncClient,
) -> dict[str, dict[str, Any] | None]:
    """Fan out 7 concurrent GETs against the daemon.

    Delegates to ``status_core.query_daemon()``. Private alias preserved
    so existing tests that patch
    ``hermes_plugins.hades.commands.status._query_daemon`` continue to work.
    """
    return await status_core.query_daemon(client)


def _degraded_line(label: str) -> str:
    """Build a degraded-mode line for the given field label.

    Spec §Q5 mandates the format:
        <label>: unavailable (daemon path down — try: hades doctor)

    Color: warn-orange (#ffa726) per spec §Q5 + Plan 18a Phase B palette.
    The label is colored too so the eye lands on the degraded marker
    immediately. C-3 ships the literal text; C-4 wires the color.
    """
    padded = label.ljust(11)
    line_text = f"{padded}{_DEGRADED_HINT}"
    colored = _safe_colorize(line_text, "warn")
    return f"  {colored}"


def _render_human(responses: dict[str, dict[str, Any] | None]) -> str:
    """Render the 8-line human-readable block per spec §Q5 template.

    Per-field degraded mode: if a response is None (endpoint returned
    non-2xx, raised a transport error, or returned malformed JSON), the
    corresponding line surfaces the degraded hint instead of the
    happy-path text. The other fields continue to render normally.

    Color application per spec §Q5 + Plan 18a Phase B palette:
      - 'ok' / 'live' state markers: ok-green #10b981
      - Body text (PID, UDS, counts, percentages): muted-gray #999
      - Degraded fields (whole line): warn-orange #ffa726
      - Top-level errors (handled via _render_error): fail-crimson #c41e3a

    Spec §Q5: NEVER fails whole command on single field degradation.
    """
    health = responses.get("/v1/health")
    cascade = responses.get("/v1/cascade/state")
    bypass = responses.get("/v1/bypass/status")
    cost = responses.get("/v1/cost/24h")
    context = responses.get("/v1/context/used")
    profile = responses.get("/v1/profile/active")
    cwd = responses.get("/v1/cwd")

                                                                   
    version = (health or {}).get("version", "unknown")
    header = f"HADES system v{version} — runtime status"
    lines: list[str] = [_safe_colorize(header, "ok")]

                 
    if health is None:
        lines.append(_degraded_line("daemon:"))
    else:
        pid = health.get("pid", "?")
        uds = health.get("uds_path", "?")
        ok_marker = _safe_colorize("ok", "ok")
        body = _safe_colorize(f"(PID {pid}, UDS {uds})", "muted")
        lines.append(f"  daemon:    {ok_marker} {body}")

                                                                
    if health is None:
        lines.append(_degraded_line("model:"))
    else:
        model = health.get("active_model", "?")
        lines.append(f"  model:     {_safe_colorize(str(model), 'muted')}")

                  
    if cascade is None:
        lines.append(_degraded_line("cascade:"))
    else:
        tier = cascade.get("active_tier", "?")
        tier_name = cascade.get("tier_name", "?")
        provider_count = cascade.get("provider_count", "?")
        body = _safe_colorize(
            f"tier {tier} ({tier_name}) · {provider_count} providers registered",
            "muted",
        )
        lines.append(f"  cascade:   {body}")

                                                                    
    if bypass is None:
        lines.append(_degraded_line("bypass:"))
    else:
        bypass_status = bypass.get("status", "?")
        success_rate = bypass.get("success_rate_24h", 0.0)
        success_pct = f"{success_rate * 100:.1f}%"
        if bypass_status == "live":
            status_marker = _safe_colorize(str(bypass_status), "ok")
        elif bypass_status == "degraded":
            status_marker = _safe_colorize(str(bypass_status), "warn")
        else:
            status_marker = _safe_colorize(str(bypass_status), "muted")
        body = _safe_colorize(f"· success 24h: {success_pct}", "muted")
        lines.append(f"  bypass:    {status_marker} {body}")

               
    if cost is None:
        lines.append(_degraded_line("cost 24h:"))
    else:
        spend_24h = cost.get("spend_24h_usd", 0.0)
        spend_session = cost.get("spend_session_usd", 0.0)
        body = _safe_colorize(
            f"${spend_24h:.3f} (this session: ${spend_session:.3f})",
            "muted",
        )
        lines.append(f"  cost 24h:  {body}")

                  
    if context is None:
        lines.append(_degraded_line("context:"))
    else:
        used = context.get("used_tokens", 0)
        max_tokens = context.get("max_tokens", 0)
        pct = int(round(used / max_tokens * 100)) if max_tokens else 0
        used_fmt = f"{used:,}"
        max_fmt = f"{max_tokens:,}"
        body = _safe_colorize(f"{pct}% ({used_fmt} / {max_fmt} tokens)", "muted")
        lines.append(f"  context:   {body}")

                  
    if profile is None:
        lines.append(_degraded_line("profile:"))
    else:
        profile_name = profile.get("profile_name", "?")
        profile_kind = profile.get("kind", "?")
        body = _safe_colorize(f"{profile_name} ({profile_kind})", "muted")
        lines.append(f"  profile:   {body}")

                                                
    if cwd is None:
        lines.append(_degraded_line("cwd:"))
    else:
        cwd_path = cwd.get("cwd", "?")
        home = os.environ.get("HOME", "")
        if home and cwd_path.startswith(home):
            cwd_path = "~" + cwd_path[len(home) :]
        lines.append(f"  cwd:       {_safe_colorize(str(cwd_path), 'muted')}")

    return "\n".join(lines)


def _classify_field_state(response: dict[str, Any] | None) -> str:
    """Return 'ok' if response is non-None, 'degraded' otherwise.

    Delegates to ``status_core.classify_field_state()``. Private alias
    preserved for backward-compat (test introspection + any patches).
    """
    return status_core.classify_field_state(response)


def _render_json(responses: dict[str, dict[str, Any] | None]) -> str:
    """Render the schema-v1 JSON payload per spec §Q5 + inv-zen-221.

    Schema-v1 shape (frozen for the lifetime of v1 consumers):
        {
          "schema_version": 1,
          "rendered_at": "<ISO-8601 UTC>",
          "fields": {
            "daemon":   {"state": ..., "pid": ..., "uds_path": ...},
            "model":    {"state": ..., "active_model": ...},
            "cascade":  {"state": ..., "active_tier": ..., ...},
            "bypass":   {"state": ..., "status": ..., ...},
            "cost_24h": {"state": ..., "spend_24h_usd": ..., ...},
            "context":  {"state": ..., "used_tokens": ..., ...},
            "profile":  {"state": ..., "profile_name": ..., ...},
            "cwd":      {"state": ..., "cwd": ...}
          }
        }

    Future bumps (v2, v3 ...) per ADR-0097 (Phase G ships).
    """
    health = responses.get("/v1/health")
    cascade = responses.get("/v1/cascade/state")
    bypass = responses.get("/v1/bypass/status")
    cost = responses.get("/v1/cost/24h")
    context = responses.get("/v1/context/used")
    profile = responses.get("/v1/profile/active")
    cwd = responses.get("/v1/cwd")

                  
    daemon_field: dict[str, Any] = {"state": _classify_field_state(health)}
    if health is not None:
        daemon_field["pid"] = health.get("pid")
        daemon_field["uds_path"] = health.get("uds_path")

                                              
    model_field: dict[str, Any] = {"state": _classify_field_state(health)}
    if health is not None:
        model_field["active_model"] = health.get("active_model")

                   
    cascade_field: dict[str, Any] = {"state": _classify_field_state(cascade)}
    if cascade is not None:
        cascade_field["active_tier"] = cascade.get("active_tier")
        cascade_field["tier_name"] = cascade.get("tier_name")
        cascade_field["provider_count"] = cascade.get("provider_count")

                  
    bypass_field: dict[str, Any] = {"state": _classify_field_state(bypass)}
    if bypass is not None:
        bypass_field["status"] = bypass.get("status")
        bypass_field["success_rate_24h"] = bypass.get("success_rate_24h")

                
    cost_field: dict[str, Any] = {"state": _classify_field_state(cost)}
    if cost is not None:
        cost_field["spend_24h_usd"] = cost.get("spend_24h_usd")
        cost_field["spend_session_usd"] = cost.get("spend_session_usd")

                   
    context_field: dict[str, Any] = {"state": _classify_field_state(context)}
    if context is not None:
        context_field["used_tokens"] = context.get("used_tokens")
        context_field["max_tokens"] = context.get("max_tokens")

                   
    profile_field: dict[str, Any] = {"state": _classify_field_state(profile)}
    if profile is not None:
        profile_field["profile_name"] = profile.get("profile_name")
        profile_field["kind"] = profile.get("kind")

               
    cwd_field: dict[str, Any] = {"state": _classify_field_state(cwd)}
    if cwd is not None:
        cwd_field["cwd"] = cwd.get("cwd")

    payload = {
        "schema_version": SCHEMA_VERSION,
        "rendered_at": datetime.datetime.now(datetime.UTC).isoformat(),
        "fields": {
            "daemon": daemon_field,
            "model": model_field,
            "cascade": cascade_field,
            "bypass": bypass_field,
            "cost_24h": cost_field,
            "context": context_field,
            "profile": profile_field,
            "cwd": cwd_field,
        },
    }
    return json.dumps(payload, indent=2, sort_keys=False)


def _render_error(envelope: dict[str, str]) -> str:
    """Render the spec §Q6 three-line HADES block.

    Format:
        HADES: <title>
          <body>
          → <recovery_hint>

    Color: HADES: prefix in fail-crimson #c41e3a; body in muted-gray
    #999; recovery arrow + hint in ok-green #10b981. Mirrors the
    Phase B Go-side Render() output shape.
    """
    title = envelope.get("title", "internal error")
    body = envelope.get("body", "no body provided")
    recovery = envelope.get("recovery_hint", "no recovery hint provided")

    headline = _safe_colorize(f"HADES: {title}", "fail")
    body_colored = _safe_colorize(body, "muted")
    recovery_colored = _safe_colorize(f"→ {recovery}", "ok")

    return f"{headline}\n  {body_colored}\n  {recovery_colored}"


def _render_error_json(envelope: dict[str, str]) -> str:
    """JSON-mode counterpart to `_render_error`. Surfaces the error
    envelope under a top-level `error` key alongside `schema_version`."""
    payload = {
        "schema_version": SCHEMA_VERSION,
        "rendered_at": datetime.datetime.now(datetime.UTC).isoformat(),
        "error": {
            "code": envelope.get("code", _CODE_DAEMON_NOT_RUNNING),
            "title": envelope.get("title", ""),
            "body": envelope.get("body", ""),
            "recovery_hint": envelope.get("recovery_hint", ""),
        },
    }
    return json.dumps(payload, indent=2, sort_keys=False)


def _detect_structured_error_envelope(
    responses: dict[str, dict[str, Any] | None],
) -> dict[str, str] | None:
    """Inspect responses for the daemon's structured-error envelope.

    Delegates to ``status_core.detect_structured_error_envelope()``.
    Private alias preserved for backward-compat (test introspection).
    """
    return status_core.detect_structured_error_envelope(responses)


def _is_json_mode(raw_args: str) -> bool:
    """Whole-word `--json` flag detection in raw_args.

    Contract:
        - Case-sensitive: `--JSON` does NOT trigger json mode.
        - Whitespace-tolerant: leading/trailing/internal whitespace
          around the flag is fine.
        - Whole-token: `--json-pretty` does NOT trigger (would be
          a different flag in a hypothetical extension).
        - Forward-compat: unknown flags (e.g., `--bogus`) do NOT raise
          — they fall through to text mode silently. Strict
          rejection would surface via Phase B Render with code
          `cli.arg-validation-fail` if the handler grew an argparse
          surface in a future plan.

    Args:
        raw_args: trailing text after `/hades:status` slash command.

    Returns:
        True if `--json` appears as a discrete whitespace-separated
        token in raw_args; False otherwise.
    """
    if not raw_args or not raw_args.strip():
        return False
    return "--json" in raw_args.split()


def handle_status(raw_args: str) -> str | None:
    """Handler for /hades:status slash command.

    Args:
        raw_args: trailing text after the command name. Recognized
            tokens (whitespace-separated; case-sensitive):
                --json   Emit machine-readable JSON output per
                         schema-v1 (inv-zen-221 anchor) instead of
                         the spec §Q5 human-readable block.

            Unknown flags are tolerated (forward-compat) and fall
            through to default (text) mode. A future plan may add
            stricter argparse + reject unknown flags via Phase A
            catalog code `cli.arg-validation-fail`.

    Returns:
        Multi-line block (text mode) OR JSON payload string (--json
        mode). Returns the spec §Q6 three-line HADES error block on
        top-level failure (UDS missing / structured-error envelope).
    """
    uds_path = os.environ.get("ZEN_SWARM_UDS") or _DEFAULT_UDS_PATH

                                                         
    if not os.path.exists(uds_path):
        if _is_json_mode(raw_args):
            return _render_error_json(_LOCAL_DAEMON_NOT_RUNNING_ENVELOPE)
        return _render_error(_LOCAL_DAEMON_NOT_RUNNING_ENVELOPE)

    client = _build_client()
    try:
        responses = asyncio.run(_query_daemon(client))
    except httpx.HTTPError:
                                                                   
                                                                   
                                                              
                                                               
                                              
        if _is_json_mode(raw_args):
            return _render_error_json(_LOCAL_DAEMON_NOT_RUNNING_ENVELOPE)
        return _render_error(_LOCAL_DAEMON_NOT_RUNNING_ENVELOPE)
    finally:
        with contextlib.suppress(RuntimeError, httpx.HTTPError):
            asyncio.run(client.aclose())

                                                                 
    envelope = _detect_structured_error_envelope(responses)
    if envelope is not None:
        if _is_json_mode(raw_args):
            return _render_error_json(envelope)
        return _render_error(envelope)

                                            
    if _is_json_mode(raw_args):
        return _render_json(responses)
    return _render_human(responses)
