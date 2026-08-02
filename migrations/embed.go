package migrations

import _ "embed"

// InitSQL contains the idempotent XLWMS schema migration.
//
//go:embed 001_init.sql
var InitSQL string
