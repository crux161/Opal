"""
Friendship management endpoints.

Public API (JWT-protected):
    GET    /api/v1/friends           — list accepted friends
    GET    /api/v1/friends/requests  — list pending incoming requests
    POST   /api/v1/friends/request   — send a friend request
    POST   /api/v1/friends/{id}/accept
    POST   /api/v1/friends/{id}/decline
    DELETE /api/v1/friends/{quicdial_id}

Internal (called by the Elixir gateway proxy, no JWT):
    POST /friends/friend_request
    POST /friends/friend_accept
    POST /friends/friend_decline
    POST /friends/friend_remove

After mutating state, internal endpoints call back to the Elixir gateway's
/internal/push_event webhook so the affected user gets a real-time socket push.
"""

import logging

import httpx
from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import or_, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import settings
from app.database import get_db
from app.deps import get_current_user
from app.models import Friendship, User
from app.schemas import (
    FriendEntry,
    FriendRequestCreate,
    InternalFriendPayload,
    PendingRequest,
)

router = APIRouter()
log = logging.getLogger("omiai_api.friends")


# ===== helpers ==============================================================

async def _push_to_gateway(to_quicdial_id: str, event: str, payload: dict):
    """Fire-and-forget push to the Elixir gateway's internal webhook."""
    try:
        async with httpx.AsyncClient(timeout=5) as client:
            await client.post(
                f"{settings.gateway_url}/internal/push_event",
                json={
                    "to_quicdial_id": to_quicdial_id,
                    "event": event,
                    "payload": payload,
                },
                headers={"Authorization": f"Bearer {settings.gateway_internal_key}"},
            )
    except httpx.HTTPError as exc:
        log.warning("gateway push failed event=%s to=%s err=%s", event, to_quicdial_id, exc)


async def _resolve_user(db: AsyncSession, quicdial_id: str) -> User | None:
    result = await db.execute(select(User).where(User.quicdial_id == quicdial_id))
    return result.scalar_one_or_none()


# ===== public REST endpoints ================================================

