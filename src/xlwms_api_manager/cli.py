from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

from .client import XlwmsApiError, XlwmsClient
from .config import Settings, load_settings
from .costs import CostService
from .db import initialize_schema
from .sync import sync_warehouse_cost_details, sync_warehouse_funds_flow
from .warehouses import (
    ensure_credential_key_file,
    get_warehouse_credentials,
    list_active_warehouse_credentials,
    list_warehouses,
    load_credential_key_file,
    set_warehouse_active,
    upsert_warehouse,
)


def main(argv: list[str] | None = None) -> int:
    settings = load_settings()
    parser = build_parser(settings)
    args = parser.parse_args(argv)
    try:
        return args.func(args, settings)
    except XlwmsApiError as exc:
        if isinstance(exc.payload, dict):
            print(json.dumps(exc.payload, ensure_ascii=False, indent=2), file=sys.stderr)
        else:
            print(f"error: {exc}", file=sys.stderr)
        return 1
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1


def build_parser(settings: Settings) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="xlwms-manager")
    sub = parser.add_subparsers(dest="command", required=True)

    funds_flow = sub.add_parser("funds-flow", help="Query paginated cost funds flow")
    funds_flow.add_argument("--api-base-url")
    funds_flow.add_argument("--warehouse", help="Use an active registered warehouse")
    funds_flow.add_argument("--page", type=int, default=1)
    funds_flow.add_argument("--page-size", type=int, default=100)
    source = funds_flow.add_mutually_exclusive_group()
    source.add_argument("--params-json", default="{}")
    source.add_argument("--params-file")
    funds_flow.add_argument("--output", help="Write the raw JSON response to this file")
    funds_flow.set_defaults(func=cmd_funds_flow)

    cost_detail = sub.add_parser("cost-detail", help="Query cost detail")
    cost_detail.add_argument("--api-base-url")
    cost_detail.add_argument("--warehouse", help="Use an active registered warehouse")
    cost_detail.add_argument("--query-order-no", required=True)
    cost_detail.add_argument(
        "--query-order-type",
        type=int,
        choices=(1, 2),
        default=1,
        help="1=OMS order number, 2=cost order number",
    )
    cost_detail.add_argument("--module-type", type=int)
    cost_detail.add_argument("--output", help="Write the raw JSON response to this file")
    cost_detail.set_defaults(func=cmd_cost_detail)

    db_init = sub.add_parser("db-init", help="Create XLWMS PostgreSQL tables")
    db_init.set_defaults(func=cmd_db_init)

    warehouse_add = sub.add_parser("warehouse-add", help="Add or update warehouse credentials")
    warehouse_add.add_argument("--wh-code", required=True)
    warehouse_add.add_argument("--name")
    warehouse_add.add_argument("--api-base-url", default=settings.api_base_url)
    warehouse_add.add_argument("--app-key-env", default="XLWMS_APP_KEY")
    warehouse_add.add_argument("--app-secret-env", default="XLWMS_APP_SECRET")
    warehouse_add.add_argument("--inactive", action="store_true")
    warehouse_add.set_defaults(func=cmd_warehouse_add)

    warehouse_list = sub.add_parser("warehouse-list", help="List registered warehouses")
    warehouse_list.add_argument("--active-only", action="store_true")
    warehouse_list.set_defaults(func=cmd_warehouse_list)

    warehouse_enable = sub.add_parser("warehouse-enable", help="Enable a warehouse")
    warehouse_enable.add_argument("wh_code")
    warehouse_enable.set_defaults(func=cmd_warehouse_enable)

    warehouse_disable = sub.add_parser("warehouse-disable", help="Disable a warehouse")
    warehouse_disable.add_argument("wh_code")
    warehouse_disable.set_defaults(func=cmd_warehouse_disable)

    sync_funds = sub.add_parser("sync-funds-flow", help="Synchronize funds flow into PostgreSQL")
    target = sync_funds.add_mutually_exclusive_group(required=True)
    target.add_argument("--warehouse")
    target.add_argument("--all-active", action="store_true")
    sync_funds.add_argument("--include-disabled", action="store_true")
    sync_funds.add_argument("--page-size", type=int, default=100)
    sync_funds.add_argument("--params-json", default="{}")
    sync_funds.set_defaults(func=cmd_sync_funds_flow)

    sync_details = sub.add_parser("sync-cost-details", help="Backfill cost details into PostgreSQL")
    sync_details.add_argument("--warehouse", required=True)
    sync_details.add_argument("--include-disabled", action="store_true")
    sync_details.add_argument("--workers", type=int, default=4)
    sync_details.add_argument("--requests-per-second", type=float, default=8.0)
    sync_details.add_argument("--max-attempts", type=int, default=3)
    sync_details.add_argument("--limit", type=int)
    sync_details.set_defaults(func=cmd_sync_cost_details)
    return parser


