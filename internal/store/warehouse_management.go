package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/internal/model"
)

var ErrWarehouseConfigurationConflict = errors.New("配置已变化，请刷新页面后重试")

func (p *Postgres) WarehouseBindings(ctx context.Context, key, platform, shop string) ([]fulfillment.WarehouseBinding, error) {
	rows, err := p.pool.Query(ctx, `SELECT f.warehouse_key,f.wh_code,s.platform,s.shop_code,s.shop_name,s.enabled,
 coalesce(b.platform_warehouse_id,''),coalesce(b.platform_warehouse_name,''),coalesce(b.enabled,false),
 coalesce(b.enabled,false) AND s.enabled AND f.enabled AND coalesce(w.is_active,false) AND b.verified_at IS NOT NULL,
 coalesce(b.revision,0),f.revision,b.verified_at,coalesce(b.verification,'{}'::jsonb),b.updated_at
 FROM xlwms_fulfillment_warehouses f CROSS JOIN xlwms_fulfillment_shops s
 LEFT JOIN xlwms_warehouses w USING(wh_code)
 LEFT JOIN xlwms_platform_warehouse_bindings b ON b.warehouse_key=f.warehouse_key AND b.platform=s.platform AND b.shop_code=s.shop_code
 WHERE ($1='' OR f.warehouse_key=$1) AND ($2='' OR s.platform=$2) AND ($3='' OR s.shop_code=$3)
 ORDER BY s.platform,s.shop_name,f.inventory_priority,f.warehouse_key`, key, platform, shop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []fulfillment.WarehouseBinding{}
	for rows.Next() {
		var b fulfillment.WarehouseBinding
		var raw []byte
		if err := rows.Scan(&b.WarehouseKey, &b.OMSCode, &b.Platform, &b.ShopCode, &b.ShopName, &b.ShopEnabled, &b.PlatformWarehouseID, &b.PlatformWarehouseName, &b.Enabled, &b.Effective, &b.Revision, &b.WarehouseRevision, &b.VerifiedAt, &raw, &b.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &b.Verification)
		out = append(out, b)
	}
	return out, rows.Err()
}
func (p *Postgres) SaveWarehouseBinding(ctx context.Context, key, platform, shop, id, name string, revision, warehouseRevision int64, actor string) (fulfillment.WarehouseBinding, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fulfillment.WarehouseBinding{}, err
	}
	defer tx.Rollback(ctx)
	var wr int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM xlwms_fulfillment_warehouses WHERE warehouse_key=$1 FOR UPDATE`, key).Scan(&wr); err != nil {
		return fulfillment.WarehouseBinding{}, err
	}
	if wr != warehouseRevision {
		return fulfillment.WarehouseBinding{}, ErrWarehouseConfigurationConflict
	}
	previous, err := p.WarehouseBindings(ctx, key, platform, shop)
	if err != nil {
		return fulfillment.WarehouseBinding{}, err
	}
	if len(previous) != 1 {
		return fulfillment.WarehouseBinding{}, errors.New("店铺不存在")
	}
	b := previous[0]
	if b.Revision != revision {
		return fulfillment.WarehouseBinding{}, ErrWarehouseConfigurationConflict
	}
	var after fulfillment.WarehouseBinding
	if id == b.PlatformWarehouseID && name == b.PlatformWarehouseName { // Same ID may pause, but cannot silently reactivate.
		_, err = tx.Exec(ctx, `UPDATE xlwms_platform_warehouse_bindings SET enabled=false,revision=revision+1,updated_at=now() WHERE warehouse_key=$1 AND platform=$2 AND shop_code=$3`, key, platform, shop)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO xlwms_platform_warehouse_bindings(warehouse_key,platform,shop_code,platform_warehouse_id,platform_warehouse_name)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(warehouse_key,platform,shop_code) DO UPDATE SET platform_warehouse_id=EXCLUDED.platform_warehouse_id,platform_warehouse_name=EXCLUDED.platform_warehouse_name,enabled=false,revision=xlwms_platform_warehouse_bindings.revision+1,verified_at=NULL,verification='{}',updated_at=now()`, key, platform, shop, id, name)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return after, errors.New("该店铺的平台仓已绑定到其他物理仓")
		}
		return after, err
	}
	after = b
	after.PlatformWarehouseID = id
	after.PlatformWarehouseName = name
	after.Enabled = false
	after.Effective = false
	after.Revision = b.Revision + 1
	if err = warehouseAudit(ctx, tx, key, actor, "save_or_pause_shop", b, after); err != nil {
		return after, err
	}
	if err = tx.Commit(ctx); err != nil {
		return after, err
	}
	items, err := p.WarehouseBindings(ctx, key, platform, shop)
	if err != nil {
		return after, err
	}
	return items[0], nil
}
func (p *Postgres) SaveWarehouseVerification(ctx context.Context, c fulfillment.WarehouseReadiness, actor string) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var wr int64
	err = tx.QueryRow(ctx, `SELECT revision FROM xlwms_fulfillment_warehouses WHERE warehouse_key=$1 FOR UPDATE`, c.WarehouseKey).Scan(&wr)
	if err != nil {
		return err
	}
	if wr != c.WarehouseRevision {
		return ErrWarehouseConfigurationConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE xlwms_platform_warehouse_bindings SET verification=$4,verified_at=CASE WHEN $5 THEN now() WHEN enabled THEN verified_at ELSE NULL END WHERE warehouse_key=$1 AND platform=$2 AND shop_code=$3 AND revision=$6`, c.WarehouseKey, c.Platform, c.ShopCode, raw, c.Ready, c.Revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && c.Revision != 0 {
		return ErrWarehouseConfigurationConflict
	}
	if err = warehouseAudit(ctx, tx, c.WarehouseKey, actor, "check", nil, c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p *Postgres) ActivateWarehouseBinding(ctx context.Context, c fulfillment.WarehouseReadiness, actor string) error {
	if !c.Ready {
		return errors.New("检查未通过，不能启用")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var wr int64
	var before bool
	err = tx.QueryRow(ctx, `SELECT revision,enabled FROM xlwms_fulfillment_warehouses WHERE warehouse_key=$1 FOR UPDATE`, c.WarehouseKey).Scan(&wr, &before)
	if err != nil {
		return err
	}
	if wr != c.WarehouseRevision {
		return ErrWarehouseConfigurationConflict
	}
	var shopEnabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM xlwms_fulfillment_shops WHERE platform=$1 AND shop_code=$2 FOR SHARE`, c.Platform, c.ShopCode).Scan(&shopEnabled)
	if err != nil {
		return err
	}
	if !shopEnabled {
		return errors.New("店铺已停用")
	}
	tag, err := tx.Exec(ctx, `UPDATE xlwms_platform_warehouse_bindings SET enabled=true,revision=revision+1,verified_at=now(),verification=$5,updated_at=now() WHERE warehouse_key=$1 AND platform=$2 AND shop_code=$3 AND revision=$4 AND platform_warehouse_id<>''`, c.WarehouseKey, c.Platform, c.ShopCode, c.Revision, mustJSON(c))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWarehouseConfigurationConflict
	}
	_, err = tx.Exec(ctx, `UPDATE xlwms_fulfillment_warehouses SET enabled=true,revision=revision+1 WHERE warehouse_key=$1`, c.WarehouseKey)
	if err != nil {
		return err
	}
	if err = warehouseAudit(ctx, tx, c.WarehouseKey, actor, "activate_shop", map[string]any{"warehouse_enabled": before, "binding_revision": c.Revision}, c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p *Postgres) PauseFulfillmentWarehouse(ctx context.Context, key string, revision int64, actor string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var wr int64
	var before bool
	err = tx.QueryRow(ctx, `SELECT revision,enabled FROM xlwms_fulfillment_warehouses WHERE warehouse_key=$1 FOR UPDATE`, key).Scan(&wr, &before)
	if err != nil {
		return err
	}
	if wr != revision {
		return ErrWarehouseConfigurationConflict
	}
	_, err = tx.Exec(ctx, `UPDATE xlwms_fulfillment_warehouses SET enabled=false,revision=revision+1 WHERE warehouse_key=$1`, key)
	if err != nil {
		return err
	}
	if err = warehouseAudit(ctx, tx, key, actor, "pause_warehouse", map[string]any{"enabled": before}, map[string]any{"enabled": false}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func warehouseAudit(ctx context.Context, tx pgx.Tx, key, actor, action string, before, after any) error {
	_, err := tx.Exec(ctx, `INSERT INTO xlwms_warehouse_configuration_history(warehouse_key,actor,action,before_value,after_value) VALUES($1,$2,$3,$4,$5)`, key, actor, action, mustJSON(before), mustJSON(after))
	return err
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
func (p *Postgres) WarehouseConfigurationHistory(ctx context.Context, key string) ([]map[string]any, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,actor,action,before_value,after_value,created_at FROM xlwms_warehouse_configuration_history WHERE warehouse_key=$1 ORDER BY id DESC LIMIT 100`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, action string
		var before, after []byte
		var at any
		if err = rows.Scan(&id, &actor, &action, &before, &after, &at); err != nil {
			return nil, err
		}
		var bv, av any
		_ = json.Unmarshal(before, &bv)
		_ = json.Unmarshal(after, &av)
		out = append(out, map[string]any{"id": id, "actor": actor, "action": action, "before": bv, "after": av, "created_at": at})
	}
	return out, rows.Err()
}
func (p *Postgres) WarehouseSetupCredentials(ctx context.Context, code string) ([]model.WarehouseCredentials, error) {
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT c.credential_key,b.account_key,c.api_base_url,c.app_key_ciphertext,c.app_secret_ciphertext FROM xlwms_api_credential_inventory i JOIN xlwms_api_credentials c USING(credential_key) JOIN xlwms_oms_account_api_credentials b USING(credential_key) JOIN xlwms_oms_accounts a USING(account_key) WHERE i.wh_code=$1 AND c.is_active AND c.inventory_sync_status='ready' AND a.enabled`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WarehouseCredentials{}
	for rows.Next() {
		var w model.WarehouseCredentials
		var k, s string
		if err = rows.Scan(&w.APICredentialKey, &w.OMSAccountKey, &w.APIBaseURL, &k, &s); err != nil {
			return nil, err
		}
		w.Code = code
		w.Active = true
		w.AppKey, err = p.cipher.Decrypt(k)
		if err != nil {
			return nil, err
		}
		w.AppSecret, err = p.cipher.Decrypt(s)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type WarehouseLegacySource interface {
	LegacyWarehouseBindings(context.Context, string, string) ([]fulfillment.PlatformWarehouseOption, error)
}

func (p *Postgres) ImportLegacyWarehouseBindings(ctx context.Context, source WarehouseLegacySource) error {
	shops, err := p.ListFulfillmentShops(ctx, true)
	if err != nil {
		return err
	}
	for _, shop := range shops {
		var done bool
		err = p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xlwms_warehouse_binding_imports WHERE platform=$1 AND shop_code=$2)`, shop.Platform, shop.ShopCode).Scan(&done)
		if err != nil {
			return err
		}
		if done {
			continue
		}
		opts, err := source.LegacyWarehouseBindings(ctx, shop.Platform, shop.ShopCode)
		if err != nil {
			return err
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		for _, o := range opts {
			w, ok := fulfillment.Find(o.OMSCode)
			if !ok || strings.TrimSpace(o.ID) == "" {
				continue
			}
			_, err = tx.Exec(ctx, `INSERT INTO xlwms_platform_warehouse_bindings(warehouse_key,platform,shop_code,platform_warehouse_id,platform_warehouse_name,enabled,verified_at,verification) VALUES($1,$2,$3,$4,$5,$6,now(),'{"source":"verified_legacy"}') ON CONFLICT DO NOTHING`, w.Key, shop.Platform, shop.ShopCode, o.ID, o.Name, o.Enabled && o.CanShip && shop.Enabled && w.Key != "ARP_ATLANTA")
			if err != nil {
				break
			}
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO xlwms_warehouse_binding_imports(platform,shop_code) VALUES($1,$2) ON CONFLICT DO NOTHING`, shop.Platform, shop.ShopCode)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
