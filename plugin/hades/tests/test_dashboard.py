# SPDX-License-Identifier: MIT
"""Tests for plugin/hades/commands/dashboard.py — /hades:dashboard slash handler."""

from __future__ import annotations

import subprocess
import sys
from typing import Any

import pytest
from hermes_plugins.hades.commands.dashboard import dashboard_handler


class _RecordedSubprocessRun:
    """Records calls to subprocess.run for assertion.

    Returns a configurable CompletedProcess on each invocation (default returncode=0).
    The recorded invocations list contains (args, kwargs) tuples in invocation order.
    """

    def __init__(self, returncode: int = 0) -> None:
        self.invocations: list[tuple[tuple[Any, ...], dict[str, Any]]] = []
        self._returncode = returncode

    def __call__(self, *args: Any, **kwargs: Any) -> subprocess.CompletedProcess[Any]:
        self.invocations.append((args, kwargs))
        return subprocess.CompletedProcess(args=args[0], returncode=self._returncode)


class _RecordedTermios:
    """Records termios.tcgetattr / tcsetattr calls for assertion."""

    def __init__(self) -> None:
        self.getattr_calls: list[int] = []             
        self.setattr_calls: list[tuple[int, int, Any]] = []                     
                                                                                   
        self.sentinel_attrs = object()

    def tcgetattr(self, fd: int) -> Any:
        self.getattr_calls.append(fd)
        return self.sentinel_attrs

    def tcsetattr(self, fd: int, when: int, attrs: Any) -> None:
        self.setattr_calls.append((fd, when, attrs))


