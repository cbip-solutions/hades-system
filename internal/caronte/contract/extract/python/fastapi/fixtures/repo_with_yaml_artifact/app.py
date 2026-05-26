# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.post("/users")
def create_user():
    return {"ok": True}
