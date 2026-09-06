package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

var (
	ErrOMSAccountNotFound = errors.New("OMS account is not configured")
	ErrOMSAccountDisabled = errors.New("OMS account is disabled")
	ErrOMSAccountExists   = errors.New("OMS account already exists")
)

func normalizeOMSAccountKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func validateOMSAccountIdentity(key, label string) (string, string, error) {
	key = normalizeOMSAccountKey(key)
	label = strings.TrimSpace(label)
	if key == "" || len(key) > 64 {
		return "", "", fmt.Errorf("%w: account key must contain 1 to 64 characters", ErrInvalidFulfillmentAccount)
	}
	for index, character := range key {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			(character != '-' || index == 0) && (character != '_' || index == 0) {
			return "", "", fmt.Errorf("%w: account key may only contain lowercase letters, numbers, hyphens, and underscores", ErrInvalidFulfillmentAccount)
		}
	}
	if label == "" || len([]rune(label)) > 100 {
		return "", "", fmt.Errorf("%w: account label must contain 1 to 100 characters", ErrInvalidFulfillmentAccount)
	}
	return key, label, nil
}

func NormalizeOMSAccountIdentity(key, label string) (string, string, error) {
	return validateOMSAccountIdentity(key, label)
}

func validateOMSLogin(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("OMS username and password are required")
	}
	if len(username) > 200 || len(password) > 500 {
		return errors.New("OMS username or password is too long")
	}
	return nil
}

func (p *Postgres) OMSAccount(ctx context.Context, key string) (model.OMSLoginAccount, error) {
	key = normalizeOMSAccountKey(key)
	if key == "" {
		return model.OMSLoginAccount{}, ErrOMSAccountNotFound
	}
	var usernameCiphertext, passwordCiphertext, hint, label string
	var enabled bool
	err := p.pool.QueryRow(ctx, `
		SELECT account_key, username_ciphertext, password_ciphertext, account_hint, account_label, enabled
		FROM xlwms_oms_accounts WHERE account_key = $1
	`, key).Scan(&key, &usernameCiphertext, &passwordCiphertext, &hint, &label, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.OMSLoginAccount{}, ErrOMSAccountNotFound
	}
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("get OMS account: %w", err)
	}
	if !enabled {
		return model.OMSLoginAccount{}, ErrOMSAccountDisabled
	}
	username, err := p.cipher.Decrypt(usernameCiphertext)
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("decrypt OMS username: %w", err)
	}
	password, err := p.cipher.Decrypt(passwordCiphertext)
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("decrypt OMS password: %w", err)
	}
	return model.OMSLoginAccount{Key: key, Label: label, Username: username, Password: password, Hint: hint, Enabled: true}, nil
}

func (p *Postgres) SetOMSAccount(ctx context.Context, key, username, password string) (model.OMSLoginAccount, error) {
	key = normalizeOMSAccountKey(key)
	username = strings.TrimSpace(username)
	if key == "" {
		return model.OMSLoginAccount{}, errors.New("OMS account key is required")
	}
	if err := validateOMSLogin(username, password); err != nil {
		return model.OMSLoginAccount{}, err
	}
	usernameCiphertext, err := p.cipher.Encrypt(username)
	if err != nil {
		return model.OMSLoginAccount{}, err
	}
	passwordCiphertext, err := p.cipher.Encrypt(password)
	if err != nil {
		return model.OMSLoginAccount{}, err
	}
	hint := credentials.MaskIdentifier(username)
	var label string
	if err := p.pool.QueryRow(ctx, `
		INSERT INTO xlwms_oms_accounts (account_key, username_ciphertext, password_ciphertext, account_hint, account_label, enabled, updated_at)
		VALUES ($1, $2, $3, $4, $5, true, now())
		ON CONFLICT (account_key) DO UPDATE SET
			username_ciphertext = EXCLUDED.username_ciphertext,
			password_ciphertext = EXCLUDED.password_ciphertext,
			account_hint = EXCLUDED.account_hint,
			enabled = true,
			updated_at = now()
		RETURNING account_label
	`, key, usernameCiphertext, passwordCiphertext, hint, defaultOMSAccountLabel(key)).Scan(&label); err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("save OMS account: %w", err)
	}
	return model.OMSLoginAccount{Key: key, Label: label, Username: username, Password: password, Hint: hint, Enabled: true}, nil
}

