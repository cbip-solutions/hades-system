# SPDX-License-Identifier: MIT
"""Tests for plugin/hades/hooks/wizard_handler.py (Plan 18c Phase E)."""

from __future__ import annotations

import random
import signal
import subprocess
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from hermes_plugins.hades.hooks import wizard_handler as _wh_mod
from hermes_plugins.hades.hooks.wizard_handler import (
    _config_path,
    _is_interactive_stdin,
    _is_signal_cancel,
    _maybe_launch_wizard,
    _resolve_hades_bin,
    _should_launch_wizard,
)

                                                                             
                                                                           
                                                                             


@pytest.fixture(autouse=True)
def _assume_tty_stdin(monkeypatch):
    """Default os.isatty(0) → True for all wizard_handler tests.

    Production behavior is TTY-detection (E-8 guard); test fixtures
    that need non-TTY behavior explicitly override via their own patch.
    Uses module-object form (monkeypatch.setattr(module, attr, val))
    because the hermes_plugins namespace package does not expose .hades
    as a direct attribute (it's registered in sys.modules only).
    """
    monkeypatch.setattr(
        _wh_mod.os,
        "isatty",
        lambda fd: True,
    )


                                                                              


def test_should_launch_wizard_true_when_config_missing_and_no_escape(
    monkeypatch, tmp_path
):
    """First-run signal: config missing + no escape env → wizard SHOULD launch.

    Per spec §5.1 + 2026-05-21 amendment: the hook itself reads the config
    path (NOT the wrapper) and returns True when both conditions are met.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
                                       
    config_path = tmp_path / ".config" / "zen-swarm" / "config.toml"
    assert not config_path.exists()
    assert _should_launch_wizard() is True


def test_should_launch_wizard_false_when_config_exists(monkeypatch, tmp_path):
    """Subsequent session: config present → hook is a no-op (no wizard)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    config_dir = tmp_path / ".config" / "zen-swarm"
    config_dir.mkdir(parents=True)
    (config_dir / "config.toml").write_text(
        'schema_version = "1.0"\nname = "test"\n',
        encoding="utf-8",
    )
    assert _should_launch_wizard() is False


def test_should_launch_wizard_false_when_hades_no_wizard_set(monkeypatch, tmp_path):
    """Escape hatch: HADES_NO_WIZARD=1 → hook is a no-op even with config missing.

    Per spec §Q7 + §3.2 step 4: `hades --no-wizard` → wrapper sets
    HADES_NO_WIZARD=1 → hook honors as hard escape.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("HADES_NO_WIZARD", "1")
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
                          
    config_path = tmp_path / ".config" / "zen-swarm" / "config.toml"
    assert not config_path.exists()
    assert _should_launch_wizard() is False


def test_should_launch_wizard_honors_xdg_config_home(monkeypatch, tmp_path):
    """XDG_CONFIG_HOME override: hook reads from XDG path, not HOME-relative.

    Per internal/onboard/paths.go: filepath.Join(xdgConfigHome(), "zen-swarm", "config.toml")
    The Python hook mirrors this convention.
    """
    xdg = tmp_path / "xdg-config"
    xdg.mkdir()
    monkeypatch.setenv("XDG_CONFIG_HOME", str(xdg))
    monkeypatch.setenv("HOME", str(tmp_path))                             
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
                                       
    assert not (xdg / "zen-swarm" / "config.toml").exists()
    assert _should_launch_wizard() is True
                                                        
    (xdg / "zen-swarm").mkdir()
    (xdg / "zen-swarm" / "config.toml").write_text("name = 't'", encoding="utf-8")
    assert _should_launch_wizard() is False


                                                                              


def test_config_path_uses_xdg_when_set(monkeypatch, tmp_path):
    """_config_path returns $XDG_CONFIG_HOME/zen-swarm/config.toml when XDG set."""
    xdg = tmp_path / "xdg"
    xdg.mkdir()
    monkeypatch.setenv("XDG_CONFIG_HOME", str(xdg))
    monkeypatch.setenv("HOME", str(tmp_path))
    assert _config_path() == xdg / "zen-swarm" / "config.toml"


def test_config_path_falls_back_to_home_when_xdg_unset(monkeypatch, tmp_path):
    """_config_path defaults to ~/.config/zen-swarm/config.toml when XDG unset."""
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HOME", str(tmp_path))
    assert _config_path() == tmp_path / ".config" / "zen-swarm" / "config.toml"


                                                                              


def test_maybe_launch_wizard_spawns_subprocess_when_trigger_conditions_met(
    monkeypatch, tmp_path
):
    """Hook spawns wizard subprocess when first-run + no escape detected.

    Mock subprocess.run to capture the call; assert subprocess is invoked
    with `config init` args (Plan 13 wizard surface).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-1",
            cwd=str(tmp_path),
            source="startup",
        )

    assert mock_run.called, "subprocess.run not invoked despite trigger conditions"
    call_args = mock_run.call_args
                                           
    argv = call_args.args[0]
    assert argv[0] == "/usr/local/bin/hades"
    assert argv[1] == "config"
    assert argv[2] == "init"


