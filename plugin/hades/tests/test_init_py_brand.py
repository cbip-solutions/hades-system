# SPDX-License-Identifier: MIT
"""Sentinel brand-pass tests for surfaces missed by Phase E inv-zen-219 scope."""

from __future__ import annotations

import ast
import re
from pathlib import Path

_PLUGIN_ROOT = Path(__file__).resolve().parent.parent                 

                                                                             
         
                                                                             

_BRAND_RE = re.compile(r"zen[-_]swarm|ZenSwarm", flags=re.IGNORECASE)

                                                                       
_BORDERLINE_PATTERNS: list[str] = [
    "zen-swarm-ctld",
    "/tmp/zen-swarm.sock",
    "~/.config/zen-swarm/",
    "model-providers/zen-swarm",
                                                                           
                                                               
    "model-providers/zen-swarm",
                                    
    "zen-swarm-spike-hermes",
    "zen-swarm-plan-",
    "zen-swarm-design",
                                                                         
    "_zen_swarm_plugin_constants",
]


def _strip_borderline(text: str) -> str:
    """Remove spec §Q3 BORDERLINE occurrences before forbidden-brand scan."""
    out = text
    for pat in _BORDERLINE_PATTERNS:
        out = out.replace(pat, "<<BORDERLINE>>")
    return out


def _find_forbidden(text: str) -> list[str]:
    """Return list of non-borderline zen-swarm occurrences."""
    return _BRAND_RE.findall(_strip_borderline(text))


def _register_command_descriptions(source_path: Path) -> list[tuple[int, str]]:
    """Parse source_path AST and return (lineno, description_str) for every
    ``ctx.register_command(..., description="...")`` call.

    Returns list of (line_number, description_string) tuples.
    """
    source = source_path.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(source_path))
    results: list[tuple[int, str]] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        fn = node.func
        if not isinstance(fn, ast.Attribute):
            continue
        if fn.attr != "register_command":
            continue
        if not (isinstance(fn.value, ast.Name) and fn.value.id == "ctx"):
            continue
                                                
        for kw in node.keywords:
            if (
                kw.arg == "description"
                and isinstance(kw.value, ast.Constant)
                and isinstance(kw.value.value, str)
            ):
                results.append((node.lineno, kw.value.value))
    return results


                                                                             
                                            
                                                                             

_INIT_PY = _PLUGIN_ROOT / "__init__.py"


def test_f1_register_command_descriptions_no_zen_swarm() -> None:
    """F-1: No ``register_command(description=...)`` string contains a
    non-borderline zen-swarm occurrence.

    These descriptions surface in Hermes ``/help`` output — they are
    operator-visible brand strings and MUST be rebranded to HADES.
    """
    descs = _register_command_descriptions(_INIT_PY)
    assert descs, "No register_command(description=...) calls found — AST walker broken"

    violations: list[tuple[int, str, list[str]]] = []
    for lineno, desc_str in descs:
        forbidden = _find_forbidden(desc_str)
        if forbidden:
            violations.append((lineno, desc_str, forbidden))

    assert not violations, (
        f"{len(violations)} register_command description(s) contain forbidden "
        f"zen-swarm brand string(s):\n"
        + "\n".join(
            f"  line {ln}: {desc!r} → {matches}" for ln, desc, matches in violations
        )
    )


                                                                             
                                             
                                                                             


def test_f5_skills_list_uses_hades_skill_name() -> None:
    """F-5: The wiring summary docstring in ``__init__.py`` lists the
    meta-skill as ``hades`` (not ``zen-swarm``).

    Phase A renamed ``skills/zen-swarm/`` → ``skills/hades/``, so the skill
    is resolvable as ``hades:hades``, not ``hades:zen-swarm``.
    """
    source = _INIT_PY.read_text(encoding="utf-8")
                                                        
                                                                            
    for line in source.splitlines():
        if "13 skills" in line and "resolvable as" in line:
            forbidden = _find_forbidden(line)
            assert not forbidden, (
                f"Skills-list wiring summary line contains forbidden brand string: "
                f"{line!r} → {forbidden}"
            )
                                                        
            assert "hades, start" in line or "``hades``" in line or "hades," in line, (
                f"Skills-list line does not reference 'hades' as the first skill: {line!r}"
            )
            return
                                                                                    
    raise AssertionError(
        "Could not locate '13 skills ... resolvable as' line in __init__.py "
        "wiring summary; test_f5 needs update to match new docstring structure"
    )


                                                                             
                                   
                                                                             

_CONFTEST_PY = _PLUGIN_ROOT / "conftest.py"


def test_f6_conftest_docstring_uses_hades_brand() -> None:
    """F-6: ``conftest.py`` module docstring does not contain a forbidden
    zen-swarm brand occurrence (must say HADES, not zen-swarm).
    """
    source = _CONFTEST_PY.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(_CONFTEST_PY))
                                                                            
    docstring = ast.get_docstring(tree) or ""
    forbidden = _find_forbidden(docstring)
    assert not forbidden, (
        f"conftest.py module docstring contains forbidden brand string(s): "
        f"{forbidden}\nDocstring: {docstring!r}"
    )
    assert "HADES" in docstring, (
        f"conftest.py module docstring does not contain 'HADES'; "
        f"rebrand from zen-swarm: {docstring!r}"
    )


                                                                             
                                                         
                                                                             


