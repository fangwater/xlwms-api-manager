from __future__ import annotations

import pytest

from xlwms_api_manager.costs import COST_DETAIL_PATH, FUNDS_FLOW_PATH, CostService


class RecordingClient:
    def __init__(self) -> None:
        self.calls: list[tuple[str, dict]] = []

    def request(self, path: str, data: dict) -> dict:
        self.calls.append((path, data))
        return {"code": 200, "msg": "ok", "data": {}}


def test_page_funds_flow_adds_pagination() -> None:
    client = RecordingClient()

    CostService(client).page_funds_flow(
        {"moduleTypeList": [2]},
        page=3,
        page_size=50,
    )

    assert client.calls == [
        (FUNDS_FLOW_PATH, {"moduleTypeList": [2], "page": 3, "pageSize": 50})
    ]


def test_cost_detail_uses_documented_field_names() -> None:
    client = RecordingClient()

    CostService(client).cost_detail("OMS-1", query_order_type=1, module_type=2)

    assert client.calls == [
        (
            COST_DETAIL_PATH,
            {"queryOrderNo": "OMS-1", "queryOrderType": 1, "moduleType": 2},
        )
    ]


@pytest.mark.parametrize("query_order_type", [0, 3])
def test_cost_detail_rejects_invalid_query_order_type(query_order_type: int) -> None:
    with pytest.raises(ValueError, match="queryOrderType"):
        CostService(RecordingClient()).cost_detail(
            "OMS-1",
            query_order_type=query_order_type,
        )


def test_page_funds_flow_rejects_invalid_page() -> None:
    with pytest.raises(ValueError, match="page"):
        CostService(RecordingClient()).page_funds_flow(page=0)
