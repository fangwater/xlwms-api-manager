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

var ErrWarehouseOMSAccountNotConfigured = errors.New("warehouse OMS account is not configured")

func (p *Postgres) ListWarehousesWithOMS(ctx context.Context, activeOnly bool) ([]model.WarehouseSummary, error) {
	where := ""
	if activeOnly {
		where = "WHERE is_active"
	}
	rows, err := p.pool.Query(ctx, `
		SELECT wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint,
		       oms_username_ciphertext IS NOT NULL AND oms_password_ciphertext IS NOT NULL,
		       coalesce(oms_account_hint, ''), is_active, updated_at
		FROM xlwms_warehouses `+where+` ORDER BY wh_code
	`)
	if err != nil {
		return nil, fmt.Errorf("list warehouses with OMS accounts: %w", err)
	}
	defer rows.Close()
	warehouses := make([]model.WarehouseSummary, 0)
	for rows.Next() {
		var warehouse model.WarehouseSummary
		if err := rows.Scan(
			&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint,
			&warehouse.OMSAccountConfigured, &warehouse.OMSAccountHint, &warehouse.Active, &warehouse.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan warehouse with OMS account: %w", err)
		}
		warehouses = append(warehouses, warehouse)
	}
	return warehouses, rows.Err()
}

func (p *Postgres) SetWarehouseOMSAccount(ctx context.Context, code, username, password string) (model.WarehouseSummary, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	username = strings.TrimSpace(username)
	if code == "" || username == "" || password == "" {
		return model.WarehouseSummary{}, errors.New("warehouse code, OMS username and password are required")
	}
	if len(username) > 200 || len(password) > 500 {
		return model.WarehouseSummary{}, errors.New("OMS username or password is too long")
	}
	usernameCiphertext, err := p.cipher.Encrypt(username)
	if err != nil {
		return model.WarehouseSummary{}, err
	}
	passwordCiphertext, err := p.cipher.Encrypt(password)
	if err != nil {
		return model.WarehouseSummary{}, err
	}
	var warehouse model.WarehouseSummary
	err = p.pool.QueryRow(ctx, `
		UPDATE xlwms_warehouses SET
			oms_username_ciphertext = $2,
			oms_password_ciphertext = $3,
			oms_account_hint = $4,
			updated_at = now()
		WHERE wh_code = $1
		RETURNING wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint,
		          true, coalesce(oms_account_hint, ''), is_active, updated_at
	`, code, usernameCiphertext, passwordCiphertext, credentials.MaskIdentifier(username)).Scan(
		&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint,
		&warehouse.OMSAccountConfigured, &warehouse.OMSAccountHint, &warehouse.Active, &warehouse.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseSummary{}, errors.New("unknown warehouse")
	}
	if err != nil {
		return model.WarehouseSummary{}, fmt.Errorf("set warehouse OMS account: %w", err)
	}
	return warehouse, nil
}

func (p *Postgres) ClearWarehouseOMSAccount(ctx context.Context, code string) (model.WarehouseSummary, error) {
	var warehouse model.WarehouseSummary
	err := p.pool.QueryRow(ctx, `
		UPDATE xlwms_warehouses SET
			oms_username_ciphertext = NULL,
			oms_password_ciphertext = NULL,
			oms_account_hint = NULL,
			updated_at = now()
		WHERE wh_code = $1
		RETURNING wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint,
		          false, '', is_active, updated_at
	`, strings.ToUpper(strings.TrimSpace(code))).Scan(
		&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint,
		&warehouse.OMSAccountConfigured, &warehouse.OMSAccountHint, &warehouse.Active, &warehouse.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseSummary{}, errors.New("unknown warehouse")
	}
	if err != nil {
		return model.WarehouseSummary{}, fmt.Errorf("clear warehouse OMS account: %w", err)
	}
	return warehouse, nil
}

func (p *Postgres) WarehouseOMSAccount(ctx context.Context, code string, requireActive bool) (model.WarehouseOMSAccount, error) {
	var result model.WarehouseOMSAccount
	var usernameCiphertext, passwordCiphertext *string
	var active bool
	err := p.pool.QueryRow(ctx, `
		SELECT wh_code, oms_username_ciphertext, oms_password_ciphertext, is_active
		FROM xlwms_warehouses WHERE wh_code = $1
	`, strings.ToUpper(strings.TrimSpace(code))).Scan(
		&result.WarehouseCode, &usernameCiphertext, &passwordCiphertext, &active,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseOMSAccount{}, errors.New("unknown warehouse")
	}
	if err != nil {
		return model.WarehouseOMSAccount{}, fmt.Errorf("get warehouse OMS account: %w", err)
	}
	if requireActive && !active {
		return model.WarehouseOMSAccount{}, errors.New("warehouse is disabled")
	}
	if usernameCiphertext == nil || passwordCiphertext == nil {
		return model.WarehouseOMSAccount{}, ErrWarehouseOMSAccountNotConfigured
	}
	if result.Username, err = p.cipher.Decrypt(*usernameCiphertext); err != nil {
		return model.WarehouseOMSAccount{}, fmt.Errorf("decrypt OMS username for %s: %w", result.WarehouseCode, err)
	}
	if result.Password, err = p.cipher.Decrypt(*passwordCiphertext); err != nil {
		return model.WarehouseOMSAccount{}, fmt.Errorf("decrypt OMS password for %s: %w", result.WarehouseCode, err)
	}
	return result, nil
}
