# SPDX-License-Identifier: MIT
"""Tests for plugin/hades/commands/panel.py — /hades:panel slash handler."""

from __future__ import annotations

import subprocess
import sys
from typing import Any

import pytest
from hermes_plugins.hades.commands.panel import (
    _PANEL_VALIDATION_HADES_BLOCK,
    _PANEL_VALIDATION_RECOVERY,
    _PANEL_VALIDATION_TITLE,
    panel_handler,
)

                                                                     
_VALID_PANEL_NAMES = (
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
)

                                                              
                                                  
 
                                                                                    
                                                                             
                                                                         
                                                                            
                                                                            
                                                              

                                                         
_PHASE_A_TITLE = "Argument validation failed."

                                                                               
_PHASE_A_RECOVERY = (
    "show usage: hades <subcommand> --help (lists every flag with its "
    "constraints); common errors: --apply requires --dry-run=false; --panel "
    "requires a value from the 12-panel allowlist "
    "(workforce/cost/audit/hra/confirmations/memory/skills/doctrine/codegraph/"
    "inbox/crossproject/help)"
)


class _RecordedSubprocessRun:
    """Records subprocess.run calls for assertion. Same as test_dashboard.py pattern."""

    def __init__(self, returncode: int = 0) -> None:
        self.invocations: list[tuple[tuple[Any, ...], dict[str, Any]]] = []
        self._returncode = returncode

    def __call__(self, *args: Any, **kwargs: Any) -> subprocess.CompletedProcess[Any]:
        self.invocations.append((args, kwargs))
        return subprocess.CompletedProcess(args=args[0], returncode=self._returncode)


class _RecordedTermios:
    """Records termios calls. Same as test_dashboard.py pattern."""

    def __init__(self) -> None:
        self.getattr_calls: list[int] = []
        self.setattr_calls: list[tuple[int, int, Any]] = []
        self.sentinel_attrs = object()

    def tcgetattr(self, fd: int) -> Any:
        self.getattr_calls.append(fd)
        return self.sentinel_attrs

    def tcsetattr(self, fd: int, when: int, attrs: Any) -> None:
        self.setattr_calls.append((fd, when, attrs))


@pytest.fixture
def stub_environment(monkeypatch: pytest.MonkeyPatch) -> dict[str, Any]:
    """Sets up the monkeypatched environment for happy-path subprocess tests.

    Returns a dict with handles to the recorded fakes for per-test assertions.
    """
    fake_hades_path = "/usr/local/bin/hades"
    monkeypatch.setattr(
        "shutil.which", lambda name: fake_hades_path if name == "hades" else None
    )

    fake_run = _RecordedSubprocessRun(returncode=0)
    monkeypatch.setattr("subprocess.run", fake_run)

    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    return {
        "hades_path": fake_hades_path,
        "subprocess_run": fake_run,
        "termios": fake_termios,
    }


                                                              
                                                     
                                                              


@pytest.mark.parametrize("panel_name", _VALID_PANEL_NAMES)
def test_panel_handler_valid_name_spawns_hades_dashboard_with_panel_flag(
    panel_name: str,
    stub_environment: dict[str, Any],
) -> None:
    """Asserts /hades:panel <valid-name> spawns `hades dashboard --panel=<name>`.

    Table-driven over all 12 valid names per spec §Q8 + Plan 12 view files.
    Each name maps to a TUI view in internal/tui/views/*.go; the bubbletea
    dashboard does the routing once it receives the --panel flag (Plan 12 ship).

    Verifies the SAME subprocess handoff pattern as /hades:dashboard (D-2),
    with argv extended by --panel=<name>.
    """
    result = panel_handler(panel_name)

    fake_run = stub_environment["subprocess_run"]
    fake_termios = stub_environment["termios"]
    hades_path = stub_environment["hades_path"]

                                             
    assert len(fake_run.invocations) == 1, (
        f"expected subprocess.run exactly once for valid panel {panel_name!r}; "
        f"got {len(fake_run.invocations)}"
    )
    (args, kwargs) = fake_run.invocations[0]
    argv = args[0]

                                                             
    expected_argv = [hades_path, "dashboard", f"--panel={panel_name}"]
    assert argv == expected_argv, f"expected argv={expected_argv!r}; got {argv!r}"

                         
    assert kwargs.get("stdin") is sys.stdin
    assert kwargs.get("stdout") is sys.stdout
    assert kwargs.get("stderr") is sys.stderr
    assert kwargs.get("check") is False

                                                  
    assert fake_termios.getattr_calls == [0], (
        f"expected one tcgetattr(0); got {fake_termios.getattr_calls}"
    )
    assert len(fake_termios.setattr_calls) == 1, (
        f"expected one tcsetattr; got {fake_termios.setattr_calls}"
    )
    (fd, _when, attrs) = fake_termios.setattr_calls[0]
    assert fd == 0
    assert attrs is fake_termios.sentinel_attrs, (
        "expected restore to use the SAME attrs object that tcgetattr returned"
    )

                                    
    assert result is None, (
        f"expected None on TUI clean exit for valid panel {panel_name!r}; got {result!r}"
    )


