# SPDX-License-Identifier: MIT
"""HADES skin module — the release design release track."""

from __future__ import annotations

import logging
import os
import tomllib
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

_SKIN_NAME = "hades"
_MODULE_DIR = Path(__file__).resolve().parent
_ASSETS_DIR = _MODULE_DIR / "assets"
_PALETTE_PATH = _MODULE_DIR / "palette.toml"


def _read_asset(filename: str) -> str:
    """Read a text asset from the sibling ``assets/`` directory.

    Raises ``FileNotFoundError`` if the asset is missing — callers
    (``register_hades_skin``) treat that as a hard error since the YAML
    deploy would otherwise emit an incomplete skin.
    """
    path = _ASSETS_DIR / filename
    return path.read_text(encoding="utf-8").rstrip("\n")


def _indent_yaml_block(text: str, indent: int = 2) -> str:
    """Indent each line of ``text`` by ``indent`` spaces for YAML block scalars.

    Preserves trailing newline conventions for round-trip stability under
    ``yaml.safe_load``.
    """
    pad = " " * indent
    return "\n".join(pad + line for line in text.splitlines()) + "\n"


def _load_palette() -> dict[str, Any]:
    """Parse the HADES ``palette.toml`` as a flat dict (top-level only).

    The ``[branding]`` sub-table is preserved at key ``branding`` in the
    returned dict. Raises ``FileNotFoundError`` if ``palette.toml`` is
    missing (treated as a hard error — the YAML deploy would otherwise emit
    an incomplete skin).
    """
    with _PALETTE_PATH.open("rb") as f:
        raw = tomllib.load(f)
    # Flatten: top-level keys + the [branding] subtable
    branding = raw.pop("branding", {})
    flat = dict(raw)
    flat["branding"] = branding
    return flat


def _palette_to_hermes_colors(palette: dict[str, Any]) -> dict[str, str]:
    """Map spec §Q2 palette keys to Hermes' color schema.

    Hermes' ``SkinConfig`` carries ~30 color keys (see
    ``hermes_cli/skin_engine.py:14-49``). The 8 spec keys fan out across
    multiple Hermes keys so a single semantic role (e.g. ``accent``) covers
    every place that role is rendered (banner accent, ui accent, response
    border, etc.).
    """
    return {
        # Background
        "status_bar_bg": palette["bg"],
        "voice_status_bg": palette["bg"],
        "completion_menu_bg": palette["bg"],
        "completion_menu_meta_bg": palette["bg"],
        # Text
        "banner_text": palette["text"],
        "prompt": palette["text"],
        "status_bar_text": palette["text"],
        "banner_title": palette["text"],
        # Accent (crimson)
        "banner_accent": palette["accent"],
        "ui_accent": palette["accent"],
        "response_border": palette["accent"],
        # Meta (dim)
        "banner_dim": palette["meta"],
        "status_bar_dim": palette["meta"],
        "session_border": palette["meta"],
        "session_label": palette["meta"],
        # Status colors
        "ui_ok": palette["status_ok"],
        "status_bar_good": palette["status_ok"],
        "ui_warn": palette["status_warn"],
        "status_bar_warn": palette["status_warn"],
        # Divider
        "banner_border": palette["divider"],
        "input_rule": palette["divider"],
        "ui_label": palette["divider"],
        # Shadow
        "status_bar_bad": palette["shadow"],
        "status_bar_critical": palette["shadow"],
        "completion_menu_current_bg": palette["shadow"],
    }


def _render_colors_block(colors: dict[str, str]) -> str:
    """Render a YAML ``colors:`` block from a flat color dict.

    Keys are emitted in sorted order for deterministic byte-stable output
    (required for ``register_hades_skin`` idempotency check).
    """
    lines = ["colors:"]
    for key in sorted(colors.keys()):
        lines.append(f'  {key}: "{colors[key]}"')
    return "\n".join(lines) + "\n"


def _render_branding_block(branding: dict[str, Any]) -> str:
    """Render a YAML ``branding:`` block from the palette's branding subtable.

    The TOML ``tagline`` key is rendered into Hermes' ``welcome`` greeting
    (Hermes does not have a tagline key). Empty branding yields an empty
    string so callers can concatenate unconditionally.
    """
    if not branding:
        return ""
    lines = ["branding:"]
    for key in sorted(branding.keys()):
        if key == "tagline":
            # tagline is rendered into welcome (Hermes does not have a tagline key)
            continue
        val = str(branding[key]).replace('"', '\\"')
        lines.append(f'  {key}: "{val}"')
    welcome = branding.get("tagline", "")
    if welcome:
        lines.append(f'  welcome: "Welcome to HADES Agent — {welcome}"')
    return "\n".join(lines) + "\n"


