# SPDX-License-Identifier: MIT
"""/hades:dashboard slash command handler — subprocess handoff to TUI dashboard."""

from __future__ import annotations

from hermes_plugins.hades.commands._subprocess_handoff import run_hades_subprocess


def dashboard_handler(raw_args: str) -> str | None:
    """/hades:dashboard handler — spawn `hades dashboard` on the default panel.

    Args:
        raw_args: Operator's args after the slash command name. For
            /hades:dashboard (no args), this is the empty string. Future
            enhancements may parse sub-flags here, but release track ships bare
            invocation.

    Returns:
        None when the TUI exits cleanly (returncode 0) — Hermes convention:
        no message to display when TUI exits cleanly.
        A HADES-branded error string on any failure path (binary missing,
        terminal not TTY, subprocess crash, non-zero returncode, SIGINT cancel).

    Per spec §Q8 D-pattern. See _subprocess_handoff.run_hades_subprocess
    for the lazygit-pattern terminal save/restore implementation.
    """
    # raw_args is ignored — /hades:dashboard takes no positional args.
    # Future: parse --skin or --verbose passthrough here if needed.
    return run_hades_subprocess(extra_args=[])