func (p *Postgres) CreateOMSAccount(ctx context.Context, key, label, username, password string, credentialKeys []string) (model.OMSAccountSummary, error) {
	key, label, err := validateOMSAccountIdentity(key, label)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	username = strings.TrimSpace(username)
	if err := validateOMSLogin(username, password); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("%w: %v", ErrInvalidFulfillmentAccount, err)
	}
	credentialKeys, err = normalizeAPICredentialKeys(credentialKeys)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	usernameCiphertext, err := p.cipher.Encrypt(username)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	passwordCiphertext, err := p.cipher.Encrypt(password)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("begin OMS account create: %w", err)
	}
	defer tx.Rollback(ctx)
	if len(credentialKeys) > 0 {
		var credentialCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM xlwms_api_credentials WHERE credential_key=ANY($1)`, credentialKeys).Scan(&credentialCount); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("check OMS account API credentials: %w", err)
		}
		if credentialCount != len(credentialKeys) {
			return model.OMSAccountSummary{}, fmt.Errorf("%w: unknown OpenAPI credential", ErrInvalidFulfillmentAccount)
		}
	}
	var createdKey string
	err = tx.QueryRow(ctx, `
INSERT INTO xlwms_oms_accounts(
    account_key,username_ciphertext,password_ciphertext,account_hint,account_label,enabled,updated_at
) VALUES($1,$2,$3,$4,$5,true,now())
ON CONFLICT(account_key) DO NOTHING
RETURNING account_key
`, key, usernameCiphertext, passwordCiphertext, credentials.MaskIdentifier(username), label).Scan(&createdKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.OMSAccountSummary{}, ErrOMSAccountExists
	}
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("create OMS account: %w", err)
	}
	for _, credentialKey := range credentialKeys {
		if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_oms_account_api_credentials(credential_key,account_key,updated_at)
VALUES($1,$2,now())
ON CONFLICT(credential_key) DO UPDATE SET account_key=EXCLUDED.account_key,updated_at=now()
`, credentialKey, key); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("save OMS account API credential: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("commit OMS account create: %w", err)
	}
	return p.OMSAccountSummary(ctx, key)
}

func (p *Postgres) UpdateOMSAccountMetadata(ctx context.Context, key string, label *string, enabled *bool) (model.OMSAccountSummary, error) {
	key = normalizeOMSAccountKey(key)
	if key == "" || len(key) > 64 {
		return model.OMSAccountSummary{}, fmt.Errorf("%w: invalid account key", ErrInvalidFulfillmentAccount)
	}
	if label == nil && enabled == nil {
		return model.OMSAccountSummary{}, fmt.Errorf("%w: account label or enabled status is required", ErrInvalidFulfillmentAccount)
	}
	var normalizedLabel *string
	if label != nil {
		_, value, err := validateOMSAccountIdentity(key, *label)
		if err != nil {
			return model.OMSAccountSummary{}, err
		}
		normalizedLabel = &value
	}
	command, err := p.pool.Exec(ctx, `
UPDATE xlwms_oms_accounts
SET account_label=coalesce($2,account_label),enabled=coalesce($3,enabled),updated_at=now()
WHERE account_key=$1
`, key, normalizedLabel, enabled)
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("update OMS account: %w", err)
	}
	if command.RowsAffected() == 0 {
		return model.OMSAccountSummary{}, ErrOMSAccountNotFound
	}
	return p.OMSAccountSummary(ctx, key)
}

func defaultOMSAccountLabel(key string) string {
	return strings.TrimSpace(key)
}
