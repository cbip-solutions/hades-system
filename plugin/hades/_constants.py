# SPDX-License-Identifier: MIT
"""Shared constants for the hades Hermes plugin."""

from __future__ import annotations

# Default base URL for the zen-swarm-ctld HTTP listener (binary name
# preserved per spec section Q3 BORDERLINE). Loopback
# (127.0.0.1 not "localhost") chosen for deterministic DNS resolution;
# port 8080 is the daemon default (see ``cmd/zen-swarm-ctld/main.go``).
# Hermes' ``_validate_base_url`` accepts ``http+unix://`` — when it
# does, this single literal moves and both consumers track automatically.
DEFAULT_DAEMON_BASE_URL = "http://127.0.0.1:8080"
