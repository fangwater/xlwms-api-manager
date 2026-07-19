from __future__ import annotations

import threading
import time
from collections.abc import Callable, Mapping
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from typing import Any
from uuid import uuid4

import psycopg

from .client import XlwmsApiError, XlwmsClient
from .costs import CostService
from .db import (
    delete_stale_funds_flow_records,
    mark_detail_target_status,
    upsert_cost_detail,
    upsert_funds_flow_records,
)
from .warehouses import WarehouseCredentials


ProgressCallback = Callable[[int, int, int, int], None]
DetailProgressCallback = Callable[[int, int, int, int, int], None]


@dataclass(frozen=True)
class FundsFlowSyncResult:
    wh_code: str
    pages: int
    records_seen: int
    records_saved: int


@dataclass(frozen=True)
class DetailTarget:
    order_no: str
    module_type: int | None


@dataclass(frozen=True)
class DetailOutcome:
    target: DetailTarget
    attempts: int
    detail: dict[str, Any] | None = None
    error_code: str | None = None
    error_message: str | None = None


@dataclass(frozen=True)
class DetailSyncResult:
    wh_code: str
    targets: int
    succeeded: int
    failed: int
    cost_items: int


class RateLimiter:
    def __init__(self, requests_per_second: float) -> None:
        if requests_per_second <= 0:
            raise ValueError("requests_per_second must be greater than 0")
        self.interval = 1.0 / requests_per_second
        self.lock = threading.Lock()
        self.next_allowed = 0.0

    def wait(self) -> None:
        with self.lock:
            now = time.monotonic()
            delay = max(0.0, self.next_allowed - now)
            self.next_allowed = max(now, self.next_allowed) + self.interval
        if delay:
            time.sleep(delay)


def sync_warehouse_funds_flow(
    database_url: str,
    warehouse: WarehouseCredentials,
    *,
    parameters: Mapping[str, Any] | None = None,
    page_size: int = 100,
    progress: ProgressCallback | None = None,
) -> FundsFlowSyncResult:
    if not 1 <= page_size <= 100:
        raise ValueError("pageSize must be between 1 and 100")
    request_parameters = dict(parameters or {})
    request_parameters["whCodeList"] = [warehouse.wh_code]
    service = _cost_service(warehouse)
    records_seen = 0
    records_saved = 0
    occurrence_counts: dict[str, int] = {}
    snapshot_token = uuid4()
    page = 1
    pages = 1
    with psycopg.connect(database_url) as conn:
        while page <= pages:
            response = service.page_funds_flow(
                request_parameters,
                page=page,
                page_size=page_size,
            )
            data = response.get("data") or {}
            records = data.get("records") or []
            pages = int(data.get("pages") or 0)
            records_seen += len(records)
            records_saved += upsert_funds_flow_records(
                conn,
                warehouse.wh_code,
                records,
                occurrence_counts=occurrence_counts,
                snapshot_token=snapshot_token,
            )
            conn.commit()
            if progress:
                progress(page, pages, records_seen, records_saved)
            page += 1
        delete_stale_funds_flow_records(conn, warehouse.wh_code, snapshot_token)
        conn.commit()
    return FundsFlowSyncResult(
        wh_code=warehouse.wh_code,
        pages=pages,
        records_seen=records_seen,
        records_saved=records_saved,
    )


