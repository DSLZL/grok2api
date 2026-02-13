"""
Chat Completions API 路由
"""

from typing import Any, Dict, List, Optional, Union
import base64
import binascii

from fastapi import APIRouter
from fastapi.responses import StreamingResponse, JSONResponse
from pydantic import BaseModel, Field

from app.services.grok.services.chat import ChatService
from app.services.grok.services.model import ModelService
from app.core.exceptions import ValidationException


class MessageItem(BaseModel):
    """消息项"""

    role: str
    content: Union[str, List[Dict[str, Any]]]

    model_config = {"extra": "ignore"}


class VideoConfig(BaseModel):
    """视频生成配置"""

    aspect_ratio: Optional[str] = Field("3:2", description="视频比例: 1280x720(16:9), 720x1280(9:16), 1792x1024(3:2), 1024x1792(2:3), 1024x1024(1:1)")
    video_length: Optional[int] = Field(6, description="视频时长(秒): 6 / 10 / 15")
    resolution_name: Optional[str] = Field("480p", description="视频分辨率: 480p, 720p")
    preset: Optional[str] = Field("custom", description="风格预设: fun, normal, spicy")


class ChatCompletionRequest(BaseModel):
    """Chat Completions 请求"""

    model: str = Field(..., description="模型名称")
    messages: List[MessageItem] = Field(..., description="消息数组")
    stream: Optional[bool] = Field(None, description="是否流式输出")
    reasoning_effort: Optional[str] = Field(None, description="推理强度: none/minimal/low/medium/high/xhigh")
    temperature: Optional[float] = Field(0.8, description="采样温度: 0-2")
    top_p: Optional[float] = Field(0.95, description="nucleus 采样: 0-1")
    # 视频生成配置
    video_config: Optional[VideoConfig] = Field(None, description="视频生成参数")
    model_config = {"extra": "ignore"}


VALID_ROLES = {"developer", "system", "user", "assistant"}
USER_CONTENT_TYPES = {"text", "image_url", "input_audio", "file"}


def _validate_media_input(value: str, field_name: str, param: str):
    if not isinstance(value, str) or not value.strip():
        raise ValidationException(
            message=f"{field_name} cannot be empty",
            param=param,
            code="empty_media",
        )
    value = value.strip()
    if value.startswith("data:"):
        return
    if value.startswith("http://") or value.startswith("https://"):
        return
    candidate = "".join(value.split())
    if len(candidate) >= 32 and len(candidate) % 4 == 0:
        try:
            base64.b64decode(candidate, validate=True)
            raise ValidationException(
                message=f"{field_name} base64 must be provided as a data URI (data:<mime>;base64,...)",
                param=param,
                code="invalid_media",
            )
        except binascii.Error:
            pass
    raise ValidationException(
        message=f"{field_name} must be a URL or data URI",
        param=param,
        code="invalid_media",
    )


def _normalize_stream(value: Any) -> Optional[bool]:
    if value is None:
        return None
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        if value.lower() in ("true", "1", "yes"):
            return True
        if value.lower() in ("false", "0", "no"):
            return False
    raise ValidationException(
        message="stream must be a boolean",
        param="stream",
        code="invalid_stream",
    )


def _validate_reasoning_effort(value: Any) -> Optional[str]:
    allowed = {"none", "minimal", "low", "medium", "high", "xhigh"}
    if value is None:
        return None
    if not isinstance(value, str) or value not in allowed:
        raise ValidationException(
            message=f"reasoning_effort must be one of {sorted(allowed)}",
            param="reasoning_effort",
            code="invalid_reasoning_effort",
        )
    return value


def _validate_temperature(value: Any) -> float:
    if value is None:
        return 0.8
    try:
        val = float(value)
    except Exception:
        raise ValidationException(
            message="temperature must be a float",
            param="temperature",
            code="invalid_temperature",
        )
    if not (0 <= val <= 2):
        raise ValidationException(
            message="temperature must be between 0 and 2",
            param="temperature",
            code="invalid_temperature",
        )
    return val


