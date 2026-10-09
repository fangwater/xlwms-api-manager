package migrations

import _ "embed"

// InitSQL contains the idempotent XLWMS schema migration.
//
//go:embed 001_init.sql
var baseSQL string

//go:embed 002_fulfillment_warehouses.sql
var warehouseSQL string

//go:embed 003_warehouse_management.sql
var managementSQL string

//go:embed 004_collection_carrier_expansion.sql
var collectionCarrierSQL string

var InitSQL = baseSQL + "\n" + warehouseSQL + "\n" + managementSQL + "\n" + collectionCarrierSQL
