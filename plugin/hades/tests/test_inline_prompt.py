# SPDX-License-Identifier: MIT
"""Tests for hades.interactive.inline_prompt — render-safe confirm/choose helper."""

from __future__ import annotations

import builtins
import typing

from hades.interactive import inline_prompt

                                                                             
                                            
                                                                             


def test_confirm_gateway_dormant_returns_default_without_seam() -> None:
    """ctx lacking request_user_input → confirm returns the default, never blocks."""

    class CtxNoSeam:
        pass

    assert inline_prompt.confirm("proceed?", default=True, ctx=CtxNoSeam()) is True
    assert inline_prompt.confirm("proceed?", default=False, ctx=CtxNoSeam()) is False


def test_confirm_no_ctx_non_interactive_returns_default(monkeypatch: object) -> None:
    """No ctx, no live app, not HERMES_INTERACTIVE → return default immediately."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.delenv("HERMES_INTERACTIVE", raising=False)  # type: ignore[attr-defined]
    assert inline_prompt.confirm("proceed?", default=True) is True
    assert inline_prompt.confirm("proceed?", default=False) is False


                                                                             
                                                     
                                                                             


def test_confirm_uses_seam_when_present() -> None:
    """ctx exposing request_user_input → confirm delegates to it."""

    class CtxSeam:
        def request_user_input(self, prompt: str, choices: list[str]) -> str:
            return "y"

    assert inline_prompt.confirm("proceed?", default=False, ctx=CtxSeam()) is True


def test_confirm_seam_truthy_variants() -> None:
    """y / yes / o / once / session / always → all True."""
    for answer in ("y", "yes", "Y", "YES", "o", "once", "session", "always"):

        class CtxSeam:
            _answer = answer

            def request_user_input(self, prompt: str, choices: list[str]) -> str:
                return self._answer

        assert inline_prompt.confirm("proceed?", default=False, ctx=CtxSeam()) is True, (
            f"Expected True for answer={answer!r}"
        )


def test_confirm_seam_falsy_answer() -> None:
    """Anything not in truthy set → False."""

    class CtxSeam:
        def request_user_input(self, prompt: str, choices: list[str]) -> str:
            return "n"

    assert inline_prompt.confirm("proceed?", default=True, ctx=CtxSeam()) is False


                                                                             
                                                                   
                                                                             


def test_confirm_hermes_interactive_yes(monkeypatch: object) -> None:
    """No live app + HERMES_INTERACTIVE=1 + input returns 'y' → True."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.setenv("HERMES_INTERACTIVE", "1")  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "y")  # type: ignore[attr-defined]
    assert inline_prompt.confirm("proceed?", default=False) is True


def test_confirm_hermes_interactive_no(monkeypatch: object) -> None:
    """HERMES_INTERACTIVE branch with 'n' → False even when default=True."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.setenv("HERMES_INTERACTIVE", "1")  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "n")  # type: ignore[attr-defined]
    assert inline_prompt.confirm("proceed?", default=True) is False


def test_confirm_hermes_interactive_eof_returns_default(monkeypatch: object) -> None:
    """EOFError during input (pipe closed) → fall back to default."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.setenv("HERMES_INTERACTIVE", "1")  # type: ignore[attr-defined]

    def raise_eof(_prompt: str) -> str:
        raise EOFError

    monkeypatch.setattr(builtins, "input", raise_eof)  # type: ignore[attr-defined]
    assert inline_prompt.confirm("proceed?", default=True) is True
    assert inline_prompt.confirm("proceed?", default=False) is False


                                                                             
                                                               
                                                                             


def test_confirm_live_app_yes(monkeypatch: object) -> None:
    """Live app present: _run_in_terminal invoked; answer propagates as True."""
    fake_app = object()                                          
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: fake_app)  # type: ignore[attr-defined]

                                                                            
    def fake_run_in_terminal(func: object) -> None:
        typing.cast("typing.Callable[[], None]", func)()

    monkeypatch.setattr(inline_prompt, "_run_in_terminal", fake_run_in_terminal)  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "yes")  # type: ignore[attr-defined]

    assert inline_prompt.confirm("proceed?", default=False) is True


