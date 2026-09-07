package store

import (
	"context"
	"errors"
	"fmt"
)

type APIScopeCredentials struct {
	BaseURL   string `json:"-"`
	AppKey    string `json:"-"`
	AppSecret string `json:"-"`
}

func (p *Postgres) APIScopeCredentials(ctx context.Context, key string) (APIScopeCredentials, error) {
	var result APIScopeCredentials
	var appKey, appSecret string
	err := p.pool.QueryRow(ctx, `SELECT api_base_url,app_key_ciphertext,app_secret_ciphertext
 FROM xlwms_api_credentials WHERE credential_key=$1 AND is_active`, key).Scan(&result.BaseURL, &appKey, &appSecret)
	if err != nil {
		return result, errors.New("active API credential unavailable")
	}
	result.AppKey, err = p.cipher.Decrypt(appKey)
	if err != nil {
		return APIScopeCredentials{}, errors.New("API credential decryption failed")
	}
	result.AppSecret, err = p.cipher.Decrypt(appSecret)
	if err != nil {
		return APIScopeCredentials{}, errors.New("API credential decryption failed")
	}
	return result, nil
}

func (p *Postgres) SaveAPIScopeInventory(ctx context.Context, key string, inventory []WarehouseAPIInventoryItem) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE xlwms_api_credentials SET inventory_sync_status='ready',last_verified_at=now()
 WHERE credential_key=$1 AND is_active`, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("active API credential unavailable")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_api_credential_inventory WHERE credential_key=$1`, key); err != nil {
		return err
	}
	for _, item := range normalizeWarehouseAPIInventory(inventory) {
		if _, err := tx.Exec(ctx, `INSERT INTO xlwms_api_credential_inventory
  (credential_key,wh_code,warehouse_name,warehouse_sku,product_name,last_seen_at)
  VALUES($1,$2,$3,$4,$5,now())`, key, item.WarehouseCode, item.WarehouseName, item.WarehouseSKU, item.ProductName); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit API inventory scope: %w", err)
	}
	return p.registerDiscoveredWarehouses(ctx, key)
}

func (p *Postgres) FailAPIScopeInventory(ctx context.Context, key string) error {
	_, err := p.pool.Exec(ctx, `UPDATE xlwms_api_credentials SET inventory_sync_status='failed' WHERE credential_key=$1`, key)
	return err
}

// registerDiscoveredWarehouses makes a new API-discovered warehouse eligible
// for scheduled inventory refreshes. It never re-enables an existing warehouse.
func (p *Postgres) registerDiscoveredWarehouses(ctx context.Context, key string) error {
	_, err := p.pool.Exec(ctx, `
WITH discovered AS (
    SELECT upper(btrim(wh_code)) AS wh_code,
           coalesce(nullif(max(btrim(warehouse_name)),''), upper(btrim(wh_code))) AS warehouse_name
    FROM xlwms_api_credential_inventory
    WHERE credential_key=$1 AND btrim(wh_code)<>''
    GROUP BY upper(btrim(wh_code))
)
INSERT INTO xlwms_warehouses (
    wh_code,warehouse_name,api_base_url,app_key_ciphertext,app_secret_ciphertext,
    app_key_hint,is_active,disabled_at,updated_at
)
SELECT discovered.wh_code,discovered.warehouse_name,credential.api_base_url,
       credential.app_key_ciphertext,credential.app_secret_ciphertext,
       credential.app_key_hint,true,NULL,now()
FROM discovered
JOIN xlwms_api_credentials credential ON credential.credential_key=$1 AND credential.is_active
ON CONFLICT (wh_code) DO UPDATE SET
    warehouse_name=coalesce(nullif(xlwms_warehouses.warehouse_name,''),EXCLUDED.warehouse_name),
    updated_at=now()
`, key)
	if err != nil {
		return fmt.Errorf("register discovered warehouses: %w", err)
	}
	return nil
}
