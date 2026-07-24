//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
	"github.com/jackc/pgx/v5"
)

func TestPostgreSQLDateRoundTrip(t *testing.T) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL is required for the integration suite")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(ctx) })
	if _, err := connection.Exec(ctx, `create temporary table calendar_dates (value date not null)`); err != nil {
		t.Fatal(err)
	}
	values := []any{
		calendarpg.NewDate(calendar.MustDate(2024, time.February, 29)),
		calendarpg.NewInfinityDate(calendarpg.NegativeInfinity),
		calendarpg.NewInfinityDate(calendarpg.PositiveInfinity),
	}
	for _, value := range values {
		if _, err := connection.Exec(ctx, `insert into calendar_dates (value) values ($1)`, value); err != nil {
			t.Fatalf("insert %T: %v", value, err)
		}
	}
	rows, err := connection.Query(ctx, `select value from calendar_dates order by value`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var negative, positive calendarpg.InfinityDate
	var finite calendarpg.Date
	if !rows.Next() || rows.Scan(&negative) != nil || negative.Kind() != calendarpg.NegativeInfinity {
		t.Fatal("negative infinity did not round trip")
	}
	if !rows.Next() || rows.Scan(&finite) != nil || finite.CalendarDate().String() != "2024-02-29" {
		t.Fatal("finite date did not round trip")
	}
	if !rows.Next() || rows.Scan(&positive) != nil || positive.Kind() != calendarpg.PositiveInfinity {
		t.Fatal("positive infinity did not round trip")
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatalf("unexpected PostgreSQL rows: %v", rows.Err())
	}
}
