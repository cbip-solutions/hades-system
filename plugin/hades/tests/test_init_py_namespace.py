# SPDX-License-Identifier: MIT
"""Compliance sentinel for plugin/hades/__init__.py slash command namespace."""

from __future__ import annotations

import ast
from pathlib import Path

                                                                                    
                                                                  
                                                                  
_INIT_PY_PATH = Path(__file__).resolve().parent.parent / "__init__.py"

                                                                    
                                                                           
                                                                            
                                                                           
EXPECTED_HADES_COMMANDS: frozenset[str] = frozenset(
    {
        "hades:handoff",
        "hades:install-mcps",
        "hades:start",
        "hades:brainstorm",
        "hades:execute-plan",
        "hades:write-plan",
        "hades:amendment-ack",
        "hades:amendment-deny",
        "hades:amendment-list",
        "hades:amendment-show",
        "hades:doctrine",
        "hades:audit-impact",
        "hades:doctrine-drift-check",
        "hades:impact-pre-merge",
        "hades:knowledge-promote",
        "hades:knowledge-query",
        "hades:full",
        "hades:voice",
        "hades:openspec-apply",
        "hades:openspec-archive",
        "hades:openspec-propose",
        "hades:openspec-resume",
                                                        
        "hades:status",
                                        
        "hades:dashboard",
        "hades:panel",
    }
)


def _collect_register_command_names(source_path: Path) -> list[str]:
    """Walk the AST of ``source_path`` and return every first-positional-arg
    string passed to a ``ctx.register_command(...)`` call, in source order.

    Robust to multi-line call expressions, keyword arguments (returns the
    first positional regardless of trailing kwargs), and commented-out code
    (commented lines are stripped before AST parsing per Python tokenizer).

    Raises:
        FileNotFoundError: if ``source_path`` does not exist.
        SyntaxError: if ``source_path`` is not valid Python (Phase A
            regression signal — halt and investigate).
        AssertionError: if a register_command call's first positional arg
            is NOT a string constant (defensive — current code uses string
            literals exclusively, so this would signal a refactor we did
            not anticipate).
    """
    source = source_path.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(source_path))
    names: list[str] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        fn = node.func
                                                                        
                                                                           
                                       
        if not isinstance(fn, ast.Attribute):
            continue
        if fn.attr != "register_command":
            continue
        if not isinstance(fn.value, ast.Name) or fn.value.id != "ctx":
            continue
        # First positional arg MUST be a string constant.
        assert node.args, (
            f"ctx.register_command() call at line {node.lineno} has no positional args"
        )
        first = node.args[0]
        assert isinstance(first, ast.Constant) and isinstance(first.value, str), (
            f"ctx.register_command() at line {node.lineno} first arg is not a string"
            f" constant; got {type(first).__name__} (refactor not anticipated by Phase B)"
        )
        names.append(first.value)
    return names


def test_register_command_count_is_25() -> None:
    """Assertion 1: exactly 25 ctx.register_command(...) calls.

    Plan 18b B baseline was 22 (Phase H' 3 + Plan 12 B-3..B-8 19). Plan 18c
    adds 3: Phase C hades:status + Phase D hades:dashboard + hades:panel.
    Regression guard: if this fires, something accidentally deleted or
    duplicated a call.
    """
    names = _collect_register_command_names(_INIT_PY_PATH)
    assert len(names) == 25, (
        f"expected 25 ctx.register_command(...) calls "
        f"(Plan 18b B's 22 + Plan 18c C's 1 + Plan 18c D's 2); got {len(names)}\n"
        f"names: {sorted(names)}"
    )


def test_register_command_includes_dashboard_and_panel() -> None:
    """Plan 18c Phase D adds /hades:dashboard + /hades:panel (Task D-7)."""
    names = set(_collect_register_command_names(_INIT_PY_PATH))
    assert "hades:dashboard" in names, (
        f"expected 'hades:dashboard' registered; got {sorted(names)}"
    )
    assert "hades:panel" in names, (
        f"expected 'hades:panel' registered; got {sorted(names)}"
    )


def test_register_command_names_match_expected_set() -> None:
    """Assertion 2: the 25 names equal the post-rebrand + Plan 18c expected set.

    Pre-B-3+B-4: this FAILS because 3 names start with `zen-swarm:`
    and 19 are bare (no namespace).
    Post-B-3+B-4: this PASSES; the 22 names all match `hades:<slug>`.
    Post-Plan-18c: 25 names (hades:status + hades:dashboard + hades:panel added).
    """
    actual = sorted(_collect_register_command_names(_INIT_PY_PATH))
    expected = sorted(EXPECTED_HADES_COMMANDS)
    assert actual == expected, (
        "register_command name set does not match expected.\n"
        f"missing (expected but not in actual): "
        f"{sorted(EXPECTED_HADES_COMMANDS - set(actual))}\n"
        f"extra (in actual but not expected): "
        f"{sorted(set(actual) - EXPECTED_HADES_COMMANDS)}"
    )


def test_every_command_starts_with_hades_prefix() -> None:
    """Assertion 3: every registered command name starts with `hades:`.

    This is the core post-rebrand contract. Pre-rebrand FAILS for all
    22 names; post-rebrand PASSES for all 22.
    """
    names = _collect_register_command_names(_INIT_PY_PATH)
    violations = [n for n in names if not n.startswith("hades:")]
    assert not violations, (
        f"{len(violations)} register_command name(s) do NOT start with `hades:` prefix.\n"
        f"violations: {sorted(violations)}\n"
        f"(post-Plan-18b spec §Q4: `/hades:*` is the only valid namespace)"
    )


