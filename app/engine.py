
from __future__ import annotations

import os
from dataclasses import asdict, is_dataclass
from enum import Enum
from inspect import isawaitable
from typing import Any

from app.anizone import AniZoneError, anizone


class ProviderEngine:
    """
    Central provider manager.

    Responsibilities:
    - Provider registration
    - Provider enable/disable checks
    - Capability validation
    - Action dispatch
    - Standardized error responses
    """

    ACTIONS = {
        "search",
        "details",
        "episodes",
    }

    def __init__(self) -> None:
        self.providers: dict[str, Any] = {
            "anizone": anizone,
        }

    @staticmethod
    def _enabled(provider_id: str, provider: Any) -> bool:
        env_name = f"{provider_id.upper()}_ENABLED"
        value = os.getenv(env_name, "true").strip().lower()

        if value in {"false", "0", "no", "off"}:
            return False

        return True

    @staticmethod
    def _metadata(provider_id: str, provider: Any) -> dict:
        metadata = getattr(provider, "metadata", {})

        if callable(metadata):
            metadata = metadata()

        if not isinstance(metadata, dict):
            metadata = {}

        return {
            "id": provider_id,
            "name": metadata.get("name", provider_id.title()),
            "enabled": ProviderEngine._enabled(provider_id, provider),
            "capabilities": [
                action
                for action in ProviderEngine.ACTIONS
                if callable(getattr(provider, action, None))
            ],
        }

    @staticmethod
    def _serialize(value: Any) -> Any:
        """Convert provider results into JSON-compatible data."""

        if is_dataclass(value) and not isinstance(value, type):
            return ProviderEngine._serialize(asdict(value))

        if isinstance(value, Enum):
            return value.value

        if isinstance(value, dict):
            return {
                str(key): ProviderEngine._serialize(item)
                for key, item in value.items()
            }

        if isinstance(value, (list, tuple, set)):
            return [
                ProviderEngine._serialize(item)
                for item in value
            ]

        if hasattr(value, "model_dump"):
            return ProviderEngine._serialize(value.model_dump())

        if hasattr(value, "dict") and callable(value.dict):
            return ProviderEngine._serialize(value.dict())

        if value is None or isinstance(
            value, (str, int, float, bool)
        ):
            return value

        return str(value)

    @staticmethod
    def _success(
        provider_id: str,
        action: str,
        data: Any,
    ) -> dict:
        return {
            "status": "ok",
            "provider": provider_id,
            "action": action,
            "data": ProviderEngine._serialize(data),
        }

    @staticmethod
    def _error(
        code: str,
        message: str,
        provider_id: str | None = None,
        action: str | None = None,
        retryable: bool = False,
    ) -> dict:
        return {
            "status": "error",
            "error": {
                "code": code,
                "message": message,
                "provider": provider_id,
                "action": action,
                "retryable": retryable,
            },
        }

    def list_providers(self) -> dict:
        items = [
            self._metadata(provider_id, provider)
            for provider_id, provider in self.providers.items()
        ]

        return {
            "status": "ok",
            "count": len(items),
            "providers": items,
        }

    def get_provider(self, provider_id: str) -> Any | None:
        return self.providers.get(provider_id.lower().strip())

    async def execute(
        self,
        provider_id: str,
        action: str,
        **params: Any,
    ) -> dict:
        provider_id = provider_id.lower().strip()
        action = action.lower().strip()

        provider = self.get_provider(provider_id)

        if provider is None:
            return self._error(
                code="PROVIDER_NOT_FOUND",
                message=f"Provider '{provider_id}' is not registered.",
                provider_id=provider_id,
                action=action,
            )

        if not self._enabled(provider_id, provider):
            return self._error(
                code="PROVIDER_DISABLED",
                message=f"Provider '{provider_id}' is disabled.",
                provider_id=provider_id,
                action=action,
            )

        if action not in self.ACTIONS:
            return self._error(
                code="UNSUPPORTED_ACTION",
                message=f"Action '{action}' is not supported.",
                provider_id=provider_id,
                action=action,
            )

        handler = getattr(provider, action, None)

        if not callable(handler):
            return self._error(
                code="CAPABILITY_UNAVAILABLE",
                message=f"Provider does not implement '{action}'.",
                provider_id=provider_id,
                action=action,
            )

        try:
            result = handler(**params)

            if isawaitable(result):
                result = await result

            return self._success(
                provider_id=provider_id,
                action=action,
                data=result,
            )

        except AniZoneError as exc:
            error_name = type(exc).__name__

            if error_name == "NotFound":
                code = "NOT_FOUND"
                retryable = False
            elif error_name in {"Network", "Timeout"}:
                code = "UPSTREAM_ERROR"
                retryable = True
            else:
                code = "PROVIDER_ERROR"
                retryable = False

            return self._error(
                code=code,
                message=str(exc),
                provider_id=provider_id,
                action=action,
                retryable=retryable,
            )

        except TypeError as exc:
            return self._error(
                code="INVALID_PARAMETERS",
                message=str(exc),
                provider_id=provider_id,
                action=action,
            )

        except Exception:
            return self._error(
                code="INTERNAL_ERROR",
                message="An unexpected provider error occurred.",
                provider_id=provider_id,
                action=action,
                retryable=False,
            )


engine = ProviderEngine()
