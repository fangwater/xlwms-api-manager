-- Upgrade the former four-carrier limit once. Later operator changes must
-- survive application restarts and bootstrap migration replays.
CREATE TABLE IF NOT EXISTS xlwms_schema_migrations (
 migration_key text PRIMARY KEY,
 applied_at timestamptz NOT NULL DEFAULT now()
);

DO $collection_carrier_expansion$
DECLARE applied boolean;
BEGIN
 INSERT INTO xlwms_schema_migrations(migration_key)
 VALUES ('004_collection_carrier_expansion')
 ON CONFLICT DO NOTHING RETURNING true INTO applied;
 IF applied THEN
  UPDATE xlwms_warehouse_carrier_capabilities
  SET allowed_carrier_codes = allowed_carrier_codes
   || CASE WHEN 'SPEEDX'=ANY(allowed_carrier_codes) THEN ARRAY[]::text[] ELSE ARRAY['SPEEDX'] END
   || CASE WHEN 'CBS'=ANY(allowed_carrier_codes) THEN ARRAY[]::text[] ELSE ARRAY['CBS'] END
  WHERE wh_code IN ('ARP06A','ARPGA') AND cardinality(allowed_carrier_codes)>0;

  UPDATE xlwms_platform_warehouse_carrier_rules
  SET allowed_carrier_codes = allowed_carrier_codes
   || CASE WHEN 'SPEEDX'=ANY(allowed_carrier_codes) THEN ARRAY[]::text[] ELSE ARRAY['SPEEDX'] END
   || CASE WHEN platform='shein' AND NOT ('CBS'=ANY(allowed_carrier_codes)) THEN ARRAY['CBS'] ELSE ARRAY[]::text[] END,
   updated_at=now()
  WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA');

  UPDATE xlwms_platform_carrier_policies
  SET enabled=true, updated_at=now()
  WHERE warehouse_key IN ('ARP_HOUSTON','ARP_ATLANTA')
   AND (carrier_code='SPEEDX' OR (platform='shein' AND carrier_code='CBS'));
 END IF;
END $collection_carrier_expansion$;
