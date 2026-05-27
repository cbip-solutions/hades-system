# SPDX-License-Identifier: MIT
"""Unit tests for plugin.zen_swarm.skins.hades (HADES skin module).

Each test runs in isolation: any side effects on Hermes' skin engine
(``set_active_skin`` cache, ``_skins_dir()`` file system) are restored via
fixtures so the module is testable without polluting ``~/.hermes/``.
"""

from __future__ import annotations

import importlib
import sys

SKINS_PKG = "hermes_plugins.hades.skins"


def _import_hades_module():
    """Fresh-import the hades module. Mirrors the conftest preload pattern."""
    mod_name = f"{SKINS_PKG}.hades"
    if mod_name in sys.modules:
        del sys.modules[mod_name]
    return importlib.import_module(mod_name)


def test_hades_module_is_importable():
    """B-2: the module loads without error against Python 3.14 stdlib only."""
    mod = _import_hades_module()
    assert mod is not None


def test_hades_module_exposes_required_symbols():
    """B-2: the public surface includes the helpers  will populate."""
    mod = _import_hades_module()
    for sym in ("register_hades_skin", "_build_hades_yaml", "_maybe_activate_hades"):
        assert hasattr(mod, sym), f"hades module is missing required symbol {sym!r}"


def test_build_hades_yaml_returns_parseable_yaml():
    """B-2 skeleton: ``_build_hades_yaml()`` returns a str that ``yaml.safe_load``
    parses. Content asserts arrive in B-3/B-4/B-5 as assets are layered in."""
    import yaml  # type: ignore[import-not-found, import-untyped]

    mod = _import_hades_module()
    rendered = mod._build_hades_yaml()
    assert isinstance(rendered, str)
    assert rendered.strip(), "rendered YAML must not be empty"
    parsed = yaml.safe_load(rendered)
    assert isinstance(parsed, dict)
    assert parsed.get("name") == "hades", "skin YAML must declare name: hades"


def test_hades_yaml_includes_banner_logo():
    """B-3: the rendered skin YAML embeds the HADES wordmark as banner_logo."""
    import yaml  # type: ignore[import-not-found, import-untyped]

    mod = _import_hades_module()
    parsed = yaml.safe_load(mod._build_hades_yaml())
    logo = parsed.get("banner_logo", "")
    assert "HADES" not in logo, (
        "banner_logo is Rich-markup wrapped FIGlet ASCII, not literal 'HADES'"
    )
                              
    rows = [r for r in logo.splitlines() if r.strip()]
    assert len(rows) == 6, f"FIGlet ANSI Shadow has 6 rows; got {len(rows)}"
    assert all(r.startswith("[") and r.endswith("[/]") for r in rows), (
        "every row must be Rich-markup wrapped (per Hermes built-in convention)"
    )
                                                                     
    assert "#c41e3a" in logo, "banner_logo must use the HADES crimson accent"
                                                                            
                                                                             
                                                                         
                                                                            
                                                                             
                  
    assert "┳" in logo, (
        "banner_logo must compose the Bident on the title line "
        "(two-prong junction glyph ┳ expected beside the wordmark)"
    )


def test_hades_yaml_banner_hero_renders_kynee():
    """UI-fix 2026-05-23: with the Bident now living in ``banner_logo`` (title
    line), ``banner_hero`` renders the **Kynée** — Hades' helm of invisibility —
    as the panel's left-column emblem. Fills the column with a themed Hades
    figure (privacy-by-default doctrine: the wearer is unseen) that follows
    Hermes' canonical hero idiom (braille + central glyph + dim epigram, 13-14
    rows, ≤25 cols, vertical 4-stop crimson→shadow gradient; see ares /
    poseidon / sisyphus / charizard built-ins in hermes_cli/skin_engine.py).

    Truthy invariant preserved: Hermes' ``banner.py`` falls back to
    ``HERMES_CADUCEUS`` if ``banner_hero`` is falsy — the Kynée block is
    multiline-truthy so the caduceus default never fires.
    """
    import yaml  # type: ignore[import-not-found, import-untyped]

    mod = _import_hades_module()
    parsed = yaml.safe_load(mod._build_hades_yaml())
    hero = parsed.get("banner_hero", "")
                                                                          
    assert hero, (
        "banner_hero must be truthy to suppress Hermes' HERMES_CADUCEUS "
        "fallback; an empty/None value re-triggers the caduceus"
    )
                                                                            
                                                                     
    assert hero.strip() != "", "banner_hero must render the Kynée emblem, not blank"
                                                                      
                                                                     
    assert "╨" in hero, (
        "Kynée must compose the compact Bident glyph (╨) inside the visor "
        "as the HADES identity anchor"
    )
                                                                   
                                                             
    assert "the helm hides" in hero, (
        "Kynée must close with the canonical Hermes-idiom dim epigram"
    )
                                                                          
                                                                     
    for stop, label in (
        ("#e0e0e0", "highlight (crown)"),
        ("#c41e3a", "crimson accent (helm body)"),
        ("#6b1828", "crimson dark (helm bottom)"),
        ("#3a2d5c", "shadow purple (base)"),
    ):
        assert stop in hero, f"Kynée gradient missing palette stop {stop} ({label})"
                                                                         
    assert "[dim #3a2d5c]" in hero, "epigram must use [dim #3a2d5c] per Hermes idiom"
                                                                       
                                                                         
    rows = [r for r in hero.splitlines() if r.strip()]
    assert 13 <= len(rows) <= 15, (
        f"Kynée must run 13-15 non-blank rows to match Hermes canon; got {len(rows)}"
    )


