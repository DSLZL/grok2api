"""
Reverse interface: LiveKit token + WebSocket.
"""

import orjson
from typing import Any, Dict
from urllib.parse import urlencode
from curl_cffi.requests import AsyncSession

from app.core.logger import logger
from app.core.config import get_config
from app.core.exceptions import UpstreamException
from app.services.token.service import TokenService
from app.services.reverse.utils.headers import build_headers
from app.services.reverse.utils.retry import retry_on_status
from app.services.reverse.utils.websocket import WebSocketClient, WebSocketConnection

LIVEKIT_TOKEN_API = "https://grok.com/rest/livekit/tokens"
LIVEKIT_WS_URL = "wss://livekit.grok.com"


class LivekitTokenReverse:
    """/rest/livekit/tokens reverse interface."""

    @staticmethod
    async def request(
        session: AsyncSession,
        token: str,
        voice: str = "ara",
        personality: str = "assistant",
        speed: float = 1.0,
        livekit_url: str = LIVEKIT_WS_URL,
    ) -> Dict[str, Any]:
        """Fetch LiveKit token.
        
        Args:
            session: AsyncSession, the session to use for the request.
            token: str, the SSO token.
            voice: str, the voice to use for the request.
            personality: str, the personality to use for the request.
            speed: float, the speed to use for the request.
            livekit_url: str, the LiveKit URL to use for the request.

        Returns:
            Dict[str, Any]: The LiveKit token.
        """
        try:
            # Get proxies
            base_proxy = get_config("network.base_proxy_url")
            proxies = {"http": base_proxy, "https": base_proxy} if base_proxy else None

            # Build headers
            headers = build_headers(
                cookie_token=token,
                content_type="application/json",
                origin="https://grok.com",
                referer="https://grok.com/",
            )

            # Build payload
            payload = {
                "sessionPayload": orjson.dumps(
                    {
                        "voice": voice,
                        "personality": personality,
                        "playback_speed": speed,
                        "enable_vision": False,
                        "turn_detection": {"type": "server_vad"},
                    }
                ).decode(),
                "requestAgentDispatch": False,
                "livekitUrl": livekit_url,
                "params": {"enable_markdown_transcript": "true"},
            }

            # Curl Config
            timeout = get_config("network.timeout")
            browser = get_config("security.browser")

            async def _do_request():
                response = await session.post(
                    LIVEKIT_TOKEN_API,
                    headers=headers,
                    data=orjson.dumps(payload),
                    timeout=timeout,
                    proxies=proxies,
                    impersonate=browser,
                )

                if response.status_code != 200:
                    body = response.text[:200]
                    logger.error(
                        f"LivekitTokenReverse: Request failed, {response.status_code}, body={body}"
                    )
                    raise UpstreamException(
                        message=f"LivekitTokenReverse: Request failed, {response.status_code}",
                        details={"status": response.status_code, "body": response.text},
                    )

                return response

            return await retry_on_status(_do_request)


        except Exception as e:
            # Handle upstream exception
            if isinstance(e, UpstreamException):
                status = None
                if e.details and "status" in e.details:
                    status = e.details["status"]
                else:
                    status = getattr(e, "status_code", None)
                if status == 401:
                    try:
                        await TokenService.record_fail(
                            token, status, "livekit_token_auth_failed"
                        )
                    except Exception:
                        pass
                raise

            # Handle other non-upstream exceptions
            logger.error(
                f"LivekitTokenReverse: Request failed, {str(e)}",
                extra={"error_type": type(e).__name__},
            )
            raise UpstreamException(
                message=f"LivekitTokenReverse: Request failed, {str(e)}",
                details={"status": 502, "error": str(e)},
            )


class LivekitWebSocketReverse:
    """LiveKit WebSocket reverse interface."""

    def __init__(self) -> None:
        self._client = WebSocketClient()

    def build_url(
        self,
        access_token: str,
        *,
        livekit_url: str = LIVEKIT_WS_URL,
        auto_subscribe: bool = True,
        sdk: str = "js",
        version: str = "2.11.4",
        protocol: int = 15,
    ) -> str:
        """Build LiveKit WebSocket URL.
        
        Args:
            access_token: str, the LiveKit access token.
            livekit_url: str, the LiveKit URL to use for the request.
            auto_subscribe: bool, whether to auto subscribe to the WebSocket.
            sdk: str, the SDK to use for the request.
            version: str, the version to use for the request.
            protocol: int, the protocol to use for the request.

        Returns:
            str: The LiveKit WebSocket URL.
        """
        # Build base URL
        base = livekit_url.rstrip("/")
        if not base.endswith("/rtc"):
            base = f"{base}/rtc"

        # Build parameters
        params = {
            "access_token": access_token,
            "auto_subscribe": str(int(auto_subscribe)),
            "sdk": sdk,
            "version": version,
            "protocol": str(protocol),
        }

        return f"{base}?{urlencode(params)}"

    def _build_headers(self, extra: Dict[str, str] | None = None) -> Dict[str, str]:
        """Build LiveKit WebSocket headers."""
        # Build headers
        headers = {
            "Origin": "https://grok.com",
            "User-Agent": get_config("security.user_agent"),
            "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
            "Cache-Control": "no-cache",
            "Pragma": "no-cache",
        }

        # Update headers
        if extra:
            headers.update(extra)
        return headers

    async def connect(
        self,
        access_token: str,
        *,
        livekit_url: str = LIVEKIT_WS_URL,
        auto_subscribe: bool = True,
        sdk: str = "js",
        version: str = "2.11.4",
        protocol: int = 15,
        headers: Dict[str, str] | None = None,
        timeout: float | None = None,
    ) -> WebSocketConnection:
        """Connect to the LiveKit WebSocket.
        
        Args:
            access_token: str, the LiveKit access token.
            livekit_url: str, the LiveKit URL to use for the request.
            auto_subscribe: bool, whether to auto subscribe to the WebSocket.
            sdk: str, the SDK to use for the request.
            version: str, the version to use for the request.
            protocol: int, the protocol to use for the request.
            headers: Dict[str, str], the headers to send.
            timeout: float, the timeout to use for the request.

        Returns:
            WebSocketConnection: The LiveKit WebSocket connection.
        """
        # Build URL
        url = self.build_url(
            access_token,
            livekit_url=livekit_url,
            auto_subscribe=auto_subscribe,
            sdk=sdk,
            version=version,
            protocol=protocol,
        )

        # Build WebSocket headers
        ws_headers = self._build_headers(headers)

        # Build timeout
        if timeout is None:
            timeout = get_config("network.timeout")

        # Connect to the LiveKit WebSocket
        try:
            return await self._client.connect(url, headers=ws_headers, timeout=timeout)
        except Exception as e:
            logger.error(f"LivekitWebSocketReverse: Connect failed, {e}")
            raise UpstreamException(
                f"LivekitWebSocketReverse: Connect failed, {str(e)}"
            )


__all__ = [
    "LivekitTokenReverse",
    "LivekitWebSocketReverse",
    "LIVEKIT_TOKEN_API",
    "LIVEKIT_WS_URL",
]
