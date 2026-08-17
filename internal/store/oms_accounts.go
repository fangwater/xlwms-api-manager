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

var ErrOMSAccountNotFound = errors.New("OMS account is not configured")

func normalizeOMSAccountKey(key string) string {
	return strings.TrimSpace(key)
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
	var usernameCiphertext, passwordCiphertext, hint string
	err := p.pool.QueryRow(ctx, `
		SELECT account_key, username_ciphertext, password_ciphertext, account_hint
		FROM xlwms_oms_accounts WHERE account_key = $1
	`, key).Scan(&key, &usernameCiphertext, &passwordCiphertext, &hint)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.OMSLoginAccount{}, ErrOMSAccountNotFound
	}
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("get OMS account: %w", err)
	}
	username, err := p.cipher.Decrypt(usernameCiphertext)
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("decrypt OMS username: %w", err)
	}
	password, err := p.cipher.Decrypt(passwordCiphertext)
	if err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("decrypt OMS password: %w", err)
	}
	return model.OMSLoginAccount{Key: key, Username: username, Password: password, Hint: hint}, nil
}

func (p *Postgres) EnsureOMSAccount(ctx context.Context, key, username, password string) error {
	key = normalizeOMSAccountKey(key)
	if key == "" {
		return errors.New("OMS account key is required")
	}
	if err := validateOMSLogin(username, password); err != nil {
		return err
	}
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xlwms_oms_accounts WHERE account_key = $1)`, key).Scan(&exists); err != nil {
		return fmt.Errorf("check OMS account: %w", err)
	}
	if exists {
		return nil
	}
	_, err := p.SetOMSAccount(ctx, key, username, password)
	return err
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
	if _, err := p.pool.Exec(ctx, `
		INSERT INTO xlwms_oms_accounts (account_key, username_ciphertext, password_ciphertext, account_hint, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (account_key) DO UPDATE SET
			username_ciphertext = EXCLUDED.username_ciphertext,
			password_ciphertext = EXCLUDED.password_ciphertext,
			account_hint = EXCLUDED.account_hint,
			updated_at = now()
	`, key, usernameCiphertext, passwordCiphertext, hint); err != nil {
		return model.OMSLoginAccount{}, fmt.Errorf("save OMS account: %w", err)
	}
	return model.OMSLoginAccount{Key: key, Username: username, Password: password, Hint: hint}, nil
}
