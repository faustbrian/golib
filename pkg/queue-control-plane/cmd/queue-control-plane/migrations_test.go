package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestExecuteMigrationsAppliesAndClosesDatabase(t *testing.T) {
	t.Parallel()

	database := migrationDatabase(t)
	applied := false
	err := executeMigrations(
		context.Background(),
		"postgres://database/control",
		func(driver, dsn string) (*sql.DB, error) {
			if driver != "pgx" || dsn != "postgres://database/control" {
				t.Fatalf("open(%q, %q)", driver, dsn)
			}

			return database, nil
		},
		func(got *sql.DB) (migrationApplier, error) {
			if got != database {
				t.Fatalf("runner database = %p, want %p", got, database)
			}

			return migrationApplierFunc(func(context.Context) (migrations.Result, error) {
				applied = true

				return migrations.Result{}, nil
			}), nil
		},
	)
	if err != nil || !applied {
		t.Fatalf("executeMigrations() = %v, applied = %t", err, applied)
	}
	if err := database.PingContext(context.Background()); err == nil {
		t.Fatal("database remained usable after migration execution")
	}
}

func TestExecuteMigrationsPropagatesEveryFailure(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed")
	tests := map[string]struct {
		opener  func(string, string) (*sql.DB, error)
		factory migrationApplierFactory
		want    error
	}{
		"open": {
			opener: func(string, string) (*sql.DB, error) { return nil, stageErr },
			want:   stageErr,
		},
		"runner": {
			opener: func(string, string) (*sql.DB, error) { return migrationDatabase(t), nil },
			factory: func(*sql.DB) (migrationApplier, error) {
				return nil, stageErr
			},
			want: stageErr,
		},
		"apply": {
			opener: func(string, string) (*sql.DB, error) { return migrationDatabase(t), nil },
			factory: func(*sql.DB) (migrationApplier, error) {
				return migrationApplierFunc(func(context.Context) (migrations.Result, error) {
					return migrations.Result{}, stageErr
				}), nil
			},
			want: stageErr,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := executeMigrations(
				context.Background(),
				"postgres://database/control",
				test.opener,
				test.factory,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("executeMigrations() error = %v, want %v", err, test.want)
			}
		})
	}
}

func migrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("pgx", "postgres://localhost/control_plane")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	return database
}

type migrationApplierFunc func(context.Context) (migrations.Result, error)

func (apply migrationApplierFunc) Up(ctx context.Context) (migrations.Result, error) {
	return apply(ctx)
}
