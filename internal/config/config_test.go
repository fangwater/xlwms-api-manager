package config

import (
	"path/filepath"
	"testing"
)

func TestLoadUsesXLWMSDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://example.test/xlwms")
	t.Setenv("XLWMS_CREDENTIAL_KEY_FILE", "")
	t.Setenv("XLWMS_API_BASE_URL", "")
	t.Setenv("XLWMS_OMS_BASE_URL", "")
	t.Setenv("XLWMS_OMS_USERNAME", "")
	t.Setenv("XLWMS_OMS_PASSWORD", "")
	t.Setenv("TEMU_GO_BASE_URL", "")
	t.Setenv("SHEIN_GO_BASE_URL", "")
	t.Setenv("XLWMS_INVENTORY_SYNC_INTERVAL", "")
	t.Setenv("XLWMS_FULFILLMENT_TRACKING_LIMIT", "")
	t.Setenv("XLWMS_FULFILLMENT_TRACKING_CONCURRENCY", "")
	t.Setenv("XLWMS_LISTEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("unexpected base URL %q", cfg.APIBaseURL)
	}
	if cfg.OMSBaseURL != DefaultOMSBaseURL {
		t.Fatalf("unexpected OMS base URL %q", cfg.OMSBaseURL)
	}
	if cfg.TemuGoBaseURL != DefaultTemuGoBaseURL {
		t.Fatalf("unexpected Temu Go base URL %q", cfg.TemuGoBaseURL)
	}
	if cfg.SheinGoBaseURL != DefaultSheinGoBaseURL {
		t.Fatalf("unexpected SHEIN Go base URL %q", cfg.SheinGoBaseURL)
	}
	if cfg.FulfillmentTrackingLimit != DefaultFulfillmentTrackingLimit || cfg.FulfillmentTrackingWorkers != DefaultFulfillmentTrackingWorkers {
		t.Fatalf("unexpected tracking worker configuration: limit=%d workers=%d", cfg.FulfillmentTrackingLimit, cfg.FulfillmentTrackingWorkers)
	}
	if cfg.InventorySyncInterval != DefaultInventorySyncInterval {
		t.Fatalf("unexpected inventory sync interval %s", cfg.InventorySyncInterval)
	}
	if cfg.Listen != DefaultListen {
		t.Fatalf("unexpected listen address %q", cfg.Listen)
	}
	if filepath.Base(cfg.CredentialKeyFile) != ".warehouse_credentials_key" {
		t.Fatalf("unexpected key path %q", cfg.CredentialKeyFile)
	}
}

func TestLoadRejectsInvalidInventorySyncInterval(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://example.test/xlwms")
	t.Setenv("XLWMS_OMS_USERNAME", "")
	t.Setenv("XLWMS_OMS_PASSWORD", "")
	t.Setenv("XLWMS_INVENTORY_SYNC_INTERVAL", "0s")
	if _, err := Load(); err == nil || err.Error() != "XLWMS_INVENTORY_SYNC_INTERVAL must be a positive duration" {
		t.Fatalf("unexpected error: %v", err)
	}
}