def test_palette_toml_parses_and_has_8_color_keys():
    """B-5: palette.toml parses + declares the 8 spec §Q2 color keys."""
    mod = _import_hades_module()
    palette = mod._load_palette()
    expected = {
        "bg",
        "text",
        "accent",
        "meta",
        "status_ok",
        "status_warn",
        "divider",
        "shadow",
    }
    assert expected.issubset(palette.keys()), (
        f"palette missing keys: {expected - palette.keys()}"
    )
                          
    assert palette["accent"] == "#c41e3a"
    assert palette["bg"] == "#0d0d0d"
    assert palette["shadow"] == "#3a2d5c"


def test_hades_yaml_applies_palette_to_hermes_color_keys():
    """B-5: rendered YAML carries the palette values onto Hermes color keys."""
    import yaml  # type: ignore[import-not-found, import-untyped]

    mod = _import_hades_module()
    parsed = yaml.safe_load(mod._build_hades_yaml())
    colors = parsed.get("colors", {})
                                                        
    assert colors.get("banner_accent") == "#c41e3a"
    assert colors.get("ui_accent") == "#c41e3a"
    assert colors.get("banner_title") == "#e0e0e0"
    assert colors.get("response_border") == "#c41e3a"
                                                  
    assert colors.get("status_bar_bg") == "#0d0d0d"
    assert colors.get("voice_status_bg") == "#0d0d0d"
                                          
    assert colors.get("banner_border") == "#555555"
    assert colors.get("input_rule") == "#555555"
                             
    assert colors.get("ui_ok") == "#10b981"
    assert colors.get("ui_warn") == "#ffa726"
              
    branding = parsed.get("branding", {})
    assert branding.get("agent_name") == "HADES Agent"
    assert branding.get("prompt_symbol") == "╨"


def test_register_hades_skin_writes_yaml(tmp_path, monkeypatch):
    """B-6: ``register_hades_skin`` writes ``hades.yaml`` under the Hermes
    skins dir + the file's content matches the rendered YAML byte-for-byte."""
    fake_home = tmp_path / "fake-hermes-home"
    fake_skins = fake_home / "skins"
    mod = _import_hades_module()
    monkeypatch.setattr(mod, "_user_skins_dir", lambda: fake_skins)

    out = mod.register_hades_skin()
    assert out == fake_skins / "hades.yaml"
    assert out.is_file()
    content = out.read_text(encoding="utf-8")
    assert content.startswith("name: hades")
    assert "banner_logo:" in content
    assert "banner_hero:" in content
    assert "colors:" in content


def test_register_hades_skin_is_idempotent(tmp_path, monkeypatch):
    """B-6: re-running ``register_hades_skin`` does not re-write if content
    matches (byte-compare gate preserves mtime so downstream tooling sees
    "no change")."""
    fake_skins = tmp_path / "skins"
    mod = _import_hades_module()
    monkeypatch.setattr(mod, "_user_skins_dir", lambda: fake_skins)

    first = mod.register_hades_skin()
    first_mtime = first.stat().st_mtime_ns
    second = mod.register_hades_skin()
    assert first == second
    assert second.stat().st_mtime_ns == first_mtime, (
        "idempotent re-write must skip the write when content is unchanged"
    )


def test_maybe_activate_hades_noop_when_env_unset(monkeypatch):
    """B-6: ``_maybe_activate_hades`` is a no-op when ``HERMES_SKIN`` is unset."""
    monkeypatch.delenv("HERMES_SKIN", raising=False)
    called: list[str] = []

    class FakeSkinEngine:
        @staticmethod
        def set_active_skin(name: str) -> None:
            called.append(name)

        @staticmethod
        def get_active_skin_name() -> str:
            return "default"

    monkeypatch.setitem(sys.modules, "hermes_cli.skin_engine", FakeSkinEngine)
    mod = _import_hades_module()
    mod._maybe_activate_hades()
    assert called == [], "must not call set_active_skin when env is unset"


