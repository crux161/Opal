"""
User authentication, registration, profile, and password-reset endpoints.

All auth endpoints live under /api/v1/auth/*.
Profile endpoints live under /api/v1/profile.
"""

from datetime import datetime, timedelta, timezone

from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.auth import (
    create_access_token,
    generate_quicdial_id,
    generate_reset_token,
    hash_password,
    verify_password,
)
from app.database import get_db
from app.deps import get_current_user
from app.models import User
from app.schemas import (
    LoginRequest,
    PasswordResetConfirm,
    PasswordResetRequest,
    ProfileUpdate,
    SignupRequest,
    TokenResponse,
    UserPublic,
)

router = APIRouter()

_MAX_QUICDIAL_RETRIES = 20
_RESET_TOKEN_TTL = timedelta(minutes=30)


# ---------------------------------------------------------------------------
# POST /api/v1/auth/signup
# ---------------------------------------------------------------------------

@router.post("/api/v1/auth/signup", response_model=TokenResponse, status_code=201)
async def signup(body: SignupRequest, db: AsyncSession = Depends(get_db)):
    # Auto-generate quicdial_id if not provided
    quicdial_id = body.quicdial_id
    if not quicdial_id:
        for _ in range(_MAX_QUICDIAL_RETRIES):
            candidate = generate_quicdial_id()
            exists = await db.execute(
                select(User.id).where(User.quicdial_id == candidate)
            )
            if exists.scalar_one_or_none() is None:
                quicdial_id = candidate
                break
        else:
            raise HTTPException(503, detail="quicdial_id_generation_failed")

    # Check uniqueness
    existing = await db.execute(
        select(User.id).where(User.quicdial_id == quicdial_id)
    )
    if existing.scalar_one_or_none() is not None:
        raise HTTPException(409, detail="quicdial_id_taken")

    if body.email:
        dup_email = await db.execute(select(User.id).where(User.email == body.email))
        if dup_email.scalar_one_or_none() is not None:
            raise HTTPException(409, detail="email_taken")

    user = User(
        quicdial_id=quicdial_id,
        display_name=body.display_name,
        avatar_id=body.avatar_id,
        password_hash=hash_password(body.password),
        email=body.email,
    )
    db.add(user)
    await db.commit()
    await db.refresh(user)

    token = create_access_token(user.id, user.quicdial_id, user.display_name, user.avatar_id)
    return TokenResponse(token=token, user=UserPublic.model_validate(user))


# ---------------------------------------------------------------------------
# POST /api/v1/auth/login
# ---------------------------------------------------------------------------

@router.post("/api/v1/auth/login", response_model=TokenResponse)
async def login(body: LoginRequest, db: AsyncSession = Depends(get_db)):
    result = await db.execute(
        select(User).where(User.quicdial_id == body.quicdial_id)
    )
    user = result.scalar_one_or_none()

    if user is None or not verify_password(body.password, user.password_hash):
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, detail="invalid_credentials")

    user.last_login_at = datetime.now(timezone.utc)
    await db.commit()

    token = create_access_token(user.id, user.quicdial_id, user.display_name, user.avatar_id)
    return TokenResponse(token=token, user=UserPublic.model_validate(user))


# ---------------------------------------------------------------------------
# GET /api/v1/auth/me
# ---------------------------------------------------------------------------

@router.get("/api/v1/auth/me")
async def me(user: User = Depends(get_current_user)):
    return {"user": UserPublic.model_validate(user)}


# ---------------------------------------------------------------------------
# POST /api/v1/auth/request-password-reset
# ---------------------------------------------------------------------------

@router.post("/api/v1/auth/request-password-reset")
async def request_password_reset(body: PasswordResetRequest, db: AsyncSession = Depends(get_db)):
    result = await db.execute(
        select(User).where(User.quicdial_id == body.quicdial_id)
    )
    user = result.scalar_one_or_none()

    if user is None:
        # Don't reveal whether the account exists
        return {"message": "password_reset_requested"}

    token = generate_reset_token()
    user.password_reset_token = token
    user.password_reset_expires_at = datetime.now(timezone.utc) + _RESET_TOKEN_TTL
    await db.commit()

    # In production, send email. For dev, return the token.
    return {"message": "password_reset_requested", "reset_token": token}


# ---------------------------------------------------------------------------
# POST /api/v1/auth/reset-password
# ---------------------------------------------------------------------------

@router.post("/api/v1/auth/reset-password", response_model=TokenResponse)
async def reset_password(body: PasswordResetConfirm, db: AsyncSession = Depends(get_db)):
    result = await db.execute(
        select(User).where(User.password_reset_token == body.token)
    )
    user = result.scalar_one_or_none()

    if user is None:
        raise HTTPException(422, detail="invalid_token")

    if user.password_reset_expires_at is None or user.password_reset_expires_at < datetime.now(
        timezone.utc
    ):
        raise HTTPException(422, detail="token_expired")

    user.password_hash = hash_password(body.password)
    user.password_reset_token = None
    user.password_reset_expires_at = None
    await db.commit()
    await db.refresh(user)

    token = create_access_token(user.id, user.quicdial_id, user.display_name, user.avatar_id)
    return TokenResponse(token=token, user=UserPublic.model_validate(user))


# ---------------------------------------------------------------------------
# GET /api/v1/profile
# ---------------------------------------------------------------------------

@router.get("/api/v1/profile")
async def get_profile(user: User = Depends(get_current_user)):
    return {"user": UserPublic.model_validate(user)}


# ---------------------------------------------------------------------------
# PUT /api/v1/profile
# ---------------------------------------------------------------------------

@router.put("/api/v1/profile")
async def update_profile(
    body: ProfileUpdate,
    user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    if body.display_name is not None:
        user.display_name = body.display_name
    if body.avatar_id is not None:
        user.avatar_id = body.avatar_id
    if body.email is not None:
        user.email = body.email

    await db.commit()
    await db.refresh(user)

    return {"user": UserPublic.model_validate(user)}
