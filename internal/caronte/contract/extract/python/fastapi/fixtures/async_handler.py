# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.get("/async")
async def get_async():
    return {"async": True}