def test_maybe_activate_hades_activates_when_env_is_hades(monkeypatch):
    """B-6: ``_maybe_activate_hades`` calls ``set_active_skin('hades')``
    when ``HERMES_SKIN=hades`` env is set + the active skin is not already
    ``hades``."""
    monkeypatch.setenv("HERMES_SKIN", "hades")
    called: list[str] = []

    class FakeSkinEngine:
        @staticmethod
        def set_active_skin(name: str) -> None:
            called.append(name)

        @staticmethod
        def get_active_skin_name() -> str:
            return "default"

    monkeypatch.setitem(sys.modules, "hermes_cli.skin_engine", FakeSkinEngine)
    mod = _import_hades_module()
    mod._maybe_activate_hades()
    assert called == ["hades"]


def test_maybe_activate_hades_skips_when_already_active(monkeypatch):
    """B-6: ``_maybe_activate_hades`` does not re-activate if already active."""
    monkeypatch.setenv("HERMES_SKIN", "hades")
    called: list[str] = []

    class FakeSkinEngine:
        @staticmethod
        def set_active_skin(name: str) -> None:
            called.append(name)

        @staticmethod
        def get_active_skin_name() -> str:
            return "hades"                  

    monkeypatch.setitem(sys.modules, "hermes_cli.skin_engine", FakeSkinEngine)
    mod = _import_hades_module()
    mod._maybe_activate_hades()
    assert called == [], "must not re-activate when already active"


def test_hades_smoke_active_skin_resolves_correctly(tmp_path, monkeypatch):
    """B-7 smoke: deploy HADES YAML to fake skins dir + set HERMES_SKIN=hades
    + invoke ``_maybe_activate_hades`` + assert ``get_active_skin`` returns
    the HADES ``SkinConfig`` with banner_logo, banner_hero, and palette
    intact.

    Uses Hermes' real ``skin_engine`` (not the FakeSkinEngine shim from
    B-6) so this exercises the full pipeline: YAML deploy → Hermes load →
    activation."""
    fake_home = tmp_path / "hermes-home"
    fake_skins = fake_home / "skins"
    monkeypatch.setenv("HERMES_SKIN", "hades")

                                                               
    import hermes_constants  # type: ignore[import-not-found, import-untyped]

    monkeypatch.setattr(hermes_constants, "get_hermes_home", lambda: fake_home)
    import hermes_cli.skin_engine as engine  # type: ignore[import-not-found, import-untyped]

    monkeypatch.setattr(engine, "_skins_dir", lambda: fake_skins)

    mod = _import_hades_module()
    monkeypatch.setattr(mod, "_user_skins_dir", lambda: fake_skins)

                     
    deployed = mod.register_hades_skin()
    assert deployed.is_file()

                                                                     
    monkeypatch.setattr(engine, "_active_skin", None, raising=False)
    monkeypatch.setattr(engine, "_active_skin_name", "default", raising=False)

                  
    mod._maybe_activate_hades()
    assert engine.get_active_skin_name() == "hades"

    skin = engine.get_active_skin()
                                                                                
    assert skin.banner_logo, "active skin must have banner_logo"
    assert skin.banner_logo.count("\n") >= 5, "banner_logo is 6 rows"
    assert "#c41e3a" in skin.banner_logo
    assert "┳" in skin.banner_logo, (
        "Bident must be composed into banner_logo (title line) "
        "post banner-fix 2026-05-23"
    )

                                                                       
                                                                      
                                                                         
                                                                      
                                                  
                                                                      
                                                             
    assert skin.banner_hero, "active skin must have banner_hero (Kynée emblem)"
    assert "╨" in skin.banner_hero, "Kynée must carry the compact Bident glyph"
    assert "the helm hides" in skin.banner_hero, (
        "Kynée must carry its dim epigram per Hermes hero idiom"
    )

                     
    assert skin.get_color("banner_accent") == "#c41e3a"
    assert skin.get_color("status_bar_bg") == "#0d0d0d"
    assert skin.get_color("banner_text") == "#e0e0e0"

              
    assert skin.get_branding("agent_name") == "HADES Agent"
    assert skin.get_branding("prompt_symbol") == "╨"