def test_panel_handler_strips_whitespace_around_name(
    stub_environment: dict[str, Any],
) -> None:
    """Asserts /hades:panel handler strips whitespace around the panel name.

    Hermes may pass raw_args with leading/trailing whitespace depending on how
    the operator typed the command.
    """
    result = panel_handler("  codegraph  ")

    fake_run = stub_environment["subprocess_run"]
    assert len(fake_run.invocations) == 1
    (args, _kwargs) = fake_run.invocations[0]
    argv = args[0]
    assert argv[2] == "--panel=codegraph", (
        f"expected --panel=codegraph (whitespace stripped); got {argv[2]!r}"
    )
    assert result is None


def test_panel_handler_rejects_extra_args_after_panel_name(
    stub_environment: dict[str, Any],
) -> None:
    """Asserts /hades:panel handler rejects multi-token raw_args.

    Per spec §Q8 + master Phase D scope, the panel slash command takes
    EXACTLY ONE positional arg (the panel name). Extra args indicate
    operator confusion or typo and render the catalog block locally with a
    recovery hint.
    """
    result = panel_handler("codegraph extra-token")

                                         
    fake_run = stub_environment["subprocess_run"]
    assert len(fake_run.invocations) == 0, (
        f"expected NO subprocess.run for invalid multi-arg input; "
        f"got {fake_run.invocations}"
    )

                                              
    assert result is not None
    assert "HADES" in result
    # Recovery hint MUST mention the 12-panel allowlist.
    assert "workforce" in result and "cost" in result and "help" in result, (
        f"expected recovery hint listing the 12 panel names; got {result!r}"
    )


                                                              
                                                                 
                                                              


def test_panel_validation_block_matches_phase_a_catalog_byte_for_byte() -> None:
    """Sister-test: _PANEL_VALIDATION_HADES_BLOCK matches Phase A's catalog
    `cli.arg-validation-fail` Title + RecoveryHint byte-for-byte.

    Per Stage 2 C-5 operator decision + plan Architecture lines 7/25: Phase D
    renders the error block LOCALLY using Phase A catalog literals (no daemon
    roundtrip). Phase A's catalog is the single source of truth — it lives in
    Go at internal/errors/codes.go → ErrorCode["cli.arg-validation-fail"].

    Per memory `feedback_sister_test_pattern.md`: this assertion gates drift.
    If Phase A's RecoveryHint text changes in the Go catalog, the constant here
    diverges and this test fails, forcing reconciliation. The expected literals
    (_PHASE_A_TITLE / _PHASE_A_RECOVERY) are hardcoded above with a comment
    pointing to the canonical Go source (option (a) per the C-5 rework brief).

    Bite-check: change a single character of _PANEL_VALIDATION_RECOVERY in
    panel.py and this test MUST fail (proves it gates the byte-for-byte claim).
    """
    # The panel.py module's literals MUST equal the Phase A catalog literals.
    assert _PANEL_VALIDATION_TITLE == _PHASE_A_TITLE, (
        "panel.py _PANEL_VALIDATION_TITLE diverged from Phase A catalog "
        "cli.arg-validation-fail Title (internal/errors/codes.go)"
    )
    assert _PANEL_VALIDATION_RECOVERY == _PHASE_A_RECOVERY, (
        "panel.py _PANEL_VALIDATION_RECOVERY diverged from Phase A catalog "
        "cli.arg-validation-fail RecoveryHint (internal/errors/codes.go); "
        "reconcile the constant with the Go source — Phase A is canonical"
    )

    # The rendered block MUST embed the Phase A Title + RecoveryHint verbatim.
    assert _PHASE_A_TITLE in _PANEL_VALIDATION_HADES_BLOCK, (
        "rendered HADES block missing Phase A catalog Title"
    )
    assert _PHASE_A_RECOVERY in _PANEL_VALIDATION_HADES_BLOCK, (
        "rendered HADES block missing Phase A catalog RecoveryHint byte-for-byte"
    )


