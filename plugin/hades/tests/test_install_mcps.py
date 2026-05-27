# SPDX-License-Identifier: MIT
"""Tests for the /hades:install-mcps slash command + the
hermes-config-snippet.yaml structure."""

from __future__ import annotations

import os
from pathlib import Path
from unittest.mock import patch

from hermes_plugins.hades._constants import DEFAULT_DAEMON_BASE_URL
from hermes_plugins.hades.commands.install_mcps import (
    SNIPPET_PATH,
    handle_install_mcps,
    load_snippet,
)

REQUIRED_MCPS = {
    "zen-mcp-research",
    "zen-mcp-budget",
    "zen-mcp-audit",
    "zen-mcp-sshexec",
                                                                         
}


def test_snippet_file_exists():
    assert SNIPPET_PATH.is_file(), f"snippet missing at {SNIPPET_PATH}"


def test_default_base_url_single_source_of_truth():
    """Reviewer M1: the daemon base URL constant lives in
    ``_constants.py``; both consumers (``commands.install_mcps`` and
    ``providers``) must import from the same source to keep the literal
    in lock-step on operator upgrade.
    """
    from hermes_plugins.hades import providers as prov
    from hermes_plugins.hades.commands import install_mcps as ic

                                                                       
                            
    assert ic.DEFAULT_BASE_URL == DEFAULT_DAEMON_BASE_URL
    assert prov._DEFAULT_BASE_URL == DEFAULT_DAEMON_BASE_URL
                                                                    
                                                   
    assert DEFAULT_DAEMON_BASE_URL.startswith("http://127.0.0.1:")


def test_no_duplicated_base_url_assignment_in_source():
    """Reviewer M1: keep ``DEFAULT_BASE_URL`` / ``_DEFAULT_BASE_URL``
    assignments out of plugin Python source as bare literals — both
    consumers MUST resolve through ``_constants.py`` so a single edit
    propagates everywhere. The grep guard catches a future refactor
    that reintroduces a hard-coded constant-name = ``"http://...":...``
    line outside ``_constants.py``.

    Defensive fallback values inside helper functions are allowed (they
    intentionally mirror the constant for the corrupted-install case
    and are documented as such); only module-level assignments to the
    constant name are flagged.
    """
    import re

    plugin_root = Path(__file__).resolve().parent.parent
                                                  
                                                                         
                                               
    assignment_re = re.compile(
        r'^(?:_?DEFAULT_BASE_URL)\s*=\s*[\'"]http://[^\'"]+[\'"]',
        re.MULTILINE,
    )
    offenders: list[str] = []
    for py in plugin_root.rglob("*.py"):
        if py.name == "_constants.py":
                              
            continue
        if "tests" in py.parts:
            continue
        text = py.read_text(encoding="utf-8")
        if assignment_re.search(text):
            offenders.append(str(py.relative_to(plugin_root)))
    assert not offenders, (
        "duplicated DEFAULT_BASE_URL assignment in plugin source — "
        f"import from ``_constants.py`` instead: {offenders}"
    )


def test_snippet_parses_as_yaml():
    parsed = load_snippet()
    assert "mcp_servers" in parsed


def test_snippet_contains_all_required_mcps():
    parsed = load_snippet()
    servers = set(parsed["mcp_servers"].keys())
    missing = REQUIRED_MCPS - servers
    assert not missing, f"snippet missing MCPs: {sorted(missing)}"


def test_snippet_each_entry_has_command():
    parsed = load_snippet()
    for name, entry in parsed["mcp_servers"].items():
        assert isinstance(entry, dict), f"{name} entry is not a dict"
        assert entry.get("command"), f"{name} missing command field"


def test_snippet_caronte_has_no_mcp_entry():
    """Caronte code-graph is in-process: the snippet must NOT
    contain a gitnexus or caronte MCP entry — no external binary required."""
    parsed = load_snippet()
    servers = set(parsed["mcp_servers"].keys())
    assert "gitnexus" not in servers, (
        "gitnexus MCP removed in Plan 19 (caronte is in-process)"
    )
    assert "caronte" not in servers, "caronte runs in-process — no MCP entry needed"


def test_snippet_zen_mcps_set_daemon_socket_env():
    """Per invariant single-egress, every zen-mcp-* entry must set
    ZEN_DAEMON_SOCKET env."""
    parsed = load_snippet()
    zen_mcps = [k for k in parsed["mcp_servers"].keys() if k.startswith("zen-mcp-")]
    for name in zen_mcps:
        env = parsed["mcp_servers"][name].get("env", {})
        assert "ZEN_DAEMON_SOCKET" in env, f"{name} env missing ZEN_DAEMON_SOCKET"


def test_handle_install_mcps_returns_string():
    """Handler returns a string regardless of whether `hermes` binary is on
    PATH (it should report dry-run guidance in that case)."""
    with patch.dict(os.environ, {"ZEN_INSTALL_MCPS_DRY_RUN": "1"}):
        result = handle_install_mcps("")
    assert isinstance(result, str)
    assert "gitnexus" not in result, (
        "gitnexus MCP removed in Plan 19 (caronte is in-process)"
    )
                                                              
    for name in REQUIRED_MCPS:
        assert name in result, f"output missing mention of {name}"