def test_maybe_launch_wizard_no_op_when_config_exists(monkeypatch, tmp_path):
    """Hook is a no-op when config.toml exists (subsequent session)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    config_dir = tmp_path / ".config" / "zen-swarm"
    config_dir.mkdir(parents=True)
    (config_dir / "config.toml").write_text("name = 't'", encoding="utf-8")

    with patch(
        "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
    ) as mock_run:
        _maybe_launch_wizard(
            session_id="sess-2",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called, "subprocess.run invoked despite config present"


def test_maybe_launch_wizard_returns_none(monkeypatch, tmp_path):
    """Hook is an observer; return value is None (Hermes ignores it)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")
                                                                 
    monkeypatch.delenv("HERMES_SKIN", raising=False)
    result = _maybe_launch_wizard(
        session_id="sess-3", cwd=str(tmp_path), source="startup"
    )
    assert result is None


                                                                              


def test_maybe_launch_wizard_preserves_home_env_for_migrate_detection(
    monkeypatch, tmp_path
):
    """Step 1 (migrate detection): wizard subprocess inherits HOME so
    Plan 13's `~/.claude/` lookup logic works correctly.

    Per spec §Q7 step 1: if `~/.claude/` exists, the wizard offers
    `hades migrate claude-code` inline. The hook's responsibility is
    to NOT clobber the HOME env when subprocessing; Plan 13's
    `~/.claude/` detection reads HOME via os.UserHomeDir().
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

                                                                      
    claude_dir = tmp_path / ".claude"
    claude_dir.mkdir()
    (claude_dir / "settings.json").write_text("{}", encoding="utf-8")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-migrate",
            cwd=str(tmp_path),
            source="startup",
        )

    assert mock_run.called
    call_args = mock_run.call_args
    # The subprocess MUST be invoked without an env override that hides
                                                                      
                               
    env = call_args.kwargs.get("env", None)
    if env is not None:
        assert "HOME" in env, (
            "subprocess env override removes HOME; "
            "Plan 13 step 1 migrate detection cannot find ~/.claude/"
        )
        assert env["HOME"] == str(tmp_path)


def test_maybe_launch_wizard_does_not_pass_no_migrate_detection_flag(
    monkeypatch, tmp_path
):
    """The hook MUST NOT pass any flag that suppresses Plan 13 step 1
    migrate detection (e.g., no `--no-migrate-detection`, no
    `--skip-migrate`). The wizard owns the step-1 prompt logic; the
    hook only ensures the wizard has the data it needs.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-no-flag",
            cwd=str(tmp_path),
            source="startup",
        )

    argv = mock_run.call_args.args[0]
                                                  
    forbidden_flags = {
        "--no-migrate-detection",
        "--skip-migrate",
        "--no-migrate",
        "--skip-claude",
    }
    for flag in forbidden_flags:
        assert flag not in argv, (
            f"argv {argv!r} contains forbidden flag {flag!r}; "
            "Plan 13 step 1 migrate detection must remain enabled"
        )


def test_maybe_launch_wizard_logs_step_outcome_at_debug_level(
    monkeypatch, tmp_path, caplog
):
    """The hook logs the subprocess outcome at DEBUG level for traceability
    (operator runs `HADES_LOG_LEVEL=debug hades` to surface the chain).
    Step 1 outcome (migrate yes/no/skip) is opaque to the hook — the
    log is a coarse "launched + outcome" trace, not a per-step parse.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-log",
            cwd=str(tmp_path),
            source="startup",
        )

                                                
    debug_records = [r for r in caplog.records if r.levelname == "DEBUG"]
    assert any("wizard" in r.getMessage().lower() for r in debug_records), (
        "No wizard-related debug log emitted"
    )


                                                                              


def test_maybe_launch_wizard_logs_success_when_subprocess_exits_zero(
    monkeypatch, tmp_path, caplog
):
    """Steps 2-5 happy path: subprocess exits 0 → hook logs success at INFO."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("INFO", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-success",
            cwd=str(tmp_path),
            source="startup",
        )

    info_records = [r for r in caplog.records if r.levelname == "INFO"]
    assert any(
        "complete" in r.getMessage().lower() or "wizard" in r.getMessage().lower()
        for r in info_records
    ), f"No success-path INFO log emitted; got {[r.getMessage() for r in info_records]!r}"


