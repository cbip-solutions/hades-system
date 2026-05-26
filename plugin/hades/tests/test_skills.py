# SPDX-License-Identifier: MIT
"""Tests for SKILL.md files — frontmatter validation per Hermes parser."""

from __future__ import annotations

from pathlib import Path

import yaml

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SKILLS_DIR = PLUGIN_ROOT / "skills"


def _parse_skill_md(path: Path) -> tuple[dict, str]:
    """Mirror of agent/skill_utils.parse_frontmatter(). Returns (frontmatter_dict, body)."""
    content = path.read_text(encoding="utf-8")
    if not content.startswith("---"):
        return {}, content
                                                                           
    rest = content[3:]                      
    end_idx = rest.find("\n---")
    if end_idx == -1:
        return {}, content
    yaml_block = rest[:end_idx]
    body = rest[end_idx + 4 :].lstrip("\n")
    parsed = yaml.safe_load(yaml_block) or {}
    if not isinstance(parsed, dict):
        return {}, body
    return parsed, body


REQUIRED_SKILLS = ["hades", "start", "handoff"]


def test_all_required_skills_exist():
    for name in REQUIRED_SKILLS:
        path = SKILLS_DIR / name / "SKILL.md"
        assert path.exists(), f"missing SKILL.md for {name} at {path}"


def test_zen_swarm_skill_frontmatter():
    fm, body = _parse_skill_md(SKILLS_DIR / "hades" / "SKILL.md")
                                                                        
    assert fm.get("name") == "hades"
    assert fm.get("description"), "description required"
                                                
    assert len(body.strip()) > 500, (
        "hades SKILL.md body too short — looks like placeholder"
    )


def test_zen_swarm_skill_mentions_doctrine():
    fm, body = _parse_skill_md(SKILLS_DIR / "hades" / "SKILL.md")
    text = body.lower()
    assert "doctrine" in text or "max-scope" in text or "max scope" in text


def test_zen_swarm_skill_mentions_no_claude_attribution():
    fm, body = _parse_skill_md(SKILLS_DIR / "hades" / "SKILL.md")
    text = body.lower()
    assert (
        "inv-zen-004" in text
        or "no claude attribution" in text
        or "no ai attribution" in text
    )


def test_zen_swarm_skill_mentions_workflow():
    fm, body = _parse_skill_md(SKILLS_DIR / "hades" / "SKILL.md")
    text = body.lower()
    assert "brainstorm" in text and "plan" in text


                                                                              


def test_start_skill_frontmatter_present():
    fm, _body = _parse_skill_md(SKILLS_DIR / "start" / "SKILL.md")
    assert fm.get("name") == "start"


def test_handoff_skill_frontmatter_present():
    fm, _body = _parse_skill_md(SKILLS_DIR / "handoff" / "SKILL.md")
    assert fm.get("name") == "handoff"


                                                                             


def test_all_skill_frontmatter_safe_to_parse_with_yaml_safe_load():
    """Hermes uses yaml.safe_load; verify all SKILL.md files parse without
    raising under safe loader (no tags, no anchors that require unsafe)."""
    for name in REQUIRED_SKILLS:
        path = SKILLS_DIR / name / "SKILL.md"
        fm, _ = _parse_skill_md(path)
        assert isinstance(fm, dict), f"{name} frontmatter did not parse as dict"
