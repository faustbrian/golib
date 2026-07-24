package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	migrations "github.com/faustbrian/golib/pkg/migrations"
)

type migrationApplier interface {
	Up(context.Context) (migrations.Result, error)
}

type migrationApplierFactory func(*sql.DB) (migrationApplier, error)

func executeMigrations(
	ctx context.Context,
	dsn string,
	open func(string, string) (*sql.DB, error),
	newApplier migrationApplierFactory,
) (resultErr error) {
	database, err := open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("queue-control-plane: open migration database: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()

	applier, err := newApplier(database)
	if err != nil {
		return fmt.Errorf("queue-control-plane: build migration runner: %w", err)
	}
	if _, err := applier.Up(ctx); err != nil {
		return fmt.Errorf("queue-control-plane: apply migrations: %w", err)
	}

	return nil
}
