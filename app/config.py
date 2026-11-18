
from functools import lru_cache

from pydantic import Field, HttpUrl
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
        validate_default=True,
    )

    # Application
    APP_NAME: str = "Prophecy AniZone Sub Server"
    APP_VERSION: str = "1.0.0"
    APP_ENV: str = "production"

    HOST: str = "0.0.0.0"
    PORT: int = Field(default=8000, ge=1, le=65535)

    # AniZone Provider
    ANIZONE_ENABLED: bool = True
    ANIZONE_BASE_URL: HttpUrl = "https://anizone.to"

    # HTTP Client
    REQUEST_TIMEOUT: float = Field(
        default=20.0,
        gt=0,
        le=120,
    )

    REQUEST_RETRIES: int = Field(
        default=2,
        ge=0,
        le=5,
    )

    USER_AGENT: str = (
        "Mozilla/5.0 (compatible; ProphecyAniZoneServer/1.0)"
    )

    # Response Limits
    MAX_RESPONSE_BYTES: int = Field(
        default=4 * 1024 * 1024,
        ge=1024,
    )

    # Pagination
    MAX_PAGINATION_PAGES: int = Field(
        default=64,
        ge=1,
        le=100,
    )


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
