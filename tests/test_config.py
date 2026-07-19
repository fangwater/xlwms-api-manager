from __future__ import annotations

from pathlib import Path

from xlwms_api_manager.config import DEFAULT_API_BASE_URL, load_settings


def test_load_settings_uses_xlwms_defaults(monkeypatch) -> None:
    monkeypatch.delenv("DATABASE_URL", raising=False)
    monkeypatch.delenv("XLWMS_CREDENTIAL_KEY_FILE", raising=False)
    monkeypatch.delenv("XLWMS_API_BASE_URL", raising=False)
    monkeypatch.delenv("XLWMS_APP_KEY", raising=False)
    monkeypatch.delenv("XLWMS_APP_SECRET", raising=False)

    settings = load_settings(env_file="/does/not/exist")

    assert settings.database_url is None
    assert settings.credential_key_file.name == ".warehouse_credentials_key"
    assert settings.api_base_url == DEFAULT_API_BASE_URL
    assert settings.app_key is None
    assert settings.app_secret is None
    assert settings.credentials_configured is False


def test_load_settings_reads_credentials_without_exposing_them(monkeypatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgresql://example.test/xlwms")
    monkeypatch.setenv("XLWMS_CREDENTIAL_KEY_FILE", "~/xlwms-test.key")
    monkeypatch.setenv("XLWMS_API_BASE_URL", "https://example.test/openapi/")
    monkeypatch.setenv("XLWMS_APP_KEY", "app-key")
    monkeypatch.setenv("XLWMS_APP_SECRET", "app-secret")

    settings = load_settings(env_file="/does/not/exist")

    assert settings.database_url == "postgresql://example.test/xlwms"
    assert settings.credential_key_file == Path("~/xlwms-test.key").expanduser()
    assert settings.api_base_url == "https://example.test/openapi"
    assert settings.app_key == "app-key"
    assert settings.app_secret == "app-secret"
    assert settings.credentials_configured is True
