from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from datetime import datetime, timezone

app = FastAPI(
    title="Prophecy AniZone Server",
    description="Independent provider sub-server for Prophecy",
    version="1.0.0",
    docs_url="/docs",
    redoc_url="/redoc"
)

# CORS
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=False,
    allow_methods=["GET", "POST", "PUT", "PATCH", "DELETE"],
    allow_headers=["*"],
)


@app.get("/")
async def root():
    return {
        "name": "Prophecy AniZone Server",
        "status": "running",
        "version": "1.0.0"
    }


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "service": "prophecy-anizone-server",
        "timestamp": datetime.now(timezone.utc).isoformat()
    }


@app.get("/api/v1/manifest")
async def manifest():
    return {
        "name": "Prophecy AniZone Server",
        "version": "1.0.0",
        "protocol": "prophecy-provider-v1",
        "status": "active",
        "provider_type": "sub_server",
        "capabilities": [
            "search",
            "details",
            "episodes",
            "sources",
            "resolve",
            "custom_routes"
        ],
        "endpoints": {
            "health": "/health",
            "manifest": "/api/v1/manifest",
            "providers": "/api/v1/providers"
        }
    }


@app.get("/api/v1/providers")
async def list_providers():
    return {
        "count": 1,
        "providers": [
            {
                "id": "anizone",
                "name": "AniZone",
                "enabled": True,
                "status": "pending",
                "capabilities": [
                    "search",
                    "details",
                    "episodes",
                    "sources",
                    "resolve"
                ]
            }
        ]
    }
