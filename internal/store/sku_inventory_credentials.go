package store

import (
	"context"
	"errors"
	"xlwms-api-manager/internal/model"
)

func (p *Postgres) InventoryCredentialsForSKUs(ctx context.Context, skus []string) (map[string][]model.WarehouseCredentials, error) {
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT i.warehouse_sku,i.wh_code,c.credential_key,b.account_key,c.api_base_url,c.app_key_ciphertext,c.app_secret_ciphertext,
 c.is_active AND a.enabled AND coalesce(w.is_active,false) AND c.inventory_sync_status='ready'
 FROM xlwms_api_credential_inventory i
 JOIN xlwms_api_credentials c USING(credential_key)
 JOIN xlwms_oms_account_api_credentials b USING(credential_key)
 JOIN xlwms_oms_accounts a ON a.account_key=b.account_key
 LEFT JOIN xlwms_warehouses w ON w.wh_code=i.wh_code
 WHERE i.warehouse_sku=ANY($1)
 ORDER BY i.warehouse_sku,i.wh_code,c.credential_key`, skus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]model.WarehouseCredentials, len(skus))
	for _, sku := range skus {
		result[sku] = nil
	}
	seen := map[string]bool{}
	for rows.Next() {
		var sku, encryptedKey, encryptedSecret string
		var credential model.WarehouseCredentials
		var ready bool
		if err := rows.Scan(&sku, &credential.Code, &credential.APICredentialKey, &credential.OMSAccountKey, &credential.APIBaseURL, &encryptedKey, &encryptedSecret, &ready); err != nil {
			return nil, err
		}
		if !ready {
			return nil, errors.New("SKU API inventory scope is not ready; manual review required")
		}
		if seen[sku+"\x00"+credential.Code] {
			return nil, errors.New("SKU warehouse has multiple API scopes; manual review required")
		}
		seen[sku+"\x00"+credential.Code] = true
		credential.AppKey, err = p.cipher.Decrypt(encryptedKey)
		if err != nil {
			return nil, errors.New("API credential decryption failed")
		}
		credential.AppSecret, err = p.cipher.Decrypt(encryptedSecret)
		if err != nil {
			return nil, errors.New("API credential decryption failed")
		}
		credential.Active = true
		result[sku] = append(result[sku], credential)
	}
	return result, rows.Err()
}
