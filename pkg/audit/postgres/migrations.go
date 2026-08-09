package postgres

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the immutable engine-neutral migration source.
func Migrations() fs.FS {
	files, _ := fs.Sub(migrationFiles, "migrations")
	return files
}