def test_hades_smoke_default_skin_unchanged_without_env(tmp_path, monkeypatch):
    """B-7 negative smoke: without HERMES_SKIN, activation is a no-op and
    Hermes' active skin remains 'default' (idempotent / safe under absence)."""
    fake_skins = tmp_path / "skins"
    monkeypatch.delenv("HERMES_SKIN", raising=False)
    import hermes_cli.skin_engine as engine  # type: ignore[import-not-found, import-untyped]

    monkeypatch.setattr(engine, "_skins_dir", lambda: fake_skins)
    monkeypatch.setattr(engine, "_active_skin", None, raising=False)
    monkeypatch.setattr(engine, "_active_skin_name", "default", raising=False)

    mod = _import_hades_module()
    monkeypatch.setattr(mod, "_user_skins_dir", lambda: fake_skins)

    mod.register_hades_skin()                
    mod._maybe_activate_hades()                     

    assert engine.get_active_skin_name() == "default"


def test_render_branding_block_returns_empty_string_for_empty_dict():
    """Sister test (B-5/B-6): ``_render_branding_block`` returns "" for
    empty branding dict so callers can concatenate unconditionally."""
    mod = _import_hades_module()
    assert mod._render_branding_block({}) == ""
    assert mod._render_branding_block({"tagline": "only-a-tagline"}).startswith(
        "branding:"
    )


def test_user_skins_dir_falls_back_when_hermes_constants_missing(monkeypatch):
    """Sister test: ``_user_skins_dir`` falls back to ``~/.hermes/skins/``
    when ``hermes_constants`` is not importable (test env without Hermes)."""
    import builtins

    real_import = builtins.__import__

    def fake_import(name, *args, **kwargs):
        if name == "hermes_constants":
            raise ImportError("simulated hermes_constants missing")
        return real_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", fake_import)
    monkeypatch.setitem(sys.modules, "hermes_constants", None)
    if "hermes_constants" in sys.modules and sys.modules["hermes_constants"] is None:
        del sys.modules["hermes_constants"]

    mod = _import_hades_module()
    skins_dir = mod._user_skins_dir()
                                                                          
                             
    assert skins_dir.parts[-2:] == (".hermes", "skins")


def test_user_skins_dir_uses_hermes_constants_when_available(tmp_path, monkeypatch):
    """Sister test: ``_user_skins_dir`` returns ``get_hermes_home() / "skins"``
    when ``hermes_constants`` IS importable (the happy path)."""
    import hermes_constants  # type: ignore[import-not-found, import-untyped]

    fake_home = tmp_path / "hermes-home"
    monkeypatch.setattr(hermes_constants, "get_hermes_home", lambda: fake_home)

    mod = _import_hades_module()
    skins_dir = mod._user_skins_dir()
    assert skins_dir == fake_home / "skins", (
        f"expected {fake_home / 'skins'}, got {skins_dir}"
    )


def test_branding_inline_glyphs_are_bident_not_fleur_de_lis():
    """GAP-4: all inline branding glyphs use the bident family ╨ (U+2568),
    never the fleur-de-lis ⚜.  palette.toml [branding] is the single source
    of truth; this test gates all four glyph-bearing keys."""
    mod = _import_hades_module()
    palette = mod._load_palette()
    branding = palette["branding"]

                                                        
    for key in ("prompt_symbol", "goodbye", "response_label", "help_header"):
        assert "⚜" not in branding[key], (
            f"branding[{key!r}] still contains fleur-de-lis ⚜; expected bident ╨"
        )

                                                         
    assert branding["prompt_symbol"] == "╨", (
        f"prompt_symbol must be exactly '╨', got {branding['prompt_symbol']!r}"
    )
    assert "╨" in branding["response_label"], (
        f"response_label must contain '╨', got {branding['response_label']!r}"
    )
    assert branding["goodbye"].endswith("╨"), (
        f"goodbye must end with '╨', got {branding['goodbye']!r}"
    )
    assert "╨" in branding["help_header"], (
        f"help_header must contain '╨', got {branding['help_header']!r}"
    )


def test_maybe_activate_hades_silently_skips_when_skin_engine_missing(monkeypatch):
    """Sister test: when ``HERMES_SKIN=hades`` but ``hermes_cli.skin_engine``
    is not importable (defensive), ``_maybe_activate_hades`` returns without
    raising (logger.debug only)."""
    import builtins

    monkeypatch.setenv("HERMES_SKIN", "hades")
    real_import = builtins.__import__

    def fake_import(name, *args, **kwargs):
        if name == "hermes_cli.skin_engine" or name.startswith("hermes_cli"):
            raise ImportError("simulated hermes_cli missing")
        return real_import(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", fake_import)
                                                             
    for cached in list(sys.modules):
        if cached == "hermes_cli" or cached.startswith("hermes_cli."):
            del sys.modules[cached]

    mod = _import_hades_module()
                                                               
    mod._maybe_activate_hades()
