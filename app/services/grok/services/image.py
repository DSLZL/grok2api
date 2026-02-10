"""
Grok Imagine WebSocket image service.
"""

from app.services.reverse.ws_imagine import ImagineWebSocketReverse


ImageService = ImagineWebSocketReverse
image_service = ImagineWebSocketReverse()

__all__ = ["image_service", "ImageService"]
