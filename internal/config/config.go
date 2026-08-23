package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIBaseURL                 = "https://api.xlwms.com/openapi"
	DefaultOMSBaseURL                 = "https://oms.xlwms.com"
	DefaultTemuGoBaseURL              = "http://127.0.0.1:18082/temu"
	DefaultSheinGoBaseURL             = "http://127.0.0.1:18084"
	DefaultListen                     = "127.0.0.1:18083"
	DefaultInventorySyncInterval      = 3 * time.Minute
	DefaultFulfillmentTrackingLimit   = 500
	DefaultFulfillmentTrackingWorkers = 8
)

type Config struct {
	DatabaseURL                string
	CredentialKeyFile          string
	APIBaseURL                 string
	AppKey                     string
	AppSecret                  string
	OMSBaseURL                 string
	OMSUsername                string
	OMSPassword                string
	TemuGoBaseURL              string
	SheinGoBaseURL             string
	Listen                     string
	RequestTimeout             time.Duration
	SyncTimeout                time.Duration
	InventorySyncInterval      time.Duration
	FulfillmentAuditInterval   time.Duration
	FulfillmentTrackingLimit   int
	FulfillmentTrackingWorkers int
}

func Load() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		DatabaseURL:                strings.TrimSpace(os.Getenv("DATABASE_URL")),
		CredentialKeyFile:          envOrDefault("XLWMS_CREDENTIAL_KEY_FILE", filepath.Join(cwd, ".warehouse_credentials_key")),
		APIBaseURL:                 strings.TrimRight(envOrDefault("XLWMS_API_BASE_URL", DefaultAPIBaseURL), "/"),
		AppKey:                     os.Getenv("XLWMS_APP_KEY"),
		AppSecret:                  os.Getenv("XLWMS_APP_SECRET"),
		OMSBaseURL:                 strings.TrimRight(envOrDefault("XLWMS_OMS_BASE_URL", DefaultOMSBaseURL), "/"),
		OMSUsername:                strings.TrimSpace(os.Getenv("XLWMS_OMS_USERNAME")),
		OMSPassword:                os.Getenv("XLWMS_OMS_PASSWORD"),
		TemuGoBaseURL:              strings.TrimRight(envOrDefault("TEMU_GO_BASE_URL", DefaultTemuGoBaseURL), "/"),
		SheinGoBaseURL:             strings.TrimRight(envOrDefault("SHEIN_GO_BASE_URL", DefaultSheinGoBaseURL), "/"),
		Listen:                     envOrDefault("XLWMS_LISTEN", DefaultListen),
		RequestTimeout:             30 * time.Second,
		SyncTimeout:                30 * time.Minute,
		InventorySyncInterval:      DefaultInventorySyncInterval,
		FulfillmentAuditInterval:   time.Hour,
		FulfillmentTrackingLimit:   DefaultFulfillmentTrackingLimit,
		FulfillmentTrackingWorkers: DefaultFulfillmentTrackingWorkers,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if (cfg.OMSUsername == "") != (cfg.OMSPassword == "") {
		return Config{}, errors.New("XLWMS_OMS_USERNAME and XLWMS_OMS_PASSWORD must be configured together")
	}
	if cfg.RequestTimeout, err = positiveDuration("XLWMS_REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.SyncTimeout, err = positiveDuration("XLWMS_SYNC_TIMEOUT", cfg.SyncTimeout); err != nil {
		return Config{}, err
	}
	if cfg.InventorySyncInterval, err = positiveDuration("XLWMS_INVENTORY_SYNC_INTERVAL", cfg.InventorySyncInterval); err != nil {
		return Config{}, err
	}
	if cfg.FulfillmentAuditInterval, err = positiveDuration("XLWMS_FULFILLMENT_AUDIT_INTERVAL", cfg.FulfillmentAuditInterval); err != nil {
		return Config{}, err
	}
	if cfg.FulfillmentTrackingLimit, err = PositiveInt("XLWMS_FULFILLMENT_TRACKING_LIMIT", cfg.FulfillmentTrackingLimit); err != nil {
		return Config{}, err
	}
	if cfg.FulfillmentTrackingWorkers, err = PositiveInt("XLWMS_FULFILLMENT_TRACKING_CONCURRENCY", cfg.FulfillmentTrackingWorkers); err != nil {
		return Config{}, err
	}
	if cfg.FulfillmentTrackingWorkers > 32 {
		return Config{}, errors.New("XLWMS_FULFILLMENT_TRACKING_CONCURRENCY must not exceed 32")
	}
	return cfg, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func positiveDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, errors.New(name + " must be a positive duration")
	}
	return value, nil
}

func PositiveInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New(name + " must be a positive integer")
	}
	return value, nil
}
