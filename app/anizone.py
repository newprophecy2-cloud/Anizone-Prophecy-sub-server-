
from __future__ import annotations

import asyncio
import html
import json
import re

from dataclasses import dataclass
from typing import Any, Optional
from urllib.parse import urljoin, urlparse

import httpx

from app.config import settings


class AniZoneError(Exception):
    def __init__(
        self,
        message: str,
        code: str = "ANIZONE_ERROR",
    ):
        self.message = message
        self.code = code
        super().__init__(message)


@dataclass
class SearchItem:
    slug: str
    title: str
    media_type: str
    year: Optional[int] = None
    poster: Optional[str] = None
    episode_count: Optional[int] = None


@dataclass
class EpisodeItem:
    id: str
    number: int
    title: Optional[str] = None
    description: Optional[str] = None
    season: Optional[int] = None


@dataclass
class EpisodePage:
    items: list
    snapshot: str
    cursor: Optional[str]
    has_more: bool
    csrf: str
    cookies: dict
    series_title: Optional[str] = None


class AniZoneClient:

    def __init__(self):
        configured_url = str(
            settings.ANIZONE_BASE_URL
        ).rstrip("/")

        parsed = urlparse(configured_url)

        if parsed.scheme != "https" or not parsed.hostname:
            raise AniZoneError(
                "AniZone base URL must use HTTPS",
                "INVALID_BASE_URL",
            )

        self.base_url = configured_url
        self.timeout = settings.REQUEST_TIMEOUT
        self.max_retries = settings.REQUEST_RETRIES
        self.max_response_bytes = settings.MAX_RESPONSE_BYTES
        self.max_pages = settings.MAX_PAGINATION_PAGES

        self.client = httpx.AsyncClient(
            timeout=httpx.Timeout(self.timeout),
            follow_redirects=True,
            headers={
                "User-Agent": settings.USER_AGENT,
                "Accept-Language": "en-US,en;q=0.9",
                "Accept": (
                    "text/html,application/xhtml+xml,"
                    "application/xml;q=0.9,*/*;q=0.8"
                ),
            },
        )

    async def close(self):
        if not self.client.is_closed:
            await self.client.aclose()

    def _url(self, path: str) -> str:
        return urljoin(
            self.base_url + "/",
            path.lstrip("/"),
        )

    async def _request(
        self,
        method: str,
        path: str,
        *,
        params=None,
        json_body=None,
        headers=None,
    ) -> httpx.Response:

        url = self._url(path)
        last_error = None

        for attempt in range(self.max_retries + 1):
            try:
                response = await self.client.request(
                    method,
                    url,
                    params=params,
                    json=json_body,
                    headers=headers,
                )

                response.raise_for_status()

                if len(response.content) > self.max_response_bytes:
                    raise AniZoneError(
                        "Response exceeded configured size limit",
                        "RESPONSE_TOO_LARGE",
                    )

                return response

            except AniZoneError:
                raise

            except httpx.HTTPStatusError as exc:
                status = exc.response.status_code

                if status == 404:
                    raise AniZoneError(
                        "AniZone item not found",
                        "NOT_FOUND",
                    ) from exc

                if status not in (408, 429, 500, 502, 503, 504):
                    raise AniZoneError(
                        f"AniZone HTTP error: {status}",
                        "UPSTREAM_HTTP_ERROR",
                    ) from exc

                last_error = exc

            except httpx.TimeoutException as exc:
                last_error = exc

            except httpx.RequestError as exc:
                last_error = exc

            if attempt < self.max_retries:
                await asyncio.sleep(
                    min(0.5 * (attempt + 1), 3.0)
                )

        raise AniZoneError(
            f"AniZone request failed: {last_error}",
            "NETWORK_ERROR",
        )

    async def _get_page(self, path: str, params=None):
        response = await self._request(
            "GET",
            path,
            params=params,
            headers={
                "Referer": self.base_url + "/",
            },
        )

        try:
            content = response.content.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise AniZoneError(
                "AniZone response is not UTF-8",
                "INVALID_ENCODING",
            ) from exc

        cookies = response.headers.get_list("set-cookie")

        return content, cookies

    # --------------------------------------------------
    # SEARCH
    # --------------------------------------------------

    async def search(self, query: str, page: int = 1):

        query = query.strip()

        if not query:
            return []

        if page < 1 or page > self.max_pages:
            raise AniZoneError(
                "Invalid search page",
                "INVALID_PAGE",
            )

        content, cookies = await self._get_page(
            "/anime",
            params={"search": query},
        )

        state = self._initial_page(content, cookies)
        seen_cursors = set()

        for _ in range(1, page):

            if not state.has_more:
                state.items = []
                break

            if not state.cursor:
                raise AniZoneError(
                    "Missing search pagination cursor",
                    "PAGINATION_ERROR",
                )

            if state.cursor in seen_cursors:
                raise AniZoneError(
                    "Repeated search pagination cursor",
                    "PAGINATION_ERROR",
                )

            seen_cursors.add(state.cursor)

            state = await self._load_page(
                "/anime",
                state,
            )

        parsed = self._parse_search_items(state.items)

        return [
            {
                "id": item.slug,
                "provider": "anizone",
                "title": item.title,
                "media_type": item.media_type,
                "year": item.year,
                "poster": item.poster,
                "episode_count": item.episode_count,
                "season_count": None,
            }
            for item in parsed
        ]

    # --------------------------------------------------
    # DETAILS
    # --------------------------------------------------

    async def details(self, slug: str):

        self._validate_slug(slug)

        state = await self._scrape_series(slug)
        episodes = self._parse_episodes(state.items)

        seasons = {}

        for episode in episodes:
            season_number = episode.season or 1

            seasons.setdefault(
                season_number,
                [],
            ).append({
                "id": episode.id,
                "number": episode.number,
                "season": season_number,
                "title": episode.title,
                "description": episode.description,
                "provider_episode_id": episode.id,
                "provider_season_id": str(season_number),
            })

        season_data = [
            {
                "number": season_number,
                "provider_season_id": str(season_number),
                "episodes": sorted(
                    season_episodes,
                    key=lambda item: item["number"],
                ),
            }
            for season_number, season_episodes in sorted(
                seasons.items()
            )
        ]

        return {
            "id": slug,
            "provider": "anizone",
            "title": state.series_title or slug.replace("-", " "),
            "media_type": "series",
            "year": None,
            "description": None,
            "poster": None,
            "genres": [],
            "season_count": len(season_data),
            "seasons": season_data,
        }

    # --------------------------------------------------
    # EPISODES
    # --------------------------------------------------

    async def episodes(self, slug: str):

        self._validate_slug(slug)

        state = await self._scrape_series(slug)
        parsed = self._parse_episodes(state.items)

        return {
            "provider": "anizone",
            "anime_id": slug,
            "title": state.series_title,
            "episodes": [
                {
                    "id": item.id,
                    "number": item.number,
                    "season": item.season or 1,
                    "title": item.title,
                    "description": item.description,
                }
                for item in parsed
            ],
        }

    # --------------------------------------------------
    # SERIES PAGINATION
    # --------------------------------------------------

    async def _scrape_series(self, slug: str):

        content, cookies = await self._get_page(
            f"/anime/{slug}"
        )

        state = self._initial_page(content, cookies)

        seen_cursors = set()
        page_count = 1

        while state.has_more and state.cursor:

            if state.cursor in seen_cursors:
                raise AniZoneError(
                    "Repeated episode pagination cursor",
                    "PAGINATION_ERROR",
                )

            seen_cursors.add(state.cursor)

            next_state = await self._load_page(
                f"/anime/{slug}",
                state,
            )

            next_state.items = self._merge_items(
                state.items,
                next_state.items,
            )

            state = next_state
            page_count += 1

            if page_count >= self.max_pages and state.has_more:
                raise AniZoneError(
                    "Episode pagination limit exceeded",
                    "PAGINATION_LIMIT",
                )

        if not state.items:
            raise AniZoneError(
                "No episodes found",
                "NOT_FOUND",
            )

        return state

    async def _load_page(
        self,
        referer_path: str,
        state: EpisodePage,
    ):

        if not state.cursor:
            raise AniZoneError(
                "Missing pagination cursor",
                "PAGINATION_ERROR",
            )

        body = {
            "components": [
                {
                    "snapshot": state.snapshot,
                    "updates": {},
                    "calls": [
                        {
                            "path": "",
                            "method": "loadPage",
                            "params": [state.cursor],
                        }
                    ],
                }
            ]
        }

        headers = {
            "User-Agent": settings.USER_AGENT,
            "Accept": "application/json, text/plain, */*",
            "Content-Type": "application/json",
            "X-Livewire": "",
            "X-CSRF-TOKEN": state.csrf,
            "X-Requested-With": "XMLHttpRequest",
            "Origin": self.base_url,
            "Referer": self._url(referer_path),
            "Cookie": self._cookie_header(state.cookies),
        }

        response = await self._request(
            "POST",
            "/livewire/update",
            json_body=body,
            headers=headers,
        )

        try:
            value = response.json()
        except ValueError as exc:
            raise AniZoneError(
                "Invalid Livewire JSON",
                "INVALID_RESPONSE",
            ) from exc

        components = value.get("components")

        if not isinstance(components, list) or not components:
            raise AniZoneError(
                "Missing Livewire component",
                "PAGINATION_ERROR",
            )

        component = components[0]

        if not isinstance(component, dict):
            raise AniZoneError(
                "Invalid Livewire component",
                "PAGINATION_ERROR",
            )

        snapshot = component.get("snapshot")
        effects = component.get("effects") or {}
        dispatches = effects.get("dispatches")

        if not isinstance(snapshot, str):
            raise AniZoneError(
                "Missing Livewire snapshot",
                "PAGINATION_ERROR",
            )

        if not isinstance(dispatches, list):
            raise AniZoneError(
                "Missing Livewire dispatch",
                "PAGINATION_ERROR",
            )

        dispatch = next(
            (
                item for item in dispatches
                if isinstance(item, dict)
                and item.get("name") == "items-loaded"
            ),
            None,
        )

        if dispatch is None:
            raise AniZoneError(
                "Missing items-loaded event",
                "PAGINATION_ERROR",
            )

        params = dispatch.get("params") or {}
        items = params.get("items")

        if not isinstance(items, list):
            raise AniZoneError(
                "Invalid pagination items",
                "PAGINATION_ERROR",
            )

        cookies = self._merge_cookies(
            state.cookies,
            response.headers.get_list("set-cookie"),
        )

        return EpisodePage(
            items=items,
            snapshot=snapshot,
            cursor=params.get("nextCursor"),
            has_more=bool(params.get("hasMore", False)),
            csrf=state.csrf,
            cookies=cookies,
            series_title=state.series_title,
        )

    # --------------------------------------------------
    # INITIAL LIVEWIRE STATE
    # --------------------------------------------------

    def _initial_page(self, content: str, cookies: list):

        items_value = self._json_argument(
            content,
            "items",
        )

        if not isinstance(items_value, list):
            raise AniZoneError(
                "Initial items payload not found",
                "PARSE_ERROR",
            )

        marker = content.find("items: JSON.parse('")

        if marker < 0:
            raise AniZoneError(
                "Livewire items marker not found",
                "PARSE_ERROR",
            )

        snapshot = self._html_attribute_before(
            content,
            marker,
        )

        csrf = self._csrf_token(content)

        if not snapshot or not csrf:
            raise AniZoneError(
                "Livewire state is incomplete",
                "PARSE_ERROR",
            )

        titles = self._json_argument(
            content,
            "anmTitles",
        )

        return EpisodePage(
            items=items_value,
            snapshot=snapshot,
            cursor=self._js_property_string(
                content,
                "nextCursor",
            ),
            has_more=(
                self._js_property_bool(content, "hasMore")
                or False
            ),
            csrf=csrf,
            cookies=self._merge_cookies({}, cookies),
            series_title=self._pick_title(titles),
        )

    # --------------------------------------------------
    # SEARCH PARSER
    # --------------------------------------------------

    def _parse_search_items(self, items: list):

        results = []

        for item in items:

            if not isinstance(item, dict):
                continue

            slug = item.get("slug")

            if not isinstance(slug, str):
                continue

            if not re.fullmatch(r"[A-Za-z0-9-]{1,180}", slug):
                continue

            title = (
                self._pick_title(item.get("title_list"))
                or item.get("main_title")
            )

            if not isinstance(title, str) or not title.strip():
                continue

            media_type = str(
                item.get("type") or ""
            ).lower()

            year = item.get("start_year")
            if not isinstance(year, int):
                year = None

            cover = item.get("cover")
            if not isinstance(cover, str):
                cover = None

            count = item.get("episode_count")
            if not isinstance(count, int):
                count = None

            results.append(
                SearchItem(
                    slug=slug,
                    title=title.strip(),
                    media_type=(
                        "movie"
                        if media_type == "movie"
                        else "series"
                    ),
                    year=year,
                    poster=cover,
                    episode_count=count,
                )
            )

        return results

    # --------------------------------------------------
    # EPISODE PARSER
    # --------------------------------------------------

    def _parse_episodes(self, items: list):

        results = []
        seen = set()

        for item in items:

            if not isinstance(item, dict):
                continue

            source_number = None
            raw_slug = item.get("slug")

            if isinstance(raw_slug, str) and raw_slug.isdigit():
                source_number = int(raw_slug)

            if source_number is None:
                raw_url = item.get("url")

                if isinstance(raw_url, str):
                    last = raw_url.rstrip("/").rsplit("/", 1)[-1]

                    if last.isdigit():
                        source_number = int(last)

            if not source_number:
                continue

            season = self._parse_season(item)
            identity = (season or 1, source_number)

            if identity in seen:
                continue

            seen.add(identity)

            title = self._pick_title(
                item.get("title_list")
            ) or f"Episode {source_number}"

            summary = item.get("summary")

            if not isinstance(summary, str):
                summary = None

            results.append(
                EpisodeItem(
                    id=str(source_number),
                    number=source_number,
                    title=title,
                    description=summary,
                    season=season,
                )
            )

        results.sort(
            key=lambda item: (
                item.season or 1,
                item.number,
            )
        )

        return results

    @staticmethod
    def _parse_season(item):

        for key in (
            "season",
            "season_number",
            "seasonNumber",
        ):
            value = item.get(key)

            if isinstance(value, int) and value > 0:
                return value

        return None

    # --------------------------------------------------
    # JAVASCRIPT / JSON HELPERS
    # --------------------------------------------------

    @staticmethod
    def _extract_js_string(content: str, marker: str):

        start = content.find(marker)

        if start < 0:
            return None

        start += len(marker)

        output = []
        escaped = False

        for char in content[start:]:

            if not escaped and char == "'":
                break

            if escaped:
                output.append("\\")
                output.append(char)
                escaped = False

            elif char == "\\":
                escaped = True

            else:
                output.append(char)

        if escaped:
            output.append("\\")

        return "".join(output)

    @staticmethod
    def _decode_js_string(raw: str):

        output = []
        index = 0

        while index < len(raw):

            char = raw[index]

            if char != "\\":
                output.append(char)
                index += 1
                continue

            index += 1

            if index >= len(raw):
                break

            char = raw[index]

            escapes = {
                "n": "\n",
                "r": "\r",
                "t": "\t",
                "b": "\b",
                "f": "\f",
                "v": "\v",
                "0": "\0",
            }

            if char in escapes:
                output.append(escapes[char])

            elif char == "u" and index + 4 < len(raw):
                code = raw[index + 1:index + 5]

                try:
                    output.append(chr(int(code, 16)))
                    index += 4
                except ValueError:
                    output.append("u")

            elif char == "x" and index + 2 < len(raw):
                code = raw[index + 1:index + 3]

                try:
                    output.append(chr(int(code, 16)))
                    index += 2
                except ValueError:
                    output.append("x")

            else:
                output.append(char)

            index += 1

        return "".join(output)

    @classmethod
    def _json_argument(cls, content: str, name: str):

        marker = f"{name}: JSON.parse('")

        raw = cls._extract_js_string(content, marker)

        if raw is None:
            return None

        try:
            return json.loads(cls._decode_js_string(raw))
        except (ValueError, TypeError):
            return None

    # --------------------------------------------------
    # HTML / LIVEWIRE HELPERS
    # --------------------------------------------------

    @staticmethod
    def _pick_title(value):

        if not isinstance(value, dict):
            return None

        for key in ("1", "5", "8"):
            title = value.get(key)

            if isinstance(title, str) and title.strip():
                return title.strip()

        for title in value.values():
            if isinstance(title, str) and title.strip():
                return title.strip()

        return None

    @staticmethod
    def _csrf_token(content):

        patterns = [
            r'<meta[^>]+name=["\']csrf-token["\'][^>]+content=["\']([^"\']+)',
            r'<meta[^>]+content=["\']([^"\']+)["\'][^>]+name=["\']csrf-token',
        ]

        for pattern in patterns:
            match = re.search(pattern, content, re.I)

            if match:
                return html.unescape(match.group(1))

        return None

    @staticmethod
    def _html_attribute_before(content: str, before: int):

        prefix = content[:before]
        marker = 'wire:snapshot="'
        start = prefix.rfind(marker)

        if start < 0:
            return None

        start += len(marker)
        end = prefix.find('"', start)

        if end < 0:
            return None

        return html.unescape(prefix[start:end])

    @staticmethod
    def _js_property_string(content: str, name: str):

        match = re.search(
            rf"\b{re.escape(name)}\s*:\s*(['\"])(.*?)\1",
            content,
            re.S,
        )

        if not match:
            return None

        return AniZoneClient._decode_js_string(
            match.group(2)
        )

    @staticmethod
    def _js_property_bool(content: str, name: str):

        match = re.search(
            rf"\b{re.escape(name)}\s*:\s*(true|false)",
            content,
        )

        if not match:
            return None

        return match.group(1) == "true"

    # --------------------------------------------------
    # COOKIE HELPERS
    # --------------------------------------------------

    @staticmethod
    def _merge_cookies(existing: dict, incoming: list):

        result = dict(existing)

        for cookie in incoming:

            if not isinstance(cookie, str):
                continue

            name, separator, rest = cookie.partition("=")

            if not separator:
                continue

            result[name.strip()] = (
                rest.split(";", 1)[0].strip()
            )

        return result

    @staticmethod
    def _cookie_header(cookies: dict):

        return "; ".join(
            f"{name}={value}"
            for name, value in cookies.items()
        )

    # --------------------------------------------------
    # VALIDATION
    # --------------------------------------------------

    @staticmethod
    def _validate_slug(slug: str):

        if not isinstance(slug, str):
            raise AniZoneError(
                "Invalid AniZone ID",
                "INVALID_ID",
            )

        if not re.fullmatch(r"[A-Za-z0-9-]{1,180}", slug):
            raise AniZoneError(
                "Invalid AniZone ID",
                "INVALID_ID",
            )

    @staticmethod
    def _merge_items(existing: list, incoming: list):

        merged = list(existing)

        def identity(item):
            if not isinstance(item, dict):
                return None

            return item.get("slug") or item.get("url")

        seen = {
            identity(item)
            for item in merged
            if identity(item) is not None
        }

        for item in incoming:

            item_id = identity(item)

            if item_id is None or item_id not in seen:
                merged.append(item)

                if item_id is not None:
                    seen.add(item_id)

        return merged

    # --------------------------------------------------
    # PROVIDER METADATA
    # --------------------------------------------------

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
            ],
        }


anizone = AniZoneClient()
