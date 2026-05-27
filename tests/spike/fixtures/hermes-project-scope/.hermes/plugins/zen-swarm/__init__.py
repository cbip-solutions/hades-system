# SPDX-License-Identifier: MIT
""" spike fixture — project-scope plugin module.

Minimal `register(ctx)` entry-point required by Hermes plugin loader
(see hermes_cli/plugins.py:1037). The fixture exists to prove Hermes
head walks `<cwd>/.hermes/plugins/zen-swarm/` when
HERMES_ENABLE_PROJECT_PLUGINS=1; runtime behaviour is intentionally
empty since the spike artifact only verifies the loader path.
"""


def register(ctx) -> None:  # noqa: ANN001 — ctx is hermes-defined
    """No-op registration. Loader path verification only."""
    return None
