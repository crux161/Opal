"""
JWT generation and password hashing.

The JWT produced here uses the exact same HS256 secret and claims layout
that the refactored Elixir gateway expects, so the gateway can blindly
trust the token without any database call.

Expected claims:
    sub          — user ID (string UUID)
    quicdial_id  — calling code
    display_name — human-readable name
    avatar_id    — avatar identifier
    exp          — expiration (unix timestamp)
"""

import random
import secrets
from datetime import datetime, timedelta, timezone

import bcrypt
from jose import jwt

from app.config import settings


# ---------------------------------------------------------------------------
# Password hashing (bcrypt)
# ---------------------------------------------------------------------------

def hash_password(plain: str) -> str:
    return bcrypt.hashpw(plain.encode(), bcrypt.gensalt()).decode()


def verify_password(plain: str, hashed: str) -> bool:
    return bcrypt.checkpw(plain.encode(), hashed.encode())


# ---------------------------------------------------------------------------
# JWT
# ---------------------------------------------------------------------------

def create_access_token(
    user_id: str,
    quicdial_id: str,
    display_name: str,
    avatar_id: str,
) -> str:
    """Build a JWT that the Elixir gateway will accept."""
    claims = {
        "sub": user_id,
        "quicdial_id": quicdial_id,
        "display_name": display_name,
        "avatar_id": avatar_id,
        "exp": datetime.now(timezone.utc) + timedelta(days=settings.jwt_expire_days),
    }
    return jwt.encode(claims, settings.jwt_secret, algorithm=settings.jwt_algorithm)


def decode_access_token(token: str) -> dict:
    """Decode and validate a JWT.  Raises jose.JWTError on failure."""
    return jwt.decode(token, settings.jwt_secret, algorithms=[settings.jwt_algorithm])


# ---------------------------------------------------------------------------
# Quicdial ID generation  (format: ###-###-###)
# ---------------------------------------------------------------------------

def generate_quicdial_id() -> str:
    parts = [f"{random.randint(0, 999):03d}" for _ in range(3)]
    return "-".join(parts)


# ---------------------------------------------------------------------------
# Password-reset tokens
# ---------------------------------------------------------------------------

def generate_reset_token() -> str:
    return secrets.token_urlsafe(32)
