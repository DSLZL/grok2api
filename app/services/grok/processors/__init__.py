"""
OpenAI 响应格式处理器
"""

from .video import VideoStreamProcessor, VideoCollectProcessor
from .image import (
    ImageStreamProcessor,
    ImageCollectProcessor,
    ImageWSStreamProcessor,
    ImageWSCollectProcessor,
)

__all__ = [
    "VideoStreamProcessor",
    "VideoCollectProcessor",
    "ImageStreamProcessor",
    "ImageCollectProcessor",
    "ImageWSStreamProcessor",
    "ImageWSCollectProcessor",
]
