import uuid
from datetime import datetime, timezone

from sqlalchemy import (
    Column,
    DateTime,
    Enum as SAEnum,
    ForeignKey,
    String,
    UniqueConstraint,
    func,
)
from sqlalchemy.orm import DeclarativeBase, relationship


def _utcnow():
    return datetime.now(timezone.utc)


def _new_id():
    return str(uuid.uuid4())


class Base(DeclarativeBase):
    pass


class User(Base):
    __tablename__ = "users"

    id = Column(String(36), primary_key=True, default=_new_id)
    quicdial_id = Column(String(64), unique=True, nullable=False, index=True)
    display_name = Column(String(100), nullable=False)
    avatar_id = Column(String(64), nullable=False, default="kyu-kun")
    password_hash = Column(String(128), nullable=False)

    email = Column(String(255), unique=True, nullable=True)

    password_reset_token = Column(String(64), nullable=True, index=True)
    password_reset_expires_at = Column(DateTime(timezone=True), nullable=True)
    last_login_at = Column(DateTime(timezone=True), nullable=True)

    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    updated_at = Column(
        DateTime(timezone=True),
        server_default=func.now(),
        onupdate=_utcnow,
        nullable=False,
    )

    # Relationships
    sent_requests = relationship(
        "Friendship",
        foreign_keys="Friendship.requester_id",
        back_populates="requester",
        cascade="all, delete-orphan",
    )
    received_requests = relationship(
        "Friendship",
        foreign_keys="Friendship.addressee_id",
        back_populates="addressee",
        cascade="all, delete-orphan",
    )


class Friendship(Base):
    __tablename__ = "friendships"

    id = Column(String(36), primary_key=True, default=_new_id)
    requester_id = Column(
        String(36), ForeignKey("users.id", ondelete="CASCADE"), nullable=False
    )
    addressee_id = Column(
        String(36), ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True
    )
    status = Column(
        SAEnum("pending", "accepted", "declined", "blocked", name="friendship_status"),
        nullable=False,
        default="pending",
    )

    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    updated_at = Column(
        DateTime(timezone=True),
        server_default=func.now(),
        onupdate=_utcnow,
        nullable=False,
    )

    requester = relationship("User", foreign_keys=[requester_id], back_populates="sent_requests")
    addressee = relationship("User", foreign_keys=[addressee_id], back_populates="received_requests")

    __table_args__ = (
        UniqueConstraint("requester_id", "addressee_id", name="uq_requester_addressee"),
    )
