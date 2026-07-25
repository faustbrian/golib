// Command migrations applies embedded schema changes as a deployment job.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	migrationpostgres "github.com/faustbrian/golib/pkg/migrations/postgres"
	postgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := postgres.New(ctx, postgres.Config{
		DSN:             dsn,
		MaxConns:        2,
		AcquireTimeout:  30 * time.Second,
		ShutdownTimeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("open migration pool: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := pool.Close(closeCtx); err != nil {
			log.Printf("close migration pool: %v", err)
		}
	}()

	database := stdlib.OpenDBFromPool(pool.Raw())
	defer func() { _ = database.Close() }()
	source, err := migrations.NewFSSource(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	backend, err := migrationpostgres.New(
		database,
		migrationpostgres.WithLockTimeout(30*time.Second),
		migrationpostgres.WithStatementTimeout(5*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("create migration backend: %w", err)
	}
	runner, err := migrations.NewRunner(source, backend)
	if err != nil {
		return fmt.Errorf("create migration runner: %w", err)
	}
	plan, err := runner.Plan(ctx)
	if err != nil {
		return fmt.Errorf("plan migrations: %w", err)
	}
	for _, step := range plan.Steps() {
		log.Printf(
			"planned action=%d version=%s name=%s",
			step.Action(),
			step.Migration().Version(),
			step.Migration().Name(),
		)
	}
	result, err := runner.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	log.Printf("completed migrations=%d", len(result.Records()))

	return nil
}
