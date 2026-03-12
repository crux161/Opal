from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    # JWT — shared with the Elixir gateway (MUST match OMIAI_JWT_SECRET)
    jwt_secret: str = "omiai_dev_jwt_secret_change_in_prod"
    jwt_algorithm: str = "HS256"
    jwt_expire_days: int = 30

    # Database
    database_url: str = "sqlite+aiosqlite:///./omiai_api.db"

    # Elixir gateway callback
    gateway_url: str = "http://localhost:4000"
    gateway_internal_key: str = "omiai_dev_internal_key"

    # Matchmaking
    matchmaking_interval_seconds: float = 2.0

    model_config = {"env_file": ".env", "extra": "ignore"}


settings = Settings()
