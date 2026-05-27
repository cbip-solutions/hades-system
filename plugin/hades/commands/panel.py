# SPDX-License-Identifier: MIT
"""/hades:panel <name> slash command handler — subprocess handoff to TUI panel."""

from __future__ import annotations

from typing import Final

from hermes_plugins.hades.commands._subprocess_handoff import (
    render_hades_block,
    run_hades_subprocess,
)

# Frozen 12-panel enum per spec §Q8 + the release design view files.
# Membership check uses the frozenset for O(1) lookup.
_VALID_PANELS: frozenset[str] = frozenset(
    {
        "workforce",
        "cost",
        "audit",
        "hra",
        "confirmations",
        "memory",
        "skills",
        "doctrine",
        "codegraph",
        "inbox",
        "crossproject",
        "help",
    }
)

# ------------------------------------------------------------------
# C-5 local-only render (release stage operator policy).
#
# release track `cli.arg-validation-fail` catalog entry is the single source of
# truth for the error text. Its Title / BodyTemplate / RecoveryHint fields are
# defined in Go at internal/errors/codes.go (search "cli.arg-validation-fail").
# release track renders the block LOCALLY — no daemon error-render roundtrip —
# using the catalog literals reproduced below. The sister-test in
# test_panel.py asserts these literals match release track catalog fields
# byte-for-byte, so any drift in the Go catalog surfaces as a failing test
# that forces reconciliation (release track remains canonical).
#
# CANONICAL SOURCE (do not edit here without reconciling Go catalog):
#   internal/errors/codes.go → ErrorCode["cli.arg-validation-fail"]
# ------------------------------------------------------------------

# Title field of release track cli.arg-validation-fail catalog entry (byte-for-byte).
_PANEL_VALIDATION_TITLE: Final[str] = "Argument validation failed."

# BodyTemplate field of release track cli.arg-validation-fail catalog entry
# (byte-for-byte).
_PANEL_VALIDATION_BODY: Final[str] = (
    "One of the flags or positional arguments failed validation (wrong type, "
    "out of range, conflicting flags, or a required flag missing). The "
    "subcommand did not run."
)

# RecoveryHint field of release track cli.arg-validation-fail catalog entry
# (byte-for-byte). This enumerates the 12-panel allowlist verbatim, so release track
# does NOT duplicate the panel enumeration — it reuses release track canonical text.
_PANEL_VALIDATION_RECOVERY: Final[str] = (
    "show usage: hades <subcommand> --help (lists every flag with its "
    "constraints); common errors: --apply requires --dry-run=false; --panel "
    "requires a value from the 12-panel allowlist "
    "(workforce/cost/audit/hra/confirmations/memory/skills/doctrine/codegraph/"
    "inbox/crossproject/help)"
)

# Pre-rendered HADES block for the `cli.arg-validation-fail` (panel-name) path.
# Static constant per plan Architecture line 25 — rendered once at import time
# using the release track catalog literals above. The three-line HADES format mirrors
# release track Go-side Render() (headline / body / recovery-hint) per spec §Q6.
_PANEL_VALIDATION_HADES_BLOCK: Final[str] = render_hades_block(
    title=_PANEL_VALIDATION_TITLE,
    body=_PANEL_VALIDATION_BODY,
    recovery=_PANEL_VALIDATION_RECOVERY,
)


def panel_handler(raw_args: str) -> str | None:
    """/hades:panel <name> handler — spawn `hades dashboard --panel=<name>`.

    Args:
        raw_args: Operator's args after the slash command name. Expected: a
            single panel name with optional surrounding whitespace.
            Examples: "codegraph", "  workforce  ", "help".

    Returns:
        None when the TUI exits cleanly (returncode 0).
        A HADES-branded error string when:
        - raw_args is empty (no panel name supplied)
        - raw_args contains multiple tokens (e.g., "codegraph extra")
        - panel name is not in the 12-panel allowlist
        - `hades` binary not on PATH
        - subprocess.run raises
        - subprocess returncode != 0

    Per spec §Q8 D-pattern: lazygit-style subprocess handoff. Terminal mode is
    captured before spawn and restored after (via _subprocess_handoff helper).

    Per release stage C-5 operator decision (2026-05-21): invalid panel names render
    the `cli.arg-validation-fail` HADES block LOCALLY (static
    _PANEL_VALIDATION_HADES_BLOCK) — no daemon roundtrip. inv-hades-088 is
    preserved trivially: this path makes no network calls at all.
    """
    # 1. Parse + validate the panel name.
    stripped = raw_args.strip()
    if not stripped:
        # Empty raw_args — no panel name supplied. Render catalog block locally.
        return _PANEL_VALIDATION_HADES_BLOCK

    tokens = stripped.split()
    if len(tokens) != 1:
        # Multi-token input — operator typo / confusion. Render locally.
        return _PANEL_VALIDATION_HADES_BLOCK

    panel_name = tokens[0]
    if panel_name not in _VALID_PANELS:
        # Not in the 12-panel allowlist — render catalog block locally.
        return _PANEL_VALIDATION_HADES_BLOCK

    # 2. Delegate the subprocess handoff to the shared helper.
    return run_hades_subprocess(extra_args=[f"--panel={panel_name}"])
