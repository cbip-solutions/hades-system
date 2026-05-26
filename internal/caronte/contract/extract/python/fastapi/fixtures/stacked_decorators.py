# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.get("/ping")
@app.head("/ping")
def ping():
    return "pong"
