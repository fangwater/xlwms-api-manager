package migrations

import _ "embed"

// InitSQL contains the idempotent XLWMS schema migration.
//
//go:embed 001_init.sql
var baseSQL string

//go:embed 002_fulfillment_warehouses.sql
var warehouseSQL string

var InitSQL = baseSQL + "\n" + warehouseSQL