def cmd_funds_flow(args: argparse.Namespace, settings: Settings) -> int:
    parameters = load_parameters(args.params_json, args.params_file)
    if args.warehouse:
        apply_warehouse_filter(parameters, args.warehouse)
    service = CostService(client_for_request(
        settings,
        warehouse=args.warehouse,
        base_url=args.api_base_url,
    ))
    response = service.page_funds_flow(parameters, page=args.page, page_size=args.page_size)
    data = response.get("data") or {}
    records = data.get("records") if isinstance(data, dict) else []
    return render_response(response, args.output, summary={
        "page": data.get("page") if isinstance(data, dict) else None,
        "pageSize": data.get("pageSize") if isinstance(data, dict) else None,
        "total": data.get("total") if isinstance(data, dict) else None,
        "records": len(records) if isinstance(records, list) else None,
    })


def cmd_cost_detail(args: argparse.Namespace, settings: Settings) -> int:
    service = CostService(client_for_request(
        settings,
        warehouse=args.warehouse,
        base_url=args.api_base_url,
    ))
    response = service.cost_detail(
        args.query_order_no,
        query_order_type=args.query_order_type,
        module_type=args.module_type,
    )
    data = response.get("data") or {}
    items = data.get("costItemList") if isinstance(data, dict) else []
    return render_response(response, args.output, summary={
        "costItems": len(items) if isinstance(items, list) else None,
        "currencyCode": data.get("currencyCode") if isinstance(data, dict) else None,
    })


def cmd_db_init(args: argparse.Namespace, settings: Settings) -> int:
    result = initialize_schema(require_database_url(settings))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


def cmd_warehouse_add(args: argparse.Namespace, settings: Settings) -> int:
    app_key = os.getenv(args.app_key_env)
    app_secret = os.getenv(args.app_secret_env)
    if not app_key or not app_secret:
        raise ValueError(
            f"missing credential environment variables: {args.app_key_env}, {args.app_secret_env}"
        )
    key = ensure_credential_key_file(settings.credential_key_file)
    warehouse = upsert_warehouse(
        require_database_url(settings),
        key,
        wh_code=args.wh_code,
        warehouse_name=args.name,
        api_base_url=args.api_base_url,
        app_key=app_key,
        app_secret=app_secret,
        is_active=not args.inactive,
    )
    print(json.dumps(warehouse_summary_dict(warehouse), ensure_ascii=False, indent=2))
    return 0


def cmd_warehouse_list(args: argparse.Namespace, settings: Settings) -> int:
    warehouses = list_warehouses(
        require_database_url(settings),
        active_only=args.active_only,
    )
    print(json.dumps([warehouse_summary_dict(item) for item in warehouses], ensure_ascii=False, indent=2))
    return 0


def cmd_warehouse_enable(args: argparse.Namespace, settings: Settings) -> int:
    warehouse = set_warehouse_active(
        require_database_url(settings), args.wh_code, is_active=True
    )
    print(json.dumps(warehouse_summary_dict(warehouse), ensure_ascii=False, indent=2))
    return 0


def cmd_warehouse_disable(args: argparse.Namespace, settings: Settings) -> int:
    warehouse = set_warehouse_active(
        require_database_url(settings), args.wh_code, is_active=False
    )
    print(json.dumps(warehouse_summary_dict(warehouse), ensure_ascii=False, indent=2))
    return 0


