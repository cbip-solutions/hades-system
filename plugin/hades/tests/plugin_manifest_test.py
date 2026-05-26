# SPDX-License-Identifier: MIT
"""Smoke test for optional plugin.yaml Hermes PluginManifest shape."""

from __future__ import annotations

import pathlib

import pytest
import yaml

PLUGIN_ROOT = pathlib.Path(__file__).resolve().parents[1]
MANIFEST = PLUGIN_ROOT / "plugin.yaml"

                                         
REAL_MANIFEST_FIELDS = {
    "name",
    "version",
    "description",
    "provides_tools",
    "provides_hooks",
}

                                                                        
                                                                    
FICTIONAL_MANIFEST_FIELDS = {
    "apiVersion",
    "kind",
    "metadata",
    "spec",
    "slash_commands",
    "skills",
    "agent_templates",
    "hooks",
    "transports",
    "renderers",
    "inkComponents",
    "entry_points",
    "mcp_servers",
}


@pytest.fixture(scope="module")
def manifest_data() -> dict:
    """Load plugin.yaml; SKIP if absent (optional per spike §2)."""
    if not MANIFEST.exists():
        pytest.skip(
            "plugin.yaml is optional per spike §2 (0 of 11 bundled "
            "Hermes plugins have one); register(ctx) is the load-bearing "
            "contract. Tests skip when manifest absent."
        )
    return yaml.safe_load(MANIFEST.read_text())


def test_manifest_only_real_fields(manifest_data: dict) -> None:
    """plugin.yaml top-level keys must be subset of REAL_MANIFEST_FIELDS."""
    actual_fields = set(manifest_data.keys())
    extra = actual_fields - REAL_MANIFEST_FIELDS
    assert not extra, (
        f"plugin.yaml contains fields outside Hermes PluginManifest schema: "
        f"{extra}. Per spike §3, only {REAL_MANIFEST_FIELDS} are recognized. "
        f"Pre-spike plan revisions speculated keys like slash_commands, "
        f"skills, agent_templates, hooks, transports — those are fictional."
    )


def test_manifest_no_fictional_fields(manifest_data: dict) -> None:
    """plugin.yaml must NOT contain any fictional manifest field."""
    fictional_found = set(manifest_data.keys()) & FICTIONAL_MANIFEST_FIELDS
    assert not fictional_found, (
        f"plugin.yaml contains fictional manifest fields {fictional_found} "
        f"that Hermes' PluginManifest dataclass does NOT recognize. "
        f"See spike §3 + verification report §3."
    )


def test_manifest_name_is_hades(manifest_data: dict) -> None:
    """name field must be 'hades'."""
    assert manifest_data.get("name") == "hades"


def test_manifest_version_set(manifest_data: dict) -> None:
    """version field present + non-empty + Plan 12 bumped to 0.12.0."""
    version = manifest_data.get("version", "")
    assert version, "version must be non-empty"
    assert version == "0.12.0", (
        f"Plan 12 release per master plan §Release flow targets v0.12.0; "
        f"plugin.yaml has version={version!r}"
    )


def test_skills_exist() -> None:
    """skills/{hades,start,handoff}/SKILL.md must exist with agentskills_version.

    Note: skills/hades/ was skills/zen-swarm/ pre-Plan-18b. The directory was
    renamed in Phase A (A-6). Phase E-10a (Plan 18b) completed the frontmatter
    name flip from 'zen-swarm' to 'hades'. The directory-name and frontmatter
    name now both use 'hades'.
    """
                                                                         
    skill_dir_to_fm_name = {"hades": "hades", "start": "start", "handoff": "handoff"}
    for dir_name, fm_name in skill_dir_to_fm_name.items():
        p = PLUGIN_ROOT / "skills" / dir_name / "SKILL.md"
        assert p.exists(), f"skills/{dir_name}/SKILL.md missing"
        content = p.read_text()
        assert content.startswith("---\n"), f"{dir_name}/SKILL.md must have frontmatter"
        fm_end = content.index("\n---\n", 4)
        frontmatter = yaml.safe_load(content[4:fm_end])
        assert frontmatter.get("name") == fm_name, (
            f"frontmatter name should be '{fm_name}' (dir={dir_name})"
        )
                                                   
        assert frontmatter.get("agentskills_version"), (
            f"{dir_name}/SKILL.md must declare agentskills_version per inv-zen-169"
        )
        assert frontmatter["agentskills_version"] == 1.0, (
            "agentskills_version must be 1.0"
        )
        assert frontmatter.get("license"), f"{dir_name}/SKILL.md must declare license"


def test_plan12_new_skills_exist() -> None:
    """10 NEW Plan 12 Phase B skills must exist with correct agentskills_version."""
    new_skills = [
        "brainstorm",
        "write-plan",
        "execute-plan",
        "doctrine",
        "amendment",
        "impact-pre-merge",
        "audit-impact",
        "doctrine-drift-check",
        "knowledge-query",
        "knowledge-promote",
    ]
    for name in new_skills:
        p = PLUGIN_ROOT / "skills" / name / "SKILL.md"
        assert p.exists(), f"skills/{name}/SKILL.md missing (Plan 12 Phase B Task B-9)"
        content = p.read_text()
        assert content.startswith("---\n"), f"{name}/SKILL.md must have frontmatter"
        fm_end = content.index("\n---\n", 4)
        frontmatter = yaml.safe_load(content[4:fm_end])
        assert frontmatter.get("name") == name, f"frontmatter name should be '{name}'"
        assert frontmatter.get("agentskills_version") == 1.0, (
            f"{name}/SKILL.md must have agentskills_version: 1.0"
        )
        assert frontmatter.get("license"), f"{name}/SKILL.md must declare license"


def test_manifest_provides_hooks_subset_of_valid_hooks(manifest_data: dict) -> None:
    """provides_hooks entries must all be members of Hermes VALID_HOOKS.

    Per spike §4, VALID_HOOKS is a frozenset of 17 entries in
    hermes_cli/plugins.py:81-127. Any hook name outside this set is
    fictional (e.g., pre_completion, pre_tool_use, session_start —
    all REJECTED per spike §4 table).
    """
    VALID_HOOKS = {
                            
        "pre_tool_call",
        "post_tool_call",
        "transform_terminal_output",
        "transform_tool_result",
                           
        "transform_llm_output",
        "pre_llm_call",
        "post_llm_call",
        "pre_api_request",
        "post_api_request",
                               
        "on_session_start",
        "on_session_end",
        "on_session_finalize",
        "on_session_reset",
                      
        "subagent_stop",
                     
        "pre_gateway_dispatch",
                      
        "pre_approval_request",
        "post_approval_response",
    }
    assert len(VALID_HOOKS) == 17, "VALID_HOOKS canonical count is 17 per spike §4"

    provides = manifest_data.get("provides_hooks", [])
    for hook_name in provides:
        assert hook_name in VALID_HOOKS, (
            f"provides_hooks entry {hook_name!r} not in Hermes VALID_HOOKS "
            f"set (17 entries per spike §4). Check verification report §3 "
            f"for fictional hook names (pre_completion, pre_tool_use, etc.)."
        )
