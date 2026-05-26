# SPDX-License-Identifier: MIT
"""Slash command handler for /hades:install-mcps."""

from __future__ import annotations

import datetime as _dt
import logging
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

import yaml

from .._constants import DEFAULT_DAEMON_BASE_URL

logger = logging.getLogger(__name__)

                                                                                
SNIPPET_PATH = Path(__file__).resolve().parent.parent / "hermes-config-snippet.yaml"

                                                                        
PROVIDER_PLUGIN_SRC = Path(__file__).resolve().parent.parent / "providers"

                                                                        
                                                                       
                                                                    
                                                                            
PROVIDER_NAME = "zen-swarm"
                                                                        
                                                                   
DEFAULT_BASE_URL = DEFAULT_DAEMON_BASE_URL


def load_snippet() -> dict:
    """Parse hermes-config-snippet.yaml and return the structure."""
    body = SNIPPET_PATH.read_text(encoding="utf-8")
    parsed = yaml.safe_load(body) or {}
    if not isinstance(parsed, dict):
        return {}
    return parsed


def _hermes_on_path() -> bool:
    return shutil.which("hermes") is not None


def _format_manual_install_block(entries: dict[str, dict]) -> str:
    """Generate operator-facing manual install commands."""
    lines = ["### Manual install (run these in your terminal)", ""]
    lines.append("```bash")
    for name, entry in entries.items():
        cmd_parts = [entry.get("command", name)] + (entry.get("args") or [])
                                                                            
                                                                         
                                                                        
                                                  
        env_block = ""
        if entry.get("env"):
            env_block = "".join(f" --env {k}={v}" for k, v in entry["env"].items())
                                                                              
                                                                            
                                                         
        lines.append(
            f"hermes mcp add {name} --command {' '.join(cmd_parts)!r}{env_block}"
        )
    lines.append("```")
    lines.append("")
    lines.append("Or merge the snippet directly:")
    lines.append("")
    lines.append("```bash")
    lines.append(f"cat {SNIPPET_PATH} >> ~/.hermes/config.yaml")
    lines.append("hermes plugins refresh  # or restart hermes")
    lines.append("```")
    return "\n".join(lines)


def _run_hermes_mcp_add(name: str, entry: dict) -> tuple[bool, str]:
    """Invoke `hermes mcp add` for a single entry. Returns (success, output_msg).

    Idempotent: if the MCP already exists, treat the failure as success.
    """
    cmd = entry.get("command", name)
    args = entry.get("args") or []
    env = entry.get("env") or {}
    invocation = ["hermes", "mcp", "add", name, "--command", " ".join([cmd] + list(args))]
    for k, v in env.items():
        invocation.extend(["--env", f"{k}={v}"])
    try:
        result = subprocess.run(
            invocation,
            capture_output=True,
            text=True,
            timeout=10.0,
            check=False,
        )
    except (subprocess.SubprocessError, OSError) as exc:
        return False, f"{name}: invocation failed: {exc}"
    if result.returncode == 0:
        return True, f"{name}: added"
                                                                    
                                                                   
                                                                   
                                                                    
                                                                      
                                                                            
    stderr_lc = (result.stderr or "").lower()
    if "already exists" in stderr_lc or "already registered" in stderr_lc:
        return True, f"{name}: already present"
    return (
        False,
        f"{name}: hermes mcp add exited {result.returncode}: {result.stderr.strip()}",
    )


                                                                             
                                                                              
                                                                             


def _resolve_hermes_home() -> Path:
    """Return ``$HERMES_HOME`` or ``~/.hermes`` if unset.

    Mirrors ``hermes_constants.get_hermes_home()`` but kept local so this
    module has zero hermes-internals coupling beyond the public CLI.
    Operators running with non-default profiles via the ``active_profile``
    sticky file are expected to export ``HERMES_HOME`` explicitly when
    invoking ``/hades:install-mcps`` (same expectation Hermes itself
    documents for subprocess spawners).
    """
    val = os.environ.get("HERMES_HOME", "").strip()
    if val:
        return Path(val)
    return Path.home() / ".hermes"


