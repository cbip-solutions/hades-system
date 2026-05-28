# SPDX-License-Identifier: MIT
"""Shared subprocess-handoff helper for /hades:dashboard + /hades:panel."""

from __future__ import annotations

import contextlib
import shutil
import subprocess
import sys
import termios
from typing import Final


def render_hades_block(title: str, body: str, recovery: str) -> str:
    """Construct a HADES three-line block per design contract

    Format:
        HADES: <title>
          <body>
          → <recovery>

    Used by both dashboard.py and panel.py to render error blocks LOCALLY
    (stage C-5 operator policy: no daemon error-render
    roundtrip; render locally using stage catalog text). Mirrors the
    three-line format of stage Go-side Render().

    Per invariant (visible-strings-HADES preserved): all strings returned by
    this function contain the HADES brand by construction.
    """
    return f"HADES: {title}\n  {body}\n  → {recovery}\n"


# POSIX convention: subprocess killed by signal N exits with code 128 + N.
# SIGINT = 2 → returncode 130.
_SIGINT_RETURNCODE: Final[int] = 130


def run_hades_subprocess(extra_args: list[str]) -> str | None:
    """Run the `hades dashboard [extra_args...]` subprocess with terminal handoff.

    Preconditions:
        - `hades` binary is (or might be) on PATH.
        - stdin is (or might be) TTY-attached.
        - extra_args is a list of additional args to pass after "dashboard"
          (e.g., [] for /hades:dashboard, ["--panel=codegraph"] for /hades:panel).

    Args:
        extra_args: Args to append after "dashboard" in the argv. For
            /hades:dashboard (no panel), this is []. For /hades:panel,
            this is ["--panel=<name>"].

    Returns:
        None on clean exit (returncode 0) — Hermes convention.
        A HADES-branded string for all error paths:
        - hades binary not on PATH
        - stdin not TTY-attached (termios.error on tcgetattr)
        - subprocess.run raises (OSError, SubprocessError)
        - KeyboardInterrupt mid-subprocess (operator Ctrl+C)
        - subprocess returncode 130 (POSIX SIGINT)
        - subprocess returncode non-zero other

    per design contract: lazygit-style subprocess handoff. Terminal mode
    is captured before spawn and restored in finally block.

    Per invariant prep (stage): this function NEVER raises at the
    slash-command boundary; all paths return either a rendered string or
    None.
    """
    # 1. Locate hades binary.
    hades_bin = shutil.which("hades")
    if hades_bin is None:
        return render_hades_block(
            title="HADES wrapper binary not found.",
            body=(
                "The slash command requires the `hades` wrapper binary to be "
                "on PATH. shutil.which('hades') returned None."
            ),
            recovery=(
                "verify: which hades (expected: /usr/local/bin/hades or similar); "
                "install: make build && make install; "
                "see docs/operations/hades-entry-point.md §installation"
            ),
        )

    # 2. Capture terminal mode.
    stdin_fd = sys.stdin.fileno()
    try:
        original_attrs = termios.tcgetattr(stdin_fd)
    except termios.error:
        return render_hades_block(
            title="Terminal not TTY-attached.",
            body=(
                "The slash command requires a TTY-attached stdin to hand off "
                "terminal control to the bubbletea TUI. termios.tcgetattr "
                "raised — stdin is piped or redirected."
            ),
            recovery=(
                "verify: tty (expected: a /dev/ttysNNN path, NOT 'not a tty'); "
                "see docs/operations/tui.md §terminal-compat"
            ),
        )

    # 3. Run the subprocess in try/finally — restore terminal mode no matter what.
    argv = [hades_bin, "dashboard", *extra_args]
    returncode: int | None = None
    error_path: str | None = None
    try:
        try:
            completed = subprocess.run(
                argv,
                stdin=sys.stdin,
                stdout=sys.stdout,
                stderr=sys.stderr,
                check=False,
            )
            returncode = completed.returncode
        except KeyboardInterrupt:
            # SIGINT arrived mid-subprocess (operator hit Ctrl+C). The subprocess
            # may have exited cleanly via its own signal handler; we treat this
            # as a SIGINT cancel.
            returncode = _SIGINT_RETURNCODE
            error_path = "sigint"
        except (OSError, subprocess.SubprocessError) as exc:
            error_path = f"subprocess_error:{type(exc).__name__}:{exc}"
    finally:
        # Restore terminal mode EXACTLY ONCE. The finally block guarantees this
        # runs whether subprocess.run returned normally, raised, or
        # KeyboardInterrupt was caught above.
        # tcsetattr can fail if fd was closed mid-flight; silently absorb.
        # A failed restore is logged separately; the user-visible result
        # is already collected above.
        with contextlib.suppress(termios.error):
            termios.tcsetattr(stdin_fd, termios.TCSADRAIN, original_attrs)

    # 4. Map subprocess outcome to return value.
    if error_path == "sigint":
        return render_hades_block(
            title="HADES dashboard cancelled by operator.",
            body=(
                "The bubbletea TUI was interrupted (SIGINT, returncode 130). "
                "Terminal context restored; Hermes session resumes."
            ),
            recovery=(
                "to re-launch the dashboard: /hades:dashboard; "
                "to land on a specific panel: /hades:panel <name>"
            ),
        )
    if error_path is not None:
        return render_hades_block(
            title="HADES dashboard subprocess failed.",
            body=(
                f"subprocess.run({argv!r}) raised: {error_path}. "
                "The bubbletea TUI did not complete cleanly."
            ),
            recovery=(
                "verify hades binary: ls -la $(which hades); "
                "verify daemon: hades doctor; "
                "see docs/operations/hades-entry-point.md §troubleshooting"
            ),
        )
    if returncode == 0:
        return None  # Clean exit.
    if returncode == _SIGINT_RETURNCODE:
        return render_hades_block(
            title="HADES dashboard cancelled by operator.",
            body=(
                "The bubbletea TUI exited via SIGINT (returncode 130). "
                "Terminal context restored; Hermes session resumes."
            ),
            recovery=("to re-launch: /hades:dashboard or /hades:panel <name>"),
        )
    # Any other non-zero returncode.
    return render_hades_block(
        title=f"HADES dashboard exited with code {returncode}.",
        body=(
            f"The bubbletea TUI exited non-zero (returncode {returncode}). "
            "This typically indicates the daemon became unreachable, the "
            "terminal lost TTY status, or bubbletea hit an internal error."
        ),
        recovery=(
            "check daemon: hades doctor; "
            f"re-run with verbose: hades dashboard {' '.join(extra_args)} --verbose; "
            "see docs/operations/tui.md §troubleshooting"
        ),
    )
