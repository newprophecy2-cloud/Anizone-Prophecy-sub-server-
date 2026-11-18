from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel, Field

from app.config import settings
from app.engine import engine


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.engine = engine
    yield

    # Close provider HTTP clients during shutdown.
    for provider in engine.providers.values():
        close = getattr(provider, "close", None)

        if callable(close):
            result = close()

            if hasattr(result, "__await__"):
                await result


app = FastAPI(
    title="Prophecy AniZone Sub Server",
    description="Standalone provider API for Prophecy Backend.",
    version="1.0.0",
    lifespan=lifespan,
)


class ExecuteRequest(BaseModel):
    action: str = Field(
        min_length=1,
        max_length=30,
        description="Provider action",
    )

    params: dict[str, Any] = Field(default_factory=dict)


@app.get("/")
async def root():
    return {
        "name": "Prophecy AniZone Sub Server",
        "status": "online",
        "version": app.version,
        "docs": "/docs",
        "health": "/health",
    }


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "service": "prophecy-anizone",
    }


@app.get("/api/v1/manifest")
async def manifest():
    return {
        "name": "Prophecy AniZone Sub Server",
        "version": app.version,
        "protocol": "prophecy-provider-v1",
        "status": "ready",
        "providers_endpoint": "/api/v1/providers",
        "execute_endpoint": "/api/v1/providers/{provider_id}/execute",
        "capabilities": [
            "search",
            "details",
            "episodes",
        ],
    }


@app.get("/api/v1/providers")
async def list_providers():
    return engine.list_providers()


@app.get("/api/v1/providers/{provider_id}")
async def provider_details(provider_id: str):
    provider = engine.get_provider(provider_id)

    if provider is None:
        raise HTTPException(
            status_code=404,
            detail={
                "code": "PROVIDER_NOT_FOUND",
                "provider": provider_id,
            },
        )

    metadata = engine._metadata(provider_id, provider)

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
        **request.params,
    )


# Convenient search endpoint.
@app.get("/api/v1/anime/search")
async def search_anime(
    q: str = Query(min_length=1, max_length=150),
    page: int = Query(default=1, ge=1, le=64),
):
    return await engine.execute(
        provider_id="anizone",
        action="search",
        query=q,
        page=page,
    )


# Convenient details endpoint.
@app.get("/api/v1/anime/{slug}")
async def anime_details(slug: str):
    return await engine.execute(
        provider_id="anizone",
        action="details",
        slug=slug,
    )


# Convenient episode endpoint.
@app.get("/api/v1/anime/{slug}/episodes")
async def anime_episodes(slug: str):
    return await engine.execute(
        provider_id="anizone",
        action="episodes",
        slug=slug,
    )