def _validate_top_p(value: Any) -> float:
    if value is None:
        return 0.95
    try:
        val = float(value)
    except Exception:
        raise ValidationException(
            message="top_p must be a float",
            param="top_p",
            code="invalid_top_p",
        )
    if not (0 <= val <= 1):
        raise ValidationException(
            message="top_p must be between 0 and 1",
            param="top_p",
            code="invalid_top_p",
        )
    return val


def _normalize_video_config(config: Optional[VideoConfig]) -> VideoConfig:
    if config is None:
        config = VideoConfig()

    ratio_map = {
        "1280x720": "16:9",
        "720x1280": "9:16",
        "1792x1024": "3:2",
        "1024x1792": "2:3",
        "1024x1024": "1:1",
        "16:9": "16:9",
        "9:16": "9:16",
        "3:2": "3:2",
        "2:3": "2:3",
        "1:1": "1:1",
    }
    if config.aspect_ratio is None:
        config.aspect_ratio = "3:2"
    if config.aspect_ratio not in ratio_map:
        raise ValidationException(
            message=f"aspect_ratio must be one of {list(ratio_map.keys())}",
            param="video_config.aspect_ratio",
            code="invalid_aspect_ratio",
        )
    config.aspect_ratio = ratio_map[config.aspect_ratio]

    if config.video_length not in (6, 10, 15):
        raise ValidationException(
            message="video_length must be 6, 10, or 15 seconds",
            param="video_config.video_length",
            code="invalid_video_length",
        )
    if config.resolution_name not in ("480p", "720p"):
        raise ValidationException(
            message="resolution_name must be one of ['480p', '720p']",
            param="video_config.resolution_name",
            code="invalid_resolution",
        )
    if config.preset not in ("fun", "normal", "spicy", "custom"):
        raise ValidationException(
            message="preset must be one of ['fun', 'normal', 'spicy', 'custom']",
            param="video_config.preset",
            code="invalid_preset",
        )
    return config


