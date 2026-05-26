# SPDX-License-Identifier: MIT
"""Tests for the zen-swarm Hermes provider profile."""

from __future__ import annotations

import sys
from pathlib import Path

from providers.base import ProviderProfile

from providers import register_provider  # noqa: F401  (proves import path)

PROVIDER_NAME = "zen-swarm"


def _fresh_load_zen_provider_module() -> object:
    """Import (or re-import) the zen-swarm provider module so the
    ``register_provider(...)`` call at module-level fires under the current
    test environment (incl. monkeypatched ``ZEN_SWARM_BASE_URL``).

    The module lives at ``plugin/zen-swarm/providers/__init__.py``. We
    load it under a stable dotted name via ``importlib.util.spec_from_file_location``
    so re-imports are deterministic and don't collide with Hermes' own
    user-plugin loader namespace.
    """
    import importlib.util

    plugin_dir = Path(__file__).resolve().parent.parent                     
    init_file = plugin_dir / "providers" / "__init__.py"
    module_name = "_zen_swarm_test_provider_load"

                                                                            
                             
    sys.modules.pop(module_name, None)

    spec = importlib.util.spec_from_file_location(module_name, init_file)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


def test_providers_init_file_exists():
    """Sanity: the file Hermes discovers must exist at the canonical path."""
    plugin_dir = Path(__file__).resolve().parent.parent
    init_file = plugin_dir / "providers" / "__init__.py"
    assert init_file.is_file(), f"missing {init_file}"


def test_zen_swarm_provider_registered_on_import():
    """After loading the module, ``get_provider_profile('zen-swarm')`` must
    return a non-None ``ProviderProfile``.

    Verifies the module-level ``register_provider(profile)`` call fires.
    """
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None, f"provider {PROVIDER_NAME!r} not registered"
    assert isinstance(profile, ProviderProfile)


def test_zen_swarm_profile_api_mode_anthropic_messages():
    """Profile MUST use ``api_mode=anthropic_messages`` so Hermes' real
    HTTP path (``build_anthropic_client``) POSTs native Anthropic JSON to
    the daemon's existing ``NewAnthropicProxy``."""
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
    assert profile.api_mode == "anthropic_messages", (
        f"api_mode={profile.api_mode!r} would route via OpenAI-wire; "
        "daemon NewAnthropicProxy expects anthropic_messages format"
    )


def test_zen_swarm_profile_base_url_defaults_to_local_loopback(monkeypatch):
    """Without ``ZEN_SWARM_BASE_URL`` env, base_url must default to a
    local-loopback URL so Hermes-by-default talks to the operator's
    zen-swarm-ctld (single-egress)."""
    monkeypatch.delenv("ZEN_SWARM_BASE_URL", raising=False)
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
                                                                 
    assert "127.0.0.1" in profile.base_url or "localhost" in profile.base_url, (
        f"base_url={profile.base_url!r} not loopback"
    )
                                                                                    
    assert profile.base_url.startswith("http://"), (
        f"base_url={profile.base_url!r} must use http:// scheme"
    )


def test_zen_swarm_profile_base_url_respects_env_override(monkeypatch):
    """``ZEN_SWARM_BASE_URL`` env var must override the default base_url
    (operator-friendly: lets ops point at a non-default port or a
    different host for testing/staging)."""
    custom = "http://127.0.0.1:9999"
    monkeypatch.setenv("ZEN_SWARM_BASE_URL", custom)
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
    assert profile.base_url == custom, (
        f"ZEN_SWARM_BASE_URL override ignored; got {profile.base_url!r}"
    )


def test_zen_swarm_profile_name_and_aliases():
    """Profile must register canonical name ``zen-swarm`` plus operator
    aliases (``zen``, ``zenswarm``) so ``hermes model zen`` works."""
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
    assert profile.name == PROVIDER_NAME
    aliases = set(profile.aliases)
    assert "zen" in aliases, f"aliases missing 'zen'; got {aliases}"
    assert "zenswarm" in aliases, f"aliases missing 'zenswarm'; got {aliases}"

                                                                 
    by_alias = get_provider_profile("zen")
    assert by_alias is profile, "alias 'zen' did not resolve to canonical profile"


def test_zen_swarm_profile_env_vars_declares_api_key():
    """Profile must declare an env-var for the API key (Plan 11 Phase F
    will issue per-operator daemon-side API keys)."""
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
    assert "ZEN_SWARM_API_KEY" in profile.env_vars, (
        f"env_vars missing ZEN_SWARM_API_KEY; got {profile.env_vars}"
    )


def test_zen_swarm_profile_does_not_leak_external_hostnames():
    """Adversarial: the profile's ``base_url`` must NOT point at any
    forbidden upstream LLM hostname (inv-zen-164 single-egress)."""
    _fresh_load_zen_provider_module()
    from providers import get_provider_profile

    profile = get_provider_profile(PROVIDER_NAME)
    assert profile is not None
    forbidden_hosts = (
        "api.anthropic.com",
        "api.openai.com",
        "openai.com",
        "generativelanguage.googleapis.com",
        "huggingface.co",
        "api.cohere.ai",
    )
    for host in forbidden_hosts:
        assert host not in profile.base_url, (
            f"profile.base_url={profile.base_url!r} contains forbidden host {host!r}"
        )


def test_zen_swarm_profile_module_uses_register_provider():
    """Sanity grep: the module file must actually call ``register_provider``.

    Belt-and-braces with the test that re-imports the module: if a future
    refactor accidentally drops the registration call, this test fails
    even without exercising the registry.
    """
    plugin_dir = Path(__file__).resolve().parent.parent
    src = (plugin_dir / "providers" / "__init__.py").read_text(encoding="utf-8")
    assert "register_provider(" in src, (
        "providers/__init__.py must call register_provider(profile)"
    )
    assert "ProviderProfile(" in src, (
        "providers/__init__.py must instantiate ProviderProfile(...)"
    )


def test_load_default_base_url_falls_back_when_constants_missing(monkeypatch):
    """Reviewer M1 defensive path: when ``_constants.py`` is absent on
    disk (corrupted install — should never happen in normal operation),
    ``_load_default_base_url`` returns the documented loopback URL so
    the plugin remains operational while doctor surfaces the missing
    file. This test exercises the corrupted-install fallback branch.
    """
    mod = _fresh_load_zen_provider_module()

                                                                       
                                          
    sys.modules.pop("_zen_swarm_plugin_constants", None)

                                                                      
                                                         
    from pathlib import Path as _Path

    real_is_file = _Path.is_file

    def fake_is_file(self):
        if str(self).endswith("_constants.py"):
            return False
        return real_is_file(self)

    monkeypatch.setattr(_Path, "is_file", fake_is_file)
    url = mod._load_default_base_url()
    # The defensive fallback MUST match the canonical value in
                                                                    
                                                                  
                                                   
    assert url == "http://127.0.0.1:8080"
