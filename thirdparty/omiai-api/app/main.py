"""
Omiai-API — FastAPI microservice that owns all business logic for Omiai.

The Elixir gateway handles WebSocket connections and WebRTC signaling;
this service handles users, auth, friendships, and matchmaking.
"""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app import matchmaking_worker
from app.database import engine
from app.models import Base
from app.routers import friends, matchmaking, users

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup: create tables (idempotent) and start the matchmaking worker
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    await matchmaking_worker.start()

    yield

    # Shutdown
    await engine.dispose()


app = FastAPI(
    title="Omiai API",
    version="0.1.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(users.router)
app.include_router(friends.router)
app.include_router(matchmaking.router)


@app.get("/health")
async def health():
    return {"status": "ok", "service": "omiai-api"}
