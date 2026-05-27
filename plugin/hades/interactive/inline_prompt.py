# SPDX-License-Identifier: MIT
"""Render-safe inline confirm/choose helper for HADES slash-handlers."""

from __future__ import annotations

import os
import threading
from collections.abc import Callable
from typing import Any

# Default TUI wait timeout in seconds. Tests may monkeypatch to a small value.
_TUI_TIMEOUT: float = 60.0

# Truthy answer set for confirm().
_TRUTHY_ANSWERS = frozenset({"y", "yes", "o", "once", "session", "always"})


# ---------------------------------------------------------------------------
# Private prompt_toolkit indirections
# ---------------------------------------------------------------------------


def _get_app_or_none() -> object | None:
    """Return the running prompt_toolkit Application, or None.

    Lazy-imports prompt_toolkit; returns None on ImportError or when no app
    is currently running.  Tests monkeypatch this to control the branch.
    """
    try:
        from prompt_toolkit.application.current import (  # type: ignore[import-not-found]
            get_app_or_none,
        )

        result: object | None = get_app_or_none()
        return result
    except ImportError:
        return None


def _run_in_terminal(func: Callable[[], None]) -> None:
    """Schedule *func* to run inside prompt_toolkit's ``run_in_terminal``.

    Lazy-imports prompt_toolkit; on ImportError calls *func* directly
    (best-effort fallback for environments without prompt_toolkit). Tests
    monkeypatch this to call *func* synchronously.
    """
    try:
        from prompt_toolkit.application import (  # type: ignore[import-not-found]
            run_in_terminal,
        )

        run_in_terminal(func)
    except ImportError:
        func()


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------


def _truthy(answer: str | None, default: bool) -> bool:
    """Map a raw answer string to True/False.

    An answer in ``_TRUTHY_ANSWERS`` (case-insensitive) → True.
    Anything else → False. ``None`` (e.g. a Hermes seam that returns None
    when the user cancels) returns *default* instead of raising AttributeError,
    preserving the documented "NEVER raises" contract on ``confirm()``.
    """
    if answer is None:
        return default
    return answer.strip().lower() in _TRUTHY_ANSWERS


def _prompt_terminal_confirm(question: str, default: bool) -> bool:
    """Obtain a yes/no answer via the appropriate terminal path.

    Uses the ``run_in_terminal`` + threading.Event rendezvous when a live
    TUI is running (uses run_in_terminal for render-safe input).
    Falls back to raw ``input()`` for HERMES_INTERACTIVE.
    Returns ``default`` for gateway/non-tty or on any I/O failure.
    """
    app = _get_app_or_none()

    hint = "[y/N]" if not default else "[Y/n]"
    full_prompt = f"{question} {hint} "

    if app is not None:
        # Live TUI: schedule input on the event-loop thread, wait from here.
        result: list[bool] = [default]
        done = threading.Event()

        def _ask() -> None:
            try:
                raw = input(full_prompt)
                result[0] = _truthy(raw, default)
            except (EOFError, KeyboardInterrupt):
                result[0] = default
            finally:
                done.set()

        _run_in_terminal(_ask)
        done.wait(timeout=_TUI_TIMEOUT)
        return result[0]

    if os.getenv("HERMES_INTERACTIVE"):
        try:
            raw = input(full_prompt)
            return _truthy(raw, default)
        except (EOFError, KeyboardInterrupt):
            return default

    # Gateway / non-tty: never block.
    return default


def _prompt_terminal_choose(prompt: str, options: list[str]) -> str | None:
    """Obtain a choice from *options* via the appropriate terminal path.

    Returns the chosen option string iff it is in *options*; None otherwise.
    Returns None for gateway/non-tty or on any I/O failure.
    """
    app = _get_app_or_none()

    opts_display = "/".join(options)
    full_prompt = f"{prompt} ({opts_display}): "

    if app is not None:
        result: list[str | None] = [None]
        done = threading.Event()

        def _ask() -> None:
            try:
                raw = input(full_prompt).strip()
                result[0] = raw if raw in options else None
            except (EOFError, KeyboardInterrupt):
                result[0] = None
            finally:
                done.set()

        _run_in_terminal(_ask)
        done.wait(timeout=_TUI_TIMEOUT)
        return result[0]

    if os.getenv("HERMES_INTERACTIVE"):
        try:
            raw = input(full_prompt).strip()
            return raw if raw in options else None
        except (EOFError, KeyboardInterrupt):
            return None

    # Gateway / non-tty: never block.
    return None


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


def confirm(
    question: str,
    *,
    default: bool,
    ctx: Any = None,
) -> bool:
    """Ask the operator a yes/no question, returning a bool.

    Args:
        question: The question to display (e.g. ``"Overwrite config.yaml?"``).
        default:  The answer returned when no interactive surface is available,
                  when the operator sends EOF, or when the prompt times out.
        ctx:      Optional Hermes plugin context object. When ``ctx`` exposes
                  ``request_user_input``, that seam is used (per-call hasattr
                  guard; dormant in Hermes v0.13.0).

    Returns:
        ``True`` if the operator answered affirmatively (y/yes/o/once/session/
        always), ``False`` otherwise.  NEVER raises; NEVER blocks a
        gateway/non-tty session.

    Pre-condition: *question* is a non-empty human-readable string.
    Post-condition: return value is a plain ``bool``; no I/O side-effect
        when no interactive surface is present.

    Example::

        if confirm("Overwrite config.yaml?", default=False, ctx=ctx):
            config_path.write_text(new_content)
    """
    # 1. Hermes gateway seam (hasattr-guarded — inv-hades-262).
    if ctx is not None and hasattr(ctx, "request_user_input"):
        hint = "[y/N]" if not default else "[Y/n]"
        answer: str = ctx.request_user_input(
            f"{question} {hint} ",
            ["y", "n"],
        )
        return _truthy(answer, default)

    # 2-4. Terminal path (live TUI / HERMES_INTERACTIVE / gateway fallback).
    return _prompt_terminal_confirm(question, default)


def choose(
    prompt: str,
    options: list[str],
    *,
    ctx: Any = None,
) -> str | None:
    """Ask the operator to pick one of *options*, returning the chosen string.

    Args:
        prompt:  The question to display (e.g. ``"Select backend"``).
        options: Non-empty list of valid option strings.
        ctx:     Optional Hermes plugin context.  When ``ctx`` exposes
                 ``request_user_input``, that seam is used (hasattr-guarded).

    Returns:
        The selected option string if it is in *options*; ``None`` when no
        interactive surface is available, when the answer is not in *options*,
        on EOF/KeyboardInterrupt, or on timeout. NEVER raises.

    Pre-condition: *options* is a non-empty list of strings.
    Post-condition: return value is either a member of *options* or ``None``.

    Example::

        backend = choose("Select backend", ["ollama", "anthropic"], ctx=ctx)
        if backend is not None:
            cfg["backend"] = backend
    """
    # 1. Hermes gateway seam (hasattr-guarded — inv-hades-262).
    if ctx is not None and hasattr(ctx, "request_user_input"):
        answer = ctx.request_user_input(
            f"{prompt} ({'/'.join(options)}): ",
            options,
        )
        return answer if answer in options else None

    # 2-4. Terminal path.
    return _prompt_terminal_choose(prompt, options)
