package config

import (
	"path/filepath"
	"testing"
)

func TestLoadUsesXLWMSDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://example.test/xlwms")
	t.Setenv("XLWMS_CREDENTIAL_KEY_FILE", "")
	t.Setenv("XLWMS_API_BASE_URL", "")
	t.Setenv("XLWMS_LISTEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("unexpected base URL %q", cfg.APIBaseURL)
	}
	if cfg.Listen != DefaultListen {
		t.Fatalf("unexpected listen address %q", cfg.Listen)
	}
	if filepath.Base(cfg.CredentialKeyFile) != ".warehouse_credentials_key" {
		t.Fatalf("unexpected key path %q", cfg.CredentialKeyFile)
	}
}
