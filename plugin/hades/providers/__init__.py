# SPDX-License-Identifier: MIT
"""Zen-swarm provider profile for Hermes."""

from __future__ import annotations

import importlib.util
import os
import sys
from pathlib import Path

from providers import register_provider
from providers.base import ProviderProfile


def _load_default_base_url() -> str:
    """Return the canonical default daemon base URL.

    Reviewer M1: the literal lives in ``plugin/hades/_constants.py``
    as a single source of truth. This module is loaded by Hermes via
    ``importlib.util.spec_from_file_location`` with
    ``submodule_search_locations=[<providers-dir>]`` (see
    ``providers/__init__.py:_import_plugin_dir`` in Hermes itself), which
    means relative imports above the providers/ directory (``from
    .._constants``) do NOT resolve in production — there is no parent
    package wrapping providers/ in that load mode. The test environment
    DOES wrap it under ``hermes_plugins.hades.providers`` (see
    plugin's ``conftest.py``), where relative imports would work.

    To keep the literal in lock-step across both load modes, we resolve
    ``_constants.py`` by absolute path from the providers/ dir and
    exec-load it under a stable module name. The constant is then read
    from that module. Failure modes:

      - constants module missing on disk (impossible in a correct install
        — _constants.py ships in the plugin tree): fall back to the
        documented loopback URL so import does not abort and the operator
        can still drive the plugin.
    """
    constants_path = Path(__file__).resolve().parent.parent / "_constants.py"
    # Stable module name keeps repeated loads idempotent (sys.modules
    # cache hit on subsequent imports).
    module_name = "_zen_swarm_plugin_constants"
    cached = sys.modules.get(module_name)
    if cached is not None:
        return cached.DEFAULT_DAEMON_BASE_URL
    if not constants_path.is_file():
        # Defensive fallback — operator install is corrupt; documented
        # default keeps the plugin operational while doctor surfaces the
        # missing file. Matches the value in _constants.py exactly.
        return "http://127.0.0.1:8080"
    spec = importlib.util.spec_from_file_location(module_name, constants_path)
    if spec is None or spec.loader is None:  # pragma: no cover - defensive
        return "http://127.0.0.1:8080"
    mod = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = mod
    spec.loader.exec_module(mod)
    return mod.DEFAULT_DAEMON_BASE_URL


# Default base_url — operator override via env var ``ZEN_SWARM_BASE_URL``.
# Canonical literal lives in ``plugin/hades/_constants.py`` (reviewer
# M1: single source of truth across providers + install_mcps consumers).
# Loaded via absolute path because Hermes loads this module standalone
# (no parent package); see ``_load_default_base_url`` for the rationale.
_DEFAULT_BASE_URL = _load_default_base_url()


def _resolve_base_url() -> str:
    """Resolve the daemon base URL from env, with safe default.

    Read at MODULE IMPORT TIME — Hermes imports each provider plugin once
    per session, so subsequent env mutations don't propagate. Operators
    needing a runtime change must restart Hermes.
    """
    return os.environ.get("ZEN_SWARM_BASE_URL", _DEFAULT_BASE_URL)


zen_swarm = ProviderProfile(
    name="zen-swarm",
    aliases=("zen", "zenswarm"),
    api_mode="anthropic_messages",
    env_vars=("ZEN_SWARM_API_KEY",),
    base_url=_resolve_base_url(),
    display_name="zen-swarm",
    description="zen-swarm-ctld single-egress proxy (Anthropic Messages format)",
    auth_type="api_key",
)

register_provider(zen_swarm)
