"""
NSFW (Unhinged) 模式服务

使用 gRPC-Web 协议开启账号的 NSFW 功能。
"""

from dataclasses import dataclass
from typing import Optional

from curl_cffi.requests import AsyncSession

from app.core.logger import logger
from app.core.config import get_config
from app.core.exceptions import UpstreamException
from app.services.reverse import NsfwMgmtReverse, SetBirthReverse
from app.services.reverse.utils.grpc import GrpcStatus

@dataclass
class NSFWResult:
    """NSFW 操作结果"""

    success: bool
    http_status: int
    grpc_status: Optional[int] = None
    grpc_message: Optional[str] = None
    error: Optional[str] = None


class NSFWService:
    """NSFW 模式服务"""

    async def enable(self, token: str) -> NSFWResult:
        """为单个 token 开启 NSFW 模式"""
        try:
            browser = get_config("security.browser")
            async with AsyncSession(impersonate=browser) as session:
                # 先设置出生日期
                try:
                    await SetBirthReverse.request(session, token)
                except UpstreamException as e:
                    status = None
                    if e.details and "status" in e.details:
                        status = e.details["status"]
                    else:
                        status = getattr(e, "status_code", None)
                    return NSFWResult(
                        success=False,
                        http_status=status or 0,
                        error=f"Set birth date failed: {str(e)}",
                    )

                # 开启 NSFW
                grpc_status: GrpcStatus = await NsfwMgmtReverse.request(session, token)
                success = grpc_status.code in (-1, 0)

                return NSFWResult(
                    success=success,
                    http_status=200,
                    grpc_status=grpc_status.code,
                    grpc_message=grpc_status.message or None,
                )

        except Exception as e:
            logger.error(f"NSFW enable failed: {e}")
            return NSFWResult(success=False, http_status=0, error=str(e)[:100])


__all__ = ["NSFWService", "NSFWResult"]