def test_handle_install_mcps_handles_missing_hermes(monkeypatch):
    """If `hermes` not on PATH, handler emits a clear install hint instead
    of crashing."""
    monkeypatch.setenv("PATH", "/usr/bin:/bin")                              
    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)
                                                     
    from hermes_plugins.hades.commands import install_mcps as mod

    with patch.object(mod, "_hermes_on_path", return_value=False):
        result = handle_install_mcps("")
    assert isinstance(result, str)
    assert "hermes" in result.lower()
    assert "install" in result.lower() or "brew" in result.lower()


def test_handle_install_mcps_not_placeholder():
    import inspect

    from hermes_plugins.hades.commands import install_mcps as mod

    src = inspect.getsource(mod.handle_install_mcps)
    assert "placeholder" not in src.lower(), "handle_install_mcps still a placeholder"


                                                                             
                                                               
                                                                             


def test_load_snippet_returns_empty_dict_for_non_dict_yaml(tmp_path):
    """load_snippet returns {} when yaml.safe_load returns a non-dict value."""
    from hermes_plugins.hades.commands import install_mcps as mod

    fake_snippet = tmp_path / "hermes-config-snippet.yaml"
    fake_snippet.write_text("- list\n- item\n", encoding="utf-8")
    orig_path = mod.SNIPPET_PATH
    mod.SNIPPET_PATH = fake_snippet
    try:
        result = mod.load_snippet()
        assert result == {}
    finally:
        mod.SNIPPET_PATH = orig_path


def test_handle_install_mcps_oserror_on_load(tmp_path):
    """handle_install_mcps returns error string when snippet file is missing."""
    from hermes_plugins.hades.commands import install_mcps as mod

    missing = tmp_path / "no-such-snippet.yaml"
    orig_path = mod.SNIPPET_PATH
    mod.SNIPPET_PATH = missing
    try:
        result = handle_install_mcps("")
    finally:
        mod.SNIPPET_PATH = orig_path
    assert isinstance(result, str)
    assert "ERROR" in result
    assert "failed to load snippet" in result


def test_handle_install_mcps_empty_mcp_servers(tmp_path):
    """handle_install_mcps returns error when mcp_servers block is empty."""
    from hermes_plugins.hades.commands import install_mcps as mod

    fake_snippet = tmp_path / "hermes-config-snippet.yaml"
    fake_snippet.write_text("mcp_servers: {}\n", encoding="utf-8")
    orig_path = mod.SNIPPET_PATH
    mod.SNIPPET_PATH = fake_snippet
    try:
        result = handle_install_mcps("")
    finally:
        mod.SNIPPET_PATH = orig_path
    assert isinstance(result, str)
    assert "ERROR" in result
    assert "no mcp_servers entries" in result


def test_handle_install_mcps_dry_run_flag():
    """--dry-run argument triggers dry-run path (hermes on path, no env var)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.dict(os.environ, {}, clear=False):
                                       
            os.environ.pop("ZEN_INSTALL_MCPS_DRY_RUN", None)
            result = handle_install_mcps("--dry-run")
    assert isinstance(result, str)
    assert "dry-run" in result.lower()
    assert "Manual install" in result


def test_run_hermes_mcp_add_success(monkeypatch):
    """_run_hermes_mcp_add returns (True, added) on returncode=0."""

    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 0
        stderr = ""

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-research", {"command": "zen-mcp-research", "args": [], "env": {}}
        )
    assert ok is True
    assert "added" in msg


def test_run_hermes_mcp_add_already_exists(monkeypatch):
    """_run_hermes_mcp_add returns (True, already present) when stderr says already exists."""
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 1
        stderr = "Error: MCP already exists in config"

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-audit", {"command": "zen-mcp-audit", "args": [], "env": {}}
        )
    assert ok is True
    assert "already present" in msg


def test_run_hermes_mcp_add_hard_failure(monkeypatch):
    """_run_hermes_mcp_add returns (False, error msg) on non-zero returncode without 'already'."""
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 127
        stderr = "command not found: hermes"

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-budget", {"command": "zen-mcp-budget", "args": [], "env": {}}
        )
    assert ok is False
    assert "127" in msg


def test_run_hermes_mcp_add_does_not_misclassify_path_not_exist(monkeypatch):
    """Reviewer M2: the 'already exists' idempotence detector must NOT
    treat any stderr containing 'exist' as success. The prior loose
    match (``\"already\" in stderr_lc or \"exist\" in stderr_lc``)
    false-positives on common errors like ``\"path does not exist\"``
    or ``\"binary does not exist\"`` — those are HARD failures that
    must surface.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 2
        stderr = "Error: configuration path does not exist: /etc/hermes/foo.yaml"

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-sshexec", {"command": "zen-mcp-sshexec", "args": [], "env": {}}
        )
    assert ok is False, (
        "_run_hermes_mcp_add misclassified a 'path does not exist' error "
        "as idempotent success — the heuristic must require a fuller "
        "'already exists' or 'already registered' phrase."
    )
    assert "2" in msg or "does not exist" in msg


