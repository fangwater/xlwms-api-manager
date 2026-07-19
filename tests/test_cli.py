from __future__ import annotations

from pathlib import Path

from xlwms_api_manager.cli import write_private_text


def test_write_private_text_restricts_file_permissions(tmp_path: Path) -> None:
    output = tmp_path / "exports" / "cost-detail.json"

    write_private_text(output, "{}\n")

    assert output.read_text(encoding="utf-8") == "{}\n"
    assert output.stat().st_mode & 0o777 == 0o600

from xlwms_api_manager.cli import apply_warehouse_filter


def test_apply_warehouse_filter_scopes_shared_credentials() -> None:
    parameters = {"moduleTypeList": [2]}

    apply_warehouse_filter(parameters, "dpsny002")

    assert parameters["whCodeList"] == ["DPSNY002"]


def test_apply_warehouse_filter_rejects_conflicting_filter() -> None:
    import pytest

    with pytest.raises(ValueError, match="conflicts"):
        apply_warehouse_filter({"whCodeList": ["DPSCA004"]}, "DPSNY002")
