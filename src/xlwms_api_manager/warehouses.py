from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

import psycopg
from cryptography.fernet import Fernet, InvalidToken


@dataclass(frozen=True)
class WarehouseSummary:
    wh_code: str
    warehouse_name: str | None
    api_base_url: str
    app_key_hint: str
    is_active: bool


@dataclass(frozen=True)
class WarehouseCredentials(WarehouseSummary):
    app_key: str
    app_secret: str


def ensure_credential_key_file(path: Path) -> bytes:
    path = path.expanduser()
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError:
        pass
    else:
        with os.fdopen(fd, "wb") as handle:
            handle.write(Fernet.generate_key() + b"\n")
    path.chmod(0o600)
    key = path.read_bytes().strip()
    try:
        Fernet(key)
    except (ValueError, TypeError) as exc:
        raise ValueError(f"invalid credential key file: {path}") from exc
    return key


def load_credential_key_file(path: Path) -> bytes:
    path = path.expanduser()
    if not path.exists():
        raise ValueError(f"credential key file does not exist: {path}")
    return ensure_credential_key_file(path)


def mask_app_key(app_key: str) -> str:
    if len(app_key) <= 8:
        return "*" * len(app_key)
    return f"{app_key[:4]}...{app_key[-4:]}"


def upsert_warehouse(
    database_url: str,
    encryption_key: bytes,
    *,
    wh_code: str,
    warehouse_name: str | None,
    api_base_url: str,
    app_key: str,
    app_secret: str,
    is_active: bool = True,
) -> WarehouseSummary:
    wh_code = wh_code.strip().upper()
    if not wh_code:
        raise ValueError("whCode is required")
    app_key = app_key.strip()
    app_secret = app_secret.strip()
    if not app_key or not app_secret:
        raise ValueError("appKey and appSecret are required")
    cipher = Fernet(encryption_key)
    app_key_ciphertext = cipher.encrypt(app_key.encode()).decode()
    app_secret_ciphertext = cipher.encrypt(app_secret.encode()).decode()
    app_key_hint = mask_app_key(app_key)
    api_base_url = api_base_url.rstrip("/")

    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO xlwms_warehouses (
                    wh_code, warehouse_name, api_base_url,
                    app_key_ciphertext, app_secret_ciphertext, app_key_hint,
                    is_active, disabled_at, updated_at
                )
                VALUES (%s, %s, %s, %s, %s, %s, %s,
                        CASE WHEN %s THEN NULL ELSE now() END, now())
                ON CONFLICT (wh_code) DO UPDATE SET
                    warehouse_name = EXCLUDED.warehouse_name,
                    api_base_url = EXCLUDED.api_base_url,
                    app_key_ciphertext = EXCLUDED.app_key_ciphertext,
                    app_secret_ciphertext = EXCLUDED.app_secret_ciphertext,
                    app_key_hint = EXCLUDED.app_key_hint,
                    is_active = EXCLUDED.is_active,
                    disabled_at = CASE
                        WHEN EXCLUDED.is_active THEN NULL
                        ELSE COALESCE(xlwms_warehouses.disabled_at, now())
                    END,
                    updated_at = now()
                RETURNING wh_code, warehouse_name, api_base_url, app_key_hint, is_active
                """,
                (
                    wh_code,
                    warehouse_name.strip() if warehouse_name else None,
                    api_base_url,
                    app_key_ciphertext,
                    app_secret_ciphertext,
                    app_key_hint,
                    is_active,
                    is_active,
                ),
            )
            row = cur.fetchone()
    return WarehouseSummary(*row)


def list_warehouses(
    database_url: str,
    *,
    active_only: bool = False,
) -> list[WarehouseSummary]:
    where = "WHERE is_active" if active_only else ""
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                f"""
                SELECT wh_code, warehouse_name, api_base_url, app_key_hint, is_active
                FROM xlwms_warehouses
                {where}
                ORDER BY wh_code
                """
            )
            return [WarehouseSummary(*row) for row in cur.fetchall()]


def list_active_warehouse_credentials(
    database_url: str,
    encryption_key: bytes,
) -> list[WarehouseCredentials]:
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT wh_code, warehouse_name, api_base_url, app_key_hint, is_active,
                       app_key_ciphertext, app_secret_ciphertext
                FROM xlwms_warehouses
                WHERE is_active
                ORDER BY wh_code
                """
            )
            rows = cur.fetchall()
    return [_decrypt_credentials(row, encryption_key) for row in rows]


def get_warehouse_credentials(
    database_url: str,
    encryption_key: bytes,
    wh_code: str,
    *,
    require_active: bool = True,
) -> WarehouseCredentials:
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT wh_code, warehouse_name, api_base_url, app_key_hint, is_active,
                       app_key_ciphertext, app_secret_ciphertext
                FROM xlwms_warehouses
                WHERE wh_code = %s
                """,
                (wh_code.strip().upper(),),
            )
            row = cur.fetchone()
    if row is None:
        raise ValueError(f"unknown warehouse: {wh_code}")
    if require_active and not row[4]:
        raise ValueError(f"warehouse is disabled: {row[0]}")
    return _decrypt_credentials(row, encryption_key)


def set_warehouse_active(
    database_url: str,
    wh_code: str,
    *,
    is_active: bool,
) -> WarehouseSummary:
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE xlwms_warehouses
                SET is_active = %s,
                    disabled_at = CASE WHEN %s THEN NULL ELSE now() END,
                    updated_at = now()
                WHERE wh_code = %s
                RETURNING wh_code, warehouse_name, api_base_url, app_key_hint, is_active
                """,
                (is_active, is_active, wh_code.strip().upper()),
            )
            row = cur.fetchone()
    if row is None:
        raise ValueError(f"unknown warehouse: {wh_code}")
    return WarehouseSummary(*row)


def _decrypt_credentials(row: tuple, encryption_key: bytes) -> WarehouseCredentials:
    cipher = Fernet(encryption_key)
    try:
        app_key = cipher.decrypt(row[5].encode()).decode()
        app_secret = cipher.decrypt(row[6].encode()).decode()
    except InvalidToken as exc:
        raise ValueError(f"cannot decrypt credentials for warehouse: {row[0]}") from exc
    return WarehouseCredentials(*row[:5], app_key=app_key, app_secret=app_secret)
