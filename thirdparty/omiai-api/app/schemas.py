"""Pydantic request / response models."""

from datetime import datetime

from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Auth
# ---------------------------------------------------------------------------

class SignupRequest(BaseModel):
    quicdial_id: str | None = None  # auto-generated when omitted
    display_name: str = Field(..., min_length=1, max_length=100)
    password: str = Field(..., min_length=6, max_length=128)
    avatar_id: str = "kyu-kun"
    email: str | None = None


class LoginRequest(BaseModel):
    quicdial_id: str
    password: str


class PasswordResetRequest(BaseModel):
    quicdial_id: str


class PasswordResetConfirm(BaseModel):
    token: str
    password: str = Field(..., min_length=6, max_length=128)


class TokenResponse(BaseModel):
    token: str
    user: "UserPublic"


class UserPublic(BaseModel):
    quicdial_id: str
    display_name: str
    avatar_id: str
    email: str | None = None

    model_config = {"from_attributes": True}


# ---------------------------------------------------------------------------
# Profile
# ---------------------------------------------------------------------------

class ProfileUpdate(BaseModel):
    display_name: str | None = None
    avatar_id: str | None = None
    email: str | None = None


# ---------------------------------------------------------------------------
# Friends
# ---------------------------------------------------------------------------

class FriendRequestCreate(BaseModel):
    quicdial_id: str  # target quicdial_id


class FriendEntry(BaseModel):
    friendship_id: str
    quicdial_id: str
    display_name: str
    avatar_id: str


class PendingRequest(BaseModel):
    friendship_id: str
    from_quicdial_id: str
    from_display_name: str
    from_avatar_id: str
    created_at: datetime


# ---------------------------------------------------------------------------
# Internal (from Elixir gateway)
# ---------------------------------------------------------------------------

class InternalFriendPayload(BaseModel):
    """Payload the Elixir gateway sends when proxying a friend event."""
    user_id: str
    quicdial_id: str
    to_quicdial_id: str | None = None
    friendship_id: str | None = None


# ---------------------------------------------------------------------------
# Matchmaking
# ---------------------------------------------------------------------------

class MatchmakingEnqueue(BaseModel):
    user_id: str
    quicdial_id: str
    payload: dict = {}
