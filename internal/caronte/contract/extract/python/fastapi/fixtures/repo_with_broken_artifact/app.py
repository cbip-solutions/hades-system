# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.get("/ok")
def ok():
    return "ok"
