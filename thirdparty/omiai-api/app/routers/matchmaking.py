"""
Matchmaking endpoint.

POST /matchmaking/enqueue  — called by the Elixir gateway when a user
                             sends a "find_match" event via WebSocket.

The request is enqueued into an in-memory FIFO queue.  The background
matchmaking worker pairs users and calls back to the Elixir gateway's
/internal/match_found webhook.
"""

from fastapi import APIRouter

from app.matchmaking_worker import enqueue
from app.schemas import MatchmakingEnqueue

router = APIRouter()


@router.post("/matchmaking/enqueue")
async def matchmaking_enqueue(body: MatchmakingEnqueue):
    enqueue(body.user_id, body.quicdial_id, body.payload)
    return {"status": "queued"}
