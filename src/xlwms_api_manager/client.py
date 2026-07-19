from __future__ import annotations

import hashlib
import hmac
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Mapping
from typing import Any, Callable


class XlwmsApiError(RuntimeError):
    def __init__(
        self,
        message: str,
        *,
        status: int | None = None,
        payload: Any = None,
    ) -> None:
        super().__init__(message)
        self.status = status
        self.payload = payload


def canonicalize(value: Any) -> Any:
    if isinstance(value, Mapping):
        return {
            key: canonicalize(value[key])
            for key in sorted(value, key=lambda item: str(item).lower())
        }
    if isinstance(value, list):
        return [canonicalize(item) for item in value]
    return value


def serialize_sign_data(data: Mapping[str, Any]) -> str:
    return json.dumps(
        canonicalize(data),
        separators=(",", ":"),
        ensure_ascii=False,
    )


def build_authcode(
    *,
    app_key: str,
    app_secret: str,
    req_time: str,
    data: Mapping[str, Any],
) -> str:
    sign_text = f"{app_key}{serialize_sign_data(data)}{req_time}"
    return hmac.new(
        app_secret.encode("utf-8"),
        sign_text.encode("utf-8"),
        hashlib.sha256,
    ).hexdigest()


class XlwmsClient:
    def __init__(
        self,
        *,
        base_url: str,
        app_key: str,
        app_secret: str,
        timeout_seconds: int = 30,
        clock: Callable[[], float] = time.time,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.app_key = app_key
        self.app_secret = app_secret
        self.timeout_seconds = timeout_seconds
        self.clock = clock

    def build_request(
        self,
        path: str,
        data: Mapping[str, Any],
    ) -> tuple[str, dict[str, Any]]:
        if not path.startswith("/"):
            raise ValueError("XLWMS API path must start with '/'")
        req_time = str(int(self.clock()))
        request_data = dict(data)
        authcode = build_authcode(
            app_key=self.app_key,
            app_secret=self.app_secret,
            req_time=req_time,
            data=request_data,
        )
        query = urllib.parse.urlencode({"authcode": authcode})
        return (
            f"{self.base_url}{path}?{query}",
            {
                "appKey": self.app_key,
                "reqTime": req_time,
                "data": request_data,
            },
        )

    def request(self, path: str, data: Mapping[str, Any]) -> dict[str, Any]:
        url, payload = self.build_request(path, data)
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
        request = urllib.request.Request(
            url,
            data=body,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                raw = response.read().decode("utf-8", errors="replace")
                status = response.status
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")
            parsed = _parse_json(raw)
            raise XlwmsApiError(
                f"XLWMS HTTP {exc.code}",
                status=exc.code,
                payload=parsed,
            ) from exc
        except urllib.error.URLError as exc:
            raise XlwmsApiError(f"XLWMS request failed: {exc.reason}") from exc

        parsed = _parse_json(raw)
        if not isinstance(parsed, dict):
            raise XlwmsApiError(
                "XLWMS returned non-object JSON",
                status=status,
                payload=parsed,
            )
        if str(parsed.get("code")) != "200":
            raise XlwmsApiError(
                f"XLWMS API error code={parsed.get('code')} msg={parsed.get('msg')}",
                status=status,
                payload=parsed,
            )
        return parsed


def _parse_json(raw: str) -> Any:
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise XlwmsApiError("XLWMS returned invalid JSON") from exc