@router.get("/api/v1/friends", response_model=list[FriendEntry])
async def list_friends(
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    result = await db.execute(
        select(Friendship)
        .where(
            Friendship.status == "accepted",
            or_(
                Friendship.requester_id == user.id,
                Friendship.addressee_id == user.id,
            ),
        )
    )
    friendships = result.scalars().all()

    friend_ids = [
        f.addressee_id if f.requester_id == user.id else f.requester_id
        for f in friendships
    ]

    if not friend_ids:
        return []

    users_result = await db.execute(select(User).where(User.id.in_(friend_ids)))
    users_by_id = {u.id: u for u in users_result.scalars().all()}

    out = []
    for f in friendships:
        fid = f.addressee_id if f.requester_id == user.id else f.requester_id
        u = users_by_id.get(fid)
        if u:
            out.append(FriendEntry(
                friendship_id=f.id,
                quicdial_id=u.quicdial_id,
                display_name=u.display_name,
                avatar_id=u.avatar_id,
            ))
    return out


@router.get("/api/v1/friends/requests", response_model=list[PendingRequest])
async def pending_requests(
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    result = await db.execute(
        select(Friendship)
        .where(Friendship.addressee_id == user.id, Friendship.status == "pending")
    )
    friendships = result.scalars().all()

    requester_ids = [f.requester_id for f in friendships]
    if not requester_ids:
        return []

    users_result = await db.execute(select(User).where(User.id.in_(requester_ids)))
    users_by_id = {u.id: u for u in users_result.scalars().all()}

    out = []
    for f in friendships:
        u = users_by_id.get(f.requester_id)
        if u:
            out.append(PendingRequest(
                friendship_id=f.id,
                from_quicdial_id=u.quicdial_id,
                from_display_name=u.display_name,
                from_avatar_id=u.avatar_id,
                created_at=f.created_at,
            ))
    return out


@router.post("/api/v1/friends/request", status_code=201)
async def create_request(
    body: FriendRequestCreate,
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    return await _do_friend_request(db, user.id, user.quicdial_id, body.quicdial_id)


@router.post("/api/v1/friends/{friendship_id}/accept")
async def accept_request(
    friendship_id: str,
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    return await _do_accept(db, friendship_id, user.id, user.quicdial_id)


@router.post("/api/v1/friends/{friendship_id}/decline")
async def decline_request(
    friendship_id: str,
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    return await _do_decline(db, friendship_id, user.id, user.quicdial_id)


@router.delete("/api/v1/friends/{quicdial_id}")
async def remove_friend(
    quicdial_id: str,
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    return await _do_remove(db, user.id, user.quicdial_id, quicdial_id)


# ===== internal endpoints (called by Elixir gateway proxy) ==================

@router.post("/friends/friend_request")
async def internal_friend_request(
    body: InternalFriendPayload,
    db: AsyncSession = Depends(get_db),
):
    return await _do_friend_request(db, body.user_id, body.quicdial_id, body.to_quicdial_id)


@router.post("/friends/friend_accept")
async def internal_friend_accept(
    body: InternalFriendPayload,
    db: AsyncSession = Depends(get_db),
):
    return await _do_accept(db, body.friendship_id, body.user_id, body.quicdial_id)


@router.post("/friends/friend_decline")
async def internal_friend_decline(
    body: InternalFriendPayload,
    db: AsyncSession = Depends(get_db),
):
    return await _do_decline(db, body.friendship_id, body.user_id, body.quicdial_id)


@router.post("/friends/friend_remove")
async def internal_friend_remove(
    body: InternalFriendPayload,
    db: AsyncSession = Depends(get_db),
):
    return await _do_remove(db, body.user_id, body.quicdial_id, body.to_quicdial_id)


# ===== shared business logic ================================================

async def _do_friend_request(
    db: AsyncSession,
    from_user_id: str,
    from_quicdial_id: str,
    to_quicdial_id: str,
):
    if from_quicdial_id == to_quicdial_id:
        raise HTTPException(422, detail="cannot_friend_self")

    recipient = await _resolve_user(db, to_quicdial_id)
    if recipient is None:
        raise HTTPException(404, detail="recipient_not_found")

    # Check for existing relationship in either direction
    existing = await db.execute(
        select(Friendship).where(
            or_(
                (Friendship.requester_id == from_user_id) & (Friendship.addressee_id == recipient.id),
                (Friendship.requester_id == recipient.id) & (Friendship.addressee_id == from_user_id),
            )
        )
    )
    dup = existing.scalar_one_or_none()
    if dup:
        if dup.status == "accepted":
            raise HTTPException(409, detail="already_friends")
        if dup.status == "pending":
            raise HTTPException(409, detail="already_pending")

    friendship = Friendship(requester_id=from_user_id, addressee_id=recipient.id)
    db.add(friendship)
    await db.commit()
    await db.refresh(friendship)

    # Notify the recipient via Elixir gateway
    sender = await db.get(User, from_user_id)
    await _push_to_gateway(to_quicdial_id, "friend_request_received", {
        "friendship_id": friendship.id,
        "from_quicdial_id": sender.quicdial_id if sender else from_quicdial_id,
        "from_display_name": sender.display_name if sender else from_quicdial_id,
        "from_avatar_id": sender.avatar_id if sender else "default",
    })

    return {"friendship_id": friendship.id, "status": friendship.status}


async def _do_accept(
    db: AsyncSession,
    friendship_id: str,
    user_id: str,
    user_quicdial_id: str,
):
    friendship = await db.get(Friendship, friendship_id)
    if friendship is None:
        raise HTTPException(404, detail="not_found")
    if friendship.addressee_id != user_id:
        raise HTTPException(403, detail="unauthorized")

    friendship.status = "accepted"
    await db.commit()

    requester = await db.get(User, friendship.requester_id)
    accepter = await db.get(User, user_id)
    if requester:
        await _push_to_gateway(requester.quicdial_id, "friend_accepted", {
            "friendship_id": friendship.id,
            "by_quicdial_id": user_quicdial_id,
            "by_display_name": accepter.display_name if accepter else user_quicdial_id,
            "by_avatar_id": accepter.avatar_id if accepter else "default",
        })

    return {"status": "accepted"}


async def _do_decline(
    db: AsyncSession,
    friendship_id: str,
    user_id: str,
    user_quicdial_id: str,
):
    friendship = await db.get(Friendship, friendship_id)
    if friendship is None:
        raise HTTPException(404, detail="not_found")
    if friendship.addressee_id != user_id:
        raise HTTPException(403, detail="unauthorized")

    friendship.status = "declined"
    await db.commit()

    requester = await db.get(User, friendship.requester_id)
    if requester:
        await _push_to_gateway(requester.quicdial_id, "friend_declined", {
            "friendship_id": friendship.id,
            "by_quicdial_id": user_quicdial_id,
        })

    return {"status": "declined"}


async def _do_remove(
    db: AsyncSession,
    user_id: str,
    user_quicdial_id: str,
    target_quicdial_id: str,
):
    target = await _resolve_user(db, target_quicdial_id)
    if target is None:
        raise HTTPException(404, detail="not_found")

    result = await db.execute(
        select(Friendship).where(
            Friendship.status == "accepted",
            or_(
                (Friendship.requester_id == user_id) & (Friendship.addressee_id == target.id),
                (Friendship.requester_id == target.id) & (Friendship.addressee_id == user_id),
            ),
        )
    )
    friendship = result.scalar_one_or_none()
    if friendship is None:
        raise HTTPException(404, detail="not_found")

    await db.delete(friendship)
    await db.commit()

    await _push_to_gateway(target_quicdial_id, "friend_removed", {
        "by_quicdial_id": user_quicdial_id,
    })

    return {"status": "removed"}