def sync_warehouse_cost_details(
    database_url: str,
    warehouse: WarehouseCredentials,
    *,
    workers: int = 4,
    requests_per_second: float = 8.0,
    max_attempts: int = 3,
    limit: int | None = None,
    progress: DetailProgressCallback | None = None,
) -> DetailSyncResult:
    if workers < 1:
        raise ValueError("workers must be greater than or equal to 1")
    if max_attempts < 1:
        raise ValueError("max_attempts must be greater than or equal to 1")
    targets = _load_pending_detail_targets(database_url, warehouse.wh_code, limit=limit)
    limiter = RateLimiter(requests_per_second)
    service = _cost_service(warehouse)

    def fetch(target: DetailTarget) -> DetailOutcome:
        return _fetch_detail(service, limiter, target, max_attempts=max_attempts)

    succeeded = 0
    failed = 0
    cost_items = 0
    processed = 0
    with psycopg.connect(database_url) as conn:
        with ThreadPoolExecutor(max_workers=workers) as executor:
            for outcome in executor.map(fetch, targets):
                if outcome.detail is not None:
                    _, item_count = upsert_cost_detail(
                        conn,
                        warehouse.wh_code,
                        outcome.target.order_no,
                        1,
                        outcome.detail,
                    )
                    mark_detail_target_status(
                        conn,
                        warehouse.wh_code,
                        outcome.target.order_no,
                        outcome.target.module_type,
                        status="success",
                        attempts=outcome.attempts,
                    )
                    succeeded += 1
                    cost_items += item_count
                else:
                    mark_detail_target_status(
                        conn,
                        warehouse.wh_code,
                        outcome.target.order_no,
                        outcome.target.module_type,
                        status="error",
                        attempts=outcome.attempts,
                        error_code=outcome.error_code,
                        error_message=outcome.error_message,
                    )
                    failed += 1
                processed += 1
                if processed % 50 == 0:
                    conn.commit()
                if progress and (processed % 100 == 0 or processed == len(targets)):
                    progress(processed, len(targets), succeeded, failed, cost_items)
        conn.commit()
    return DetailSyncResult(
        wh_code=warehouse.wh_code,
        targets=len(targets),
        succeeded=succeeded,
        failed=failed,
        cost_items=cost_items,
    )


def _load_pending_detail_targets(
    database_url: str,
    warehouse_code: str,
    *,
    limit: int | None,
) -> list[DetailTarget]:
    limit_clause = "LIMIT %s" if limit is not None else ""
    params: list[Any] = [warehouse_code.strip().upper()]
    if limit is not None:
        if limit < 1:
            raise ValueError("limit must be greater than or equal to 1")
        params.append(limit)
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                f"""
                SELECT order_no, module_type
                FROM xlwms_funds_flows
                WHERE wh_code = %s
                  AND coalesce(order_no, '') <> ''
                GROUP BY order_no, module_type
                HAVING bool_and(detail_sync_status = 'success') = false
                ORDER BY max(cost_time) DESC NULLS LAST, order_no
                {limit_clause}
                """,
                params,
            )
            return [DetailTarget(*row) for row in cur.fetchall()]


def _fetch_detail(
    service: CostService,
    limiter: RateLimiter,
    target: DetailTarget,
    *,
    max_attempts: int,
) -> DetailOutcome:
    last_code: str | None = None
    last_message: str | None = None
    for attempt in range(1, max_attempts + 1):
        limiter.wait()
        try:
            response = service.cost_detail(
                target.order_no,
                query_order_type=1,
                module_type=target.module_type,
            )
        except XlwmsApiError as exc:
            payload = exc.payload if isinstance(exc.payload, dict) else {}
            last_code = str(payload.get("code") or exc.status or "transport")
            last_message = str(payload.get("msg") or exc)
            if attempt < max_attempts:
                time.sleep(0.5 * (2 ** (attempt - 1)))
            continue
        detail = response.get("data")
        if not isinstance(detail, dict) or not detail.get("costNo"):
            return DetailOutcome(
                target=target,
                attempts=attempt,
                error_code="invalid_response",
                error_message="costDetail response is missing data.costNo",
            )
        return DetailOutcome(target=target, attempts=attempt, detail=detail)
    return DetailOutcome(
        target=target,
        attempts=max_attempts,
        error_code=last_code,
        error_message=last_message,
    )


def _cost_service(warehouse: WarehouseCredentials) -> CostService:
    return CostService(
        XlwmsClient(
            base_url=warehouse.api_base_url,
            app_key=warehouse.app_key,
            app_secret=warehouse.app_secret,
        )
    )