def test_maybe_launch_wizard_does_not_recheck_config_after_subprocess(
    monkeypatch, tmp_path
):
    """The hook MUST NOT re-stat config.toml after subprocess returns.

    Rationale: Plan 13's wizard writer is the authoritative source. The
    hook re-checking would create a TOCTOU race (config written → hook
    re-stat → file disappears via concurrent cleanup → hook logs error
    despite wizard succeeding). The hook trusts exit code 0 as the
    completion signal.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0

                                                                                 
                                                                              
                                                                    
    is_file_calls = []
    original_is_file = Path.is_file

    def counting_is_file(self):
        is_file_calls.append(str(self))
        return original_is_file(self)

    monkeypatch.setattr(Path, "is_file", counting_is_file)

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-no-recheck",
            cwd=str(tmp_path),
            source="startup",
        )

                                                                   
    config_str = str(tmp_path / ".config" / "zen-swarm" / "config.toml")
    config_checks = [p for p in is_file_calls if p == config_str]
                                                                      
                                                                      
    assert len(config_checks) <= 1, (
        f"config.toml is_file called {len(config_checks)} times; "
        "post-subprocess re-check creates TOCTOU race with wizard writer"
    )


def test_maybe_launch_wizard_returns_none_on_success(monkeypatch, tmp_path):
    """Observer-hook contract: return None regardless of subprocess outcome."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        result = _maybe_launch_wizard(
            session_id="sess-return-none",
            cwd=str(tmp_path),
            source="startup",
        )

    assert result is None


                                                                              


def test_maybe_launch_wizard_treats_sigint_as_cancel_not_error(
    monkeypatch, tmp_path, caplog
):
    """SIGINT mid-wizard: subprocess exits 130 (Unix convention) → hook
    logs at INFO with "cancelled" semantic, not at WARN/ERROR.

    Operator workflow: starts wizard → realizes they need to do
    `hades migrate claude-code` first → Ctrl-C → next session re-
    launches the wizard.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 130                                                       
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-sigint",
            cwd=str(tmp_path),
            source="startup",
        )

                                        
    msgs = [r.getMessage() for r in caplog.records]
                                                            
    assert any("cancel" in m.lower() for m in msgs), (
        f"SIGINT exit treated as non-cancel; got logs: {msgs!r}"
    )
                                           
                                             
    error_records = [r for r in caplog.records if r.levelname == "ERROR"]
    assert not error_records, (
        f"SIGINT logged as ERROR; got: {[r.getMessage() for r in error_records]!r}"
    )


def test_maybe_launch_wizard_treats_negative_signal_as_cancel(
    monkeypatch, tmp_path, caplog
):
    """Python subprocess returncode for signal: NEGATIVE signal number.
    e.g., returncode = -signal.SIGINT = -2 (Python signals subprocess
    when subprocess.run is interrupted by a signal).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = -signal.SIGINT                     
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-sigint-neg",
            cwd=str(tmp_path),
            source="startup",
        )

    msgs = [r.getMessage() for r in caplog.records]
    assert any("cancel" in m.lower() for m in msgs), (
        f"Negative-signal exit treated as non-cancel; got: {msgs!r}"
    )


