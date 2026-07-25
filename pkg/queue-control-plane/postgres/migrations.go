// Package postgres provides PostgreSQL persistence for control-plane state.
package postgres

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	migrationpostgres "github.com/faustbrian/golib/pkg/migrations/postgres"
)

const (
	migrationLockTimeout      = 30 * time.Second
	migrationStatementTimeout = 5 * time.Minute
)

// ErrInvalidMigrationDatabase reports a missing migration connection pool.
var ErrInvalidMigrationDatabase = errors.New("postgres: migration database is nil")

type migrationFactories struct {
	source  func() (migrations.Source, error)
	backend func(*sql.DB) (migrations.Backend, error)
	runner  func(migrations.Source, migrations.Backend) (*migrations.Runner, error)
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

// MigrationSource returns the immutable embedded control-plane schema history.
func MigrationSource() (migrations.Source, error) {
	return migrations.NewFSSource(migrationFiles, "migrations")
}

// NewMigrationRunner builds the bounded migrations runner for the embedded
// control-plane schema. The caller retains ownership of database.
func NewMigrationRunner(database *sql.DB) (*migrations.Runner, error) {
	if database == nil {
		return nil, ErrInvalidMigrationDatabase
	}

	return newMigrationRunner(database, migrationFactories{
		source:  MigrationSource,
		backend: newMigrationBackend,
		runner: func(source migrations.Source, backend migrations.Backend) (*migrations.Runner, error) {
			return migrations.NewRunner(source, backend)
		},
	})
}

func newMigrationRunner(
	database *sql.DB,
	factories migrationFactories,
) (*migrations.Runner, error) {
	source, err := factories.source()
	if err != nil {
		return nil, fmt.Errorf("postgres: create migration source: %w", err)
	}
	backend, err := factories.backend(database)
	if err != nil {
		return nil, fmt.Errorf("postgres: create migration backend: %w", err)
	}
	runner, err := factories.runner(source, backend)
	if err != nil {
		return nil, fmt.Errorf("postgres: create migration runner: %w", err)
	}

	return runner, nil
}

func newMigrationBackend(database *sql.DB) (migrations.Backend, error) {
	return migrationpostgres.New(
		database,
		migrationpostgres.WithLockTimeout(migrationLockTimeout),
		migrationpostgres.WithStatementTimeout(migrationStatementTimeout),
	)
}
