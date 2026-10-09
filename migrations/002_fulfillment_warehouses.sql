CREATE TABLE IF NOT EXISTS xlwms_fulfillment_warehouses (
 warehouse_key text PRIMARY KEY,
 wh_code text NOT NULL UNIQUE,
 display_name text NOT NULL,
 provider text NOT NULL,
 region text NOT NULL,
 inventory_priority integer NOT NULL,
 enabled boolean NOT NULL DEFAULT true
);
INSERT INTO xlwms_fulfillment_warehouses(warehouse_key,wh_code,display_name,provider,region,inventory_priority,enabled)
VALUES ('DPS002','DPSNY002','DPS-纽约','DPS','east',1,true),
 ('ARP_EAST','HYTX30','ARP-宾夕法尼亚','ARP','east',2,true),
 ('DPS004','DPSCA004','DPS-加州','DPS','west',1,true),
 ('ARP_WEST','ARPCA01','ARP-洛杉矶','ARP','west',2,true),
 ('ARP_HOUSTON','ARP06A','ARP-休斯顿','ARP','central',2,true),
 ('ARP_ATLANTA','ARPGA','ARP-亚特兰大','ARP','east',2,false)
ON CONFLICT(warehouse_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS xlwms_warehouse_carrier_capabilities (
 wh_code text PRIMARY KEY REFERENCES xlwms_fulfillment_warehouses(wh_code),
 allowed_carrier_codes text[] NOT NULL
);
INSERT INTO xlwms_warehouse_carrier_capabilities VALUES
 ('ARP06A',ARRAY['USPS','GOFO','UPS','FEDEX','SPEEDX','CBS']),('ARPGA',ARRAY['USPS','GOFO','UPS','FEDEX','SPEEDX','CBS'])
ON CONFLICT(wh_code) DO NOTHING;

DO $registry_constraints$
DECLARE t text;
BEGIN
 FOREACH t IN ARRAY ARRAY['xlwms_platform_carrier_policies','xlwms_platform_warehouse_carrier_rules',
 'xlwms_platform_sku_carrier_policies','xlwms_platform_sku_disabled_warehouses'] LOOP
  EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I',t,t||'_warehouse_key_check');
  IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid=t::regclass AND conname=t||'_warehouse_key_fkey') THEN
   EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY(warehouse_key) REFERENCES xlwms_fulfillment_warehouses(warehouse_key)',t,t||'_warehouse_key_fkey');
  END IF;
 END LOOP;
END $registry_constraints$;

INSERT INTO xlwms_platform_carrier_policies(platform,warehouse_key,carrier_code,priority,enabled)
SELECT p,k,c,n,c IN ('USPS','GOFO','UPS','FEDEX','SPEEDX') OR (p='shein' AND c='CBS')
FROM (VALUES('temu'),('shein')) platforms(p)
CROSS JOIN (VALUES('ARP_HOUSTON'),('ARP_ATLANTA')) warehouses(k)
CROSS JOIN (VALUES('GOFO',1),('SWIFTX',2),('SPEEDX',3),('YANWEN',4),('UPS',5),('USPS',6),('FEDEX',7),('CBS',8)) carriers(c,n)
ON CONFLICT(platform,warehouse_key,carrier_code) DO NOTHING;
INSERT INTO xlwms_platform_warehouse_carrier_rules(platform,warehouse_key,allowed_carrier_codes,allow_signature,allowed_currency_codes,selection_mode,max_price_delta,warehouse_tie_priority)
SELECT p,k,CASE WHEN p='shein' THEN ARRAY['USPS','GOFO','UPS','FEDEX','SPEEDX','CBS'] ELSE ARRAY['USPS','GOFO','UPS','FEDEX','SPEEDX'] END,p='shein',CASE WHEN p='temu' THEN ARRAY['USD']::text[] ELSE ARRAY[]::text[] END,
 CASE WHEN p='temu' THEN 'carrier_priority_within_delta' ELSE 'lowest_price' END,CASE WHEN p='temu' THEN 0.50 ELSE 0 END,2
FROM (VALUES('temu'),('shein')) platforms(p) CROSS JOIN(VALUES('ARP_HOUSTON'),('ARP_ATLANTA')) warehouses(k)
ON CONFLICT(platform,warehouse_key) DO NOTHING;