def test_confirm_live_app_no(monkeypatch: object) -> None:
    """Live app present: input returns 'n' → False."""
    fake_app = object()
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: fake_app)  # type: ignore[attr-defined]

    def fake_run_in_terminal(func: object) -> None:
        typing.cast("typing.Callable[[], None]", func)()

    monkeypatch.setattr(inline_prompt, "_run_in_terminal", fake_run_in_terminal)  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "n")  # type: ignore[attr-defined]

    assert inline_prompt.confirm("proceed?", default=True) is False


def test_confirm_live_app_timeout_returns_default(monkeypatch: object) -> None:
    """Live app: _run_in_terminal never invokes func (timeout path) → default."""
    fake_app = object()
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: fake_app)  # type: ignore[attr-defined]

                                                                                   
    def fake_run_in_terminal_noop(func: object) -> None:
        pass                                                 

    monkeypatch.setattr(inline_prompt, "_run_in_terminal", fake_run_in_terminal_noop)  # type: ignore[attr-defined]

                                                            
    monkeypatch.setattr(inline_prompt, "_TUI_TIMEOUT", 0.05)  # type: ignore[attr-defined]

    assert inline_prompt.confirm("proceed?", default=True) is True
    assert inline_prompt.confirm("proceed?", default=False) is False


                                                                             
                                    
                                                                             


def test_choose_no_seam_non_interactive_returns_none(monkeypatch: object) -> None:
    """No ctx seam + not interactive → None."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.delenv("HERMES_INTERACTIVE", raising=False)  # type: ignore[attr-defined]
    result = inline_prompt.choose("Pick one", ["a", "b", "c"])
    assert result is None


def test_choose_seam_valid_answer() -> None:
    """ctx seam returns a valid option → choose returns it."""

    class CtxSeam:
        def request_user_input(self, prompt: str, choices: list[str]) -> str:
            return "b"

    result = inline_prompt.choose("Pick", ["a", "b", "c"], ctx=CtxSeam())
    assert result == "b"


def test_choose_seam_invalid_answer_returns_none() -> None:
    """ctx seam returns something not in options → None."""

    class CtxSeam:
        def request_user_input(self, prompt: str, choices: list[str]) -> str:
            return "z"

    result = inline_prompt.choose("Pick", ["a", "b", "c"], ctx=CtxSeam())
    assert result is None


def test_choose_hermes_interactive(monkeypatch: object) -> None:
    """HERMES_INTERACTIVE + valid input → return the chosen option."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.setenv("HERMES_INTERACTIVE", "1")  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "b")  # type: ignore[attr-defined]
    result = inline_prompt.choose("Pick", ["a", "b", "c"])
    assert result == "b"


def test_choose_hermes_interactive_invalid_returns_none(monkeypatch: object) -> None:
    """HERMES_INTERACTIVE + input not in options → None."""
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: None)  # type: ignore[attr-defined]
    monkeypatch.setenv("HERMES_INTERACTIVE", "1")  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "z")  # type: ignore[attr-defined]
    result = inline_prompt.choose("Pick", ["a", "b", "c"])
    assert result is None


def test_choose_live_app(monkeypatch: object) -> None:
    """Live app: answer propagates through run_in_terminal."""
    fake_app = object()
    monkeypatch.setattr(inline_prompt, "_get_app_or_none", lambda: fake_app)  # type: ignore[attr-defined]

    def fake_run_in_terminal(func: object) -> None:
        typing.cast("typing.Callable[[], None]", func)()

    monkeypatch.setattr(inline_prompt, "_run_in_terminal", fake_run_in_terminal)  # type: ignore[attr-defined]
    monkeypatch.setattr(builtins, "input", lambda _prompt: "c")  # type: ignore[attr-defined]

    result = inline_prompt.choose("Pick", ["a", "b", "c"])
    assert result == "c"


                                                                             
                                                                
                                                                             


def test_confirm_seam_returns_none_falls_back_to_default() -> None:
    """ctx.request_user_input returning None → confirm returns default, never raises.

    This is the critical gate for the 'NEVER raises' post-condition documented
    on confirm(). On the *old* str-only _truthy, None.strip() would raise
    AttributeError here. The fix: _truthy(None, default) returns default.
    """

    class CtxSeamNone:
        def request_user_input(self, prompt: str, choices: list[str]) -> None:  # type: ignore[override]
            return None                                                       

                                              
    assert inline_prompt.confirm("proceed?", default=True, ctx=CtxSeamNone()) is True
    assert inline_prompt.confirm("proceed?", default=False, ctx=CtxSeamNone()) is False