def test_f7_conftest_path_comment_uses_hades_path() -> None:
    """F-7: The inline comment ``# plugin/zen-swarm/providers/ does NOT shadow``
    in conftest.py must reference the current path ``plugin/hades/providers/``
    (Phase A renamed plugin/zen-swarm/ → plugin/hades/).
    """
    source = _CONFTEST_PY.read_text(encoding="utf-8")
    for lineno, line in enumerate(source.splitlines(), start=1):
                                                          
        if "does NOT shadow" in line and "providers" in line:
                                                                    
            forbidden = _find_forbidden(line)
            assert not forbidden, (
                f"conftest.py:{lineno}: 'does NOT shadow' comment still references "
                f"pre-rename path; forbidden brand: {forbidden}\n  line: {line!r}"
            )
                                            
            assert "plugin/hades" in line, (
                f"conftest.py:{lineno}: comment does not reference plugin/hades/ "
                f"(post-rename path); line: {line!r}"
            )
            return
    raise AssertionError(
        "Could not locate 'does NOT shadow' comment in conftest.py; test_f7 needs update"
    )


                                                                             
                                             
                                                                             

_RENDERERS_INIT = _PLUGIN_ROOT / "renderers" / "__init__.py"


def test_f8_renderers_docstring_uses_hades_brand() -> None:
    """F-8: ``renderers/__init__.py`` module docstring says HADES, not
    zen-swarm.
    """
    source = _RENDERERS_INIT.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(_RENDERERS_INIT))
    docstring = ast.get_docstring(tree) or ""
    forbidden = _find_forbidden(docstring)
    assert not forbidden, (
        f"renderers/__init__.py docstring contains forbidden brand string(s): "
        f"{forbidden}\nDocstring: {docstring!r}"
    )
    assert "HADES" in docstring, (
        f"renderers/__init__.py docstring does not contain 'HADES'; "
        f"current docstring: {docstring!r}"
    )


                                                                             
                                                             
                                                                             

_PROVIDERS_INIT = _PLUGIN_ROOT / "providers" / "__init__.py"


def test_f9_providers_docstring_slash_command_ref() -> None:
    """F-9a: ``providers/__init__.py`` docstring slash command ref uses
    ``/hades:install-mcps``, not ``/zen-swarm:install-mcps``.
    """
    source = _PROVIDERS_INIT.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(_PROVIDERS_INIT))
    docstring = ast.get_docstring(tree) or ""
                                                             
    assert "/zen-swarm:install-mcps" not in docstring, (
        "providers/__init__.py docstring still references /zen-swarm:install-mcps; "
        "rebrand to /hades:install-mcps"
    )


def test_f9_providers_docstring_source_path_uses_hades() -> None:
    """F-9b: The symlink source path in ``providers/__init__.py`` docstring
    references ``plugin/hades/providers`` (post-rename), not
    ``plugin/zen-swarm/providers``.

    Note: the symlink TARGET path (``~/.hermes/plugins/model-providers/zen-swarm``)
    is BORDERLINE per spec §Q3 and stays as-is.
    """
    source = _PROVIDERS_INIT.read_text(encoding="utf-8")
                                                                              
                                                                            
    for lineno, line in enumerate(source.splitlines(), start=1):
        if "ln -s" in line and "plugin/" in line and "providers" in line:
                                              
                                                                                              
            assert "plugin/zen-swarm/providers" not in line, (
                f"providers/__init__.py:{lineno}: symlink source still references "
                f"pre-rename path 'plugin/zen-swarm/providers'; "
                f"should be 'plugin/hades/providers'\n  line: {line!r}"
            )
            return
                                                                  
    lines = source.splitlines()
    for lineno, line in enumerate(lines, start=1):
        if "plugin/zen-swarm/providers" in line and "ln" not in line:
                                                                                
            raise AssertionError(
                f"providers/__init__.py:{lineno}: still references pre-rename "
                f"'plugin/zen-swarm/providers'\n  line: {line!r}"
            )


def test_f9_providers_inline_comments_reference_hades_constants() -> None:
    """F-9c: Inline comments in ``providers/__init__.py`` referencing
    ``_constants.py`` use ``plugin/hades/_constants.py`` (post-rename), not
    ``plugin/zen-swarm/_constants.py``.
    """
    source = _PROVIDERS_INIT.read_text(encoding="utf-8")
    violations: list[tuple[int, str]] = []
    for lineno, line in enumerate(source.splitlines(), start=1):
                                                                             
        if "_constants.py" in line and "plugin/zen-swarm" in line:
            violations.append((lineno, line.strip()))
    assert not violations, (
        f"{len(violations)} inline comment(s) in providers/__init__.py still "
        f"reference 'plugin/zen-swarm/_constants.py' (pre-rename path):\n"
        + "\n".join(f"  line {ln}: {src}" for ln, src in violations)
    )


                                                                             
                                          
                                                                             

_SKINS_INIT = _PLUGIN_ROOT / "skins" / "__init__.py"


def test_f10_skins_docstring_uses_hades_plugin_name() -> None:
    """F-10: ``skins/__init__.py`` docstring references the plugin as
    ``HADES plugin's register(ctx)`` rather than ``zen-swarm plugin's``.
    """
    source = _SKINS_INIT.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(_SKINS_INIT))
    docstring = ast.get_docstring(tree) or ""
    forbidden = _find_forbidden(docstring)
    assert not forbidden, (
        f"skins/__init__.py docstring contains forbidden brand string(s): "
        f"{forbidden}\nDocstring: {docstring!r}"
    )
