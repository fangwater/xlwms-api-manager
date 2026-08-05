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
	DefaultAPIBaseURL = "https://api.xlwms.com/openapi"
	DefaultListen     = "127.0.0.1:18083"
)

type Config struct {
	DatabaseURL              string
	CredentialKeyFile        string
	APIBaseURL               string
	AppKey                   string
	AppSecret                string
	Listen                   string
	RequestTimeout           time.Duration
	SyncTimeout              time.Duration
	FulfillmentAuditInterval time.Duration
}

func Load() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		CredentialKeyFile:        envOrDefault("XLWMS_CREDENTIAL_KEY_FILE", filepath.Join(cwd, ".warehouse_credentials_key")),
		APIBaseURL:               strings.TrimRight(envOrDefault("XLWMS_API_BASE_URL", DefaultAPIBaseURL), "/"),
		AppKey:                   os.Getenv("XLWMS_APP_KEY"),
		AppSecret:                os.Getenv("XLWMS_APP_SECRET"),
		Listen:                   envOrDefault("XLWMS_LISTEN", DefaultListen),
		RequestTimeout:           30 * time.Second,
		SyncTimeout:              30 * time.Minute,
		FulfillmentAuditInterval: time.Hour,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.RequestTimeout, err = positiveDuration("XLWMS_REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.SyncTimeout, err = positiveDuration("XLWMS_SYNC_TIMEOUT", cfg.SyncTimeout); err != nil {
		return Config{}, err
	}
	if cfg.FulfillmentAuditInterval, err = positiveDuration("XLWMS_FULFILLMENT_AUDIT_INTERVAL", cfg.FulfillmentAuditInterval); err != nil {
		return Config{}, err
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
