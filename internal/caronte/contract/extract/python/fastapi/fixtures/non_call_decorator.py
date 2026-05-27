# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


# A bare decorator (not a call) — not a valid FastAPI route, but tree-sitter
# parses it. Exercises the parseDecorator early-return when the decorator
# expression is not a `call` node.
@app
def bare_decorator_target():
    return {}


# A non-attribute call decorator — also unrecognised by FastAPI.
@cached
def cached_target():
    return {}


# A decorator whose object is a complex expression (not a plain identifier) —
# unrecognised; exercises the obj.Type() != identifier branch.
@(app.dep_chain).get("/skip")
def complex_decorator():
    return {}