def test_no_command_uses_legacy_zen_swarm_prefix() -> None:
    """Assertion 4: no registered command uses the legacy `zen-swarm:` prefix.

    Spec §Q4 hard cutover: `/zen-swarm:*` is removed; invocations get
    Hermes' built-in command-not-found (Plan 18c ships polished
    did-you-mean hint).
    """
    names = _collect_register_command_names(_INIT_PY_PATH)
    violations = [n for n in names if n.startswith("zen-swarm:")]
    assert not violations, (
        f"{len(violations)} register_command name(s) still use legacy `zen-swarm:` prefix.\n"
        f"violations: {sorted(violations)}\n"
        f"(rebrand them to `hades:<name>` per Phase B-3)"
    )


def test_no_command_is_bare() -> None:
    """Assertion 5: no registered command is bare (lacks `:` separator).

    Plan 12 era registered 19 commands without any namespace prefix
    (`brainstorm`, `write-plan`, etc.). Spec §Q4 requires a single
    consistent namespace; bare names are not allowed post-Plan-18.
    """
    names = _collect_register_command_names(_INIT_PY_PATH)
    violations = [n for n in names if ":" not in n]
    assert not violations, (
        f"{len(violations)} register_command name(s) are bare (no namespace).\n"
        f"violations: {sorted(violations)}\n"
        f"(promote each to `hades:<name>` per Phase B-4)"
    )


                                                                           


_PLUGIN_HADES_ROOT = Path(__file__).resolve().parent.parent                 


def _all_python_files_in_plugin_hades() -> list[Path]:
    """Return every .py file under plugin/hades/ (used by sentinel-2 sweeps).

    Excludes:
    - The plugin's tests/ tree (tests legitimately mention 'zen-swarm:'
      strings in test fixtures, expected-violation patterns, etc.).
    - Compiled artifacts (__pycache__).
    """
    out: list[Path] = []
    for path in _PLUGIN_HADES_ROOT.rglob("*.py"):
                                  
        if "tests" in path.parts:
            continue
                                    
        if "__pycache__" in path.parts:
            continue
        out.append(path)
    return out


def test_no_zen_swarm_namespace_call_in_plugin_hades_tree() -> None:
    """Sentinel-2 sweep: no `ctx.register_command("zen-swarm:...")` calls
    remain anywhere in `plugin/hades/` (excluding tests/).

    This is the per-phase regression guard for Plan 18b Phase B. Phase J's
    full inv-zen-219 AST grep is the comprehensive coverage across all
    post-rebrand surfaces (`internal/cli/`, `internal/tui/`, etc.); this
    sentinel-2 sweep is the Phase B-specific guardrail.
    """
    violations: list[tuple[Path, int, str]] = []
    for path in _all_python_files_in_plugin_hades():
        text = path.read_text(encoding="utf-8")
        for line_num, line in enumerate(text.splitlines(), start=1):
                                                                        
                                                                        
                                                                       
            if "ctx.register_command(" in line and '"zen-swarm:' in line:
                violations.append((path, line_num, line.strip()))

    assert not violations, (
        f'{len(violations)} ctx.register_command("zen-swarm:...") call(s) '
        f"remain in plugin/hades/ (excluding tests/).\n"
        + "\n".join(
            f"  {p.relative_to(_PLUGIN_HADES_ROOT)}:{ln}: {src}"
            for p, ln, src in violations
        )
    )


def test_no_bare_register_command_call_in_plugin_hades_tree() -> None:
    """Sentinel-2 sweep: no `ctx.register_command("<bare-name>", ...)` calls
    (without `hades:` prefix) remain anywhere in `plugin/hades/` (excluding
    tests/).

    The expected pattern post-rebrand: every register_command call's first
    positional arg is `"hades:<name>"`. This test walks the tree, finds
    every register_command call, and verifies the FIRST positional arg
    starts with `hades:`.

    Excludes: test fixtures (already filtered via _all_python_files_in_plugin_hades).
    """
    violations: list[tuple[Path, int, str]] = []
    for path in _all_python_files_in_plugin_hades():
        text = path.read_text(encoding="utf-8")
                                                                
        try:
            tree = ast.parse(text, filename=str(path))
        except SyntaxError:
                                                                               
                                                                       
            continue
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            fn = node.func
            if not isinstance(fn, ast.Attribute):
                continue
            if fn.attr != "register_command":
                continue
            if not isinstance(fn.value, ast.Name) or fn.value.id != "ctx":
                continue
            if not node.args:
                continue
            first = node.args[0]
            if not (isinstance(first, ast.Constant) and isinstance(first.value, str)):
                continue
            name = first.value
            if not name.startswith("hades:"):
                src_line = text.splitlines()[node.lineno - 1].strip()
                violations.append((path, node.lineno, src_line + "  # name=" + name))

    assert not violations, (
        f"{len(violations)} ctx.register_command(...) call(s) in plugin/hades/ "
        f"do NOT start with `hades:` prefix.\n"
        + "\n".join(
            f"  {p.relative_to(_PLUGIN_HADES_ROOT)}:{ln}: {src}"
            for p, ln, src in violations
        )
    )
