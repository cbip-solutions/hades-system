# SPDX-License-Identifier: MIT
"""Parent conftest for the plugin/ directory tree.

PURPOSE
-------
Suppresses pytest ``Package`` node creation for individual Hermes plugin
directories (e.g. ``plugin/hades/``). Hermes plugins have ``__init__.py``
files that use relative imports and are designed to be loaded by Hermes' own
package loader (``hermes_cli/plugins.py:1030-1065``) — never by pytest's
default ``Package.setup``.

This conftest sits ABOVE the plugin dir so pytest loads it before deciding
whether each plugin subdir should be a Package or a plain Dir.

We also pre-load the hades plugin via Hermes' loader pattern here (not just
in the child conftest) because pytest imports child conftests as Python
modules — ``hades.conftest`` — which triggers ``hades/__init__.py`` BEFORE
the child conftest's module-level code runs. The full pre-load here ensures
``hermes_plugins.hades`` is populated before that import chain executes.

The child conftest (``plugin/hades/conftest.py``) will early-exit from
``_preload_plugin_as_hermes_does()`` on the ``if module_name in sys.modules``
guard, so the pre-load is idempotent.

We delegate to the plugin's own ``conftest.py`` (at
``plugin/<name>/conftest.py``) for the actual Hermes-style pre-load.
"""

from __future__ import annotations

import importlib
import importlib.util
import sys
import types
from pathlib import Path

import pytest
from _pytest import nodes
from _pytest.main import Dir

_PLUGIN_PARENT_DIR = Path(__file__).resolve().parent

                                                                          
                                                                             
                                                                         
                                                                        
                                                    
_NS_PARENT = "hermes_plugins"
_HADES_DIR = _PLUGIN_PARENT_DIR / "hades"
if _HADES_DIR.is_dir():
    _module_name = f"{_NS_PARENT}.hades"
    if _module_name not in sys.modules:
        if _NS_PARENT not in sys.modules:
            _ns_pkg = types.ModuleType(_NS_PARENT)
            _ns_pkg.__path__ = []  # type: ignore[assignment]
            _ns_pkg.__package__ = _NS_PARENT
            sys.modules[_NS_PARENT] = _ns_pkg
        _spec = importlib.util.spec_from_file_location(
            _module_name,
            _HADES_DIR / "__init__.py",
            submodule_search_locations=[str(_HADES_DIR)],
        )
        if _spec is not None and _spec.loader is not None:
            _mod = importlib.util.module_from_spec(_spec)
            _mod.__package__ = _module_name
            _mod.__path__ = [str(_HADES_DIR)]  # type: ignore[attr-defined]
            sys.modules[_module_name] = _mod
            try:
                _spec.loader.exec_module(_mod)
            except Exception:
                                                                         
                                                                           
                                              
                sys.modules.pop(_module_name, None)
                raise


@pytest.hookimpl(tryfirst=True)
def pytest_collect_directory(
    path: Path, parent: nodes.Collector
) -> nodes.Collector | None:
    """Return a plain Dir for any Hermes plugin dir under ``plugin/``.

    A Hermes plugin dir is a direct child of ``plugin/`` containing
    ``__init__.py`` (Hermes' required entry-point file). By returning
    ``Dir.from_parent`` instead of letting pytest default to ``Package``,
    we prevent pytest's ``Package.setup`` from trying to import the
    plugin's ``__init__.py`` as a test-package init (which would fail
    due to relative imports).

    Pytest still descends into the plugin's subdirectories so tests under
    e.g. ``plugin/hades/tests/`` are discovered normally. The plugin
    itself is pre-loaded by the plugin's own ``conftest.py`` via Hermes'
    loader-mirror.
    """
    if path.parent == _PLUGIN_PARENT_DIR and (path / "__init__.py").is_file():
        return Dir.from_parent(parent, path=path)
    return None                           
