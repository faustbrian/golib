//go:build integration

package postgres_test

import (
	"io/fs"
	"strings"
	"testing"

	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
)

func migrationUpSQL(t testing.TB) string {
	t.Helper()

	contents := migrationContents(t)
	up := strings.Index(contents, "-- +migrations Up\n")
	down := strings.Index(contents, "-- +migrations Down\n")
	if up != 0 || down <= up {
		t.Fatalf("invalid canonical migration directives")
	}

	return contents[len("-- +migrations Up\n"):down]
}

func migrationDownSQL(t testing.TB) string {
	t.Helper()

	contents := migrationContents(t)
	marker := "-- +migrations Down\n"
	down := strings.Index(contents, marker)
	if down < 0 {
		t.Fatal("canonical migration has no down section")
	}

	return contents[down+len(marker):]
}

func migrationContents(t testing.TB) string {
	t.Helper()

	contents, err := fs.ReadFile(outboxpostgres.Migrations(), "000001_create_outbox.sql")
	if err != nil {
		t.Fatalf("read canonical migration: %v", err)
	}

	return string(contents)
}
