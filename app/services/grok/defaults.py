"""
Grok 服务默认配置

此文件定义所有 Grok 相关服务的默认值，会在应用启动时注册到配置系统中。
"""

# Grok 服务默认配置
GROK_DEFAULTS = {
    "app": {
        "app_url": "",
        "app_key": "grok2api",
        "api_key": "",
        "public_key": "",
        "public_enabled": False,
        "image_format": "url",
        "video_format": "html",
        "temporary": True,
        "disable_memory": True,
        "stream": True,
        "thinking": True,
        "dynamic_statsig": True,
        "filter_tags": ["xaiartifact", "xai:tool_usage_card", "grok:render"],
    },
    "proxy": {
        "base_proxy_url": "",
        "asset_proxy_url": "",
        "cf_clearance": "",
        "browser": "chrome136",
        "user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
    },
    "voice": {
        "timeout": 120,
    },
    "chat": {
        "concurrent": 10,
        "timeout": 60,
        "stream_timeout": 60,
    },
    "video": {
        "concurrent": 10,
        "timeout": 60,
        "stream_timeout": 60,
    },
    "retry": {
        "max_retry": 3,
        "retry_status_codes": [401, 429, 403],
        "retry_backoff_base": 0.5,
        "retry_backoff_factor": 2.0,
        "retry_backoff_max": 30.0,
        "retry_budget": 90.0,
    },
    "image": {
        "timeout": 120,
        "stream_timeout": 120,
        "final_timeout": 15,
        "nsfw": True,
        "medium_min_bytes": 30000,
        "final_min_bytes": 100000,
    },
    "token": {
        "auto_refresh": True,
        "refresh_interval_hours": 8,
        "super_refresh_interval_hours": 2,
        "fail_threshold": 5,
        "save_delay_ms": 500,
        "reload_interval_sec": 30,
    },
    "cache": {
        "enable_auto_clean": True,
        "limit_mb": 1024,
    },
    "asset": {
        "upload_concurrent": 30,
        "upload_timeout": 60,
        "download_concurrent": 30,
        "download_timeout": 60,
        "list_concurrent": 10,
        "list_timeout": 60,
        "list_batch_size": 10,
        "delete_concurrent": 10,
        "delete_timeout": 60,
        "delete_batch_size": 10,
    },
    "nsfw": {
        "concurrent": 10,
        "batch_size": 50,
        "timeout": 60,
    },
    "usage": {
        "concurrent": 10,
        "batch_size": 50,
        "timeout": 60,
    },
}


def get_grok_defaults():
    """获取 Grok 默认配置"""
    return GROK_DEFAULTS


__all__ = ["GROK_DEFAULTS", "get_grok_defaults"]
