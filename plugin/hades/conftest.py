# SPDX-License-Identifier: MIT
"""Pytest configuration for the HADES Hermes plugin test suite."""

from __future__ import annotations

import importlib
import importlib.util
import sys
import types
from pathlib import Path

import pytest
from _pytest import nodes
from _pytest.main import Dir

_PLUGIN_DIR = Path(__file__).resolve().parent
_NS_PARENT = "hermes_plugins"


def _preload_hermes_providers_registry() -> None:
    """Pre-import Hermes' real ``providers`` package so it occupies
    ``sys.modules['providers']`` before any local sibling shadows it.

    Why: pytest auto-adds the plugin source directory
    (``plugin/hades/``) to ``sys.path`` for test discovery. That puts
    the plugin's own ``providers/`` sub-directory in the import search
    path under the same dotted name as Hermes' real ``providers`` package.
    Without this pre-load, ``from providers import register_provider``
    inside ``plugin/hades/providers/__init__.py`` would resolve to
    itself — circular import.

    In production this is NOT a problem because Hermes loads each user
    plugin via ``importlib.util.spec_from_file_location`` with an
    arbitrary module name (``_hermes_user_provider_hades``); the
    plugin's own dir is never added to ``sys.path``. The bare
    ``from providers import register_provider`` inside the plugin then
    resolves to Hermes' real ``providers`` via the standard site-packages
    lookup.

    So this pre-load is a TEST-ENVIRONMENT-ONLY compensation. Once
    Hermes' real package is in ``sys.modules['providers']``, Python's
    import machinery returns the cached module on subsequent lookups
    instead of re-executing whatever lives at ``plugin/hades/providers/
    __init__.py`` — exactly matching production semantics.
    """
    if "providers" in sys.modules:
        return

                                                                       
                                                                        
                                                                      
                                                                
                                                                        
                                                     
    import importlib.util as _util

                                                                
                                                                         
                                                                               
                                                                        
    hermes_providers_init: Path | None = None
    for entry in sys.path:
        if not entry:
            continue
        candidate = Path(entry) / "providers" / "__init__.py"
        if not candidate.is_file():
            continue
                                                     
        try:
            if candidate.resolve().is_relative_to(_PLUGIN_DIR.resolve()):
                continue
        except (ValueError, OSError):
            pass
        text = candidate.read_text(encoding="utf-8", errors="ignore")
        if "register_provider" in text and "ProviderProfile" in text:
            hermes_providers_init = candidate
            break

    if hermes_providers_init is None:
                                                                         
                                                                        
                             
        return

    spec = _util.spec_from_file_location(
        "providers",
        hermes_providers_init,
        submodule_search_locations=[str(hermes_providers_init.parent)],
    )
    if spec is None or spec.loader is None:
        return
    mod = _util.module_from_spec(spec)
    sys.modules["providers"] = mod
    spec.loader.exec_module(mod)


def _preload_plugin_as_hermes_does() -> None:
    """Mirror ``hermes_cli/plugins.py:1030-1065`` to register the plugin as
    ``hermes_plugins.hades`` before pytest collection runs.
    """
    module_name = f"{_NS_PARENT}.hades"
    if module_name in sys.modules:
        return

    if _NS_PARENT not in sys.modules:
        ns_pkg = types.ModuleType(_NS_PARENT)
        ns_pkg.__path__ = []
        ns_pkg.__package__ = _NS_PARENT
        sys.modules[_NS_PARENT] = ns_pkg

    spec = importlib.util.spec_from_file_location(
        module_name,
        _PLUGIN_DIR / "__init__.py",
        submodule_search_locations=[str(_PLUGIN_DIR)],
    )
    if spec is None or spec.loader is None:
        return

    mod = importlib.util.module_from_spec(spec)
    mod.__package__ = module_name
    mod.__path__ = [str(_PLUGIN_DIR)]  # type: ignore[attr-defined]
    sys.modules[module_name] = mod
    spec.loader.exec_module(mod)


def _locate_hermes_site_packages() -> Path | None:
    """Best-effort discovery of the installed ``hermes-agent`` site-packages.

    WHY: the tests below import Hermes' real ``providers`` package. Hermes
    ships it inside its own Homebrew-formula virtualenv (``hermes-agent``),
    NOT in this plugin's test venv — so a standalone ``pytest`` run raises
    ``ModuleNotFoundError: No module named 'providers'`` unless the caller
    exports ``PYTHONPATH`` by hand. We remove that manual step: resolve
    whatever ``hermes`` is on ``PATH`` and return its ``site-packages`` so
    the pre-load below finds the real ``providers`` automatically.

    Version-agnostic: resolve the ``hermes`` symlink, then glob
    ``lib/python*/site-packages`` under the install prefix and select the
    dir that actually contains ``providers/__init__.py``. A Homebrew
    version bump (``2026.5.7`` -> ``2026.5.8``) needs no edit here.

    INVARIANT: never raises; returns ``None`` when ``hermes`` is absent or
    its ``providers`` package is not found. An explicitly-set ``PYTHONPATH``
    still wins because it is already on ``sys.path`` before this fallback
    runs.
    """
    import shutil

    hermes = shutil.which("hermes")
    if not hermes:
        return None
    resolved = Path(hermes).resolve()
    for prefix in resolved.parents:
        for site_packages in sorted(prefix.glob("lib/python*/site-packages")):
            if (site_packages / "providers" / "__init__.py").is_file():
                return site_packages
    return None


                                                                         
                                                      
 
                                                                          
                                                                     
                                                                            
                                                                            
_hermes_site_packages = _locate_hermes_site_packages()
if _hermes_site_packages is not None and str(_hermes_site_packages) not in sys.path:
    sys.path.insert(0, str(_hermes_site_packages))

# Order matters: Hermes' real ``providers`` MUST be in sys.modules BEFORE
                                                                         
_preload_hermes_providers_registry()
_preload_plugin_as_hermes_does()


@pytest.hookimpl(tryfirst=True)
def pytest_collect_directory(
    path: Path, parent: nodes.Collector
) -> nodes.Collector | None:
    """Suppress Package node creation for the plugin root.

    Pytest's default ``pytest_collect_directory`` returns a ``Package`` node
    for any directory containing ``__init__.py``. Its ``setup()`` then imports
    that ``__init__.py`` as a test-package init — which fails for our plugin
    (relative imports + hyphenated dir name).

    Returning ``None`` from this hook for the plugin root delegates to pytest's
    default Directory handling (no Package node), letting pytest still descend
    into ``tests/`` while leaving ``__init__.py`` to be loaded only by the
    Hermes loader-mirror in ``_preload_plugin_as_hermes_does`` above.
    """
    if path == _PLUGIN_DIR:
                                                                            
                                                                      
        return Dir.from_parent(parent, path=path)
    return None                                                         
