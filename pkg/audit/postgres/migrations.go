package postgres

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const freshInstallPreflightSQL = `DO $audit_fresh_install_preflight$
BEGIN
    CREATE ROLE audit_writer NOLOGIN;
    CREATE ROLE audit_reader NOLOGIN;
    CREATE ROLE audit_retention NOLOGIN;
END
$audit_fresh_install_preflight$;`

// Migrations returns the immutable engine-neutral migration source.
func Migrations() fs.FS {
	files, _ := fs.Sub(migrationFiles, "migrations")
	return files
}

// FreshInstallPreflightSQL atomically reserves the fixed NOLOGIN roles before
// an empty database applies migration 1. Existing role-name collisions fail
// with PostgreSQL duplicate-object error 42710; concurrent collisions wait for
// the reservation and then fail under PostgreSQL role-name uniqueness. Execute
// it in the same outer transaction as every migration returned by Migrations
// so a failed installation cannot retain the reserved roles. Existing
// installations must apply the forward migrations instead.
func FreshInstallPreflightSQL() string { return freshInstallPreflightSQL }