def _install_provider_plugin_symlink(hermes_home: Path) -> tuple[bool, str]:
    """Symlink the HADES provider plugin into Hermes' user plugins dir.

    Creates (or replaces) ``<hermes_home>/plugins/model-providers/zen-swarm
    -> <repo>/plugin/hades/providers``. Target name stays as ``zen-swarm``
    per spec §Q3 BORDERLINE (keychain/provider-name preservation; see the
    constant PROVIDER_NAME above). Refuses to clobber a real (non-symlink)
    directory at that path — operator-managed installs (e.g. ``make
    plugin-install`` copy mode) win.

    Returns ``(ok, message)``. Idempotent on success.

    Anchor: ``providers/__init__.py:_import_plugin_dir`` discovers user
    plugins at ``<hermes_home>/plugins/model-providers/<name>/``.
    """
    target_parent = hermes_home / "plugins" / "model-providers"
    target_parent.mkdir(parents=True, exist_ok=True)
    link = target_parent / PROVIDER_NAME

    canonical_src = PROVIDER_PLUGIN_SRC.resolve()

    if link.is_symlink():
        try:
            current = link.resolve()
        except OSError:
            current = None
        if current == canonical_src:
            return True, f"provider plugin already linked: {link} -> {canonical_src}"
                                                                     
                               
        link.unlink()
        try:
            link.symlink_to(canonical_src)
        except OSError as exc:
                                                                 
                                                                    
                                                                  
                                                                       
            return (
                False,
                f"provider plugin symlink creation failed: {exc} "
                f"(target {link} -> {canonical_src}; stale link was already removed)",
            )
        return True, f"provider plugin link refreshed: {link} -> {canonical_src}"

    if link.exists():
                                                                    
                                                                        
                                                               
        return (
            False,
            f"provider plugin path exists but is NOT a symlink (real directory): {link}. "
            "Remove it manually if you want /hades:install-mcps to manage it.",
        )

    try:
        link.symlink_to(canonical_src)
    except OSError as exc:
                                                                       
                                                                   
        return (
            False,
            f"provider plugin symlink creation failed: {exc} "
            f"(target {link} -> {canonical_src})",
        )
    return True, f"provider plugin linked: {link} -> {canonical_src}"


def _update_hermes_config_provider(hermes_home: Path, base_url: str) -> tuple[bool, str]:
    """Write ``model.provider: zen-swarm`` (BORDERLINE stays) + ``model.base_url`` into
    ``<hermes_home>/config.yaml``.

    Behavior:
      - Creates ``config.yaml`` if absent (no backup needed — nothing to
        save).
      - If config.yaml exists, copies its current contents to
        ``config.yaml.bak.<YYYYMMDD-HHMMSS>`` BEFORE mutating, so operator
        can recover from a bad rewrite.
      - Atomic write: serialize the updated config to a tempfile in the
        same directory, then ``os.replace`` it onto the target. Avoids
        partial-write corruption if the process is killed mid-write.
      - Preserves all other keys (plugins.enabled, theme, model.default,
        model.max_tokens, mcp_servers, etc.).
      - Idempotent: re-running with the same base_url leaves the file
        unchanged (content-wise).

    Returns ``(ok, message)``.
    """
    config_path = hermes_home / "config.yaml"

    pre_existing = config_path.is_file()
    if pre_existing:
        try:
            original_text = config_path.read_text(encoding="utf-8")
            parsed = yaml.safe_load(original_text) or {}
        except (OSError, yaml.YAMLError) as exc:
            return False, f"failed to read existing config.yaml: {exc}"
        if not isinstance(parsed, dict):
            return (
                False,
                f"existing config.yaml at {config_path} parses as "
                f"{type(parsed).__name__}, expected dict",
            )
    else:
        original_text = ""
        parsed = {}

                                                                          
                                          
    model_cfg = parsed.get("model") if isinstance(parsed.get("model"), dict) else {}
    model_cfg = dict(model_cfg)                                          
                                                                      
                                                                        
                                                                     
    model_cfg.pop("api_key", None)
    model_cfg.pop("api_mode", None)
    model_cfg["provider"] = PROVIDER_NAME
    model_cfg["base_url"] = base_url.rstrip("/")
    parsed["model"] = model_cfg

    new_text = yaml.safe_dump(parsed, sort_keys=False)

                                                                               
    if pre_existing and new_text == original_text:
        return True, f"config.yaml already wired for {PROVIDER_NAME}; no changes"

                                            
    if pre_existing:
                                                                  
                                                                   
                                                                       
                                                                     
                                                                       
                                                                 
                            
        ts = _dt.datetime.now().strftime("%Y%m%d-%H%M%S")
        backup = config_path.with_suffix(f".yaml.bak.{ts}")
        try:
                                                             
            with open(backup, "x", encoding="utf-8") as fh:
                fh.write(original_text)
        except FileExistsError:
                                                                   
            ts_micro = _dt.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
            backup = config_path.with_suffix(f".yaml.bak.{ts_micro}")
            try:
                with open(backup, "x", encoding="utf-8") as fh:
                    fh.write(original_text)
            except OSError as exc:
                return False, f"failed to write backup {backup}: {exc}"
        except OSError as exc:
            return False, f"failed to write backup {backup}: {exc}"

                                                      
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            dir=str(hermes_home),
            prefix=".config.yaml.",
            suffix=".tmp",
            delete=False,
        ) as tf:
            tf.write(new_text)
            tmp_path = Path(tf.name)
        os.replace(tmp_path, config_path)
    except OSError as exc:
        return False, f"failed to write config.yaml: {exc}"

    action = "updated" if pre_existing else "created"
    return True, f"config.yaml {action}: {config_path} (model.provider={PROVIDER_NAME})"


