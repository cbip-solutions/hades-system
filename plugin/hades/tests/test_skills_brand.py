# SPDX-License-Identifier: MIT
"""Sentinel brand-pass tests for HADES rebrand of the 13 SKILL.md files."""

from __future__ import annotations

import re
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SKILLS_ROOT = PLUGIN_ROOT / "skills"

                                                                
                                                                            
                                                                         
                                                                       
                                                      
SKILLS_UNDER_REBRAND: list[tuple[str, bool]] = [
    ("hades", True),
    ("start", False),
    ("handoff", False),
    ("brainstorm", False),
    ("write-plan", False),
    ("execute-plan", False),
    ("doctrine", False),
    ("doctrine-drift-check", False),
    ("amendment", False),
    ("impact-pre-merge", False),
    ("audit-impact", False),
    ("knowledge-query", False),
    ("knowledge-promote", False),
]

                                                           
                                                            
                                                      
                                                                               
HISTORICAL_ALLOWLIST: list[str] = [
    "github.com/cbip-solutions/hades-system",
    "(formerly zen-swarm)",
    "zen-swarm era",
    "Plan 12 era of zen-swarm",
    "zen-swarm-ctld",
    "/tmp/zen-swarm.sock",
    "~/.config/zen-swarm/",
    "-path-to-projects-hades-system",               
    "the-operator-projects-hades-system",
    "/zen-swarm/",             
                                                                  
    "cwd_starts_with: /path/to/projects/hades-system",
    "cwd_contains: zen-swarm",
    "cwd_contains: hades-system",
    "zen-swarm project",                                
                                                                              
    "zen-swarm-<topic>",
    "zen-swarm-plan-",
    "zen-swarm-design",
    "zen-swarm-gitnexus",
    "zen-swarm-spike",
                                                                              
                                                                          
    "mcp_zen-swarm_caronte_query",
    "mcp_zen-swarm_caronte_context",
    "zen-swarm-unified",
    "zen-swarm-surfaces",
    "zen-swarm-hades",
    ".zen-swarm.toml",
    "zen://audit/",
                                      
    "pgrep -f zen-swarm-ctld",
    "until zen-swarm-ctld",
                                     
    "introduced to zen-swarm",
    "the zen-swarm migration",
    "the zen-swarm Hermes plugin",
    "the zen-swarm project",
    "a zen-swarm project",
    "zen-swarm project directory",
                                                            
    "zen-swarm:zen-swarm",
]

_BRAND_FORBIDDEN_PATTERN = re.compile(
    r"zen[-_]swarm|ZenSwarm",
    flags=re.IGNORECASE,
)


def _strip_allowlist(text: str) -> str:
    """Redact historical-allowlist substrings before brand-string scan."""
    out = text
    for allowed in HISTORICAL_ALLOWLIST:
        out = out.replace(allowed, "<<ALLOWLIST_REDACTED>>")
    return out


def _scan_forbidden(text: str) -> list[str]:
    """Return list of forbidden brand-string matches (post-allowlist strip)."""
    stripped = _strip_allowlist(text)
    return _BRAND_FORBIDDEN_PATTERN.findall(stripped)


@pytest.mark.parametrize(
    "skill_name,requires_name_hades",
    SKILLS_UNDER_REBRAND,
    ids=[s[0] for s in SKILLS_UNDER_REBRAND],
)
def test_skill_md_has_hades_branding(
    skill_name: str,
    requires_name_hades: bool,
) -> None:
    """ rebrand: every SKILL.md prose body contains 'HADES' as a
    current-product reference and contains NO non-allowlisted 'zen-swarm'.
    """
    skill_path = SKILLS_ROOT / skill_name / "SKILL.md"
    assert skill_path.is_file(), f"{skill_name}: SKILL.md missing at {skill_path}"

    body = skill_path.read_text(encoding="utf-8")

    assert "HADES" in body, (
        f"{skill_name}/SKILL.md missing HADES brand string; Phase E-10 rebrand incomplete"
    )

    leaks = _scan_forbidden(body)
    assert not leaks, (
        f"{skill_name}/SKILL.md contains {len(leaks)} forbidden "
        f"brand-string match(es): {leaks}. "
        "Either rebrand to HADES or extend HISTORICAL_ALLOWLIST with explicit "
        "rationale per spec §Q3 BORDERLINE."
    )


@pytest.mark.parametrize(
    "skill_name,requires_name_hades",
    [s for s in SKILLS_UNDER_REBRAND if s[1]],
    ids=[s[0] for s in SKILLS_UNDER_REBRAND if s[1]],
)
def test_skill_md_frontmatter_name_field_is_hades(
    skill_name: str,
    requires_name_hades: bool,
) -> None:
    """Meta-skill SKILL.md: frontmatter `name: hades` is canonical per
     A-6 rename +  rebrand.
    """
    skill_path = SKILLS_ROOT / skill_name / "SKILL.md"
    body = skill_path.read_text(encoding="utf-8")

                                                         
    frontmatter_match = re.match(
        r"^---\n(.+?)\n---\n",
        body,
        flags=re.DOTALL,
    )
    assert frontmatter_match, (
        f"{skill_name}/SKILL.md: frontmatter not found (must begin with '---')"
    )

    frontmatter = frontmatter_match.group(1)
    assert re.search(r"^name:\s*hades\s*$", frontmatter, flags=re.MULTILINE), (
        f"{skill_name}/SKILL.md frontmatter: 'name: hades' not present; "
        f"current frontmatter:\n{frontmatter}"
    )


def test_skill_count_unchanged() -> None:
    """Sister-test:  MUST NOT add or remove SKILL.md files.

    The expected count is exactly 13 (per spec §Q3 IN-table line "SKILL.md
    content (13 skills)").  renamed `zen-swarm/SKILL.md` to
    `hades/SKILL.md` — count remains 13.
    """
    skill_paths = sorted(SKILLS_ROOT.glob("*/SKILL.md"))
    assert len(skill_paths) == 13, (
        f"Expected 13 SKILL.md files at {SKILLS_ROOT}; got {len(skill_paths)}: "
        f"{[p.relative_to(SKILLS_ROOT).parent.name for p in skill_paths]}"
    )

    expected_dirs = {name for name, _ in SKILLS_UNDER_REBRAND}
    actual_dirs = {p.parent.name for p in skill_paths}
    assert expected_dirs == actual_dirs, (
        f"SKILL.md directory mismatch.\n"
        f"  expected: {sorted(expected_dirs)}\n"
        f"  actual: {sorted(actual_dirs)}"
    )
