//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestLivePostgreSQLJSONBRoundTrip(t *testing.T) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	connection, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	if _, err := connection.Exec(context.Background(), `
		create temporary table opening_hours_round_trip (
			id bigint generated always as identity primary key,
			schedule jsonb not null
		)
	`); err != nil {
		t.Fatal(err)
	}
	want := testSchedule(t)
	var got openinghours.Schedule
	if err := connection.QueryRow(context.Background(), `
		insert into opening_hours_round_trip (schedule)
		values ($1)
		returning schedule
	`, want).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !want.Equal(got) {
		t.Fatal("live PostgreSQL JSONB round trip changed schedule")
	}
}
