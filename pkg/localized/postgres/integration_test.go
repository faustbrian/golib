//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	localized "github.com/faustbrian/golib/pkg/localized"
	localizedpostgres "github.com/faustbrian/golib/pkg/localized/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgreSQLJSONBRoundTrip(t *testing.T) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	connection.TypeMap().RegisterType(&pgtype.Type{Name: "jsonb", OID: pgtype.JSONBOID, Codec: localizedpostgres.JSONBCodec()})

	if _, err := connection.Exec(ctx, `CREATE TEMP TABLE localized_values (id integer PRIMARY KEY, value jsonb)`); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	value, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: ""},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO localized_values (id, value) VALUES (1, $1), (2, NULL)`, value); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	var decoded localized.Text
	if err := connection.QueryRow(ctx, `SELECT value FROM localized_values WHERE id = 1`).Scan(&decoded); err != nil {
		t.Fatalf("scan localized JSONB: %v", err)
	}
	if !decoded.Equal(value) {
		t.Fatalf("decoded = %v", decoded.Entries())
	}
	if text, present := decoded.Get(mustLocale(t, "fi")); !present || text != "" {
		t.Fatalf("present-empty = %q, %v", text, present)
	}

	var nullable localizedpostgres.Text
	if err := connection.QueryRow(ctx, `SELECT value FROM localized_values WHERE id = 2`).Scan(&nullable); err != nil {
		t.Fatalf("scan NULL: %v", err)
	}
	if nullable.Valid {
		t.Fatal("NULL scanned as valid")
	}
}
