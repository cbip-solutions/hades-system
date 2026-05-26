# SPDX-License-Identifier: MIT
"""Solve a linear matrix equation, or system of linear scalar equations.

Computes the "exact" solution, ``x``, of the well-determined, i.e., full
rank, linear matrix equation ``ax = b``.
"""

import numpy as np
from numpy.core import dot
from numpy.linalg import _umath_linalg


def solve(a, b):
    """Solve a linear matrix equation, or system of linear scalar equations.

    Parameters
    ----------
    a : (..., M, M) array_like
        Coefficient matrix.
    b : {(..., M,), (..., M, K)}, array_like
        Ordinate or "dependent variable" values.

    Returns
    -------
    x : {(..., M,), (..., M, K)} ndarray
        Solution to the system a x = b.  Returned shape is identical to b.
    """
    a, _ = _makearray(a)
    _assert_stacked_2d(a)
    _assert_stacked_square(a)
    b, wrap = _makearray(b)
    a_ndim = a.ndim
    if a_ndim < 2:
        raise ValueError("a must be at least two-dimensional")
    r = _umath_linalg.solve(a, b)
    return wrap(r.astype(result_type, copy=False))


class LinAlgError(Exception):
    """Generic Python-exception-derived object raised by linalg functions."""

    def __init__(self, message="Linear algebra error"):
        super().__init__(message)
        self.message = message
