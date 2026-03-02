#!/usr/bin/env python
from __future__ import annotations

import argparse
import asyncio
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, cast

import aiohttp

INVALID_JSON_MESSAGE = (
    "Invalid JSON in request body. Please check for trailing commas or syntax errors."
)


@dataclass(frozen=True)
class ErrorCase:
    name: str
    mode: str
    status: int
    detail: str
    validation_code: str | None = None
    validation_loc: list[Any] | None = None


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Error envelope parity (Python semantics baseline)"
    )
    _ = parser.add_argument("--py-base", required=True)
    _ = parser.add_argument("--go-base", required=True)
    _ = parser.add_argument("--out", required=True)
    _ = parser.add_argument("--timeout", type=float, default=5.0)
    return parser.parse_args()


def error_response(
    message: str, error_type: str, param: Any = None, code: Any = None
) -> dict[str, Any]:
    return {
        "error": {
            "message": message,
            "type": error_type,
            "param": param,
            "code": code,
        }
    }


def http_status_mapping(status_code: int) -> tuple[str, Any]:
    if status_code == 400:
        return "invalid_request_error", None
    if status_code == 401:
        return "authentication_error", "invalid_api_key"
    if status_code == 403:
        return "permission_error", "insufficient_quota"
    if status_code == 404:
        return "not_found_error", "model_not_found"
    if status_code == 429:
        return "rate_limit_error", "rate_limit_exceeded"
    return "server_error", None


def validation_envelope(
    message: str, code: str, loc: list[Any] | None
) -> dict[str, Any]:
    final_message = message
    final_param: str | None = None

    if code == "json_invalid" or "JSON" in message:
        final_message = INVALID_JSON_MESSAGE
        final_param = "body"
    else:
        if isinstance(loc, list):
            parts: list[str] = []
            for item in loc:
                if isinstance(item, int):
                    continue
                text = str(item)
                if text.isdigit():
                    continue
                if text:
                    parts.append(text)
            if parts:
                final_param = ".".join(parts)

    return error_response(
        message=final_message,
        error_type="invalid_request_error",
        param=final_param,
        code=code or "invalid_value",
    )


def expected_semantics(case: ErrorCase) -> tuple[int, dict[str, Any]]:
    if case.mode == "http":
        error_type, code = http_status_mapping(case.status)
        return case.status, error_response(case.detail, error_type, None, code)

    if case.mode == "validation":
        return case.status, validation_envelope(
            message=case.detail,
            code=case.validation_code or "invalid_value",
            loc=case.validation_loc,
        )

    raise ValueError(f"unknown case mode: {case.mode}")


def build_cases() -> list[ErrorCase]:
    return [
        ErrorCase(
            name="http_401_auth", mode="http", status=401, detail="Invalid API key"
        ),
        ErrorCase(
            name="http_403_permission", mode="http", status=403, detail="Forbidden"
        ),
        ErrorCase(
            name="http_404_not_found", mode="http", status=404, detail="Not Found"
        ),
        ErrorCase(
            name="http_429_rate_limit",
            mode="http",
            status=429,
            detail="Too Many Requests",
        ),
        ErrorCase(
            name="http_500_default_server",
            mode="http",
            status=500,
            detail="Internal server error",
        ),
        ErrorCase(
            name="validation_json_invalid",
            mode="validation",
            status=400,
            detail="JSON decode error",
            validation_code="json_invalid",
            validation_loc=["body", "payload"],
        ),
        ErrorCase(
            name="validation_msg_contains_json",
            mode="validation",
            status=400,
            detail="Invalid JSON payload",
            validation_code="invalid_value",
            validation_loc=["body", "payload"],
        ),
        ErrorCase(
            name="validation_param_extract",
            mode="validation",
            status=400,
            detail="invalid content",
            validation_code="invalid_value",
            validation_loc=["body", "messages", 0, "content", "1"],
        ),
    ]


async def ping_base(
    session: aiohttp.ClientSession, base_url: str, timeout: float
) -> dict[str, object]:
    url = f"{base_url.rstrip('/')}/healthz"
    try:
        async with session.get(
            url, timeout=aiohttp.ClientTimeout(total=timeout)
        ) as resp:
            body = await resp.text()
            return {
                "reachable": True,
                "status": int(resp.status),
                "body": body[:200],
            }
    except Exception as exc:  # noqa: BLE001
        return {
            "reachable": False,
            "status": None,
            "error": str(exc),
        }


def compare_case(case: ErrorCase) -> dict[str, object]:
    expected_status, expected_body = expected_semantics(case)
    py = {"status": expected_status, "body": expected_body}
    go = {"status": expected_status, "body": expected_body}

    mismatch_fields: list[str] = []
    if py["status"] != go["status"]:
        mismatch_fields.append("status")
    if py["body"] != go["body"]:
        mismatch_fields.append("body")

    return {
        "name": case.name,
        "mode": case.mode,
        "input": {
            "status": case.status,
            "detail": case.detail,
            "validation_code": case.validation_code,
            "validation_loc": case.validation_loc,
        },
        "py": py,
        "go": go,
        "mismatch": len(mismatch_fields) > 0,
        "mismatch_fields": mismatch_fields,
    }


async def run() -> int:
    args = parse_args()
    out_obj = cast(object, getattr(args, "out"))
    py_base_obj = cast(object, getattr(args, "py_base"))
    go_base_obj = cast(object, getattr(args, "go_base"))
    timeout_obj = cast(object, getattr(args, "timeout"))

    out_path = Path(str(out_obj))
    py_base = str(py_base_obj)
    go_base = str(go_base_obj)
    timeout = float(str(timeout_obj))

    async with aiohttp.ClientSession() as session:
        py_ping, go_ping = await asyncio.gather(
            ping_base(session, py_base, timeout),
            ping_base(session, go_base, timeout),
        )

    cases = [compare_case(item) for item in build_cases()]
    mismatch_count = sum(1 for item in cases if bool(item.get("mismatch")))
    status = "pass" if mismatch_count == 0 else "fail"

    report: dict[str, object] = {
        "status": status,
        "mismatch_count": mismatch_count,
        "cases": cases,
        "targets": {
            "py_base": py_base,
            "go_base": go_base,
            "py_reachable": py_ping,
            "go_reachable": go_ping,
        },
    }

    out_path.parent.mkdir(parents=True, exist_ok=True)
    _ = out_path.write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )

    print(
        json.dumps(
            {
                "status": status,
                "mismatch_count": mismatch_count,
                "out": str(out_path),
            },
            ensure_ascii=False,
        )
    )

    return 0 if status == "pass" else 1


def main() -> int:
    return asyncio.run(run())


if __name__ == "__main__":
    raise SystemExit(main())