def test_dashboard_handler_happy_path_invokes_hades_dashboard(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:dashboard handler spawns `hades dashboard` via subprocess.

    Verifies:
    1. shutil.which("hades") is called → returns the binary path.
    2. termios.tcgetattr(stdin_fd) is called → captures current terminal mode.
    3. subprocess.run is invoked with argv=["<hades_path>", "dashboard"] and
       inherited stdio (stdin=sys.stdin, stdout=sys.stdout, stderr=sys.stderr).
    4. termios.tcsetattr is called with the captured mode (restore).
    5. Returns None on subprocess exit code 0 (Hermes convention: no message
       to display when TUI exits cleanly).

    Per spec §Q8 D-pattern + §5.4 TUI invocation flow +  A A-7 alias.
    """
                                                      
    fake_hades_path = "/usr/local/bin/hades"
    recorded_which: list[str] = []

    def fake_which(name: str) -> str | None:
        recorded_which.append(name)
        if name == "hades":
            return fake_hades_path
        return None

    monkeypatch.setattr("shutil.which", fake_which)

    fake_run = _RecordedSubprocessRun(returncode=0)
    monkeypatch.setattr("subprocess.run", fake_run)

    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)

                                                               
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

                               
    result = dashboard_handler("")

                                        

                                                            
    assert "hades" in recorded_which, (
        f"expected shutil.which('hades') to be called; got {recorded_which}"
    )

                                                                           
    assert fake_termios.getattr_calls == [0], (
        f"expected termios.tcgetattr(0) exactly once; got {fake_termios.getattr_calls}"
    )

                                                                                          
    assert len(fake_run.invocations) == 1, (
        f"expected subprocess.run exactly once; got {len(fake_run.invocations)}"
    )
    (args, kwargs) = fake_run.invocations[0]
    argv = args[0]
    assert argv == [fake_hades_path, "dashboard"], (
        f"expected argv=[{fake_hades_path!r}, 'dashboard']; got {argv!r}"
    )
    assert kwargs.get("stdin") is sys.stdin, (
        f"expected stdin=sys.stdin (inherited); got {kwargs.get('stdin')!r}"
    )
    assert kwargs.get("stdout") is sys.stdout, (
        f"expected stdout=sys.stdout (inherited); got {kwargs.get('stdout')!r}"
    )
    assert kwargs.get("stderr") is sys.stderr, (
        f"expected stderr=sys.stderr (inherited); got {kwargs.get('stderr')!r}"
    )
    assert kwargs.get("check") is False, (
        "expected check=False (we handle the returncode ourselves; bubbletea may "
        "exit non-zero for various reasons that are not handler failures)"
    )

                                                                       
    assert len(fake_termios.setattr_calls) == 1, (
        f"expected termios.tcsetattr exactly once; got {fake_termios.setattr_calls}"
    )
    (fd, when, attrs) = fake_termios.setattr_calls[0]
    assert fd == 0, f"expected restore on fd=0 (stdin); got fd={fd}"
    import termios

    assert when == termios.TCSADRAIN, (
        f"expected when=TCSADRAIN ({termios.TCSADRAIN}); got {when}"
    )
    assert attrs is fake_termios.sentinel_attrs, (
        "expected restore to use the SAME attrs object that tcgetattr returned "
        "(lazygit pattern: snapshot pre-spawn, restore post-spawn)"
    )

                                                                    
    assert result is None, (
        f"expected handler to return None on TUI clean exit; got {result!r}"
    )


def test_dashboard_handler_hades_binary_missing(monkeypatch: pytest.MonkeyPatch) -> None:
    """Asserts /hades:dashboard handler errors gracefully when `hades` is not on PATH.

    Per invariant: handler MUST NOT raise an unrouted exception at the
    slash-command boundary. The hades-binary-missing case routes through the catalog
    (locally constructed since we can't reach the daemon via subprocess if the wrapper
    is absent).
    """
    monkeypatch.setattr("shutil.which", lambda name: None)

    result = dashboard_handler("")

    assert result is not None, (
        "expected non-None error message when hades binary missing; "
        "handler MUST NOT silently no-op"
    )
    # Error message MUST contain HADES branding per invariant.
    assert "HADES" in result, f"expected 'HADES' in error output; got {result!r}"
    # Recovery hint MUST be concrete (mention PATH or install verification).
    assert "PATH" in result or "install" in result.lower(), (
        f"expected recovery hint mentioning PATH or install; got {result!r}"
    )


def test_dashboard_handler_stdin_not_tty_returns_error_block(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:dashboard handler errors gracefully when stdin is not TTY.

    The lazygit handoff requires a TTY-attached stdin so it can save/restore
    terminal mode. When stdin is piped/redirected, termios.tcgetattr raises
    termios.error; the handler MUST route through a HADES block (NOT raise) and
    MUST NOT spawn the subprocess (no point handing off the terminal we can't
    restore). Covers _subprocess_handoff.run_hades_subprocess termios.error path.

    Per invariant: handler never raises at the slash boundary.
    """
    import termios

    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")

    def boom_tcgetattr(fd: int) -> Any:
        raise termios.error("stdin is not a TTY (piped/redirected)")

    monkeypatch.setattr("termios.tcgetattr", boom_tcgetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    recorded_run: list[Any] = []

    def fake_run(*args: Any, **kwargs: Any) -> Any:
        recorded_run.append(args)
        return subprocess.CompletedProcess(args=args[0], returncode=0)

    monkeypatch.setattr("subprocess.run", fake_run)

    result = dashboard_handler("")

    # subprocess MUST NOT be spawned — we bail before the handoff.
    assert recorded_run == [], (
        f"expected NO subprocess.run when stdin is not a TTY; got {recorded_run}"
    )
                                                                                
    assert result is not None
    assert "HADES" in result, f"expected HADES branding; got {result!r}"
    assert "TTY" in result or "tty" in result, (
        f"expected recovery hint mentioning tty; got {result!r}"
    )


def test_dashboard_handler_subprocess_nonzero_returncode(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:dashboard handler propagates subprocess exit when non-zero.

    Subprocess returncode != 0 indicates the TUI exited with an error (e.g.,
    daemon unreachable mid-session, terminal-not-tty, bubbletea panic). Handler
    surfaces a HADES block referencing the exit code so operator can triage.

    Per spec §Q6 (error UX recovery-oriented): user MUST see HADES-branded
    output, not a raw subprocess error.
    """
    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")
    monkeypatch.setattr("subprocess.run", _RecordedSubprocessRun(returncode=7))
    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    result = dashboard_handler("")

    # Handler MUST surface a HADES block when TUI exits non-zero — silent failure
                                                                              
    assert result is not None, (
        "expected non-None message when subprocess exit != 0; "
        "silent failure would violate inv-zen-220 prep"
    )
    assert "HADES" in result, f"expected 'HADES' in non-zero-exit output; got {result!r}"
                                                              
    assert "7" in result, (
        f"expected exit code '7' in error output for operator triage; got {result!r}"
    )


def test_dashboard_handler_termios_restored_even_on_subprocess_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts terminal mode is restored even when subprocess.run raises.

    Lazygit-pattern guarantee: NO MATTER WHAT happens to the subprocess
    (crash, kill, SIGTERM, terminal-not-tty error), the parent terminal
    MUST be left in a usable state. This is the load-bearing invariant
    of D-2 + D-5; failure means the operator's shell becomes unusable
    after a TUI crash.

    Per spec §Q8 D-pattern: "When TUI exits, Hermes session resumes."
    Resumption requires a usable terminal.
    """

    def boom(*args: Any, **kwargs: Any) -> Any:
        raise OSError("simulated subprocess.run failure")

    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")
    monkeypatch.setattr("subprocess.run", boom)
    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    # Handler MUST NOT propagate the OSError — it MUST route through the catalog.
    result = dashboard_handler("")

    # Restore MUST have run despite the subprocess failure.
    assert len(fake_termios.setattr_calls) == 1, (
        f"expected termios.tcsetattr exactly once even on subprocess failure; "
        f"got {fake_termios.setattr_calls}"
    )
    # And result MUST be a HADES-branded message.
    assert result is not None and "HADES" in result, (
        f"expected HADES-branded error on subprocess failure; got {result!r}"
    )


                                                                              


def test_dashboard_handler_subprocess_sigint_returncode_130(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts /hades:dashboard handler maps subprocess returncode 130 to SIGINT path.

    POSIX convention: subprocess killed by SIGINT exits with returncode
    128 + signal_number = 128 + 2 = 130. This is what bubbletea reports when
    operator hits Ctrl+C and bubbletea cleanly exits via signal.

    Per spec §Q8 "When TUI exits, Hermes session resumes": SIGINT-driven exit
    is a clean exit, not an error. Handler returns a brief HADES block noting
    the user-initiated cancellation, distinct from the catch-all non-zero
    code path (D-2).
    """
    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")
    monkeypatch.setattr("subprocess.run", _RecordedSubprocessRun(returncode=130))
    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    result = dashboard_handler("")

                                          
    assert len(fake_termios.setattr_calls) == 1

                                                                                  
    if result is not None:
        assert "HADES" in result, (
            f"expected HADES branding in SIGINT exit; got {result!r}"
        )
                                                                                
        assert any(token in result.lower() for token in ("interrupt", "cancel", "130")), (
            f"expected SIGINT exit acknowledgment in body; got {result!r}"
        )


def test_dashboard_handler_keyboard_interrupt_mid_handler_restores_terminal(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts terminal is restored when KeyboardInterrupt arrives mid-handler.

    Race condition: SIGINT arrives between tcgetattr and tcsetattr — e.g.,
    operator hits Ctrl+C in the milliseconds before subprocess.run completes.

    Python translates SIGINT in the main thread into KeyboardInterrupt; the
    handler MUST catch it, restore the terminal (since attrs were captured),
    and return a HADES block (or None for clean cancel).
    """

    def boom_with_kbd(*args: Any, **kwargs: Any) -> Any:
        raise KeyboardInterrupt()

    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")
    monkeypatch.setattr("subprocess.run", boom_with_kbd)
    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    # Handler MUST NOT propagate KeyboardInterrupt to the Hermes session.
                                                                          
    # the slash command boundary MUST catch all signals + return cleanly.
    try:
        result = dashboard_handler("")
    except KeyboardInterrupt:
        pytest.fail(
            "dashboard_handler MUST NOT propagate KeyboardInterrupt to Hermes; "
            "the slash command boundary must catch + render"
        )

                                                           
    assert len(fake_termios.setattr_calls) == 1, (
        f"expected one tcsetattr (restore on interrupt); got {fake_termios.setattr_calls}"
    )

                                                  
    if result is not None:
        assert "HADES" in result


def test_dashboard_handler_no_double_restore_on_normal_exit(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Asserts terminal mode restore happens EXACTLY ONCE on normal exit.

    Double-restore would fight bubbletea's own cleanup + could leave the
    terminal in an inconsistent state (e.g., wrong line-discipline, wrong
    echo mode). The finally block MUST guard against re-running the restore.
    """
    monkeypatch.setattr("shutil.which", lambda name: "/usr/local/bin/hades")
    monkeypatch.setattr("subprocess.run", _RecordedSubprocessRun(returncode=0))
    fake_termios = _RecordedTermios()
    monkeypatch.setattr("termios.tcgetattr", fake_termios.tcgetattr)
    monkeypatch.setattr("termios.tcsetattr", fake_termios.tcsetattr)
    monkeypatch.setattr(sys.stdin, "fileno", lambda: 0)

    _result = dashboard_handler("")

    assert len(fake_termios.setattr_calls) == 1, (
        f"expected exactly one tcsetattr call (no double-restore); "
        f"got {len(fake_termios.setattr_calls)} calls"
    )
