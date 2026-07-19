from __future__ import annotations

import hashlib
import hmac
import json
import urllib.parse

from xlwms_api_manager.client import XlwmsClient, build_authcode, serialize_sign_data


def test_serialize_sign_data_sorts_keys_recursively() -> None:
    data = {"pageSize": 10, "filters": {"z": 1, "A": 2}, "page": 1}

    assert serialize_sign_data(data) == '{"filters":{"A":2,"z":1},"page":1,"pageSize":10}'


def test_build_authcode_uses_hmac_sha256() -> None:
    data = {"page": 1, "pageSize": 10}
    sign_text = 'app-key{"page":1,"pageSize":10}1711009072'
    expected = hmac.new(b"app-secret", sign_text.encode(), hashlib.sha256).hexdigest()

    assert build_authcode(
        app_key="app-key",
        app_secret="app-secret",
        req_time="1711009072",
        data=data,
    ) == expected


def test_build_request_uses_seconds_and_keeps_secret_out_of_payload() -> None:
    client = XlwmsClient(
        base_url="https://example.test/openapi/",
        app_key="app-key",
        app_secret="app-secret",
        clock=lambda: 1711009072.9,
    )

    url, payload = client.build_request("/v1/cost/costDetail", {"queryOrderNo": "O1"})

    assert payload == {
        "appKey": "app-key",
        "reqTime": "1711009072",
        "data": {"queryOrderNo": "O1"},
    }
    assert "app-secret" not in json.dumps(payload)
    query = urllib.parse.parse_qs(urllib.parse.urlparse(url).query)
    assert len(query["authcode"][0]) == 64