def test_maybe_launch_wizard_treats_sigterm_as_cancel(monkeypatch, tmp_path, caplog):
    """SIGTERM (rare but possible: parent process tears down session) =
    also cancel, not error. Both 143 (128+15) and -15 are accepted.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    for rc in (143, -signal.SIGTERM):
        caplog.clear()
        fake = MagicMock(spec=subprocess.CompletedProcess)
        fake.returncode = rc
        with (
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
                return_value=fake,
            ),
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
                return_value="/usr/local/bin/hades",
            ),
            caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"),
        ):
            _maybe_launch_wizard(
                session_id=f"sess-sigterm-{rc}",
                cwd=str(tmp_path),
                source="startup",
            )

        msgs = [r.getMessage() for r in caplog.records]
        assert any("cancel" in m.lower() for m in msgs), (
            f"SIGTERM(rc={rc}) treated as non-cancel; got: {msgs!r}"
        )


def test_maybe_launch_wizard_resumes_next_session_when_config_still_missing(
    monkeypatch, tmp_path
):
    """Crash-only re-ask (per Plan 13 spec §2.3 + E-1 reality check):
    after SIGINT cancel, config.toml is still missing → next session
    sees the trigger condition + re-launches the wizard.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

                              
    fake_cancel = MagicMock(spec=subprocess.CompletedProcess)
    fake_cancel.returncode = 130
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake_cancel,
        ) as mock_run_1,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(session_id="sess-1", cwd=str(tmp_path), source="startup")
    assert mock_run_1.called

                                                                
    config_path = tmp_path / ".config" / "zen-swarm" / "config.toml"
    assert not config_path.exists()

                                                           
    fake_success = MagicMock(spec=subprocess.CompletedProcess)
    fake_success.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake_success,
        ) as mock_run_2,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(session_id="sess-2", cwd=str(tmp_path), source="startup")
    assert mock_run_2.called, "second session did NOT re-launch wizard"


def test_maybe_launch_wizard_signal_classification_helper():
    """The hook exposes a private classification helper that returns True
    iff the exit code corresponds to a SIGINT/SIGTERM cancel.

    Centralized helper makes the cancel-vs-error decision testable in
    isolation. Recognized signals: SIGINT (2) and SIGTERM (15), in
    both Unix exit-code (128+sig) and Python-subprocess (-sig) forms.
    """
                         
    assert _is_signal_cancel(130) is True                   
    assert _is_signal_cancel(143) is True                     
                                       
    assert _is_signal_cancel(-signal.SIGINT) is True
    assert _is_signal_cancel(-signal.SIGTERM) is True
                      
    assert _is_signal_cancel(0) is False
    assert _is_signal_cancel(1) is False                 
    assert (
        _is_signal_cancel(2) is False
    )                                                        
    assert _is_signal_cancel(127) is False                     
                                                   
    assert _is_signal_cancel(signal.SIGINT) is False                                


                                                                              


def test_maybe_launch_wizard_logs_error_with_catalog_hint_on_non_cancel_failure(
    monkeypatch, tmp_path, caplog, capsys
):
    """Non-cancel non-zero exit → hook logs WARN + structured catalog hint.

    Per Plan 18c Phase B: the wrapper's cobra RunE catch has ALREADY
    invoked Render(err) → stderr by the time the subprocess returns
    to the Python hook. The hook does NOT re-render; it only logs
    a structured trace entry for operator traceability.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 1                        
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("WARNING", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-err",
            cwd=str(tmp_path),
            source="startup",
        )

    warn_records = [r for r in caplog.records if r.levelname == "WARNING"]
    assert warn_records, "No WARNING log emitted on wizard error exit"
    msg = warn_records[0].getMessage()
                                                                         
    assert "wizard" in msg.lower(), f"WARNING log missing 'wizard' context tag: {msg!r}"
                                                          
    captured = capsys.readouterr()
                                                                         
                                                                        
                                                                   
                                                                  
    assert "HADES:" not in captured.out
    assert "HADES:" not in captured.err


def test_maybe_launch_wizard_does_not_raise_on_non_zero_exit(monkeypatch, tmp_path):
    """Hook NEVER raises — session start must continue even on wizard error."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    for rc in (1, 2, 7, 42, 127):
        fake = MagicMock(spec=subprocess.CompletedProcess)
        fake.returncode = rc
        with (
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
                return_value=fake,
            ),
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
                return_value="/usr/local/bin/hades",
            ),
        ):
                            
            result = _maybe_launch_wizard(
                session_id=f"sess-rc{rc}",
                cwd=str(tmp_path),
                source="startup",
            )
        assert result is None


