# SPDX-License-Identifier: MIT
# This module references decorator-looking syntax in a docstring but does NOT
# import the framework. The Detect gate keeps it out of scope; Endpoints would
# return [] even if invoked (no decorators of the recognised form).
"""
Example usage:
    @app.get("/foo")
    def foo(): ...
"""

x = 1
