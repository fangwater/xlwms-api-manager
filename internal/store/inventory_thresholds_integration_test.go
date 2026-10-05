package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/migrations"

	"github.com/jackc/pgx/v5"
)

func TestInventoryThresholdSnapshotIncludesOnlyManagedFulfillmentStock(t *testing.T) {
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
	schema := fmt.Sprintf("warehouse_threshold_test_%d", time.Now().UnixNano())
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, migrations.InitSQL); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO xlwms_warehouses(wh_code,api_base_url,app_key_ciphertext,app_secret_ciphertext,app_key_hint)
 SELECT code,'https://example.invalid','unused','unused','unused' FROM unnest(ARRAY['ARP06A','ARPGA','ARP12','HYTX30']) code;
INSERT INTO xlwms_oms_accounts(account_key,username_ciphertext,password_ciphertext,account_hint) VALUES('test-arp','unused','unused','unused');
INSERT INTO xlwms_api_credentials(credential_key,credential_label,api_base_url,app_key_ciphertext,app_secret_ciphertext,app_key_hint,inventory_sync_status)
 VALUES('test-scope','test scope','https://example.invalid','unused','unused','unused','ready');
INSERT INTO xlwms_oms_account_api_credentials(credential_key,account_key) VALUES('test-scope','test-arp');
INSERT INTO xlwms_api_credential_inventory(credential_key,wh_code,warehouse_sku)
 SELECT 'test-scope',code,'test-hanger' FROM unnest(ARRAY['ARP06A','ARPGA','ARP12','HYTX30']) code;
INSERT INTO xlwms_warehouse_sku_specs(warehouse_sku) VALUES('test-hanger');
INSERT INTO xlwms_inventory_records(record_key,inventory_kind,wh_code,sku,stock_type,product_available_amount,raw_payload,api_credential_key)
 VALUES('houston-1','integrated','ARP06A','test-hanger',0,60,'{}','test-scope'),
 ('houston-2','integrated','ARP06A','test-hanger',0,40,'{}','test-scope'),
 ('houston-nonsellable','integrated','ARP06A','test-hanger',1,999,'{}','test-scope'),
 ('atlanta-disabled','integrated','ARPGA','test-hanger',0,999,'{}','test-scope'),
 ('not-operated','integrated','ARP12','test-hanger',0,999,'{}','test-scope'),
 ('another-scope','integrated','HYTX30','test-hanger',0,999,'{}','another-scope');
INSERT INTO xlwms_inventory_corrections(wh_code,warehouse_sku,correction_mode,correction_amount) VALUES('ARP06A','test-hanger','subtract',25);
`); err != nil {
		t.Fatal(err)
	}
	query := func() model.SKUInventoryThreshold {
		t.Helper()
		var item model.SKUInventoryThreshold
		err := tx.QueryRow(ctx, inventoryThresholdListSQL, []string{"DPSNY002", "HYTX30", "ARPGA"}, []string{"DPSCA004", "ARPCA01"}, "test-hanger", "temu", 30, 0).Scan(
			&item.WarehouseSKU, &item.ProductName, &item.EastAvailable, &item.WestAvailable,
			&item.TotalAvailable, &item.WarehouseAvailable, &item.EastThreshold, &item.WestThreshold, &item.TotalThreshold,
			&item.Customized, &item.Source, &item.InventoryAt, &item.UpdatedAt,
		)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	item := query()
	if item.TotalAvailable != 75 || item.WarehouseAvailable["ARP06A"] != 75 || len(item.WarehouseAvailable) != 1 || item.EastAvailable != 0 || item.WestAvailable != 0 {
		t.Fatalf("snapshot omitted Houston or counted unrelated stock: %+v", item)
	}
	if _, err = tx.Exec(ctx, `UPDATE xlwms_inventory_corrections SET correction_mode='absolute',correction_amount=0`); err != nil {
		t.Fatal(err)
	}
	if item = query(); item.TotalAvailable != 0 || item.WarehouseAvailable["ARP06A"] != 0 {
		t.Fatalf("zero correction was ignored: %+v", item)
	}
}

func TestInventoryThresholdSnapshotPreservesAccountWarehouseScope(t *testing.T) {
	item := model.SKUInventoryThreshold{
		WarehouseAvailable:  map[string]float64{"HYTX30": 10, "ARPCA01": 20, "ARP06A": 100, "ARP12": 999},
		InventoryThresholds: model.InventoryThresholds{WarehouseCodes: []string{"HYTX30", "ARPCA01"}},
	}
	applyThresholdWarehouseScope(&item)
	if item.TotalAvailable != 30 || item.EastAvailable != 10 || item.WestAvailable != 20 || len(item.WarehouseAvailable) != 2 {
		t.Fatalf("account scope was widened: %+v", item)
	}
}
