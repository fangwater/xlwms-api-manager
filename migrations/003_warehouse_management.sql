ALTER TABLE xlwms_fulfillment_warehouses ADD COLUMN IF NOT EXISTS revision bigint NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS xlwms_platform_warehouse_bindings (
 warehouse_key text NOT NULL REFERENCES xlwms_fulfillment_warehouses(warehouse_key),
 platform text NOT NULL, shop_code text NOT NULL,
 platform_warehouse_id text NOT NULL DEFAULT '', platform_warehouse_name text NOT NULL DEFAULT '',
 enabled boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 1,
 verified_at timestamptz, verification jsonb NOT NULL DEFAULT '{}'::jsonb,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(warehouse_key,platform,shop_code),
 FOREIGN KEY(platform,shop_code) REFERENCES xlwms_fulfillment_shops(platform,shop_code)
);
CREATE UNIQUE INDEX IF NOT EXISTS xlwms_platform_warehouse_unique_address
 ON xlwms_platform_warehouse_bindings(platform,shop_code,platform_warehouse_id) WHERE platform_warehouse_id<>'';
CREATE TABLE IF NOT EXISTS xlwms_warehouse_configuration_history (
 id bigserial PRIMARY KEY, warehouse_key text NOT NULL REFERENCES xlwms_fulfillment_warehouses(warehouse_key),
 actor text NOT NULL, action text NOT NULL, before_value jsonb NOT NULL, after_value jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS xlwms_warehouse_binding_imports (
 platform text NOT NULL, shop_code text NOT NULL, imported_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(platform,shop_code), FOREIGN KEY(platform,shop_code) REFERENCES xlwms_fulfillment_shops(platform,shop_code)
);