def _build_hades_yaml() -> str:
    """Render the complete HADES skin YAML.

    Composes ``name + description + banner_logo + banner_hero + colors +
    branding`` from ``hades_logo.txt`` and ``palette.toml``. The output is the
    byte-stable YAML written to ``~/.hermes/skins/hades.yaml`` by
    ``register_hades_skin``.

    Banner layout (2026-05-23 fix): the Bident is composed INTO ``banner_logo``
    (beside the HADES wordmark, on the title line). Hermes'
    ``build_welcome_banner`` renders ``banner_hero`` in the panel's left column
    *below* the title — which mis-placed the Bident next to the system info —
    so ``banner_hero`` is intentionally a blank spacer. It is kept TRUTHY (a
    single space) so Hermes does NOT fall back to its ``HERMES_CADUCEUS``
    default (``banner.py``: ``_hero = _bskin.banner_hero ... else
    HERMES_CADUCEUS``); the blank value leaves the panel's left column to the
    system info alone. The full 15-row Bident remains the canonical asset
    (``hades_bident.txt``), distilled into the compact title-line variant that
    ``hades_logo.txt`` now bakes beside the wordmark.
    """
    logo = _read_asset("hades_logo.txt")
    kynee = _read_asset("hades_kynee.txt")
    palette = _load_palette()
    colors = _palette_to_hermes_colors(palette)
    return (
        "name: hades\n"
        "description: HADES system — charcoal + crimson, on the Hermes substrate\n"
        "banner_logo: |\n"
        + _indent_yaml_block(logo, indent=2)
        # banner_hero: the Kynée (Hades' helm of invisibility) emblem fills
        # the panel's left column with a themed Hades figure. Follows Hermes'
        # canonical hero idiom (braille + central glyph + dim epigram;
        # mirrors ares/poseidon/sisyphus/charizard built-ins in
        # hermes_cli/skin_engine.py). The compact Bident (╨) inside the
        # visor binds HADES identity to the helm — Hades + invisibility
        # fused. Reads as a privacy-by-default doctrine cue (the wearer is
        # unseen). Multiline-truthy → Hermes' HERMES_CADUCEUS fallback
        # (banner.py:453) never fires.
        + "banner_hero: |\n"
        + _indent_yaml_block(kynee, indent=2)
        + _render_colors_block(colors)
        + _render_branding_block(palette["branding"])
    )


def _user_skins_dir() -> Path:
    """Resolve the Hermes user-skins directory.

    Lazy-imports ``hermes_constants.get_hermes_home`` so unit tests can
    monkey-patch the import without requiring Hermes to be on PATH at
    module import time.
    """
    try:
        from hermes_constants import (  # type: ignore[import-not-found, import-untyped]
            get_hermes_home,
        )
    except ImportError:
        return Path.home() / ".hermes" / "skins"
    return get_hermes_home() / "skins"


def _maybe_activate_hades(*_args: Any, **_kwargs: Any) -> None:
    """Hermes ``on_session_start`` hook: activate HADES skin when env requests it.

    Triggered by ``HERMES_SKIN=hades`` env (set by the release track wrapper). The
    release track amendment documents WHY this hook is needed (Hermes' own
    ``skin_engine`` does NOT consume the env var).

    Idempotent: skips activation if the active skin is already ``hades``.
    Safe to call multiple times.
    """
    if os.environ.get("HERMES_SKIN", "").strip().lower() != _SKIN_NAME:
        return
    try:
        import hermes_cli.skin_engine as skin_engine  # type: ignore[import-not-found, import-untyped]
    except ImportError:
        logger.debug("hermes_cli.skin_engine unavailable; skipping HADES activation")
        return
    try:
        if skin_engine.get_active_skin_name() == _SKIN_NAME:
            return
        skin_engine.set_active_skin(_SKIN_NAME)
        logger.info("HADES skin activated (HERMES_SKIN=%s)", _SKIN_NAME)
    except Exception as e:  # pragma: no cover — defensive
        logger.warning("HADES skin activation failed: %s", e)


def register_hades_skin() -> Path:
    """Write the HADES skin YAML to ``~/.hermes/skins/hades.yaml``.

    Returns the absolute path of the deployed file (whether newly written
    or already up-to-date). Idempotent: byte-compares the existing content
    against the freshly-rendered YAML and skips the write when they match,
    preserving the file's mtime so downstream tools can detect "no change".
    """
    skin_yaml = _build_hades_yaml()
    target = _user_skins_dir() / f"{_SKIN_NAME}.yaml"
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.is_file() and target.read_text(encoding="utf-8") == skin_yaml:
        return target
    target.write_text(skin_yaml, encoding="utf-8")
    return target