def handle_install_mcps(raw_args: str) -> str | None:
    """Handler for /hades:install-mcps.

    Args:
        raw_args: trailing text (currently unused; reserved for future flags
            like '--dry-run' or '--force').

    Returns:
        Markdown summary of install actions / manual instructions.
    """
    try:
        snippet = load_snippet()
    except (OSError, yaml.YAMLError) as exc:
        return f"## /hades:install-mcps\n\nERROR: failed to load snippet at {SNIPPET_PATH}: {exc}"

    servers = snippet.get("mcp_servers") or {}
    if not isinstance(servers, dict) or not servers:
        return f"## /hades:install-mcps\n\nERROR: snippet at {SNIPPET_PATH} has no mcp_servers entries."

    dry_run = bool(os.environ.get("ZEN_INSTALL_MCPS_DRY_RUN")) or "--dry-run" in (
        raw_args or ""
    )

    out = ["## /hades:install-mcps", ""]
    out.append(f"Snippet: `{SNIPPET_PATH}`")
    out.append(f"MCPs declared: {', '.join(sorted(servers.keys()))}")
    out.append("")

    if not _hermes_on_path():
        out.append("**`hermes` binary not on PATH.** Install Hermes first:")
        out.append("")
        out.append("```bash")
        out.append("brew install hermes-agent")
        out.append("```")
        out.append("")
        out.append(_format_manual_install_block(servers))
        return "\n".join(out)

    if dry_run:
        out.append("_(dry-run; no changes applied)_")
        out.append("")
        out.append(_format_manual_install_block(servers))
        return "\n".join(out)

                                                 
    out.append("### MCP install results")
    out.append("")
    any_failed = False
    for name in sorted(servers.keys()):
        ok, msg = _run_hermes_mcp_add(name, servers[name])
        prefix = "OK   " if ok else "FAIL "
        out.append(f"- `{prefix}` {msg}")
        if not ok:
            any_failed = True
    out.append("")

                                                                          
                                                                           
                                                                         
    hermes_home = _resolve_hermes_home()
    base_url = os.environ.get("ZEN_SWARM_BASE_URL", DEFAULT_BASE_URL)
    out.append("### Provider plugin wiring")
    out.append("")
    out.append(f"Hermes home: `{hermes_home}`")
    out.append(f"Daemon base_url: `{base_url}`")
    out.append("")

    link_ok, link_msg = _install_provider_plugin_symlink(hermes_home)
    prefix = "OK   " if link_ok else "FAIL "
    out.append(f"- `{prefix}` symlink: {link_msg}")
    if not link_ok:
        any_failed = True

    cfg_ok, cfg_msg = _update_hermes_config_provider(hermes_home, base_url)
    prefix = "OK   " if cfg_ok else "FAIL "
    out.append(f"- `{prefix}` config.yaml: {cfg_msg}")
    if not cfg_ok:
        any_failed = True

    out.append("")
    if any_failed:
        out.append("Some entries failed. Fall back to manual install:")
        out.append("")
        out.append(_format_manual_install_block(servers))
    else:
        out.append(
            f"All MCPs + provider profile installed. Restart Hermes (or "
            f"``hermes plugins refresh``) so the {PROVIDER_NAME} profile loads."
        )
        out.append("")
        out.append("Verify:")
        out.append("")
        out.append("```bash")
        out.append("hermes mcp list")
        out.append("hermes mcp test zen-mcp-research")
        out.append("# caronte code-graph is in-process — no separate MCP entry")
        out.append("# After restart: ``hermes model`` should list zen-swarm")
        out.append("```")
    return "\n".join(out)