def test_maybe_launch_wizard_log_references_known_catalog_codes(
    monkeypatch, tmp_path, caplog
):
    """The WARN log on non-zero exit mentions the canonical wizard.*
    catalog codes for grep-friendly observability + inv-zen-220
    compliance (Phase G compliance test asserts every error log
    references a catalog code).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 1
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("WARNING", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-codes",
            cwd=str(tmp_path),
            source="startup",
        )

    msgs = [r.getMessage() for r in caplog.records]
    msg_concat = " ".join(msgs).lower()
                                                             
    known_codes = (
        "wizard.config-corrupt",
        "wizard.migrate-incomplete",
        "wizard.mcp-spawn-fail",
    )
    found_any = any(code in msg_concat for code in known_codes) or "wizard." in msg_concat
    assert found_any, (
        f"WARN log did not reference any wizard.* catalog code; got: {msgs!r}"
    )


def test_maybe_launch_wizard_subprocess_oserror_routes_via_internal_uncaught(
    monkeypatch, tmp_path, caplog
):
    """Subprocess-exec OSError (e.g., binary unexpectedly removed
    between which() and run()) → defense-in-depth log mentioning the
    `internal-uncaught` fallback catalog code.

    Per spec §Q6: defense-in-depth uncaught fallback. The hook's WARN
    log identifies the failure path as internal-uncaught so operator
    can correlate with Phase A reserved overflow code.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    def fail_run(*args, **kwargs):
        raise OSError("[Errno 2] No such file or directory: '/usr/local/bin/hades'")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            side_effect=fail_run,
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        caplog.at_level("WARNING", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        _maybe_launch_wizard(
            session_id="sess-oserror",
            cwd=str(tmp_path),
            source="startup",
        )

    msgs = [r.getMessage() for r in caplog.records]
    msg_concat = " ".join(msgs).lower()
    assert (
        "internal-uncaught" in msg_concat
        or "uncaught" in msg_concat
        or "exec" in msg_concat
    ), f"OSError on subprocess.run did not log internal-uncaught context; got: {msgs!r}"


                                                                             


def test_maybe_launch_wizard_escape_hatch_emits_no_subprocess_call(monkeypatch, tmp_path):
    """HADES_NO_WIZARD=1 → no subprocess spawn even if config missing
    and HERMES_SKIN=hades."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("HADES_NO_WIZARD", "1")
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

                               
    config_path = tmp_path / ".config" / "zen-swarm" / "config.toml"
    assert not config_path.exists()

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-escape",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called, (
        "subprocess.run called despite HADES_NO_WIZARD=1 escape hatch"
    )


def test_maybe_launch_wizard_escape_hatch_emits_no_warn_or_error_log(
    monkeypatch, tmp_path, caplog
):
    """HADES_NO_WIZARD=1 → no WARN or ERROR log (operator opted out;
    silent is correct).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("HADES_NO_WIZARD", "1")
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"):
        _maybe_launch_wizard(
            session_id="sess-escape-silent",
            cwd=str(tmp_path),
            source="startup",
        )

    warn_or_error = [
        r for r in caplog.records if r.levelname in ("WARNING", "ERROR", "CRITICAL")
    ]
    assert not warn_or_error, (
        f"escape hatch emitted WARN/ERROR log: "
        f"{[(r.levelname, r.getMessage()) for r in warn_or_error]!r}"
    )


def test_maybe_launch_wizard_escape_hatch_optional_debug_log(
    monkeypatch, tmp_path, caplog
):
    """HADES_NO_WIZARD=1 → optional DEBUG log for operator traceability
    (operator runs `HADES_LOG_LEVEL=debug hades` to surface the chain).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("HADES_NO_WIZARD", "1")
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with caplog.at_level("DEBUG", logger="hermes_plugins.hades.hooks.wizard_handler"):
        _maybe_launch_wizard(
            session_id="sess-escape-debug",
            cwd=str(tmp_path),
            source="startup",
        )

    debug_records = [r for r in caplog.records if r.levelname == "DEBUG"]
                                                    
    assert any(
        "no_wizard" in r.getMessage().lower() or "escape" in r.getMessage().lower()
        for r in debug_records
    ), (
        f"escape hatch did not emit DEBUG log for traceability; got: "
        f"{[r.getMessage() for r in debug_records]!r}"
    )


def test_maybe_launch_wizard_escape_only_accepts_literal_1(monkeypatch, tmp_path):
    """The escape hatch env value MUST be exactly the literal string "1".

    Wrapper sets HADES_NO_WIZARD=1 (literal). Accepting truthy variants
    like "true"/"yes"/"TRUE" would surprise operators who set the env
    to a debugging string + expect the wizard to still launch.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    non_canonical_values = ["true", "TRUE", "yes", "YES", "on", "On", "y", "Y", "0", ""]
    for val in non_canonical_values:
        monkeypatch.setenv("HADES_NO_WIZARD", val)
        with (
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
                return_value=MagicMock(returncode=0),
            ) as mock_run,
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
                return_value="/usr/local/bin/hades",
            ),
        ):
            _maybe_launch_wizard(
                session_id=f"sess-val-{val}",
                cwd=str(tmp_path),
                source="startup",
            )
        if val == "0" or val == "":
                                                                     
            assert mock_run.called, f"HADES_NO_WIZARD={val!r}: wizard should launch"
        else:
            # All other non-canonical values: hook MUST conservatively
                                                                        
                                                                      
                                                                
             
                                                                       
                                                                      
            # escape MUST set exactly "1".
            assert mock_run.called, (
                f"HADES_NO_WIZARD={val!r}: only literal '1' triggers escape; "
                f"this non-canonical value should NOT prevent wizard launch"
            )


