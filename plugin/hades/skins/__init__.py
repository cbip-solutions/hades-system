# SPDX-License-Identifier: MIT
"""HADES skin subpackage — the release design release track."""

from .hades import (
    _build_hades_yaml,
    _maybe_activate_hades,
    register_hades_skin,
)

__all__ = ["register_hades_skin", "_maybe_activate_hades", "_build_hades_yaml"]
