
from typing import Any

from app.config import settings
from app.anizone import anizone, AniZoneError


class ProviderEngine:

    def __init__(self):
        self.providers = {
            "anizone": anizone,
        }

    def list_providers(self):
        return [
            provider.metadata()
            for provider in self.providers.values()
        ]

    async def execute(
        self,
        provider_id: str,
        action: str,
        **params: Any,
    ):
        provider = self.providers.get(provider_id)

        if provider is None:
            return {
                "status": "error",
                "code": "PROVIDER_NOT_FOUND",
                "provider": provider_id,
            }

        metadata = provider.metadata()

        if not metadata["enabled"]:
            return {
                "status": "error",
                "code": "PROVIDER_DISABLED",
                "provider": provider_id,
            }

        if action not in metadata["capabilities"]:
            return {
                "status": "error",
                "code": "UNSUPPORTED_ACTION",
                "provider": provider_id,
                "action": action,
            }

        try:
            if action == "search":
                result = await provider.search(
                    params.get("query", "")
                )

            elif action == "details":
                result = await provider.details(
                    params.get("slug", "")
                )

            elif action == "episodes":
                result = await provider.episodes(
                    params.get("slug", "")
                )

            else:
                return {
                    "status": "unavailable",
                    "code": "ACTION_NOT_IMPLEMENTED",
                    "provider": provider_id,
                    "action": action,
                }

            return result

        except AniZoneError as exc:
            return {
                "status": "error",
                "code": "PROVIDER_REQUEST_FAILED",
                "provider": provider_id,
                "message": str(exc),
            }


engine = ProviderEngine()
