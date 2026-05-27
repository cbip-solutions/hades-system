# SPDX-License-Identifier: MIT
"""Auto-launch the onboarding wizard on first-run if ~/.config/hades-system/config.toml absent."""

from __future__ import annotations

import logging
import os
import shutil
import signal
import subprocess
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

# The subprocess command target — HADES design A wrapper subcommand that
# dispatches to HADES design `hades config init`. Operator may override binary
# lookup via HADES_BIN env (test fixtures + custom installs). Default:
# shutil.which("hades") then shutil.which("hades") fallback.
_HADES_BIN_ENV = "HADES_BIN"
_NO_WIZARD_ENV = "HADES_NO_WIZARD"
_HERMES_SKIN_ENV = "HERMES_SKIN"
_XDG_CONFIG_ENV = "XDG_CONFIG_HOME"
_HADES_SKIN_NAME = "hades"

# The 3 wizard.* catalog codes from HADES design release track
# (internal/errors/codes.go). The hook does NOT directly route through
# Render (the Go wrapper at the subprocess boundary owns rendering); it
# logs these code names for operator traceability + release track invariant
# compliance grep.
_WIZARD_CATALOG_CODES = (
    "wizard.config-corrupt",
    "wizard.migrate-incomplete",
    "wizard.mcp-spawn-fail",
)

# The reserved defense-in-depth fallback code from HADES design release track
# A-6 (catalog row "internal-uncaught"). Used when the subprocess
# machinery itself fails before the wizard could emit its own error.
_INTERNAL_UNCAUGHT_CODE = "internal-uncaught"


def _config_path() -> Path:
    """Resolve the canonical `~/.config/hades-system/config.toml` path.

    Mirrors internal/onboard/paths.go:GlobalConfigPath() — honors
    XDG_CONFIG_HOME if set; falls back to $HOME/.config otherwise.
    """
    xdg = os.environ.get(_XDG_CONFIG_ENV, "").strip()
    if xdg:
        return Path(xdg) / "hades-system" / "config.toml"
    home = os.environ.get("HOME", "").strip()
    if not home:
        # Defensive: no HOME env (degenerate process env). Fall back to
        # the path expansion which returns ~/ verbatim in this case;
        # callers treat the file as "not present" downstream.
        return Path.home() / ".config" / "hades-system" / "config.toml"
    return Path(home) / ".config" / "hades-system" / "config.toml"


def _should_launch_wizard() -> bool:
    """Return True iff first-run conditions are met.

    Conditions (per spec §5.1 + amendment 2026-05-21):
    1. ~/.config/hades-system/config.toml does NOT exist
    2. HADES_NO_WIZARD env is NOT set to "1" (the escape hatch)

    The hook caller layers ADDITIONAL defensive guards (cwd non-empty,
    HERMES_SKIN=hades) on top — those are non-interactive-degeneracy
    short-circuits, not first-run-signal logic. They live in
    `_maybe_launch_wizard`, not here, for separation-of-concerns.
    """
    if os.environ.get(_NO_WIZARD_ENV, "").strip() == "1":
        return False
    return not _config_path().is_file()


def _is_signal_cancel(returncode: int) -> bool:
    """Return True iff returncode encodes a SIGINT or SIGTERM cancel.

    Recognizes two conventions:
    - Unix exit-code form: 128 + signal_number (e.g., 130 = SIGINT, 143 = SIGTERM)
    - Python subprocess.run form: -signal_number (e.g., -2 = SIGINT)

    Used by _maybe_launch_wizard to distinguish operator-cancellation
    (cancel, log INFO, next session re-launches) from wizard-internal
    error (route through Render with catalog code, log WARN).
    """
    # Python subprocess form: negative signal number
    if returncode == -signal.SIGINT or returncode == -signal.SIGTERM:
        return True
    # Unix exit-code form: 128 + signal_number
    return returncode in (128 + signal.SIGINT, 128 + signal.SIGTERM)


def _resolve_hades_bin() -> str | None:
    """Locate the `hades` binary for subprocess invocation.

    Precedence:
    1. HADES_BIN env (operator override; test fixtures)
    2. shutil.which("hades") — PATH lookup
    3. None — caller treats as fatal-not-found (logs + no-op)

    Returns absolute path or None. Never raises.
    """
    override = os.environ.get(_HADES_BIN_ENV, "").strip()
    if override:
        return override
    found = shutil.which("hades")
    if found:
        return found
    return None


def _is_interactive_stdin() -> bool:
    """Return True iff stdin is a TTY.

    HADES design bubbletea wizard requires a TTY for prompt rendering;
    spawning the subprocess in a non-TTY session would emit a wizard-side
    error after the subprocess-start cost. This helper detects the
    condition early so the hook can no-op silently.

    Uses os.isatty(0) — fileno 0 = stdin. Safe against detached file
    descriptors (returns False for closed / redirected stdin).
    """
    try:
        return os.isatty(0)
    except (OSError, ValueError):
        # stdin closed or invalid fd → conservatively non-interactive
        return False


