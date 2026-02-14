from pathlib import Path

import aiofiles
from fastapi import APIRouter, HTTPException
from fastapi.responses import HTMLResponse, RedirectResponse

from app.core.auth import is_public_enabled

router = APIRouter()
TEMPLATE_DIR = Path(__file__).resolve().parents[2] / "static"


async def render_template(filename: str) -> HTMLResponse:
    """渲染指定模板"""
    template_path = TEMPLATE_DIR / filename
    if not template_path.exists():
        return HTMLResponse(f"Template {filename} not found.", status_code=404)

    async with aiofiles.open(template_path, "r", encoding="utf-8") as f:
        content = await f.read()
    return HTMLResponse(content)

@router.get("/", include_in_schema=False)
async def root_redirect():
    if is_public_enabled():
        return RedirectResponse(url="/login")
    return RedirectResponse(url="/admin/login")


@router.get("/login", response_class=HTMLResponse, include_in_schema=False)
async def public_login_page():
    """Public 登录页"""
    if not is_public_enabled():
        raise HTTPException(status_code=404, detail="Not Found")
    return await render_template("public/login.html")


@router.get("/imagine", response_class=HTMLResponse, include_in_schema=False)
async def public_imagine_page():
    """Imagine 图片瀑布流"""
    if not is_public_enabled():
        raise HTTPException(status_code=404, detail="Not Found")
    return await render_template("imagine/imagine.html")


@router.get("/voice", response_class=HTMLResponse, include_in_schema=False)
async def public_voice_page():
    """Voice Live 调试页"""
    if not is_public_enabled():
        raise HTTPException(status_code=404, detail="Not Found")
    return await render_template("voice/voice.html")


@router.get("/admin", include_in_schema=False)
async def admin_root_redirect():
    return RedirectResponse(url="/admin/login")


@router.get("/admin/login", response_class=HTMLResponse, include_in_schema=False)
async def admin_login_page():
    """管理后台登录页"""
    return await render_template("login/login.html")


@router.get("/admin/config", response_class=HTMLResponse, include_in_schema=False)
async def admin_config_page():
    """配置管理页"""
    return await render_template("config/config.html")


@router.get("/admin/token", response_class=HTMLResponse, include_in_schema=False)
async def admin_token_page():
    """Token 管理页"""
    return await render_template("token/token.html")


@router.get("/admin/voice", include_in_schema=False)
async def admin_voice_redirect():
    if not is_public_enabled():
        raise HTTPException(status_code=404, detail="Not Found")
    return RedirectResponse(url="/voice")


@router.get("/admin/imagine", include_in_schema=False)
async def admin_imagine_redirect():
    if not is_public_enabled():
        raise HTTPException(status_code=404, detail="Not Found")
    return RedirectResponse(url="/imagine")


@router.get("/admin/cache", response_class=HTMLResponse, include_in_schema=False)
async def admin_cache_page():
    """缓存管理页"""
    return await render_template("cache/cache.html")
