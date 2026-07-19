from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from .client import XlwmsClient


FUNDS_FLOW_PATH = "/v1/cost/pageFundsFlow"
COST_DETAIL_PATH = "/v1/cost/costDetail"


class CostService:
    def __init__(self, client: XlwmsClient) -> None:
        self.client = client

    def page_funds_flow(
        self,
        parameters: Mapping[str, Any] | None = None,
        *,
        page: int = 1,
        page_size: int = 100,
    ) -> dict[str, Any]:
        request_data = dict(parameters or {})
        request_data["page"] = page
        request_data["pageSize"] = page_size
        validate_page(page, page_size)
        return self.client.request(FUNDS_FLOW_PATH, request_data)

    def cost_detail(
        self,
        query_order_no: str,
        *,
        query_order_type: int = 1,
        module_type: int | None = None,
    ) -> dict[str, Any]:
        query_order_no = query_order_no.strip()
        if not query_order_no:
            raise ValueError("queryOrderNo is required")
        if query_order_type not in (1, 2):
            raise ValueError("queryOrderType must be 1 (OMS order) or 2 (cost order)")
        if module_type is not None and (
            not isinstance(module_type, int)
            or isinstance(module_type, bool)
            or module_type < 1
        ):
            raise ValueError("moduleType must be a positive integer")

        request_data: dict[str, Any] = {
            "queryOrderNo": query_order_no,
            "queryOrderType": query_order_type,
        }
        if module_type is not None:
            request_data["moduleType"] = module_type
        return self.client.request(COST_DETAIL_PATH, request_data)


def validate_page(page: int, page_size: int) -> None:
    if not isinstance(page, int) or isinstance(page, bool) or page < 1:
        raise ValueError("page must be an integer greater than or equal to 1")
    if not isinstance(page_size, int) or isinstance(page_size, bool) or page_size < 1:
        raise ValueError("pageSize must be an integer greater than or equal to 1")