def test_panel_validation_block_has_hades_three_line_format() -> None:
    """Sister-test: the local block matches the spec §Q6 three-line HADES format.

    Format: "HADES: <title>\\n  <body>\\n  → <recovery>\\n"  (mirrors Phase B's
    Go-side Render()). Verifies headline / body / recovery-hint structure +
    HADES branding (inv-zen-219).
    """
    lines = _PANEL_VALIDATION_HADES_BLOCK.splitlines()
    assert len(lines) >= 3, (
        f"expected at least 3 lines (title/body/recovery); got {lines}"
    )
    assert lines[0].startswith("HADES: "), (
        f"expected first line to start with 'HADES: '; got {lines[0]!r}"
    )
    assert lines[1].startswith("  "), (
        f"expected body line to have 2-space indent; got {lines[1]!r}"
    )
    assert lines[2].startswith("  → "), (
        f"expected recovery line with arrow prefix; got {lines[2]!r}"
    )

                                                                                  
    for panel in _VALID_PANEL_NAMES:
        assert panel in _PANEL_VALIDATION_HADES_BLOCK, (
            f"recovery hint MUST enumerate panel {panel!r} per Phase A catalog text"
        )


                                                              
                                                                 
                                                              


def test_panel_handler_invalid_name_renders_local_block(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:panel <invalid-name> renders the local catalog block.

    Per Stage 2 C-5 operator decision (2026-05-21): NO daemon `/v1/errors/render`
    roundtrip — the handler renders _PANEL_VALIDATION_HADES_BLOCK directly. The
    TUI is NOT spawned for invalid names.

    Verifies:
    1. subprocess.run is NOT called (wrapper not invoked for invalid names).
    2. The returned block is _PANEL_VALIDATION_HADES_BLOCK (the catalog block).
    """
    # subprocess.run MUST NOT be called for invalid names.
    fake_run = _RecordedSubprocessRun(returncode=0)
    monkeypatch.setattr("subprocess.run", fake_run)

    result = panel_handler("badname")

                                                                                    
    assert len(fake_run.invocations) == 0, (
        f"expected NO subprocess.run for invalid panel name; got {fake_run.invocations}"
    )

                                                      
    assert result == _PANEL_VALIDATION_HADES_BLOCK, (
        f"expected the local _PANEL_VALIDATION_HADES_BLOCK; got {result!r}"
    )
    assert "HADES" in result
    assert _PHASE_A_TITLE in result
    for panel in _VALID_PANEL_NAMES:
        assert panel in result, f"local block missing panel {panel!r}"


def test_panel_handler_invalid_name_makes_no_network_calls(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts the invalid-name path makes NO network calls at all (inv-zen-088).

    Per C-5: the daemon `/v1/errors/render` roundtrip was dropped entirely.
    There is no urllib / http.client / socket machinery in panel.py. This test
    sabotages the stdlib network entry points and confirms the handler still
    returns the local block without touching them.
    """
    import socket as _socket
    import urllib.request as _urllib

    def boom_socket(*args: Any, **kwargs: Any) -> Any:
        raise AssertionError(
            "C-5 violation: invalid-name path opened a socket; render is local-only"
        )

    def boom_urlopen(*args: Any, **kwargs: Any) -> Any:
        raise AssertionError(
            "C-5 violation: invalid-name path called urlopen; render is local-only"
        )

    monkeypatch.setattr(_socket, "socket", boom_socket)
    monkeypatch.setattr(_urllib, "urlopen", boom_urlopen)
    monkeypatch.setattr("subprocess.run", _RecordedSubprocessRun(returncode=0))

    result = panel_handler("badname")

    assert result == _PANEL_VALIDATION_HADES_BLOCK


def test_panel_module_has_no_daemon_machinery() -> None:
    """Asserts panel.py exposes NO daemon-render machinery (C-5 rework).

    The pre-C-5 design had `_daemon_render_error`, `_UnixSocketHTTPConnection`,
    `_UnixSocketHTTPHandler`, `_DAEMON_UDS_PATH`. C-5 removed all of them. This
    test gates regression: those names MUST NOT reappear on the module.
    """
    from hermes_plugins.hades.commands import panel as panel_mod

    for forbidden in (
        "_daemon_render_error",
        "_UnixSocketHTTPConnection",
        "_UnixSocketHTTPHandler",
        "_DAEMON_UDS_PATH",
        "_render_panel_arg_fail_via_daemon_or_local",
    ):
        assert not hasattr(panel_mod, forbidden), (
            f"C-5 violation: panel.py still exposes {forbidden!r}; daemon "
            "render path must be removed (render is local-only)"
        )


                                                              
                                                              
                                                              


def test_panel_handler_subprocess_sigint_returncode_130(
    stub_environment: dict[str, Any],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:panel handler maps subprocess returncode 130 to SIGINT path."""
                                                                      
    fake_run = _RecordedSubprocessRun(returncode=130)
    monkeypatch.setattr("subprocess.run", fake_run)

    result = panel_handler("codegraph")

                                     
    fake_termios = stub_environment["termios"]
    assert len(fake_termios.setattr_calls) == 1

                                                                
    if result is not None:
        assert "HADES" in result


                                                              
                        
                                                              


def test_panel_handler_no_args_returns_recovery_hint_with_all_12_panels(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:panel (no args) returns a recovery hint listing all 12 panels.

    Operator types `/hades:panel` with no arg → handler MUST NOT silently no-op
    and MUST NOT spawn the TUI on the default panel (that's what /hades:dashboard
    is for). Instead, the local catalog block enumerates the 12 valid names so the
    operator can retry.

    Per spec §Q6 (error UX recovery-oriented): every error path includes a
    concrete shell command or doc link.
    """
    # subprocess.run MUST NOT be called for no-args.
    fake_run = _RecordedSubprocessRun(returncode=0)
    monkeypatch.setattr("subprocess.run", fake_run)

    result = panel_handler("")

                                 
    assert len(fake_run.invocations) == 0, (
        f"expected NO subprocess.run for no-args; got {fake_run.invocations}"
    )

    # Result MUST be the local HADES block.
    assert result == _PANEL_VALIDATION_HADES_BLOCK
    assert "HADES" in result

    # All 12 panel names MUST appear in the recovery hint.
    for panel in _VALID_PANEL_NAMES:
        assert panel in result, (
            f"no-args recovery hint MUST include panel {panel!r}; got: {result!r}"
        )

    # Recovery hint MUST reference `--panel` so operator knows the retry surface.
    assert "--panel" in result, (
        f"recovery hint MUST reference --panel for retry; got: {result!r}"
    )


def test_panel_handler_whitespace_only_args_treated_as_no_args(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts `/hades:panel    ` (whitespace only) is equivalent to no args.

    After stripping, raw_args becomes ""; the handler MUST route through the
    same no-args local-render path, NOT spawn the TUI on a default panel.
    """
    fake_run = _RecordedSubprocessRun(returncode=0)
    monkeypatch.setattr("subprocess.run", fake_run)

    result = panel_handler("   \t  ")

    assert len(fake_run.invocations) == 0
    assert result == _PANEL_VALIDATION_HADES_BLOCK
                                         
    assert "workforce" in result and "help" in result
