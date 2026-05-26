# SPDX-License-Identifier: MIT
"""HADES skin subpackage — Plan 18a Phase B."""

from .hades import (
    _build_hades_yaml,
    _maybe_activate_hades,
    register_hades_skin,
)

__all__ = ["register_hades_skin", "_maybe_activate_hades", "_build_hades_yaml"]
