package postgres

import (
	"embed"
	"io/fs"
)

// migrationFiles contains reversible, dependency-free PostgreSQL migrations.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the versioned migration files rooted at their filenames.
// The returned fs.FS can be consumed by migrations or another migration
// runner without exposing a Goose dependency to applications.
func Migrations() fs.FS {
	migrations, _ := fs.Sub(migrationFiles, "migrations")

	return migrations
}
