"""
Async matchmaking worker.

Runs as a background task inside the FastAPI lifespan.  Maintains an
in-memory queue of users waiting for a match and pairs them FIFO every
`MATCHMAKING_INTERVAL_SECONDS`.

When two peers are matched, the worker fires an HTTP POST to the Elixir
gateway's `/internal/match_found` webhook so both peers receive the
`match_found` socket event and can begin WebRTC negotiation.
"""

import asyncio
import logging
import uuid
from collections import deque
from dataclasses import dataclass, field

import httpx

from app.config import settings

log = logging.getLogger("omiai_api.matchmaking")


@dataclass
class QueueEntry:
    user_id: str
    quicdial_id: str
    payload: dict = field(default_factory=dict)


# Global matchmaking queue
_queue: deque[QueueEntry] = deque()
_seen: set[str] = set()  # prevent duplicate enqueues


def enqueue(user_id: str, quicdial_id: str, payload: dict | None = None):
    if quicdial_id in _seen:
        log.debug("matchmaking_duplicate quicdial_id=%s", quicdial_id)
        return

    _seen.add(quicdial_id)
    _queue.append(QueueEntry(user_id=user_id, quicdial_id=quicdial_id, payload=payload or {}))
    log.info("matchmaking_enqueued quicdial_id=%s queue_size=%d", quicdial_id, len(_queue))


async def _notify_gateway(peer_a: str, peer_b: str, session_id: str):
    """POST to the Elixir gateway's internal match webhook."""
    try:
        async with httpx.AsyncClient(timeout=5) as client:
            resp = await client.post(
                f"{settings.gateway_url}/internal/match_found",
                json={
                    "peer_a": peer_a,
                    "peer_b": peer_b,
                    "session_id": session_id,
                },
                headers={"Authorization": f"Bearer {settings.gateway_internal_key}"},
            )
            log.info(
                "match_callback session_id=%s peer_a=%s peer_b=%s status=%d",
                session_id, peer_a, peer_b, resp.status_code,
            )
    except httpx.HTTPError as exc:
        log.error("match_callback_failed session_id=%s err=%s", session_id, exc)


async def _match_loop():
    """Core loop: pair users from the queue and notify the gateway."""
    while True:
        await asyncio.sleep(settings.matchmaking_interval_seconds)

        while len(_queue) >= 2:
            entry_a = _queue.popleft()
            entry_b = _queue.popleft()

            _seen.discard(entry_a.quicdial_id)
            _seen.discard(entry_b.quicdial_id)

            session_id = str(uuid.uuid4())
            log.info(
                "match_made session_id=%s peer_a=%s peer_b=%s",
                session_id, entry_a.quicdial_id, entry_b.quicdial_id,
            )

            await _notify_gateway(entry_a.quicdial_id, entry_b.quicdial_id, session_id)


async def start():
    """Launch the matchmaking background task.  Call from lifespan."""
    log.info("matchmaking_worker started interval=%.1fs", settings.matchmaking_interval_seconds)
    asyncio.create_task(_match_loop())