def validate_request(request: ChatCompletionRequest):
    """验证请求参数"""
    # 验证模型
    if not ModelService.valid(request.model):
        raise ValidationException(
            message=f"The model `{request.model}` does not exist or you do not have access to it.",
            param="model",
            code="model_not_found",
        )

    # 验证消息
    for idx, msg in enumerate(request.messages):
        if not isinstance(msg.role, str) or msg.role not in VALID_ROLES:
            raise ValidationException(
                message=f"role must be one of {sorted(VALID_ROLES)}",
                param=f"messages.{idx}.role",
                code="invalid_role",
            )
        content = msg.content

        # 字符串内容
        if isinstance(content, str):
            if not content.strip():
                raise ValidationException(
                    message="Message content cannot be empty",
                    param=f"messages.{idx}.content",
                    code="empty_content",
                )

        # 列表内容
        elif isinstance(content, list):
            if not content:
                raise ValidationException(
                    message="Message content cannot be an empty array",
                    param=f"messages.{idx}.content",
                    code="empty_content",
                )

            for block_idx, block in enumerate(content):
                # 检查空对象
                if not isinstance(block, dict):
                    raise ValidationException(
                        message="Content block must be an object",
                        param=f"messages.{idx}.content.{block_idx}",
                        code="invalid_block",
                    )
                if not block:
                    raise ValidationException(
                        message="Content block cannot be empty",
                        param=f"messages.{idx}.content.{block_idx}",
                        code="empty_block",
                    )

                # 检查 type 字段
                if "type" not in block:
                    raise ValidationException(
                        message="Content block must have a 'type' field",
                        param=f"messages.{idx}.content.{block_idx}",
                        code="missing_type",
                    )

                block_type = block.get("type")

                # 检查 type 空值
                if (
                    not block_type
                    or not isinstance(block_type, str)
                    or not block_type.strip()
                ):
                    raise ValidationException(
                        message="Content block 'type' cannot be empty",
                        param=f"messages.{idx}.content.{block_idx}.type",
                        code="empty_type",
                    )

                # 验证 type 有效性
                if msg.role == "user":
                    if block_type not in USER_CONTENT_TYPES:
                        raise ValidationException(
                            message=f"Invalid content block type: '{block_type}'",
                            param=f"messages.{idx}.content.{block_idx}.type",
                            code="invalid_type",
                        )
                else:
                    if block_type != "text":
                        raise ValidationException(
                            message=f"The `{msg.role}` role only supports 'text' type, got '{block_type}'",
                            param=f"messages.{idx}.content.{block_idx}.type",
                            code="invalid_type",
                        )

                # 验证字段是否存在 & 非空
                if block_type == "text":
                    text = block.get("text", "")
                    if not isinstance(text, str) or not text.strip():
                        raise ValidationException(
                            message="Text content cannot be empty",
                            param=f"messages.{idx}.content.{block_idx}.text",
                            code="empty_text",
                        )
                elif block_type == "image_url":
                    image_url = block.get("image_url")
                    if not image_url or not isinstance(image_url, dict):
                        raise ValidationException(
                            message="image_url must have a 'url' field",
                            param=f"messages.{idx}.content.{block_idx}.image_url",
                            code="missing_url",
                        )
                    _validate_media_input(
                        image_url.get("url", ""),
                        "image_url.url",
                        f"messages.{idx}.content.{block_idx}.image_url.url",
                    )
                elif block_type == "input_audio":
                    audio = block.get("input_audio")
                    if not audio or not isinstance(audio, dict):
                        raise ValidationException(
                            message="input_audio must have a 'data' field",
                            param=f"messages.{idx}.content.{block_idx}.input_audio",
                            code="missing_audio",
                        )
                    _validate_media_input(
                        audio.get("data", ""),
                        "input_audio.data",
                        f"messages.{idx}.content.{block_idx}.input_audio.data",
                    )
                elif block_type == "file":
                    file_data = block.get("file")
                    if not file_data or not isinstance(file_data, dict):
                        raise ValidationException(
                            message="file must have a 'file_data' field",
                            param=f"messages.{idx}.content.{block_idx}.file",
                            code="missing_file",
                        )
                    _validate_media_input(
                        file_data.get("file_data", ""),
                        "file.file_data",
                        f"messages.{idx}.content.{block_idx}.file.file_data",
                    )
        else:
            raise ValidationException(
                message="Message content must be a string or array",
                param=f"messages.{idx}.content",
                code="invalid_content",
            )

    request.stream = _normalize_stream(request.stream)
    request.reasoning_effort = _validate_reasoning_effort(request.reasoning_effort)
    request.temperature = _validate_temperature(request.temperature)
    request.top_p = _validate_top_p(request.top_p)

    model_info = ModelService.get(request.model)
    if model_info and model_info.is_video:
        request.video_config = _normalize_video_config(request.video_config)


router = APIRouter(tags=["Chat"])


@router.post("/chat/completions")
async def chat_completions(request: ChatCompletionRequest):
    """Chat Completions API - 兼容 OpenAI"""
    from app.core.logger import logger

    # 参数验证
    validate_request(request)

    logger.debug(f"Chat request: model={request.model}, stream={request.stream}")

    # 检测视频模型
    model_info = ModelService.get(request.model)
    if model_info and model_info.is_video:
        from app.services.grok.services.video import VideoService

        # 提取视频配置 (默认值在 Pydantic 模型中处理)
        v_conf = request.video_config or VideoConfig()

        result = await VideoService.completions(
            model=request.model,
            messages=[msg.model_dump() for msg in request.messages],
            stream=request.stream,
            reasoning_effort=request.reasoning_effort,
            aspect_ratio=v_conf.aspect_ratio,
            video_length=v_conf.video_length,
            resolution=v_conf.resolution_name,
            preset=v_conf.preset,
        )
    else:
        result = await ChatService.completions(
            model=request.model,
            messages=[msg.model_dump() for msg in request.messages],
            stream=request.stream,
            reasoning_effort=request.reasoning_effort,
            temperature=request.temperature,
            top_p=request.top_p,
        )

    if isinstance(result, dict):
        return JSONResponse(content=result)
    else:
        return StreamingResponse(
            result,
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", "Connection": "keep-alive"},
        )


__all__ = ["router"]
