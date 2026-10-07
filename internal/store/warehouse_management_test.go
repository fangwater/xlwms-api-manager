package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/migrations"
)

func managementTestStore(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("XLWMS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("XLWMS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("integration connection failed")
	}
	schema := fmt.Sprintf("warehouse_config_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		_ = conn.Close(context.Background())
	})
	if _, err = pool.Exec(ctx, migrations.InitSQL); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO xlwms_warehouses(wh_code,warehouse_name,api_base_url,app_key_ciphertext,app_secret_ciphertext,app_key_hint,is_active) VALUES('ARP06A','test','https://example.invalid','test','test','hint',true)`); err != nil {
		t.Fatal(err)
	}
	return &Postgres{pool: pool}
}
func TestWarehouseConfigurationShopIsolationVersionsAndRestart(t *testing.T) {
	p := managementTestStore(t)
	ctx := context.Background()
	home, err := p.SaveWarehouseBinding(ctx, "ARP_HOUSTON", "temu", "panda-homes", "WH-HOME", "home", 0, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	buy, err := p.SaveWarehouseBinding(ctx, "ARP_HOUSTON", "temu", "panda-buy", "WH-BUY", "buy", 0, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if home.Enabled || buy.Enabled || home.VerifiedAt != nil {
		t.Fatal("draft enabled without verification")
	}
	check := fulfillment.WarehouseReadiness{WarehouseKey: "ARP_HOUSTON", Platform: "temu", ShopCode: "panda-homes", Revision: home.Revision, WarehouseRevision: 1, Ready: true}
	if err = p.ActivateWarehouseBinding(ctx, check, "test"); err != nil {
		t.Fatal(err)
	}
	bindings, err := p.WarehouseBindings(ctx, "ARP_HOUSTON", "temu", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		if b.ShopCode == "panda-homes" {
			home = b
			if !b.Effective {
				t.Fatal("selected shop did not activate")
			}
		}
		if b.ShopCode == "panda-buy" && b.Effective {
			t.Fatal("activation leaked to another shop")
		}
	}
	home, err = p.SaveWarehouseBinding(ctx, home.WarehouseKey, home.Platform, home.ShopCode, home.PlatformWarehouseID, home.PlatformWarehouseName, home.Revision, home.WarehouseRevision, "test")
	if err != nil {
		t.Fatal(err)
	}
	if home.Enabled || home.Effective {
		t.Fatal("pause did not stop shop")
	}
	if err = p.ActivateWarehouseBinding(ctx, check, "test"); !errors.Is(err, ErrWarehouseConfigurationConflict) {
		t.Fatalf("stale activation accepted: %v", err)
	}
	if _, err = p.SaveWarehouseBinding(ctx, "ARP_ATLANTA", "temu", "panda-homes", "WH-HOME", "duplicate", 0, 1, "test"); err == nil {
		t.Fatal("platform ID reused for another physical warehouse")
	}
	if err = p.PauseFulfillmentWarehouse(ctx, "ARP_HOUSTON", home.WarehouseRevision, "test"); err != nil {
		t.Fatal(err)
	}
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := p.FulfillmentWarehouses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rows {
		if (w.Key == "ARP_HOUSTON" || w.Key == "ARP_ATLANTA") && w.FulfillmentEnabled {
			t.Fatal("restart enabled a paused warehouse")
		}
	}
	items, err := p.WarehouseBindings(ctx, "ARP_HOUSTON", "temu", "panda-homes")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Enabled || items[0].PlatformWarehouseID != "WH-HOME" || items[0].Revision != home.Revision {
		t.Fatal("restart changed operator configuration")
	}
	history, err := p.WarehouseConfigurationHistory(ctx, "ARP_HOUSTON")
	if err != nil || len(history) < 5 {
		t.Fatalf("audit missing: %v", err)
	}
}
func TestWarehouseActivationRefusesDisabledShop(t *testing.T) {
	p := managementTestStore(t)
	ctx := context.Background()
	b, err := p.SaveWarehouseBinding(ctx, "ARP_HOUSTON", "temu", "panda-homes", "WH-HOME", "home", 0, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.pool.Exec(ctx, `UPDATE xlwms_fulfillment_shops SET enabled=false WHERE platform='temu' AND shop_code='panda-homes'`); err != nil {
		t.Fatal(err)
	}
	if err = p.ActivateWarehouseBinding(ctx, fulfillment.WarehouseReadiness{WarehouseKey: b.WarehouseKey, Platform: b.Platform, ShopCode: b.ShopCode, Revision: b.Revision, WarehouseRevision: 1, Ready: true}, "test"); err == nil {
		t.Fatal("disabled shop activated")
	}
}
