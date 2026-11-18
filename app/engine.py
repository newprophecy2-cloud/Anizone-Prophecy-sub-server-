
from __future__ import annotations

import inspect
import logging
from typing import Any

from app.config import settings
from app import anizone

logger = logging.getLogger(__name__)


class ProviderEngine:

    ACTIONS = {
        "search",
        "details",
        "episodes",
    }

    ERROR_RETRYABILITY = {
        "NETWORK_ERROR": True,
        "UPSTREAM_HTTP_ERROR": True,
        "NOT_FOUND": False,
        "INVALID_ID": False,
        "INVALID_PAGE": False,
        "INVALID_PARAMETERS": False,
        "PARSE_ERROR": False,
        "INVALID_RESPONSE": False,
        "RESPONSE_TOO_LARGE": False,
        "INVALID_BASE_URL": False,
        "PAGINATION_ERROR": False,
        "PAGINATION_LIMIT": False,
        "PROVIDER_DISABLED": False,
        "UNKNOWN_ACTION": False,
    }

    def __init__(self) -> None:
        self.providers = {
            "anizone": anizone,
        }

    def _enabled(self, provider_id: str) -> bool:
        if provider_id == "anizone":
            return settings.ANIZONE_ENABLED

        return False

    def _get_provider(self, provider_id: str):
        provider = self.providers.get(provider_id)

        if provider is None:
            return None

        return provider

    def _provider_metadata(self, provider_id: str) -> dict[str, Any]:
        provider = self._get_provider(provider_id)

        if provider is None:
            return {}

        metadata = getattr(provider, "metadata", {})

        if callable(metadata):
            metadata = metadata()

        if not isinstance(metadata, dict):
            metadata = {}

        result = dict(metadata)

        result["id"] = provider_id
        result["enabled"] = self._enabled(provider_id)
        result["actions"] = [
            action
            for action in self.ACTIONS
            if callable(getattr(provider, action, None))
        ]

        return result

    def list_providers(self) -> list[dict[str, Any]]:
        return [
            self._provider_metadata(provider_id)
            for provider_id in self.providers
        ]

    def get_provider(self, provider_id: str) -> dict[str, Any] | None:
        if provider_id not in self.providers:
            return None

        return self._provider_metadata(provider_id)

    async def execute(
        self,
        provider_id: str,
        action: str,
        params: dict[str, Any] | None = None,
    ) -> dict[str, Any]:

        params = params or {}

        if not isinstance(params, dict):
            return self._error(
                "INVALID_PARAMETERS",
                "Parameters must be an object.",
            )

        provider = self._get_provider(provider_id)

        if provider is None:
            return self._error(
                "PROVIDER_NOT_FOUND",
                f"Provider '{provider_id}' was not found.",
            )

        if not self._enabled(provider_id):
            return self._error(
                "PROVIDER_DISABLED",
                f"Provider '{provider_id}' is disabled.",
            )

        if action not in self.ACTIONS:
            return self._error(
                "UNKNOWN_ACTION",
                f"Action '{action}' is not supported.",
            )

        method = getattr(provider, action, None)

        if not callable(method):
            return self._error(
                "ACTION_NOT_IMPLEMENTED",
                f"Action '{action}' is not implemented.",
            )

        try:
            result = method(**params)

            if inspect.isawaitable(result):
                result = await result

            return {
                "status": "success",
                "provider": provider_id,
                "action": action,
                "data": result,
            }

        except Exception as exc:
            code = getattr(exc, "code", None)

            if not isinstance(code, str) or not code:
                if isinstance(exc, TypeError):
                    code = "INVALID_PARAMETERS"
                else:
                    code = "INTERNAL_ERROR"

            retryable = self.ERROR_RETRYABILITY.get(
                code,
                False,
            )

            if code == "INTERNAL_ERROR":
                logger.exception(
                    "Unexpected provider error: %s / %s",
                    provider_id,
                    action,
                )
                message = "An internal provider error occurred."
            else:
                message = str(exc)

            return self._error(
                code,
                message,
                provider=provider_id,
                action=action,
                retryable=retryable,
            )

    @staticmethod
    def _error(
        code: str,
        message: str,
        provider: str | None = None,
        action: str | None = None,
        retryable: bool = False,
    ) -> dict[str, Any]:

        return {
            "status": "error",
            "error": {
                "code": code,
                "message": message,
                "provider": provider,
                "action": action,
                "retryable": retryable,
            },
        }


engine = ProviderEngine()
