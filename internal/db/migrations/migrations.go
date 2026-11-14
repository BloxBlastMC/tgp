// Package migrations holds the versioned schema migrations. Each file
// registers one migration whose name comes from its filename
// (YYYYMMDDHHMMSS_description.go). Migrations define their own frozen table
// structs so later model changes never alter history.
package migrations

import "github.com/uptrace/bun/migrate"

// Migrations is the ordered set of all migrations.
var Migrations = migrate.NewMigrations()