def test_maybe_launch_wizard_escape_canonical_value_blocks_launch(monkeypatch, tmp_path):
    """Sanity test: HADES_NO_WIZARD=1 (exact literal) → wizard does NOT launch."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("HADES_NO_WIZARD", "1")
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-canonical",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called


                                                                              


def test_maybe_launch_wizard_no_op_when_cwd_empty(monkeypatch, tmp_path):
    """Empty cwd (degenerate session) → no subprocess spawn."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-empty-cwd",
            cwd="",             
            source="startup",
        )

    assert not mock_run.called


def test_maybe_launch_wizard_no_op_when_cwd_kwarg_default(monkeypatch, tmp_path):
    """No cwd kwarg → no subprocess spawn (defensive against caller misuse)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(session_id="sess-no-cwd", source="startup")

    assert not mock_run.called


def test_maybe_launch_wizard_no_op_when_stdin_non_tty(monkeypatch, tmp_path):
    """Non-TTY stdin (CI / piped input) → no subprocess spawn.

    The hook uses os.isatty(0) (sys.stdin.fileno() == 0) to detect TTY
    state. Test patches os.isatty to return False.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.os.isatty",
            return_value=False,
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-non-tty",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called


def test_maybe_launch_wizard_launches_when_stdin_tty(monkeypatch, tmp_path):
    """TTY stdin (operator terminal) → wizard launches as normal."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    fake = MagicMock(spec=subprocess.CompletedProcess)
    fake.returncode = 0
    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
            return_value=fake,
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.os.isatty",
            return_value=True,
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-tty",
            cwd=str(tmp_path),
            source="startup",
        )

    assert mock_run.called


def test_maybe_launch_wizard_no_op_when_hermes_skin_not_hades(monkeypatch, tmp_path):
    """HERMES_SKIN != hades → no subprocess spawn (operator opted out of brand)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
                                       
    monkeypatch.setenv("HERMES_SKIN", "default")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-other-skin",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called


def test_maybe_launch_wizard_no_op_when_hermes_skin_unset(monkeypatch, tmp_path):
    """HERMES_SKIN unset → no subprocess spawn."""
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.delenv("HERMES_SKIN", raising=False)
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
            return_value="/usr/local/bin/hades",
        ),
    ):
        _maybe_launch_wizard(
            session_id="sess-no-skin",
            cwd=str(tmp_path),
            source="startup",
        )

    assert not mock_run.called


                                                                              


def test_maybe_launch_wizard_property_random_env_states(monkeypatch, tmp_path):
    """Property-style sanity: 100 random env combinations → hook never
    panics + never raises + always returns None."""
    homedir = tmp_path / "home"
    homedir.mkdir()
    monkeypatch.setenv("HOME", str(homedir))
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    rng = random.Random(42)                                          
    for i in range(100):
                                                      
        if rng.random() < 0.5:
            monkeypatch.setenv("HADES_NO_WIZARD", "1")
        else:
            monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
        if rng.random() < 0.7:
            monkeypatch.setenv("HERMES_SKIN", "hades")
        else:
            monkeypatch.setenv("HERMES_SKIN", "default")
                                                  
        config_dir = homedir / ".config" / "zen-swarm"
        config_path = config_dir / "config.toml"
        if rng.random() < 0.5:
            config_dir.mkdir(parents=True, exist_ok=True)
            config_path.write_text("name = 't'", encoding="utf-8")
        else:
            if config_path.exists():
                config_path.unlink()
            if config_dir.exists():
                import contextlib

                with contextlib.suppress(OSError):
                    config_dir.rmdir()

        fake = MagicMock(spec=subprocess.CompletedProcess)
        fake.returncode = rng.choice([0, 1, 130, -2])
        with (
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
                return_value=fake,
            ),
            patch(
                "hermes_plugins.hades.hooks.wizard_handler.shutil.which",
                return_value="/usr/local/bin/hades",
            ),
        ):
            # MUST NOT raise / panic
            result = _maybe_launch_wizard(
                session_id=f"sess-prop-{i}",
                cwd=str(homedir),
                source=rng.choice(["startup", "resume", "clear", "compact"]),
            )
        assert result is None, f"iteration {i}: returned non-None: {result!r}"


                                                                              