def cmd_sync_funds_flow(args: argparse.Namespace, settings: Settings) -> int:
    database_url = require_database_url(settings)
    key = load_credential_key_file(settings.credential_key_file)
    if args.all_active:
        if args.include_disabled:
            raise ValueError("--include-disabled requires --warehouse")
        warehouses = list_active_warehouse_credentials(database_url, key)
    else:
        warehouses = [get_warehouse_credentials(
            database_url,
            key,
            args.warehouse,
            require_active=not args.include_disabled,
        )]
    parameters = load_parameters(args.params_json, None)
    results = []
    for warehouse in warehouses:
        def progress(page: int, pages: int, seen: int, saved: int) -> None:
            print(
                f"{warehouse.wh_code}: page {page}/{pages}, seen={seen}, saved={saved}",
                file=sys.stderr,
                flush=True,
            )
        result = sync_warehouse_funds_flow(
            database_url,
            warehouse,
            parameters=parameters,
            page_size=args.page_size,
            progress=progress,
        )
        results.append({
            "whCode": result.wh_code,
            "pages": result.pages,
            "recordsSeen": result.records_seen,
            "recordsSaved": result.records_saved,
        })
    print(json.dumps(results, ensure_ascii=False, indent=2))
    return 0


def cmd_sync_cost_details(args: argparse.Namespace, settings: Settings) -> int:
    database_url = require_database_url(settings)
    warehouse = get_warehouse_credentials(
        database_url,
        load_credential_key_file(settings.credential_key_file),
        args.warehouse,
        require_active=not args.include_disabled,
    )
    def progress(processed: int, total: int, succeeded: int, failed: int, items: int) -> None:
        print(
            f"{warehouse.wh_code}: details {processed}/{total}, "
            f"success={succeeded}, failed={failed}, items={items}",
            file=sys.stderr,
            flush=True,
        )
    result = sync_warehouse_cost_details(
        database_url,
        warehouse,
        workers=args.workers,
        requests_per_second=args.requests_per_second,
        max_attempts=args.max_attempts,
        limit=args.limit,
        progress=progress,
    )
    print(json.dumps({
        "whCode": result.wh_code,
        "targets": result.targets,
        "succeeded": result.succeeded,
        "failed": result.failed,
        "costItems": result.cost_items,
    }, ensure_ascii=False, indent=2))
    return 0


def client_for_request(
    settings: Settings,
    *,
    warehouse: str | None,
    base_url: str | None,
) -> XlwmsClient:
    if warehouse:
        credentials = get_warehouse_credentials(
            require_database_url(settings),
            load_credential_key_file(settings.credential_key_file),
            warehouse,
            require_active=True,
        )
        return XlwmsClient(
            base_url=base_url or credentials.api_base_url,
            app_key=credentials.app_key,
            app_secret=credentials.app_secret,
        )
    missing = [
        name
        for name, value in (
            ("XLWMS_APP_KEY", settings.app_key),
            ("XLWMS_APP_SECRET", settings.app_secret),
        )
        if not value
    ]
    if missing:
        raise ValueError(f"missing required environment variables: {', '.join(missing)}")
    return XlwmsClient(
        base_url=base_url or settings.api_base_url,
        app_key=settings.app_key,
        app_secret=settings.app_secret,
    )


def require_database_url(settings: Settings) -> str:
    if not settings.database_url:
        raise ValueError("DATABASE_URL is required")
    return settings.database_url


def warehouse_summary_dict(warehouse: Any) -> dict[str, Any]:
    return {
        "whCode": warehouse.wh_code,
        "name": warehouse.warehouse_name,
        "apiBaseUrl": warehouse.api_base_url,
        "appKey": warehouse.app_key_hint,
        "active": warehouse.is_active,
    }


def apply_warehouse_filter(parameters: dict[str, Any], wh_code: str) -> None:
    normalized = wh_code.strip().upper()
    existing = parameters.get("whCodeList")
    if existing is not None:
        if not isinstance(existing, list) or {str(item).upper() for item in existing} != {normalized}:
            raise ValueError("--warehouse conflicts with params whCodeList")
    parameters["whCodeList"] = [normalized]


def load_parameters(params_json: str, params_file: str | None) -> dict[str, Any]:
    raw = Path(params_file).read_text(encoding="utf-8") if params_file else params_json
    parameters = json.loads(raw)
    if not isinstance(parameters, dict):
        raise ValueError("request parameters must be a JSON object")
    return parameters


def render_response(
    response: dict[str, Any],
    output: str | None,
    *,
    summary: dict[str, Any],
) -> int:
    rendered = json.dumps(response, ensure_ascii=False, indent=2)
    if not output:
        print(rendered)
        return 0
    output_path = Path(output)
    write_private_text(output_path, rendered + "\n")
    print(json.dumps({"output": str(output_path), **summary}, ensure_ascii=False, indent=2))
    return 0


def write_private_text(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    path.chmod(0o600)
