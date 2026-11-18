from dataclasses import dataclass
from typing import Optional
from urllib.parse import urljoin
import re

import httpx

from app.config import settings


@dataclass
class AniZoneItem:
    id: str
    title: str
    media_type: str
    year: Optional[int] = None
    poster: Optional[str] = None


@dataclass
class AniZoneEpisode:
    id: str
    number: int
    title: str
    season: int = 1


class AniZoneError(Exception):
    pass


class AniZoneClient:
    BASE_URL = "https://anizone.to"

    def __init__(self):
        self.base_url = (
            settings.ANIZONE_BASE_URL or self.BASE_URL
        ).rstrip("/")

        self.timeout = settings.REQUEST_TIMEOUT

        self.client = httpx.AsyncClient(
            timeout=self.timeout,
            follow_redirects=True,
            headers={
                "User-Agent": (
                    "Mozilla/5.0 "
                    "(X11; Linux x86_64) "
                    "AppleWebKit/537.36 "
                    "Chrome/131.0.0.0 Safari/537.36"
                ),
                "Accept-Language": "en-US,en;q=0.9",
            },
        )

    async def close(self):
        await self.client.aclose()

    async def _get(self, path: str):
        url = urljoin(self.base_url + "/", path.lstrip("/"))

        try:
            response = await self.client.get(url)
            response.raise_for_status()
            return response.text

        except httpx.HTTPError as exc:
            raise AniZoneError(
                f"AniZone request failed: {exc}"
            ) from exc

    async def search(self, query: str):
        query = query.strip()

        if not query:
            return []

        html = await self._get(
            "/anime?search=" + httpx.QueryParams(
                {"search": query}
            ).get("search", "")
        )

        # Search parsing is intentionally strict.
        # Add the verified AniZone HTML/JSON schema here.
        return {
            "provider": "anizone",
            "query": query,
            "status": "parser_pending",
            "message": "Search response received; parser needs verified fixtures.",
        }

    async def details(self, slug: str):
        self._validate_slug(slug)

        html = await self._get(f"/anime/{slug}")

        return {
            "provider": "anizone",
            "id": slug,
            "status": "parser_pending",
            "message": "Details response received; parser needs verified fixtures.",
        }

    async def episodes(self, slug: str):
        self._validate_slug(slug)

        html = await self._get(f"/anime/{slug}")

        return {
            "provider": "anizone",
            "anime_id": slug,
            "status": "parser_pending",
            "episodes": [],
        }

    @staticmethod
    def _validate_slug(slug: str):
        if not re.fullmatch(r"[A-Za-z0-9-]{1,180}", slug):
            raise AniZoneError("Invalid AniZone media ID")

    def metadata(self):
        return {
            "id": "anizone",
            "name": "AniZone",
            "enabled": settings.ANIZONE_ENABLED,
            "base_url": self.base_url,
            "capabilities": [
                "search",
                "details",
                "episodes",
                "sources",
                "resolve",
            ],
        }


anizone = AniZoneClient()