def _maybe_launch_wizard(
    session_id: str = "",
    cwd: str = "",
    source: str = "",
    **kwargs: Any,
) -> None:
    """Hermes ``on_session_start`` hook: auto-launch first-run wizard.

    Triggered every session start. Short-circuits cleanly in any of:
    - HERMES_SKIN != "hades" (operator invoked Hermes directly, not via
      `hades` wrapper — opted out of HADES brand entirely)
    - cwd empty (degenerate session without working directory)
    - non-TTY stdin (CI / sandboxed Hermes invocation)
    - HADES_NO_WIZARD=1 (operator escape hatch via `hades --no-wizard`)
    - config.toml present (subsequent session, not first run)

    On trigger: spawns `hades config init` subprocess (HADES design wizard
    surface). Hands off stdin/stdout/stderr so the operator drives the
    interactive bubbletea TUI in their terminal. The subprocess writes
    config.toml on success; subsequent session-start invocations of this
    hook see the file + skip the wizard.

    Returns None (observer hook; Hermes ignores the return value).
    """
    # Defensive guard: operator must be using the HADES wrapper.
    # When HERMES_SKIN is anything else, no-op silently.
    if os.environ.get(_HERMES_SKIN_ENV, "").strip().lower() != _HADES_SKIN_NAME:
        return

    # Defensive guard: explicit escape hatch (operator opted out via --no-wizard).
    # The _should_launch_wizard predicate also checks this env; the explicit
    # log here is for operator traceability via HADES_LOG_LEVEL=debug.
    if os.environ.get(_NO_WIZARD_ENV, "").strip() == "1":
        logger.debug(
            "HADES wizard auto-launch suppressed: %s=1 escape hatch in effect",
            _NO_WIZARD_ENV,
        )
        return

    # Defensive guard: degenerate session (cwd empty/unset). Launching an
    # interactive wizard from a process without a meaningful cwd would either
    # hang waiting for input that never arrives OR write config in an
    # unexpected location. No-op silently.
    if not cwd.strip():
        logger.debug(
            "HADES wizard auto-launch suppressed: empty cwd (degenerate session)"
        )
        return

    # Defensive guard: non-TTY stdin (CI / piped invocation). HADES design wizard
    # is bubbletea-based; spawning it without a TTY would emit a wizard-side
    # error AFTER the subprocess start cost. Detect early + no-op.
    if not _is_interactive_stdin():
        logger.debug("HADES wizard auto-launch suppressed: stdin is not a TTY")
        return

    if not _should_launch_wizard():
        return

    bin_path = _resolve_hades_bin()
    if bin_path is None:
        logger.warning(
            "HADES wizard auto-launch skipped: hades binary not on PATH "
            "(set HADES_BIN env to override, or install via brew/make)."
        )
        return

    # Subprocess args: `hades config init` (HADES design release track global wizard).
    # Stdio handoff: inherit from this process so the bubbletea TUI drives
    # the operator's terminal directly. Lazygit-pattern terminal handoff —
    # bubbletea handles its own alt-screen so the post-subprocess return
    # is seamless.
    argv = [bin_path, "config", "init"]
    logger.debug(
        "HADES wizard launching: argv=%r cwd=%s session=%s source=%s",
        argv,
        cwd,
        session_id,
        source,
    )
    try:
        result = subprocess.run(
            argv,
            stdin=None,
            stdout=None,
            stderr=None,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        # Subprocess machinery itself failed (e.g., binary unexpectedly
        # removed between which() and run()). Log + no-op; do NOT raise
        # because hooks must never abort session start.
        logger.warning(
            "HADES wizard subprocess exec failed: %s. Fallback catalog "
            "code: %s. Hook continues; session start NOT aborted.",
            exc,
            _INTERNAL_UNCAUGHT_CODE,
        )
        return

    if result.returncode == 0:
        logger.info(
            "HADES wizard complete (rc=0). config.toml written; "
            "subsequent sessions will skip the wizard."
        )
    elif _is_signal_cancel(result.returncode):
        logger.info(
            "HADES wizard cancelled by operator (rc=%d). "
            "Re-launching on next session start until config.toml is written.",
            result.returncode,
        )
    else:
        # Non-cancel non-zero exit = wizard-internal error. The Go
        # wrapper has ALREADY routed through Render at its own RunE
        # boundary (HADES design release track contract); the operator-visible
        # HADES error block is already on stderr. The hook logs a
        # structured trace entry referencing the candidate wizard.*
        # catalog codes for operator traceability + invariant
        # compliance grep.
        logger.warning(
            "HADES wizard exited with error (rc=%d). The wrapper has "
            "already rendered the operator-visible HADES error block "
            "to stderr (see above). Candidate catalog codes: %s. "
            "Subsequent sessions will re-launch the wizard until "
            "config.toml is written.",
            result.returncode,
            ", ".join(_WIZARD_CATALOG_CODES),
        )
    return
