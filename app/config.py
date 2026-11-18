import os
from functools import lru_cache


class Settings:
    APP_NAME: str = "Prophecy AniZone Server"
    APP_VERSION: str = "1.0.0"

    ENVIRONMENT: str = os.getenv("ENVIRONMENT", "production")
    DEBUG: bool = os.getenv("DEBUG", "false").lower() == "true"

    API_PREFIX: str = "/api/v1"

    DATABASE_URL: str = os.getenv("DATABASE_URL", "")

    API_KEY: str = os.getenv("API_KEY", "")

    REQUEST_TIMEOUT: int = int(
        os.getenv("REQUEST_TIMEOUT", "30")
    )

    MAX_RETRIES: int = int(
        os.getenv("MAX_RETRIES", "2")
    )

    CACHE_TTL: int = int(
        os.getenv("CACHE_TTL", "300")
    )

    ANIZONE_ENABLED: bool = (
        os.getenv("ANIZONE_ENABLED", "true").lower() == "true"
    )

    ANIZONE_BASE_URL: str = os.getenv(
        "ANIZONE_BASE_URL", ""
    )


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
