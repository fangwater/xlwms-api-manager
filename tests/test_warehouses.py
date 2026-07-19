from __future__ import annotations

from pathlib import Path

from cryptography.fernet import Fernet

from xlwms_api_manager.warehouses import (
    ensure_credential_key_file,
    mask_app_key,
)


def test_ensure_credential_key_file_creates_private_valid_key(tmp_path: Path) -> None:
    path = tmp_path / "warehouse.key"

    key = ensure_credential_key_file(path)

    Fernet(key)
    assert path.stat().st_mode & 0o777 == 0o600
    assert ensure_credential_key_file(path) == key


def test_mask_app_key_keeps_only_a_short_hint() -> None:
    assert mask_app_key("1234567890abcdef") == "1234...cdef"
    assert mask_app_key("short") == "*****"