def test_run_hermes_mcp_add_does_not_misclassify_binary_not_exist(monkeypatch):
    """Reviewer M2: companion to the path-not-exist case — ``\"binary
    does not exist\"`` from a path-resolution failure must also surface
    as a hard error rather than being silently swallowed as
    'already present'.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 1
        stderr = "Resolution error: subcommand binary does not exist on PATH"

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-sshexec", {"command": "zen-mcp-sshexec", "args": [], "env": {}}
        )
    assert ok is False
                                                                 
                                                     
    assert "binary does not exist" in msg or "1" in msg


def test_run_hermes_mcp_add_recognises_already_registered_variant(monkeypatch):
    """Reviewer M2: the tightened heuristic must accept both common
    idempotence phrasings — ``\"already exists\"`` (Hermes' canonical
    wording) and ``\"already registered\"`` (an alternative phrasing
    some Hermes builds emit when the MCP was added via API rather than
    the CLI). Both indicate the entry is in place; neither is an
    actionable failure for the operator.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 1
        stderr = "Error: MCP 'zen-mcp-research' is already registered in config"

    with patch.object(mod.subprocess, "run", return_value=FakeResult()):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-research", {"command": "zen-mcp-research", "args": [], "env": {}}
        )
    assert ok is True
    assert "already present" in msg


def test_run_hermes_mcp_add_subprocess_exception(monkeypatch):
    """_run_hermes_mcp_add returns (False, invocation failed) on SubprocessError."""
    import subprocess

    from hermes_plugins.hades.commands import install_mcps as mod

    with patch.object(
        mod.subprocess, "run", side_effect=subprocess.SubprocessError("timeout")
    ):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-audit", {"command": "zen-mcp-audit", "args": [], "env": {}}
        )
    assert ok is False
    assert "invocation failed" in msg


def test_run_hermes_mcp_add_with_env():
    """_run_hermes_mcp_add correctly includes --env flags for entries with env."""
    from hermes_plugins.hades.commands import install_mcps as mod

    captured = {}

    class FakeResult:
        returncode = 0
        stderr = ""

    def fake_run(invocation, **kwargs):
        captured["invocation"] = invocation
        return FakeResult()

    with patch.object(mod.subprocess, "run", side_effect=fake_run):
        ok, msg = mod._run_hermes_mcp_add(
            "zen-mcp-research",
            {
                "command": "zen-mcp-research",
                "args": [],
                "env": {"ZEN_DAEMON_SOCKET": "/tmp/zen.sock"},
            },
        )
    assert ok is True
    inv = captured["invocation"]
    assert "--env" in inv
    env_idx = inv.index("--env")
    assert "ZEN_DAEMON_SOCKET" in inv[env_idx + 1]


def test_handle_install_mcps_live_mode_all_success(monkeypatch, tmp_path):
    """Live mode (hermes on path, no dry-run): all MCPs succeed → success summary."""
    from hermes_plugins.hades.commands import install_mcps as mod

    class FakeResult:
        returncode = 0
        stderr = ""

                                                                    
                                                                      
                                               
    monkeypatch.setenv("HERMES_HOME", str(tmp_path / "fake_hermes_home"))
    (tmp_path / "fake_hermes_home").mkdir()
    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)
    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.object(mod.subprocess, "run", return_value=FakeResult()):
            result = handle_install_mcps("")
    assert isinstance(result, str)
    assert "MCP install results" in result
    assert "All MCPs + provider profile installed" in result
    assert "hermes mcp test zen-mcp-research" in result


def test_handle_install_mcps_live_mode_partial_failure(monkeypatch, tmp_path):
    """Live mode: one MCP fails → fallback to manual install block."""
    from hermes_plugins.hades.commands import install_mcps as mod

    call_count = {"n": 0}

    class FakeResultFail:
        returncode = 1
        stderr = "some other error"

    class FakeResultOk:
        returncode = 0
        stderr = ""

                                                                        
    monkeypatch.setenv("HERMES_HOME", str(tmp_path / "fake_hermes_home"))
    (tmp_path / "fake_hermes_home").mkdir()

    def fake_run(invocation, **kwargs):
        call_count["n"] += 1
                                                                
        return FakeResultFail() if call_count["n"] == 1 else FakeResultOk()

    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)
    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.object(mod.subprocess, "run", side_effect=fake_run):
            result = handle_install_mcps("")
    assert isinstance(result, str)
    assert "Some entries failed" in result
    assert "Manual install" in result


def test_format_manual_install_block_no_env():
    """_format_manual_install_block handles entries with no env."""
    from hermes_plugins.hades.commands import install_mcps as mod

    entries = {
        "zen-mcp-sshexec": {"command": "zen-mcp-sshexec", "args": [], "env": {}},
    }
    result = mod._format_manual_install_block(entries)
    assert "hermes mcp add zen-mcp-sshexec" in result
    assert "Manual install" in result
    assert "--env" not in result                         


