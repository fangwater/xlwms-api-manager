package store

import (
	"context"
	"slices"
	"testing"
)

func TestCollectionCarrierMigrationUpgradesLegacyPoliciesOnlyOnce(t *testing.T) {
	p := managementTestStore(t)
	ctx := context.Background()
	// Reconstruct the old capability and platform configuration, including a
	// deliberate SKU override that the upgrade must preserve.
	_, err := p.pool.Exec(ctx, `
DELETE FROM xlwms_schema_migrations WHERE migration_key='004_collection_carrier_expansion';
UPDATE xlwms_warehouse_carrier_capabilities SET allowed_carrier_codes=ARRAY['USPS','GOFO','UPS','FEDEX'];
UPDATE xlwms_platform_warehouse_carrier_rules SET allowed_carrier_codes=ARRAY['USPS','GOFO','UPS','FEDEX'] WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA');
UPDATE xlwms_platform_carrier_policies SET enabled=false WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA') AND carrier_code IN ('SPEEDX','CBS');
INSERT INTO xlwms_platform_sku_carrier_policies(platform,warehouse_sku,warehouse_key,carrier_code,priority,enabled) VALUES('shein','test-sku','ARP_HOUSTON','CBS',8,false);
UPDATE xlwms_warehouse_carrier_capabilities SET allowed_carrier_codes=ARRAY[]::text[] WHERE wh_code='ARPGA';
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var physical []string
	if err := p.pool.QueryRow(ctx, `SELECT allowed_carrier_codes FROM xlwms_warehouse_carrier_capabilities WHERE wh_code='ARP06A'`).Scan(&physical); err != nil {
		t.Fatal(err)
	}
	for _, carrier := range []string{"SPEEDX", "CBS"} {
		if !slices.Contains(physical, carrier) {
			t.Fatalf("physical capability missing %s", carrier)
		}
	}
	for _, platform := range []string{"temu", "shein"} {
		for _, key := range []string{"ARP_HOUSTON", "ARP_ATLANTA"} {
			var allowed []string
			if err := p.pool.QueryRow(ctx, `SELECT allowed_carrier_codes FROM xlwms_platform_warehouse_carrier_rules WHERE platform=$1 AND warehouse_key=$2`, platform, key).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(allowed, "SPEEDX") || slices.Contains(allowed, "CBS") != (platform == "shein") {
				t.Fatalf("%s/%s has incorrect carrier rules: %v", platform, key, allowed)
			}
			var enabled bool
			if err := p.pool.QueryRow(ctx, `SELECT enabled FROM xlwms_platform_carrier_policies WHERE platform=$1 AND warehouse_key=$2 AND carrier_code='SPEEDX'`, platform, key).Scan(&enabled); err != nil || !enabled {
				t.Fatalf("SpeedX was not enabled for %s/%s: %v", platform, key, err)
			}
		}
	}
	// Subsequent restarts must preserve operators turning a channel off again.
	_, err = p.pool.Exec(ctx, `
UPDATE xlwms_platform_warehouse_carrier_rules SET allowed_carrier_codes=array_remove(array_remove(allowed_carrier_codes,'SPEEDX'),'CBS') WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA');
UPDATE xlwms_platform_carrier_policies SET enabled=false WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA') AND carrier_code IN ('SPEEDX','CBS');
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var overwritten bool
	if err := p.pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM xlwms_platform_warehouse_carrier_rules WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA') AND allowed_carrier_codes && ARRAY['SPEEDX','CBS'])
 OR EXISTS(SELECT 1 FROM xlwms_platform_carrier_policies WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA') AND carrier_code IN ('SPEEDX','CBS') AND enabled)
 OR EXISTS(SELECT 1 FROM xlwms_platform_sku_carrier_policies WHERE warehouse_sku='test-sku' AND enabled)
 OR EXISTS(SELECT 1 FROM xlwms_warehouse_carrier_capabilities WHERE wh_code='ARPGA' AND cardinality(allowed_carrier_codes)>0)`).Scan(&overwritten); err != nil || overwritten {
		t.Fatalf("migration replay changed operator configuration: %v", err)
	}
}
