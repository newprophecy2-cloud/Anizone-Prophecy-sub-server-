
from contextlib import asynccontextmanager
import inspect
from typing import Any

from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel, Field

from app.config import settings
from app.engine import engine


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.engine = engine

    yield

    for provider in engine.providers.values():
        close = getattr(provider, "close", None)

        if callable(close):
            try:
                result = close()

                if inspect.isawaitable(result):
                    await result
            except Exception:
                # Shutdown should continue even if a provider
                # client fails to close.
                pass


app = FastAPI(
    title=settings.APP_NAME,
    description="Standalone provider API for Prophecy Backend.",
    version=settings.APP_VERSION,
    lifespan=lifespan,
)


class ExecuteRequest(BaseModel):
    action: str = Field(
        min_length=1,
        max_length=30,
        description="Provider action",
    )

    params: dict[str, Any] = Field(
        default_factory=dict,
        description="Provider action parameters",
    )


@app.get("/")
async def root():
    return {
        "name": settings.APP_NAME,
        "status": "online",
        "version": app.version,
        "docs": "/docs",
        "health": "/health",
        "manifest": "/api/v1/manifest",
    }


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "service": "prophecy-anizone",
        "provider_enabled": settings.ANIZONE_ENABLED,
    }


@app.get("/api/v1/manifest")
async def manifest():
    providers = engine.list_providers()

    return {
        "name": settings.APP_NAME,
        "version": app.version,
        "protocol": "prophecy-provider-v1",
        "status": "ready",
        "providers_endpoint": "/api/v1/providers",
        "execute_endpoint": (
            "/api/v1/providers/{provider_id}/execute"
        ),
        "providers": providers,
        "capabilities": [
            "search",
            "details",
            "episodes",
        ],
    }


@app.get("/api/v1/providers")
async def list_providers():
    return {
        "status": "ok",
        "providers": engine.list_providers(),
    }


@app.get("/api/v1/providers/{provider_id}")
async def provider_details(provider_id: str):
    metadata = engine.get_provider(provider_id)

    if metadata is None:
        raise HTTPException(
            status_code=404,
            detail={
                "code": "PROVIDER_NOT_FOUND",
                "provider": provider_id,
            },
        )

    return {
        "status": "ok",
        "provider": metadata,
    }


@app.post("/api/v1/providers/{provider_id}/execute")
async def execute_provider(
    provider_id: str,
    request: ExecuteRequest,
):
    return await engine.execute(
        provider_id=provider_id,
        action=request.action,
        params=request.params,
    )


# --------------------------------------------------
# CONVENIENT ANIME API ROUTES
# --------------------------------------------------

@app.get("/api/v1/anime/search")
async def search_anime(
    q: str = Query(
        min_length=1,
        max_length=150,
    ),
    page: int = Query(
        default=1,
        ge=1,
    ),
):
    if page > settings.MAX_PAGINATION_PAGES:
        raise HTTPException(
            status_code=400,
            detail={
                "code": "INVALID_PAGE",
                "message": "Page exceeds configured limit.",
            },
        )

    return await engine.execute(
        provider_id="anizone",
        action="search",
        params={
            "query": q,
            "page": page,
        },
    )


@app.get("/api/v1/anime/{slug}/episodes")
async def anime_episodes(slug: str):
    return await engine.execute(
        provider_id="anizone",
        action="episodes",
        params={
            "slug": slug,
        },
    )


@app.get("/api/v1/anime/{slug}")
async def anime_details(slug: str):
    return await engine.execute(
        provider_id="anizone",
        action="details",
        params={
            "slug": slug,
        },
    )