def test_format_manual_install_block_with_env():
    """_format_manual_install_block includes --env for entries that have env."""
    from hermes_plugins.hades.commands import install_mcps as mod

    entries = {
        "zen-mcp-research": {
            "command": "zen-mcp-research",
            "args": [],
            "env": {"ZEN_DAEMON_SOCKET": "${ZEN_DAEMON_SOCKET:-/tmp/zen-swarm.sock}"},
        },
    }
    result = mod._format_manual_install_block(entries)
    assert "--env" in result
    assert "ZEN_DAEMON_SOCKET" in result


def test_format_manual_install_block_multi_env_uses_per_pair_flags():
    """Multi-env entries emit one '--env K=V' per pair (H'-10 NIT-2;
    matches live-mode _run_hermes_mcp_add invocation + 'hermes mcp add
    --help' syntax). The prior single-flag form
    '--env "K1=V1 K2=V2"' did not match the CLI's per-pair flag handling."""
    from hermes_plugins.hades.commands import install_mcps as mod

    entries = {
        "zen-mcp-sshexec": {
            "command": "zen-mcp-sshexec",
            "args": [],
            "env": {
                "ZEN_DAEMON_SOCKET": "/tmp/zen.sock",
                "ZEN_FOO": "bar",
            },
        },
    }
    result = mod._format_manual_install_block(entries)
                                                        
    assert "--env ZEN_DAEMON_SOCKET=/tmp/zen.sock" in result
    assert "--env ZEN_FOO=bar" in result
                                                 
    assert "--env '" not in result
    assert '--env "' not in result


                                                                             
                                                                        
                                          
 
                                                          
                                                                     
                                                              
                                                                         
                                                                         
                                                          
                                                     
                                                                          
                                                         
 
                                                   
                                                                       
                                                                             


def _plugin_providers_dir() -> Path:
    """Resolve <repo>/plugin/zen-swarm/providers/."""
                                                                      
    return Path(__file__).resolve().parent.parent / "providers"


def test_provider_plugin_dir_exists():
    """Sanity: the provider plugin source must exist (its __init__.py is
    what we symlink into Hermes' plugins/model-providers/)."""
    p = _plugin_providers_dir()
    assert p.is_dir(), f"missing provider plugin dir at {p}"
    assert (p / "__init__.py").is_file(), f"missing {p / '__init__.py'}"


def test_install_provider_plugin_symlink_creates_link(tmp_path, monkeypatch):
    """``_install_provider_plugin_symlink(hermes_home)`` must create a
    symlink at ``<hermes_home>/plugins/model-providers/zen-swarm`` that
    points at ``<repo>/plugin/zen-swarm/providers``."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    ok, msg = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is True, f"_install_provider_plugin_symlink failed: {msg}"

    link = hermes_home / "plugins" / "model-providers" / "zen-swarm"
    assert link.is_symlink(), f"expected symlink at {link}"
    assert link.resolve() == _plugin_providers_dir().resolve()
                                                                
    assert (link / "__init__.py").is_file()


def test_install_provider_plugin_symlink_is_idempotent(tmp_path):
    """Re-running must not double-create the symlink or fail when it
    already points at the correct target."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    ok1, _ = mod._install_provider_plugin_symlink(hermes_home)
    ok2, msg2 = mod._install_provider_plugin_symlink(hermes_home)
    assert ok1 is True
    assert ok2 is True, f"second call failed: {msg2}"
                                                  
    link = hermes_home / "plugins" / "model-providers" / "zen-swarm"
    assert link.is_symlink()
    assert link.resolve() == _plugin_providers_dir().resolve()


def test_install_provider_plugin_symlink_replaces_stale_link(tmp_path):
    """If a symlink already exists pointing at a WRONG target, the helper
    must replace it (operator-friendly: handles repo moves)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    plugins_dir = hermes_home / "plugins" / "model-providers"
    plugins_dir.mkdir(parents=True)
    wrong_target = tmp_path / "wrong_provider_dir"
    wrong_target.mkdir()
    bad_link = plugins_dir / "zen-swarm"
    bad_link.symlink_to(wrong_target)

    ok, _ = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is True
                                                  
    assert bad_link.is_symlink()
    assert bad_link.resolve() == _plugin_providers_dir().resolve()


def test_install_provider_plugin_symlink_refuses_to_overwrite_real_dir(tmp_path):
    """If a real DIRECTORY (not a symlink) exists at the target, the
    helper must refuse to clobber it — operator-managed install
    (e.g. ``make plugin-install`` copy mode) takes precedence."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    plugins_dir = hermes_home / "plugins" / "model-providers"
    plugins_dir.mkdir(parents=True)
    real_dir = plugins_dir / "zen-swarm"
    real_dir.mkdir()
    (real_dir / "__init__.py").write_text("# operator's own copy\n", encoding="utf-8")

    ok, msg = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is False
    assert "real directory" in msg.lower() or "not a symlink" in msg.lower()
                                     
    assert (real_dir / "__init__.py").read_text(
        encoding="utf-8"
    ) == "# operator's own copy\n"