def test_config_path_no_home_env_falls_back_to_path_home(monkeypatch):
    """_config_path defensive branch: HOME unset + XDG unset → Path.home() fallback.

    Covers line 98 (the no-HOME defensive branch). Degenerate process env
    where neither XDG_CONFIG_HOME nor HOME is set; falls back to
    Path.home() which typically returns the system home directory.
    """
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.delenv("HOME", raising=False)
    result = _config_path()
                                                                     
    assert result.name == "config.toml"
    assert result.parent.name == "zen-swarm"


def test_resolve_hades_bin_uses_hades_bin_env(monkeypatch):
    """HADES_BIN env set → _resolve_hades_bin returns it directly (no which).

    Covers line 149 (override return path).
    """
    monkeypatch.setenv("HADES_BIN", "/custom/path/to/hades")
    result = _resolve_hades_bin()
    assert result == "/custom/path/to/hades"


def test_resolve_hades_bin_returns_none_when_not_found(monkeypatch):
    """No HADES_BIN env and shutil.which returns None → _resolve_hades_bin returns None.

    Covers line 153 (None return when binary not found).
    """
    monkeypatch.delenv("HADES_BIN", raising=False)
    with patch(
        "hermes_plugins.hades.hooks.wizard_handler.shutil.which", return_value=None
    ):
        result = _resolve_hades_bin()
    assert result is None


def test_is_interactive_stdin_returns_false_on_oserror(monkeypatch):
    """_is_interactive_stdin returns False when os.isatty raises OSError.

    Covers lines 169-171 (OSError exception handler in _is_interactive_stdin).
    Simulates a closed/detached stdin fd.
    """
    with patch(
        "hermes_plugins.hades.hooks.wizard_handler.os.isatty",
        side_effect=OSError("Bad file descriptor"),
    ):
        result = _is_interactive_stdin()
    assert result is False


def test_is_interactive_stdin_returns_false_on_valueerror(monkeypatch):
    """_is_interactive_stdin returns False when os.isatty raises ValueError.

    Covers lines 169-171 (ValueError exception handler — closed file object).
    """
    with patch(
        "hermes_plugins.hades.hooks.wizard_handler.os.isatty",
        side_effect=ValueError("I/O operation on closed file"),
    ):
        result = _is_interactive_stdin()
    assert result is False


def test_maybe_launch_wizard_no_op_when_hades_binary_not_found(
    monkeypatch, tmp_path, caplog
):
    """No hades binary on PATH and no HADES_BIN env → hook logs WARNING + no-op.

    Covers lines 237-241 (bin_path is None branch in _maybe_launch_wizard).
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("HADES_NO_WIZARD", raising=False)
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)
    monkeypatch.delenv("HADES_BIN", raising=False)
    monkeypatch.setenv("HERMES_SKIN", "hades")
    monkeypatch.setenv("ZEN_HOOK_DRY_RUN", "1")

    with (
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.shutil.which", return_value=None
        ),
        patch(
            "hermes_plugins.hades.hooks.wizard_handler.subprocess.run",
        ) as mock_run,
        caplog.at_level("WARNING", logger="hermes_plugins.hades.hooks.wizard_handler"),
    ):
        result = _maybe_launch_wizard(
            session_id="sess-no-bin",
            cwd=str(tmp_path),
            source="startup",
        )

    assert result is None
    assert not mock_run.called, "subprocess.run called despite missing binary"
    warn_records = [r for r in caplog.records if r.levelname == "WARNING"]
    assert warn_records, "No WARNING log emitted when hades binary not found"
    assert any(
        "PATH" in r.getMessage() or "binary" in r.getMessage() for r in warn_records
    )
