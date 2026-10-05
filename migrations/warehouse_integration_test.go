package migrations

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This gate runs both the legacy upgrade and a restart in an isolated schema.
// The transaction is rolled back even when an assertion fails.
func TestWarehouseMigrationUpgradeAndRestart(t *testing.T) {
	databaseURL := os.Getenv("XLWMS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("XLWMS_TEST_DATABASE_URL is required for PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("cannot connect to integration database")
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	schema := fmt.Sprintf("warehouse_migration_test_%d", time.Now().UnixNano())
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, baseSQL); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, InitSQL); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE xlwms_platform_warehouse_carrier_rules SET allowed_carrier_codes=ARRAY['USPS'],max_price_delta=0.37 WHERE warehouse_key='ARP_HOUSTON'; UPDATE xlwms_fulfillment_warehouses SET enabled=false WHERE warehouse_key='ARP_HOUSTON'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, InitSQL); err != nil {
		t.Fatal(err)
	}
	var carriers []string
	var delta float64
	if err = tx.QueryRow(ctx, `SELECT allowed_carrier_codes,max_price_delta FROM xlwms_platform_warehouse_carrier_rules WHERE warehouse_key='ARP_HOUSTON' AND platform='shein'`).Scan(&carriers, &delta); err != nil {
		t.Fatal(err)
	}
	if len(carriers) != 1 || carriers[0] != "USPS" || delta != 0.37 {
		t.Fatalf("restart overwrote policy: %v %v", carriers, delta)
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT enabled FROM xlwms_fulfillment_warehouses WHERE warehouse_key='ARP_ATLANTA'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("unlisted Atlanta was enabled")
	}
	if err = tx.QueryRow(ctx, `SELECT enabled FROM xlwms_fulfillment_warehouses WHERE warehouse_key='ARP_HOUSTON'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("restart re-enabled disabled Houston")
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM xlwms_platform_carrier_policies WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 32 {
		t.Fatalf("policy seeds are missing/duplicated: %d", count)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO xlwms_platform_sku_disabled_warehouses(platform,warehouse_sku,warehouse_key) VALUES('temu','test','ARP_HOUSTON')`); err != nil {
		t.Fatal("new warehouse key rejected by upgraded constraint", err)
	}
}