def test_update_hermes_config_provider_creates_file(tmp_path):
    """``_update_hermes_config_provider(hermes_home, base_url)`` must
    create ``$HERMES_HOME/config.yaml`` with ``model.provider: zen-swarm``
    + ``model.base_url`` when no config exists yet."""
    import yaml
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True, msg

    config_path = hermes_home / "config.yaml"
    assert config_path.is_file()
    cfg = yaml.safe_load(config_path.read_text(encoding="utf-8")) or {}
    assert cfg.get("model", {}).get("provider") == "zen-swarm"
    assert cfg.get("model", {}).get("base_url") == "http://127.0.0.1:8080"


def test_update_hermes_config_provider_preserves_other_keys(tmp_path):
    """Other top-level keys + non-model.provider model fields must survive
    the rewrite. Operator-friendly: install-mcps does not nuke unrelated
    config (plugins.enabled, mcp_servers, themes, etc.)."""
    import yaml
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text(
        yaml.safe_dump(
            {
                "plugins": {"enabled": ["zen-swarm", "other-plugin"]},
                "theme": "dark",
                "model": {
                    "default": "claude-opus-4.6",
                    "provider": "anthropic",  # MUST be overwritten
                    "base_url": "https://api.anthropic.com",  # MUST be overwritten
                    "max_tokens": 32000,  # MUST survive
                },
                "mcp_servers": {"foo": {"command": "foo"}},
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )

    ok, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True
    cfg = yaml.safe_load(config_path.read_text(encoding="utf-8"))

                    
    assert cfg["model"]["provider"] == "zen-swarm"
    assert cfg["model"]["base_url"] == "http://127.0.0.1:8080"
                      
    assert cfg["plugins"]["enabled"] == ["zen-swarm", "other-plugin"]
    assert cfg["theme"] == "dark"
    assert cfg["model"]["default"] == "claude-opus-4.6"
    assert cfg["model"]["max_tokens"] == 32000
    assert cfg["mcp_servers"]["foo"]["command"] == "foo"


def test_update_hermes_config_provider_removes_stale_api_keys(tmp_path):
    """Reviewer M7: when the helper rewrites ``config.yaml`` to point
    Hermes at zen-swarm, it must drop any pre-existing
    ``model.api_mode`` and ``model.api_key`` keys (the
    ``install_mcps`` body already does this via ``model_cfg.pop(...)``
    so the registered ``ProviderProfile`` wins at runtime — but no test
    pinned the behaviour, so a future refactor could silently
    re-introduce them).

    Background: ``hermes_cli/auth.py:4192-4193`` reads ``model.api_key``
    and ``model.api_mode`` from ``config.yaml`` and overrides the
    ``ProviderProfile`` runtime values. Leaving stale keys would make
    Hermes route to the prior provider even when the operator runs
    ``/hades:install-mcps``.
    """
    import yaml as yaml_mod
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text(
        yaml_mod.safe_dump(
            {
                "model": {
                    "provider": "anthropic",
                    "base_url": "https://api.anthropic.com",
                    "api_mode": "anthropic_messages",  # MUST be removed
                    "api_key": "sk-ant-leak",  # MUST be removed
                    "default": "claude-opus-4.6",  # MUST survive
                    "max_tokens": 32000,  # MUST survive
                },
                "plugins": {"enabled": ["zen-swarm"]},  # MUST survive
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )

    ok, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True
    cfg = yaml_mod.safe_load(config_path.read_text(encoding="utf-8"))

    # Stale api_mode/api_key MUST be absent — otherwise Hermes' runtime
                                                               
    assert "api_mode" not in cfg["model"], (
        "stale model.api_mode survived the rewrite — hermes_cli/auth.py "
        "would override the zen-swarm ProviderProfile."
    )
    assert "api_key" not in cfg["model"], (
        "stale model.api_key survived the rewrite — leaks a credential "
        "the operator removed."
    )
                               
    assert cfg["model"]["provider"] == "zen-swarm"
    assert cfg["model"]["base_url"] == "http://127.0.0.1:8080"
                                 
    assert cfg["model"]["default"] == "claude-opus-4.6"
    assert cfg["model"]["max_tokens"] == 32000
    assert cfg["plugins"]["enabled"] == ["zen-swarm"]


def test_update_hermes_config_provider_handles_absent_api_keys(tmp_path):
    """Companion to the removal-of-stale-keys test (M7): when the
    operator has NEVER configured ``model.api_mode`` or ``model.api_key``,
    the rewrite must still succeed without ``KeyError`` — the pop()
    calls use the ``None`` default. This guards the no-op branch.
    """
    import yaml as yaml_mod
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text(
        yaml_mod.safe_dump(
            {"model": {"provider": "anthropic", "default": "claude-opus-4.6"}},
            sort_keys=False,
        ),
        encoding="utf-8",
    )

    ok, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True
    cfg = yaml_mod.safe_load(config_path.read_text(encoding="utf-8"))
    assert "api_mode" not in cfg["model"]
    assert "api_key" not in cfg["model"]
    assert cfg["model"]["provider"] == "zen-swarm"


def test_update_hermes_config_provider_creates_backup(tmp_path):
    """When an existing config.yaml is mutated, a ``config.yaml.bak.<ts>``
    file must be created so operator can recover. The backup must contain
    the PRE-mutation content."""
    import yaml
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    original_content = yaml.safe_dump(
        {"model": {"provider": "anthropic"}}, sort_keys=False
    )
    config_path.write_text(original_content, encoding="utf-8")

    ok, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True

    backups = sorted(hermes_home.glob("config.yaml.bak.*"))
    assert backups, (
        f"no backup created under {hermes_home}; files: {list(hermes_home.iterdir())}"
    )
    assert backups[-1].read_text(encoding="utf-8") == original_content


def test_update_hermes_config_provider_no_backup_when_no_prior_config(tmp_path):
    """Creating config.yaml for the first time should NOT leave a stray
    ``config.yaml.bak.*`` file (no pre-state to back up)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    ok, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is True
    backups = list(hermes_home.glob("config.yaml.bak.*"))
    assert backups == [], f"unexpected backup files: {backups}"


def test_update_hermes_config_provider_idempotent(tmp_path):
    """Re-running must yield identical config (no duplicate keys, no
    backup explosion). The second run's backup captures the post-first-run
    state — which is structurally identical to the target."""
    import yaml
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    ok1, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    after_first = (hermes_home / "config.yaml").read_text(encoding="utf-8")

    ok2, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    after_second = (hermes_home / "config.yaml").read_text(encoding="utf-8")

    assert ok1 is True and ok2 is True
    cfg1 = yaml.safe_load(after_first)
    cfg2 = yaml.safe_load(after_second)
    assert cfg1 == cfg2, "second run produced different config"


def test_handle_install_mcps_invokes_provider_wiring(tmp_path, monkeypatch):
    """End-to-end: ``handle_install_mcps`` (live mode) must also invoke
    the provider-plugin symlink + config.yaml update steps.

    Verifies the two helpers are called when handler runs in live mode
    against a HERMES_HOME under tmp_path.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    monkeypatch.setenv("HERMES_HOME", str(hermes_home))
    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)

    class FakeResult:
        returncode = 0
        stderr = ""

    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.object(mod.subprocess, "run", return_value=FakeResult()):
            result = handle_install_mcps("")
                                               
    assert (hermes_home / "config.yaml").is_file(), (
        "handle_install_mcps live-mode did not create config.yaml"
    )
    assert (hermes_home / "plugins" / "model-providers" / "zen-swarm").exists(), (
        "handle_install_mcps live-mode did not symlink provider plugin"
    )
                                              
    assert "provider" in result.lower()


def test_handle_install_mcps_provider_wiring_dry_run(tmp_path, monkeypatch):
    """Dry-run must NOT touch config.yaml or create the symlink — output
    should describe what would be done."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    monkeypatch.setenv("HERMES_HOME", str(hermes_home))
    monkeypatch.setenv("ZEN_INSTALL_MCPS_DRY_RUN", "1")

    with patch.object(mod, "_hermes_on_path", return_value=True):
        result = handle_install_mcps("")
    assert not (hermes_home / "config.yaml").exists(), (
        "dry-run wrote config.yaml; expected no-op"
    )
    assert not (hermes_home / "plugins" / "model-providers" / "zen-swarm").exists(), (
        "dry-run created symlink; expected no-op"
    )
    assert "dry-run" in result.lower()


                                                                             
                                                           
                                                                             


def test_resolve_hermes_home_falls_back_to_default(monkeypatch):
    """``_resolve_hermes_home()`` returns ``~/.hermes`` when ``HERMES_HOME``
    is unset or empty."""
    from hermes_plugins.hades.commands import install_mcps as mod

    monkeypatch.delenv("HERMES_HOME", raising=False)
    result = mod._resolve_hermes_home()
    assert result == Path.home() / ".hermes"


def test_resolve_hermes_home_strips_whitespace(monkeypatch):
    """``_resolve_hermes_home()`` treats whitespace-only HERMES_HOME as
    unset (matches hermes_constants.get_hermes_home behaviour)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    monkeypatch.setenv("HERMES_HOME", "   ")
    result = mod._resolve_hermes_home()
    assert result == Path.home() / ".hermes"


def test_install_provider_plugin_symlink_handles_oserror_on_symlink_create(
    tmp_path, monkeypatch
):
    """Reviewer M5: when ``Path.symlink_to`` raises OSError (filesystem
    does not support symlinks, e.g. some Windows mounts or sandboxed
    FUSE), the helper must return ``(False, message)`` rather than let
    the exception propagate as an unhandled Python traceback to the
    operator's terminal.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()

    from pathlib import Path as _Path

    original_symlink_to = _Path.symlink_to

    def fake_symlink_to(self, *a, **kw):
                                                                   
                                                            
        raise OSError("simulated: filesystem does not support symbolic links")

    monkeypatch.setattr(_Path, "symlink_to", fake_symlink_to)
    ok, msg = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is False, (
        "_install_provider_plugin_symlink should catch OSError from "
        "symlink_to and return (False, ...) — got (True, ...) which "
        "means the exception path propagated as an unhandled traceback."
    )
    assert "symlink" in msg.lower() and "failed" in msg.lower(), (
        f"failure message should mention the failed symlink: {msg!r}"
    )
                                                          
    monkeypatch.setattr(_Path, "symlink_to", original_symlink_to)


def test_install_provider_plugin_symlink_handles_oserror_on_stale_replace(
    tmp_path, monkeypatch
):
    """Reviewer M5: companion case — when the helper is REPLACING a
    stale symlink and the new symlink_to fails, the same OSError-handling
    contract applies. Without this guard, an operator with a moved repo
    would see a traceback instead of an actionable error.
    """
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    plugins_dir = hermes_home / "plugins" / "model-providers"
    plugins_dir.mkdir(parents=True)
    wrong_target = tmp_path / "old_repo_location"
    wrong_target.mkdir()
    stale = plugins_dir / "zen-swarm"
    stale.symlink_to(wrong_target)

    from pathlib import Path as _Path

    original_symlink_to = _Path.symlink_to

    def fake_symlink_to(self, *a, **kw):
        raise OSError("simulated: cross-device link not permitted")

    monkeypatch.setattr(_Path, "symlink_to", fake_symlink_to)
    ok, msg = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is False
    assert "symlink" in msg.lower() and "failed" in msg.lower()
    monkeypatch.setattr(_Path, "symlink_to", original_symlink_to)


def test_install_provider_plugin_symlink_handles_oserror_on_resolve(
    tmp_path, monkeypatch
):
    """If ``link.resolve()`` raises OSError (broken symlink), the helper
    treats the link as stale and replaces it."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    plugins_dir = hermes_home / "plugins" / "model-providers"
    plugins_dir.mkdir(parents=True)
    link = plugins_dir / "zen-swarm"

                                                                        
                                                                     
                                                                        
    link.symlink_to(tmp_path / "ghost_target")

    from pathlib import Path as _Path

    original_resolve = _Path.resolve

    def fake_resolve(self, *a, **kw):
        if str(self) == str(link):
            raise OSError("simulated broken link")
        return original_resolve(self, *a, **kw)

    monkeypatch.setattr(_Path, "resolve", fake_resolve)
    ok, msg = mod._install_provider_plugin_symlink(hermes_home)
    assert ok is True, msg


def test_update_hermes_config_provider_oserror_on_read(tmp_path, monkeypatch):
    """If reading the existing config.yaml raises OSError, helper returns
    (False, error message)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text("model:\n  provider: anthropic\n", encoding="utf-8")

                                                               
    from pathlib import Path as _Path

    original_read_text = _Path.read_text

    def fake_read_text(self, *a, **kw):
        if str(self) == str(config_path):
            raise OSError("simulated read error")
        return original_read_text(self, *a, **kw)

    monkeypatch.setattr(_Path, "read_text", fake_read_text)
    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "failed to read existing config.yaml" in msg


def test_update_hermes_config_provider_yaml_error_on_read(tmp_path):
    """If existing config.yaml is malformed YAML, helper returns (False, error)."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
                                                       
    config_path.write_text("model:\n\t: bad\n", encoding="utf-8")

    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "failed to read existing config.yaml" in msg


def test_update_hermes_config_provider_non_dict_yaml(tmp_path):
    """If existing config.yaml parses as a non-dict (e.g. a list), helper
    refuses to mutate it — operator-managed file shape we don't understand."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text("- item1\n- item2\n", encoding="utf-8")

    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "expected dict" in msg


def test_update_hermes_config_provider_backup_uses_exclusive_create(
    tmp_path, monkeypatch
):
    """Reviewer M6: backup writes must be exclusive-create (mode='x')
    so a pre-existing backup file at the same timestamp is detected
    rather than silently clobbered. Two updates within the same
    second (rare but possible — operator script triggers + slash
    command + ZEN_INSTALL_MCPS_DRY_RUN toggle hitting the second
    boundary) would otherwise overwrite the first backup and lose
    recovery state.

    The implementation must catch ``FileExistsError`` and fall back
    to a microsecond-suffixed name; the existing backup STAYS intact.
    """
    import yaml as yaml_mod
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    initial = yaml_mod.safe_dump({"model": {"provider": "anthropic"}}, sort_keys=False)
    config_path.write_text(initial, encoding="utf-8")

                                                                   
                                                                 
    class FrozenDT:
        @staticmethod
        def now():
            class _D:
                @staticmethod
                def strftime(fmt):
                    if "%f" in fmt:
                                                                           
                        return "20260515-200000-000123"
                    return "20260515-200000"

            return _D()

    monkeypatch.setattr(mod._dt, "datetime", FrozenDT)

    ok1, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok1 is True

                                                          
    first_backup = hermes_home / "config.yaml.bak.20260515-200000"
    assert first_backup.is_file()
    first_backup_content = first_backup.read_text(encoding="utf-8")

                                                                  
                                                             
    config_path.write_text(
        yaml_mod.safe_dump(
            {"model": {"provider": "anthropic", "extra": "changed"}}, sort_keys=False
        ),
        encoding="utf-8",
    )

    ok2, _ = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok2 is True

    # First backup MUST be untouched — collision protection.
    assert first_backup.read_text(encoding="utf-8") == first_backup_content, (
        "second-run backup clobbered the first — exclusive-create + "
        "microsecond-fallback was not honoured."
    )
                                                                 
    microsec_backups = list(hermes_home.glob("config.yaml.bak.*-*-*"))
    assert microsec_backups, (
        "no microsecond-suffixed backup created on collision — operator "
        "would lose recovery state across two same-second updates."
    )


def test_update_hermes_config_provider_microsecond_backup_oserror(tmp_path, monkeypatch):
    """Reviewer M6 OSError-on-microsecond-fallback path: when the first
    exclusive-create raises FileExistsError (collision), the helper
    retries under a microsecond-suffixed name; if THAT write also
    raises OSError (disk full caught at the retry), the helper still
    aborts cleanly with the failure-path return value rather than
    letting the exception bubble.
    """
    import builtins

    import yaml as yaml_mod
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    config_path.write_text(
        yaml_mod.safe_dump({"model": {"provider": "anthropic"}}, sort_keys=False),
        encoding="utf-8",
    )

                                                                        
                                                                          
                                    
    class FrozenDT:
        @staticmethod
        def now():
            class _D:
                @staticmethod
                def strftime(fmt):
                    return "20260515-200000-000123" if "%f" in fmt else "20260515-200000"

            return _D()

    monkeypatch.setattr(mod._dt, "datetime", FrozenDT)
                                                                 
    (hermes_home / "config.yaml.bak.20260515-200000").write_text("preexisting")

    real_open = builtins.open

    def fake_open(file, mode="r", *args, **kwargs):
                                                                          
                                                                       
                  
        if ".bak.20260515-200000-000123" in str(file) and "x" in mode:
            raise OSError("simulated disk full on microsecond retry")
        return real_open(file, mode, *args, **kwargs)

    monkeypatch.setattr(builtins, "open", fake_open)
    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "failed to write backup" in msg


def test_update_hermes_config_provider_backup_write_oserror(tmp_path, monkeypatch):
    """If the backup write fails (disk full, perm denied), helper aborts
    BEFORE mutating config.yaml — operator's original file stays intact.

    Post-M6: backup writes use ``open(path, 'x')`` exclusive-create
    rather than ``Path.write_text`` so this test patches the module-
    local ``open`` builtin to simulate the failure path.
    """
    import builtins

    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
    config_path = hermes_home / "config.yaml"
    original = "model:\n  provider: anthropic\n  base_url: https://example.com\n"
    config_path.write_text(original, encoding="utf-8")

    real_open = builtins.open

    def fake_open(file, mode="r", *args, **kwargs):
                                                             
                                                                       
        if ".bak." in str(file) and ("x" in mode or "w" in mode):
            raise OSError("simulated disk full")
        return real_open(file, mode, *args, **kwargs)

    monkeypatch.setattr(builtins, "open", fake_open)
    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "failed to write backup" in msg
                              
    assert config_path.read_text(encoding="utf-8") == original


def test_update_hermes_config_provider_atomic_write_oserror(tmp_path, monkeypatch):
    """If the atomic write step fails (os.replace OSError), helper returns
    (False, error). Original file may or may not survive — we just verify
    the error path is reachable."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()

    def fake_replace(*a, **kw):
        raise OSError("simulated cross-device link")

    monkeypatch.setattr(mod.os, "replace", fake_replace)
    ok, msg = mod._update_hermes_config_provider(hermes_home, "http://127.0.0.1:8080")
    assert ok is False
    assert "failed to write config.yaml" in msg


def test_handle_install_mcps_live_mode_symlink_failure(tmp_path, monkeypatch):
    """If the provider symlink helper fails, handler marks any_failed and
    appends the manual-install block."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    plugins_dir = hermes_home / "plugins" / "model-providers"
    plugins_dir.mkdir(parents=True)
                                                                                 
    (plugins_dir / "zen-swarm").mkdir()

    monkeypatch.setenv("HERMES_HOME", str(hermes_home))
    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)

    class FakeResult:
        returncode = 0
        stderr = ""

    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.object(mod.subprocess, "run", return_value=FakeResult()):
            result = handle_install_mcps("")
    assert "Some entries failed" in result
    assert "Manual install" in result


def test_handle_install_mcps_live_mode_config_failure(tmp_path, monkeypatch):
    """If the config.yaml update fails (existing file is list), handler
    marks any_failed and appends the manual-install block."""
    from hermes_plugins.hades.commands import install_mcps as mod

    hermes_home = tmp_path / "fake_hermes_home"
    hermes_home.mkdir()
                                                                           
    (hermes_home / "config.yaml").write_text("- bad shape\n", encoding="utf-8")

    monkeypatch.setenv("HERMES_HOME", str(hermes_home))
    monkeypatch.delenv("ZEN_INSTALL_MCPS_DRY_RUN", raising=False)

    class FakeResult:
        returncode = 0
        stderr = ""

    with patch.object(mod, "_hermes_on_path", return_value=True):
        with patch.object(mod.subprocess, "run", return_value=FakeResult()):
            result = handle_install_mcps("")
    assert "Some entries failed" in result
    assert "Manual install" in result
