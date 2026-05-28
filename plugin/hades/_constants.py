# SPDX-License-Identifier: MIT
"""Shared constants for the hades Hermes plugin."""

from __future__ import annotations

# Default base URL for the hades-ctld HTTP listener (binary name
# preserved per spec section design choice BORDERLINE). Loopback
# (127.0.0.1 not "localhost") chosen for deterministic DNS resolution;
# port 8080 is the daemon default (see ``cmd/hades-ctld/main.go``).
# Hermes' ``_validate_base_url`` accepts ``http+unix://`` — when it
# does, this single literal moves and both consumers track automatically.
DEFAULT_DAEMON_BASE_URL = "http://127.0.0.1:8080"
