//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMain(m *testing.M) {
	if os.Getenv(processDeathPhaseEnvironment) != "" ||
		os.Getenv("QUEUE_CONTROL_SHARED_INTEGRATION_DATABASE") == "true" {
		os.Exit(m.Run())
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(m.Run())
	}

	code, cleanup, err := prepareIntegrationDatabase(dsn, m.Run)
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare PostgreSQL integration database: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func prepareIntegrationDatabase(dsn string, run func() int) (int, func(), error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return 0, nil, err
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return 0, nil, err
	}
	databaseName := "golib_queue_control_" + hex.EncodeToString(random)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, nil, err
	}
	if _, err := database.ExecContext(ctx, "CREATE DATABASE "+databaseName); err != nil {
		_ = database.Close()
		return 0, nil, err
	}

	parsed.Path = "/" + databaseName
	previous, hadPrevious := os.LookupEnv("TEST_DATABASE_URL")
	if err := os.Setenv("TEST_DATABASE_URL", parsed.String()); err != nil {
		_, _ = database.ExecContext(ctx, "DROP DATABASE "+databaseName+" WITH (FORCE)")
		_ = database.Close()
		return 0, nil, err
	}

	cleanup := func() {
		if hadPrevious {
			_ = os.Setenv("TEST_DATABASE_URL", previous)
		} else {
			_ = os.Unsetenv("TEST_DATABASE_URL")
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(cleanupCtx, "DROP DATABASE "+databaseName+" WITH (FORCE)")
		_ = database.Close()
	}

	return run(), cleanup, nil
}
