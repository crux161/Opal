"""Initial schema — users and friendships.

Revision ID: 001
Revises:
Create Date: 2026-03-12
"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "001"
down_revision: Union[str, None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "users",
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("quicdial_id", sa.String(64), unique=True, nullable=False, index=True),
        sa.Column("display_name", sa.String(100), nullable=False),
        sa.Column("avatar_id", sa.String(64), nullable=False, server_default="kyu-kun"),
        sa.Column("password_hash", sa.String(128), nullable=False),
        sa.Column("email", sa.String(255), unique=True, nullable=True),
        sa.Column("password_reset_token", sa.String(64), nullable=True, index=True),
        sa.Column("password_reset_expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("last_login_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
    )

    op.create_table(
        "friendships",
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("requester_id", sa.String(36), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("addressee_id", sa.String(36), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True),
        sa.Column(
            "status",
            sa.Enum("pending", "accepted", "declined", "blocked", name="friendship_status"),
            nullable=False,
            server_default="pending",
        ),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("requester_id", "addressee_id", name="uq_requester_addressee"),
    )


def downgrade() -> None:
    op.drop_table("friendships")
    op.drop_table("users")
