from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

try:
    from dotenv import load_dotenv
except ModuleNotFoundError:  # Allows imports before dependencies are installed.
    def load_dotenv(*_args: object, **_kwargs: object) -> bool:
        return False


DEFAULT_API_BASE_URL = "https://api.xlwms.com/openapi"
DEFAULT_CREDENTIAL_KEY_FILE = (
    Path(__file__).resolve().parents[2] / ".warehouse_credentials_key"
)


@dataclass(frozen=True)
class Settings:
    database_url: str | None
    credential_key_file: Path
    api_base_url: str
    app_key: str | None
    app_secret: str | None

    @property
    def credentials_configured(self) -> bool:
        return bool(self.app_key and self.app_secret)


def load_settings(env_file: str | Path | None = None) -> Settings:
    load_dotenv(dotenv_path=env_file, override=False)
    return Settings(
        database_url=os.getenv("DATABASE_URL") or None,
        credential_key_file=Path(
            os.getenv("XLWMS_CREDENTIAL_KEY_FILE", DEFAULT_CREDENTIAL_KEY_FILE)
        ).expanduser(),
        api_base_url=os.getenv("XLWMS_API_BASE_URL", DEFAULT_API_BASE_URL).rstrip("/"),
        app_key=os.getenv("XLWMS_APP_KEY") or None,
        app_secret=os.getenv("XLWMS_APP_SECRET") or None,
    )
