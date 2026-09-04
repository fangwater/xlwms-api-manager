package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

type WarehouseAPIInventoryItem struct {
	WarehouseCode string
	WarehouseName string
	WarehouseSKU  string
	ProductName   string
}

var ErrWarehouseAPICredentialInUse = errors.New("warehouse API credential is used by an operational warehouse")

type legacyWarehouseAPIGroup struct {
	key                 string
	label               string
	baseURL             string
	appKeyCiphertext    string
	appSecretCiphertext string
	appKeyHint          string
	active              bool
	warehouses          map[string]string
}

func warehouseAPICredentialKey(baseURL, appKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(baseURL), "/") + "\x00" + strings.TrimSpace(appKey)))
	return "api-" + hex.EncodeToString(digest[:8])
}

// EnsureWarehouseAPICredentialGroups converts the legacy one-credential-per-warehouse
// rows into independently addressable credential groups without changing live routing.
func (p *Postgres) EnsureWarehouseAPICredentialGroups(ctx context.Context) error {
	rows, err := p.pool.Query(ctx, `
SELECT wh_code,coalesce(warehouse_name,''),api_base_url,app_key_ciphertext,
       app_secret_ciphertext,app_key_hint,is_active
FROM xlwms_warehouses
ORDER BY wh_code
`)
	if err != nil {
		return fmt.Errorf("list legacy warehouse API credentials: %w", err)
	}
	defer rows.Close()
	groups := make(map[string]*legacyWarehouseAPIGroup)
	for rows.Next() {
		var code, name, baseURL, appKeyCiphertext, appSecretCiphertext, hint string
		var active bool
		if err := rows.Scan(&code, &name, &baseURL, &appKeyCiphertext, &appSecretCiphertext, &hint, &active); err != nil {
			return fmt.Errorf("scan legacy warehouse API credentials: %w", err)
		}
		appKey, err := p.cipher.Decrypt(appKeyCiphertext)
		if err != nil {
			return fmt.Errorf("decrypt legacy App Key for %s: %w", code, err)
		}
		key := warehouseAPICredentialKey(baseURL, appKey)
		group := groups[key]
		if group == nil {
			group = &legacyWarehouseAPIGroup{
				key: key, label: "OpenAPI " + hint, baseURL: baseURL,
				appKeyCiphertext: appKeyCiphertext, appSecretCiphertext: appSecretCiphertext,
				appKeyHint: hint, warehouses: make(map[string]string),
			}
			groups[key] = group
		}
		group.active = group.active || active
		group.warehouses[strings.ToUpper(strings.TrimSpace(code))] = strings.TrimSpace(name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, group := range groups {
		if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_api_credentials(
    credential_key,credential_label,api_base_url,app_key_ciphertext,
    app_secret_ciphertext,app_key_hint,is_active,updated_at
) VALUES($1,$2,$3,$4,$5,$6,$7,now())
ON CONFLICT(credential_key) DO NOTHING
`, group.key, group.label, group.baseURL, group.appKeyCiphertext, group.appSecretCiphertext, group.appKeyHint, group.active); err != nil {
			return fmt.Errorf("migrate warehouse API credential group: %w", err)
		}
		for code, name := range group.warehouses {
			if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_api_credential_inventory(
    credential_key,wh_code,warehouse_name,warehouse_sku,product_name,last_seen_at
) VALUES($1,$2,$3,'','',now())
ON CONFLICT(credential_key,wh_code,warehouse_sku) DO UPDATE SET
    warehouse_name=EXCLUDED.warehouse_name,last_seen_at=now()
`, group.key, code, name); err != nil {
				return fmt.Errorf("migrate warehouse API credential scope: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit warehouse API credential migration: %w", err)
	}
	return nil
}

func (p *Postgres) ListWarehouseAPICredentialGroups(ctx context.Context, includeDisabled bool) ([]model.WarehouseAPICredentialGroup, error) {
	where := ""
	if !includeDisabled {
		where = "WHERE credential.is_active"
	}
	rows, err := p.pool.Query(ctx, `
SELECT credential.credential_key,credential.credential_label,credential.api_base_url,
       credential.app_key_hint,credential.is_active,credential.last_verified_at,credential.updated_at,
       coalesce(array_agg(DISTINCT inventory.wh_code ORDER BY inventory.wh_code)
           FILTER (WHERE inventory.wh_code<>''),'{}'::text[]),
       count(*) FILTER (WHERE inventory.warehouse_sku<>'')
FROM xlwms_api_credentials credential
LEFT JOIN xlwms_api_credential_inventory inventory
  ON inventory.credential_key=credential.credential_key
`+where+`
GROUP BY credential.credential_key
ORDER BY credential.credential_label,credential.credential_key
`)
	if err != nil {
		return nil, fmt.Errorf("list warehouse API credentials: %w", err)
	}
	items := make([]model.WarehouseAPICredentialGroup, 0)
	for rows.Next() {
		var item model.WarehouseAPICredentialGroup
		if err := rows.Scan(&item.Key, &item.Label, &item.APIBaseURL, &item.AppKeyHint, &item.Active,
			&item.LastVerifiedAt, &item.UpdatedAt, &item.WarehouseCodes, &item.SKUCount); err != nil {
			return nil, fmt.Errorf("scan warehouse API credential: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	inUse, err := p.legacyWarehouseAPICredentialKeys(ctx)
	if err != nil {
		return nil, err
	}
	for index := range items {
		_, used := inUse[items[index].Key]
		items[index].Deletable = !used
	}
	return items, nil
}

func (p *Postgres) UpsertWarehouseAPICredentialGroup(
	ctx context.Context, label, baseURL, appKey, appSecret string, inventory []WarehouseAPIInventoryItem,
) (model.WarehouseAPICredentialGroup, error) {
	label = strings.TrimSpace(label)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	appKey = strings.TrimSpace(appKey)
	appSecret = strings.TrimSpace(appSecret)
	if baseURL == "" || appKey == "" || appSecret == "" {
		return model.WarehouseAPICredentialGroup{}, errors.New("api_base_url, app_key and app_secret are required")
	}
	if label == "" {
		label = "OpenAPI " + credentials.MaskAppKey(appKey)
	}
	if len([]rune(label)) > 100 {
		return model.WarehouseAPICredentialGroup{}, errors.New("credential label cannot exceed 100 characters")
	}
	key := warehouseAPICredentialKey(baseURL, appKey)
	appKeyCiphertext, err := p.cipher.Encrypt(appKey)
	if err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	appSecretCiphertext, err := p.cipher.Encrypt(appSecret)
	if err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	cleaned := normalizeWarehouseAPIInventory(inventory)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_api_credentials(
    credential_key,credential_label,api_base_url,app_key_ciphertext,
    app_secret_ciphertext,app_key_hint,is_active,last_verified_at,updated_at
) VALUES($1,$2,$3,$4,$5,$6,true,now(),now())
ON CONFLICT(credential_key) DO UPDATE SET
    credential_label=EXCLUDED.credential_label,api_base_url=EXCLUDED.api_base_url,
    app_key_ciphertext=EXCLUDED.app_key_ciphertext,app_secret_ciphertext=EXCLUDED.app_secret_ciphertext,
    app_key_hint=EXCLUDED.app_key_hint,is_active=true,last_verified_at=now(),updated_at=now()
`, key, label, baseURL, appKeyCiphertext, appSecretCiphertext, credentials.MaskAppKey(appKey)); err != nil {
		return model.WarehouseAPICredentialGroup{}, fmt.Errorf("save warehouse API credential: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_api_credential_inventory WHERE credential_key=$1`, key); err != nil {
		return model.WarehouseAPICredentialGroup{}, fmt.Errorf("clear warehouse API inventory scope: %w", err)
	}
	for _, item := range cleaned {
		if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_api_credential_inventory(
    credential_key,wh_code,warehouse_name,warehouse_sku,product_name,last_seen_at
) VALUES($1,$2,$3,$4,$5,now())
`, key, item.WarehouseCode, item.WarehouseName, item.WarehouseSKU, item.ProductName); err != nil {
			return model.WarehouseAPICredentialGroup{}, fmt.Errorf("save warehouse API inventory scope: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	return p.warehouseAPICredentialGroup(ctx, key)
}

func (p *Postgres) warehouseAPICredentialGroup(ctx context.Context, key string) (model.WarehouseAPICredentialGroup, error) {
	var item model.WarehouseAPICredentialGroup
	err := p.pool.QueryRow(ctx, `
SELECT credential.credential_key,credential.credential_label,credential.api_base_url,
       credential.app_key_hint,credential.is_active,credential.last_verified_at,credential.updated_at,
       coalesce(array_agg(DISTINCT inventory.wh_code ORDER BY inventory.wh_code)
           FILTER (WHERE inventory.wh_code<>''),'{}'::text[]),
       count(*) FILTER (WHERE inventory.warehouse_sku<>'')
FROM xlwms_api_credentials credential
LEFT JOIN xlwms_api_credential_inventory inventory
  ON inventory.credential_key=credential.credential_key
WHERE credential.credential_key=$1
GROUP BY credential.credential_key
`, key).Scan(&item.Key, &item.Label, &item.APIBaseURL, &item.AppKeyHint, &item.Active,
		&item.LastVerifiedAt, &item.UpdatedAt, &item.WarehouseCodes, &item.SKUCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseAPICredentialGroup{}, errors.New("warehouse API credential was not found")
	}
	if err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	inUse, err := p.legacyWarehouseAPICredentialKeys(ctx)
	if err != nil {
		return model.WarehouseAPICredentialGroup{}, err
	}
	_, used := inUse[item.Key]
	item.Deletable = !used
	return item, nil
}

func (p *Postgres) DeleteWarehouseAPICredentialGroup(ctx context.Context, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, errors.New("warehouse API credential key is required")
	}
	inUse, err := p.legacyWarehouseAPICredentialKeys(ctx)
	if err != nil {
		return false, err
	}
	if _, used := inUse[key]; used {
		return false, ErrWarehouseAPICredentialInUse
	}
	command, err := p.pool.Exec(ctx, `DELETE FROM xlwms_api_credentials WHERE credential_key=$1`, key)
	if err != nil {
		return false, fmt.Errorf("delete warehouse API credential: %w", err)
	}
	return command.RowsAffected() > 0, nil
}

func (p *Postgres) legacyWarehouseAPICredentialKeys(ctx context.Context) (map[string]struct{}, error) {
	rows, err := p.pool.Query(ctx, `SELECT api_base_url,app_key_ciphertext FROM xlwms_warehouses`)
	if err != nil {
		return nil, fmt.Errorf("list operational warehouse API credentials: %w", err)
	}
	defer rows.Close()
	keys := make(map[string]struct{})
	for rows.Next() {
		var baseURL, appKeyCiphertext string
		if err := rows.Scan(&baseURL, &appKeyCiphertext); err != nil {
			return nil, fmt.Errorf("scan operational warehouse API credential: %w", err)
		}
		appKey, err := p.cipher.Decrypt(appKeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt operational warehouse App Key: %w", err)
		}
		keys[warehouseAPICredentialKey(baseURL, appKey)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func normalizeWarehouseAPIInventory(items []WarehouseAPIInventoryItem) []WarehouseAPIInventoryItem {
	unique := make(map[string]WarehouseAPIInventoryItem, len(items))
	for _, item := range items {
		item.WarehouseCode = strings.ToUpper(strings.TrimSpace(item.WarehouseCode))
		item.WarehouseName = strings.TrimSpace(item.WarehouseName)
		item.WarehouseSKU = strings.TrimSpace(item.WarehouseSKU)
		item.ProductName = strings.TrimSpace(item.ProductName)
		if item.WarehouseCode == "" || item.WarehouseSKU == "" {
			continue
		}
		unique[item.WarehouseCode+"\x00"+item.WarehouseSKU] = item
	}
	result := make([]WarehouseAPIInventoryItem, 0, len(unique))
	for _, item := range unique {
		result = append(result, item)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].WarehouseCode == result[right].WarehouseCode {
			return result[left].WarehouseSKU < result[right].WarehouseSKU
		}
		return result[left].WarehouseCode < result[right].WarehouseCode
	})
	return result
}

func warehouseAPIString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(record[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func WarehouseAPIInventoryItemFromRecord(record map[string]any) WarehouseAPIInventoryItem {
	return WarehouseAPIInventoryItem{
		WarehouseCode: warehouseAPIString(record, "whCode"),
		WarehouseName: warehouseAPIString(record, "whName", "warehouseName"),
		WarehouseSKU:  warehouseAPIString(record, "sku"),
		ProductName:   warehouseAPIString(record, "productName", "productTitle"),
	}
}
