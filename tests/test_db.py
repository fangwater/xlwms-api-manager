from __future__ import annotations

from xlwms_api_manager.db import SCHEMA_SQL


def test_schema_defines_xlwms_cost_tables() -> None:
    assert "CREATE TABLE IF NOT EXISTS xlwms_warehouses" in SCHEMA_SQL
    assert "CREATE TABLE IF NOT EXISTS xlwms_funds_flows" in SCHEMA_SQL
    assert "detail_sync_status text NOT NULL DEFAULT 'pending'" in SCHEMA_SQL
    assert "CREATE TABLE IF NOT EXISTS xlwms_cost_details" in SCHEMA_SQL
    assert "CREATE TABLE IF NOT EXISTS xlwms_cost_items" in SCHEMA_SQL
    assert "REFERENCES xlwms_cost_details(wh_code, cost_no) ON DELETE CASCADE" in SCHEMA_SQL

from xlwms_api_manager.db import funds_flow_source_key


def test_funds_flow_source_key_preserves_distinct_api_rows() -> None:
    record = {
        "whCode": "PA30",
        "orderNo": "ORDER-1",
        "platformOrderNo": "PLATFORM-1",
        "moduleType": 2,
        "costTime": "2026-01-01 00:00:00",
        "currencyCode": "USD",
        "costTotal": 10,
        "costStatus": 1,
        "billStatus": 1,
    }
    changed = {**record, "costTotal": 12, "costStatus": 2, "billStatus": 2}

    assert funds_flow_source_key("PA30", record) != funds_flow_source_key("PA30", changed)
    assert funds_flow_source_key("PA30", record) != funds_flow_source_key(
        "PA30", {**record, "orderNo": "ORDER-2"}
    )


def test_funds_flow_source_key_preserves_identical_occurrences() -> None:
    record = {"whCode": "PA30", "orderNo": "ORDER-1"}

    assert funds_flow_source_key("PA30", record, 1) != funds_flow_source_key(
        "PA30", record, 2
    )

from xlwms_api_manager.db import _optional_int


def test_optional_int_accepts_documented_string_statuses() -> None:
    assert _optional_int("2") == 2
    assert _optional_int(3) == 3
    assert _optional_int("") is None
